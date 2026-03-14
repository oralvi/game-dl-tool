package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

type geoIPProvider string

const (
	geoIPProviderIPWhoIS    geoIPProvider = "ipwho.is"
	geoIPProviderIPInfoLite geoIPProvider = "ipinfo_lite"
	geoIPProviderMMDB       geoIPProvider = "mmdb"
	defaultGeoIPCacheFile                 = "geoip_cache.json"
	geoIPCacheTTL                         = 30 * 24 * time.Hour
)

type geoIPPayload struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Country    string `json:"country"`
	Region     string `json:"region"`
	City       string `json:"city"`
	Connection struct {
		ASN int    `json:"asn"`
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"connection"`
}

type ipinfoLitePayload struct {
	IP          string `json:"ip"`
	Country     string `json:"country"`
	CountryName string `json:"country_name"`
	Region      string `json:"region"`
	City        string `json:"city"`
	Continent   string `json:"continent"`
	ASN         string `json:"asn"`
	ASNumber    string `json:"as_number"`
	ASName      string `json:"as_name"`
	ASDomain    string `json:"as_domain"`
	Org         string `json:"org"`
	Error       string `json:"error"`
	Title       string `json:"title"`
	Message     string `json:"message"`
}

type geoIPInfo struct {
	Geo      string
	Network  string
	Provider string
	Note     string
	Cached   bool
}

type geoIPCacheEntry struct {
	Geo       string    `json:"geo"`
	Network   string    `json:"network"`
	Provider  string    `json:"provider"`
	Note      string    `json:"note,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type geoIPCacheFile struct {
	Entries map[string]geoIPCacheEntry `json:"entries"`
}

type geoIPCacheStore struct {
	mu         sync.Mutex
	loadedPath string
	entries    map[string]geoIPCacheEntry
}

type mmdbReaderStore struct {
	mu         sync.Mutex
	cityPath   string
	cityReader *maxminddb.Reader
	asnPath    string
	asnReader  *maxminddb.Reader
}

var (
	reverseDNSCache sync.Map
	geoIPStore      geoIPCacheStore
	geoIPHTTPClient = &http.Client{Timeout: 3 * time.Second}
	mmdbStore       mmdbReaderStore
)

func defaultGeoIPConfig() geoIPConfig {
	return geoIPConfig{
		PrimaryProvider:  string(geoIPProviderIPWhoIS),
		FallbackProvider: string(geoIPProviderIPInfoLite),
		CacheFile:        defaultGeoIPCacheFile,
	}
}

func normalizeGeoIPProvider(raw string) geoIPProvider {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "none", "off", "disabled":
		return ""
	case "ipwho.is", "ipwhois", "ipwho_is":
		return geoIPProviderIPWhoIS
	case "ipinfo", "ipinfo-lite", "ipinfo_lite":
		return geoIPProviderIPInfoLite
	case "mmdb", "local_mmdb", "maxminddb":
		return geoIPProviderMMDB
	default:
		return ""
	}
}

func enrichTraceHops(cfg config, hops []traceHop) []traceHop {
	for index := range hops {
		ip := strings.TrimSpace(hops[index].IPAddress)
		if ip == "" {
			if hops[index].Status == "" {
				hops[index].Status = "timeout"
			}
			continue
		}

		hops[index].Hostname = reverseLookupHostname(ip)
		info := lookupGeoIPInfo(cfg, ip)
		hops[index].Geo = info.Geo
		hops[index].Network = info.Network
	}
	return hops
}

func reverseLookupHostname(ip string) string {
	if value, ok := reverseDNSCache.Load(ip); ok {
		return value.(string)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		reverseDNSCache.Store(ip, "-")
		return "-"
	}

	hostname := strings.TrimSuffix(strings.TrimSpace(names[0]), ".")
	if hostname == "" {
		hostname = "-"
	}
	reverseDNSCache.Store(ip, hostname)
	return hostname
}

