package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fileConfig struct {
	Games               []fileGame      `json:"games"`
	Resolvers           []string        `json:"resolvers,omitempty"`
	LegacyDomains       []string        `json:"domains,omitempty"`
	LegacyGameSelection string          `json:"-"`
	Family              string          `json:"family"`
	GeoIP               fileGeoIPConfig `json:"geoip,omitempty"`
	TunnelPort          int             `json:"tunnel_port,omitempty"`
}

type fileGame struct {
	ID                string              `json:"id,omitempty"`
	Key               string              `json:"key,omitempty"`
	Name              string              `json:"name"`
	Enabled           *bool               `json:"enabled,omitempty"`
	Domains           []string            `json:"domains,omitempty"`
	Aliases           map[string][]string `json:"aliases,omitempty"`
	Groups            []fileGameGroup     `json:"groups,omitempty"`
	PreferredProvider string              `json:"preferred_provider,omitempty"`
}

type fileGameGroup struct {
	Name    string           `json:"name"`
	Mode    string           `json:"mode,omitempty"`
	Domains []fileGameDomain `json:"domains"`
}

type fileGameDomain struct {
	Host     string   `json:"host"`
	Provider string   `json:"provider,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
}

type fileGeoIPConfig struct {
	PrimaryProvider  string `json:"primary_provider"`
	FallbackProvider string `json:"fallback_provider"`
	IPInfoToken      string `json:"ipinfo_token"`
	CacheFile        string `json:"cache_file"`
	MMDBCityPath     string `json:"mmdb_city_path"`
	MMDBASNPath      string `json:"mmdb_asn_path"`
}

func (cfg *fileConfig) UnmarshalJSON(data []byte) error {
	type rawFileConfig struct {
		Domains    []string        `json:"domains"`
		Games      json.RawMessage `json:"games"`
		Resolvers  []string        `json:"resolvers"`
		Family     string          `json:"family"`
		GeoIP      fileGeoIPConfig `json:"geoip"`
		Trace      *bool           `json:"trace"`
		UseCache   *bool           `json:"use_cache"`
		TunnelPort int             `json:"tunnel_port"`
	}

	var raw rawFileConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	cfg.LegacyDomains = append([]string(nil), raw.Domains...)
	cfg.Resolvers = append([]string(nil), raw.Resolvers...)
	cfg.Family = raw.Family
	cfg.GeoIP = raw.GeoIP
	cfg.TunnelPort = raw.TunnelPort
	_ = raw.Trace
	_ = raw.UseCache

	switch trimmed := bytes.TrimSpace(raw.Games); {
	case len(trimmed) == 0, bytes.Equal(trimmed, []byte("null")):
		return nil
	case len(trimmed) > 0 && trimmed[0] == '[':
		return json.Unmarshal(trimmed, &cfg.Games)
	default:
		return json.Unmarshal(trimmed, &cfg.LegacyGameSelection)
	}
}

func defaultFileConfig() fileConfig {
	return fileConfig{
		Games:      defaultFileGames(),
		Resolvers:  effectiveResolverValues(nil),
		Family:     "6",
		GeoIP:      defaultFileGeoIPConfig(),
		TunnelPort: 0,
	}
}

func loadFileConfig(path string) (fileConfig, bool, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return fileConfig{}, false, false, nil
	}

	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			defaultCfg := defaultFileConfig()
			if writeErr := writeFileConfig(cleanPath, defaultCfg); writeErr != nil {
				return fileConfig{}, false, false, writeErr
			}
			return defaultCfg, true, true, nil
		}
		return fileConfig{}, false, false, fmt.Errorf("read config %s: %w", cleanPath, err)
	}

	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, false, false, fmt.Errorf("parse config %s: %w", cleanPath, err)
	}
	normalizedResolvers := effectiveResolverValues(cfg.Resolvers)
	if !sameStringSlice(normalizedResolvers, cfg.Resolvers) {
		cfg.Resolvers = normalizedResolvers
		if err := writeFileConfig(cleanPath, cfg); err != nil {
			return fileConfig{}, false, false, err
		}
	}
	return cfg, true, false, nil
}

func writeFileConfig(path string, cfg fileConfig) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("prepare config dir %s: %w", dir, err)
		}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal default config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

func applyFileConfig(cfg *config, fileCfg fileConfig) error {
	cfg.GameCatalog = gameCatalogFromFileConfig(fileCfg)
	resolvers, err := resolverSpecsFromValues(effectiveResolverValues(fileCfg.Resolvers))
	if err != nil {
		return fmt.Errorf("config resolvers: %w", err)
	}
	cfg.ResolverSpecs = resolvers

	if strings.TrimSpace(fileCfg.Family) != "" {
		family, err := parseFamily(fileCfg.Family)
		if err != nil {
			return fmt.Errorf("config family: %w", err)
		}
		cfg.Family = family
	}

	cfg.GeoIP = geoIPConfigFromFile(fileCfg.GeoIP)

	if fileCfg.TunnelPort >= 0 {
		cfg.TunnelPort = fileCfg.TunnelPort
	}

	return nil
}

func effectiveResolverValues(values []string) []string {
	configured := dedupeResolverValues(values)
	localResolvers := discoverLocalResolverValues()
	if len(localResolvers) > 0 {
		configured = stripSystemResolverValues(configured)
	}
	if len(configured) == 0 {
		configured = resolverValuesFromSpecs(defaultResolverSpecs)
	}

	merged := append(localResolvers, configured...)
	return dedupeResolverValues(merged)
}

func resolverValuesFromSpecs(specs []resolverSpec) []string {
	values := make([]string, 0, len(specs))
	for _, spec := range specs {
		server := strings.TrimSpace(spec.Server)
		if server == "" {
			server = strings.TrimSpace(spec.Label)
		}
		if server == "" {
			continue
		}
		values = append(values, server)
	}
	return values
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func stripSystemResolverValues(values []string) []string {
	filtered := values[:0]
	for _, value := range values {
		if isSystemResolverValue(value) {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func resolverSpecsFromValues(values []string) ([]resolverSpec, error) {
	seen := make(map[string]struct{})
	var specs []resolverSpec
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		server, err := normalizeDNSAddress(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[server]; ok {
			continue
		}
		seen[server] = struct{}{}
		specs = append(specs, resolverSpec{
			Label:  value,
			Server: value,
		})
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("no valid DNS resolvers configured")
	}
	return specs, nil
}

func defaultFileGeoIPConfig() fileGeoIPConfig {
	return fileGeoIPConfig{
		PrimaryProvider:  string(geoIPProviderIPWhoIS),
		FallbackProvider: string(geoIPProviderIPInfoLite),
		CacheFile:        defaultGeoIPCacheFile,
		MMDBCityPath:     "",
		MMDBASNPath:      "",
	}
}

func geoIPConfigFromFile(fileCfg fileGeoIPConfig) geoIPConfig {
	cfg := defaultGeoIPConfig()

	if provider := normalizeGeoIPProvider(fileCfg.PrimaryProvider); provider != "" {
		cfg.PrimaryProvider = string(provider)
	}
	if provider := normalizeGeoIPProvider(fileCfg.FallbackProvider); provider != "" {
		cfg.FallbackProvider = string(provider)
	}
	if strings.EqualFold(strings.TrimSpace(fileCfg.FallbackProvider), "none") {
		cfg.FallbackProvider = ""
	}
	if token := strings.TrimSpace(fileCfg.IPInfoToken); token != "" {
		cfg.IPInfoToken = token
	}
	if cacheFile := strings.TrimSpace(fileCfg.CacheFile); cacheFile != "" {
		cfg.CacheFile = cacheFile
	}
	cfg.MMDBCityPath = strings.TrimSpace(fileCfg.MMDBCityPath)
	cfg.MMDBASNPath = strings.TrimSpace(fileCfg.MMDBASNPath)

	return cfg
}