func lookupGeoIPInfo(cfg config, ip string) geoIPInfo {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return geoIPInfo{Geo: "-", Network: "-", Provider: "-", Note: "invalid IP"}
	}

	if !isPublicTraceIP(parsed) {
		return geoIPInfo{
			Geo:      classifyNonPublicIP(parsed),
			Network:  "-",
			Provider: "local",
			Note:     "non-public address",
		}
	}

	if entry, ok := geoIPStore.Get(cfg.GeoIP.CacheFile, ip); ok {
		return geoIPInfo{
			Geo:      valueOrDash(entry.Geo),
			Network:  valueOrDash(entry.Network),
			Provider: valueOrDash(entry.Provider),
			Note:     valueOrDash(entry.Note),
			Cached:   true,
		}
	}

	providers := orderedGeoIPProviders(cfg.GeoIP)
	var lastErr error
	for _, provider := range providers {
		info, err := lookupGeoIPFromProvider(provider, cfg.GeoIP, ip)
		if err != nil {
			lastErr = err
			continue
		}
		info.Provider = valueOrDash(info.Provider)
		info.Geo = valueOrDash(info.Geo)
		info.Network = valueOrDash(info.Network)
		info.Note = valueOrDash(info.Note)
		geoIPStore.Put(cfg.GeoIP.CacheFile, ip, geoIPCacheEntry{
			Geo:       info.Geo,
			Network:   info.Network,
			Provider:  info.Provider,
			Note:      info.Note,
			UpdatedAt: time.Now(),
		})
		return info
	}

	note := "lookup unavailable"
	if lastErr != nil {
		note = lastErr.Error()
	}
	return geoIPInfo{
		Geo:      "-",
		Network:  "-",
		Provider: "-",
		Note:     note,
	}
}

func orderedGeoIPProviders(cfg geoIPConfig) []geoIPProvider {
	seen := make(map[geoIPProvider]struct{})
	values := []string{cfg.PrimaryProvider, cfg.FallbackProvider}
	providers := make([]geoIPProvider, 0, len(values))
	for _, raw := range values {
		provider := normalizeGeoIPProvider(raw)
		if provider == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		providers = append(providers, provider)
	}
	return providers
}

func lookupGeoIPFromProvider(provider geoIPProvider, cfg geoIPConfig, ip string) (geoIPInfo, error) {
	switch provider {
	case geoIPProviderIPWhoIS:
		return lookupViaIPWhoIS(ip)
	case geoIPProviderIPInfoLite:
		return lookupViaIPInfoLite(ip, cfg.IPInfoToken)
	case geoIPProviderMMDB:
		return lookupViaMMDB(ip, cfg)
	default:
		return geoIPInfo{}, fmt.Errorf("unsupported GeoIP provider %q", provider)
	}
}

func lookupViaIPWhoIS(ip string) (geoIPInfo, error) {
	req, err := http.NewRequest(http.MethodGet, "https://ipwho.is/"+ip, nil)
	if err != nil {
		return geoIPInfo{}, err
	}
	req.Header.Set("User-Agent", "game-dl-tool/1.0")

	resp, err := geoIPHTTPClient.Do(req)
	if err != nil {
		return geoIPInfo{}, err
	}
	defer resp.Body.Close()

	var payload geoIPPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return geoIPInfo{}, err
	}
	if !payload.Success {
		if strings.TrimSpace(payload.Message) == "" {
			return geoIPInfo{}, fmt.Errorf("ipwho.is lookup failed")
		}
		return geoIPInfo{}, fmt.Errorf("ipwho.is: %s", strings.TrimSpace(payload.Message))
	}

	return geoIPInfo{
		Geo:      joinNonEmpty(payload.City, payload.Region, payload.Country),
		Network:  formatNetworkLabel(payload.Connection.ASN, firstNonEmpty(payload.Connection.Org, payload.Connection.ISP)),
		Provider: string(geoIPProviderIPWhoIS),
	}, nil
}

func lookupViaIPInfoLite(ip string, token string) (geoIPInfo, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return geoIPInfo{}, fmt.Errorf("ipinfo lite token not configured")
	}

	baseURL := fmt.Sprintf("https://api.ipinfo.io/lite/%s", url.PathEscape(ip))
	req, err := http.NewRequest(http.MethodGet, baseURL, nil)
	if err != nil {
		return geoIPInfo{}, err
	}
	query := req.URL.Query()
	query.Set("token", token)
	req.URL.RawQuery = query.Encode()
	req.Header.Set("User-Agent", "game-dl-tool/1.0")

	resp, err := geoIPHTTPClient.Do(req)
	if err != nil {
		return geoIPInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return geoIPInfo{}, fmt.Errorf("ipinfo lite: http %d", resp.StatusCode)
	}

	var payload ipinfoLitePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return geoIPInfo{}, err
	}
	if message := firstNonEmpty(payload.Error, payload.Title, payload.Message); message != "" {
		return geoIPInfo{}, fmt.Errorf("ipinfo lite: %s", message)
	}

	geo := joinNonEmpty(payload.City, payload.Region, firstNonEmpty(payload.CountryName, payload.Country, payload.Continent))
	return geoIPInfo{
		Geo:      geo,
		Network:  formatNetworkText(firstNonEmpty(payload.ASNumber, payload.ASN), firstNonEmpty(payload.ASName, payload.Org, payload.ASDomain)),
		Provider: string(geoIPProviderIPInfoLite),
	}, nil
}

func lookupViaMMDB(ip string, cfg geoIPConfig) (geoIPInfo, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return geoIPInfo{}, fmt.Errorf("invalid IP")
	}

	cityPath := strings.TrimSpace(cfg.MMDBCityPath)
	asnPath := strings.TrimSpace(cfg.MMDBASNPath)
	if cityPath == "" && asnPath == "" {
		return geoIPInfo{}, fmt.Errorf("mmdb provider selected but no MMDB paths are configured")
	}

	geo := "-"
	network := "-"

	if cityPath != "" {
		reader, err := mmdbStore.cityReaderFor(cityPath)
		if err != nil {
			return geoIPInfo{}, fmt.Errorf("open MMDB city database: %w", err)
		}
		raw, err := lookupMMDBRaw(reader, parsed)
		if err != nil {
			return geoIPInfo{}, fmt.Errorf("lookup MMDB city database: %w", err)
		}
		if value := extractMMDBGeo(raw); value != "" {
			geo = value
		}
	}

	if asnPath != "" {
		reader, err := mmdbStore.asnReaderFor(asnPath)
		if err != nil {
			return geoIPInfo{}, fmt.Errorf("open MMDB ASN database: %w", err)
		}
		raw, err := lookupMMDBRaw(reader, parsed)
		if err != nil {
			return geoIPInfo{}, fmt.Errorf("lookup MMDB ASN database: %w", err)
		}
		if value := extractMMDBNetwork(raw); value != "" {
			network = value
		}
	}

	return geoIPInfo{
		Geo:      geo,
		Network:  network,
		Provider: string(geoIPProviderMMDB),
		Note:     "local MMDB lookup",
	}, nil
}

func (store *geoIPCacheStore) Get(path string, ip string) (geoIPCacheEntry, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if err := store.loadLocked(path); err != nil {
		return geoIPCacheEntry{}, false
	}

	entry, ok := store.entries[ip]
	if !ok {
		return geoIPCacheEntry{}, false
	}
	if time.Since(entry.UpdatedAt) > geoIPCacheTTL {
		delete(store.entries, ip)
		_ = store.saveLocked(path)
		return geoIPCacheEntry{}, false
	}
	return entry, true
}

func (store *geoIPCacheStore) Put(path string, ip string, entry geoIPCacheEntry) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if err := store.loadLocked(path); err != nil {
		return
	}
	if store.entries == nil {
		store.entries = make(map[string]geoIPCacheEntry)
	}
	store.entries[ip] = entry
	_ = store.saveLocked(path)
}

func (store *geoIPCacheStore) loadLocked(path string) error {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" {
		cleanPath = defaultGeoIPCacheFile
	}
	if store.loadedPath == cleanPath && store.entries != nil {
		return nil
	}

	store.loadedPath = cleanPath
	store.entries = make(map[string]geoIPCacheEntry)

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var payload geoIPCacheFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	for ip, entry := range payload.Entries {
		store.entries[ip] = entry
	}
	return nil
}

func (store *geoIPCacheStore) saveLocked(path string) error {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" {
		cleanPath = defaultGeoIPCacheFile
	}

	dir := filepath.Dir(cleanPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	payload, err := json.MarshalIndent(geoIPCacheFile{Entries: store.entries}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(cleanPath, payload, 0o644)
}

func (store *mmdbReaderStore) cityReaderFor(path string) (*maxminddb.Reader, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" {
		return nil, fmt.Errorf("city MMDB path is empty")
	}
	if store.cityReader != nil && store.cityPath == cleanPath {
		return store.cityReader, nil
	}
	if store.cityReader != nil {
		_ = store.cityReader.Close()
		store.cityReader = nil
	}

	reader, err := maxminddb.Open(cleanPath)
	if err != nil {
		return nil, err
	}
	store.cityPath = cleanPath
	store.cityReader = reader
	return reader, nil
}

func (store *mmdbReaderStore) asnReaderFor(path string) (*maxminddb.Reader, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "" {
		return nil, fmt.Errorf("ASN MMDB path is empty")
	}
	if store.asnReader != nil && store.asnPath == cleanPath {
		return store.asnReader, nil
	}
	if store.asnReader != nil {
		_ = store.asnReader.Close()
		store.asnReader = nil
	}

	reader, err := maxminddb.Open(cleanPath)
	if err != nil {
		return nil, err
	}
	store.asnPath = cleanPath
	store.asnReader = reader
	return reader, nil
}

func lookupMMDBRaw(reader *maxminddb.Reader, ip net.IP) (map[string]any, error) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return nil, fmt.Errorf("invalid IP address")
	}
	var raw map[string]any
	if err := reader.Lookup(addr.Unmap()).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func extractMMDBGeo(raw map[string]any) string {
	return joinNonEmpty(
		firstStringFromPaths(raw,
			[]string{"city", "names", "en"},
			[]string{"city", "name"},
			[]string{"city_name"},
		),
		firstStringFromPaths(raw,
			[]string{"subdivisions", "0", "names", "en"},
			[]string{"subdivisions", "0", "name"},
			[]string{"state_prov"},
			[]string{"region", "names", "en"},
			[]string{"region", "name"},
			[]string{"region_name"},
		),
		firstStringFromPaths(raw,
			[]string{"country", "names", "en"},
			[]string{"country", "name"},
			[]string{"country_name"},
			[]string{"country", "iso_code"},
			[]string{"country_code"},
		),
	)
}

func extractMMDBNetwork(raw map[string]any) string {
	asnText := firstStringFromPaths(raw,
		[]string{"autonomous_system_number"},
		[]string{"traits", "autonomous_system_number"},
		[]string{"asn"},
	)
	orgText := firstStringFromPaths(raw,
		[]string{"autonomous_system_organization"},
		[]string{"traits", "autonomous_system_organization"},
		[]string{"organization"},
		[]string{"org"},
		[]string{"isp"},
		[]string{"as_name"},
		[]string{"as_domain"},
	)

	if strings.TrimSpace(asnText) == "" && strings.TrimSpace(orgText) == "" {
		return ""
	}
	asnText = strings.TrimSpace(asnText)
	if asnText != "" && !strings.HasPrefix(strings.ToUpper(asnText), "AS") {
		asnText = "AS" + asnText
	}
	return formatNetworkText(asnText, orgText)
}

func firstStringFromPaths(root map[string]any, paths ...[]string) string {
	for _, path := range paths {
		if value := nestedMMDBValue(root, path...); value != nil {
			if text := stringifyMMDBValue(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func nestedMMDBValue(root any, path ...string) any {
	current := root
	for _, part := range path {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
	}
	return current
}

func stringifyMMDBValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case float32:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	default:
		return ""
	}
}

func isPublicTraceIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified())
}

func classifyNonPublicIP(ip net.IP) string {
	switch {
	case ip.IsLoopback():
		return "Loopback"
	case ip.IsPrivate():
		return "Private network"
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return "Link-local"
	case ip.IsMulticast():
		return "Multicast"
	case ip.IsUnspecified():
		return "Unspecified"
	default:
		return "Reserved"
	}
}

func joinNonEmpty(values ...string) string {
	var parts []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatNetworkLabel(asn int, org string) string {
	switch {
	case asn > 0 && org != "":
		return fmt.Sprintf("AS%d %s", asn, org)
	case asn > 0:
		return fmt.Sprintf("AS%d", asn)
	default:
		return org
	}
}

func formatNetworkText(asn string, name string) string {
	asn = strings.TrimSpace(asn)
	name = strings.TrimSpace(name)
	switch {
	case asn != "" && name != "":
		return asn + " " + name
	case asn != "":
		return asn
	default:
		return name
	}
}
