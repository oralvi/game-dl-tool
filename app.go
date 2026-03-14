package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context

	mu            sync.Mutex
	scanning      bool
	latestRows    []resultRow
	latestAliases map[string][]string
	tunnel        *localTunnelManager
}

type bootstrapData struct {
	ConfigPath      string     `json:"configPath"`
	Games           []gameView `json:"games"`
	Resolvers       []string   `json:"resolvers"`
	Family          string     `json:"family"`
	GeoIP           geoIPView  `json:"geoip"`
	TunnelPort      int        `json:"tunnelPort"`
	TunnelActive    bool       `json:"tunnelActive"`
	TunnelRuleCount int        `json:"tunnelRuleCount"`
	LogFile         string     `json:"logFile"`
	CacheFile       string     `json:"cacheFile"`
	HasResults      bool       `json:"hasResults"`
}

type gameView struct {
	ID                string   `json:"id"`
	Key               string   `json:"key"`
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	DomainCount       int      `json:"domainCount"`
	Domains           []string `json:"domains"`
	PreferredProvider string   `json:"preferredProvider"`
	ProviderOptions   []string `json:"providerOptions"`
}

type settingsPayload struct {
	Games      []gameToggle `json:"games"`
	Resolvers  []string     `json:"resolvers"`
	Family     string       `json:"family"`
	GeoIP      geoIPView    `json:"geoip"`
	TunnelPort int          `json:"tunnelPort"`
}

type gameToggle struct {
	Key               string `json:"key"`
	Enabled           bool   `json:"enabled"`
	PreferredProvider string `json:"preferredProvider"`
}

type scanResponse struct {
	UsedCache  bool            `json:"usedCache"`
	Domains    []domainSummary `json:"domains"`
	Candidates []candidateView `json:"candidates"`
}

type domainSummary struct {
	Domain         string `json:"domain"`
	CandidateCount int    `json:"candidateCount"`
	BestIP         string `json:"bestIP"`
	BestLatency    string `json:"bestLatency"`
}

type candidateView struct {
	Domain       string   `json:"domain"`
	IPAddress    string   `json:"ipAddress"`
	Resolver     string   `json:"resolver"`
	Resolvers    []string `json:"resolvers"`
	Latency      string   `json:"latency"`
	Family       string   `json:"family"`
	CNAME        string   `json:"cname"`
	Note         string   `json:"note"`
	Aliases      []string `json:"aliases"`
	ConnectOK    bool     `json:"connectOk"`
	HopCount     int      `json:"hopCount"`
	TraceStatus  string   `json:"traceStatus"`
	TraceReached bool     `json:"traceReached"`
}

type candidateRequest struct {
	Domain    string `json:"domain"`
	IPAddress string `json:"ipAddress"`
}

type traceResponse struct {
	Domain    string     `json:"domain"`
	IPAddress string     `json:"ipAddress"`
	Family    string     `json:"family"`
	Status    string     `json:"status"`
	HopCount  int        `json:"hopCount"`
	Reached   bool       `json:"reached"`
	Note      string     `json:"note"`
	Hops      []traceHop `json:"hops"`
	RawOutput string     `json:"rawOutput"`
}

type geoIPView struct {
	PrimaryProvider  string `json:"primaryProvider"`
	FallbackProvider string `json:"fallbackProvider"`
	IPInfoToken      string `json:"ipinfoToken"`
	CacheFile        string `json:"cacheFile"`
	MMDBCityPath     string `json:"mmdbCityPath"`
	MMDBASNPath      string `json:"mmdbAsnPath"`
}

type geoIPResponse struct {
	Domain    string `json:"domain"`
	IPAddress string `json:"ipAddress"`
	Geo       string `json:"geo"`
	Network   string `json:"network"`
	Provider  string `json:"provider"`
	Cached    bool   `json:"cached"`
	Note      string `json:"note"`
}

type hostsResponse struct {
	Path      string   `json:"path"`
	Hostnames []string `json:"hostnames"`
	Entries   int      `json:"entries"`
}

type tunnelResponse struct {
	Port      int      `json:"port"`
	Listener  string   `json:"listener"`
	Path      string   `json:"path"`
	Hostnames []string `json:"hostnames"`
	Entries   int      `json:"entries"`
	RuleCount int      `json:"ruleCount"`
	Active    bool     `json:"active"`
	Note      string   `json:"note"`
}

type progressPayload struct {
	Total         int     `json:"total"`
	Completed     int     `json:"completed"`
	ActiveCount   int     `json:"activeCount"`
	ActiveSummary string  `json:"activeSummary"`
	LastCompleted string  `json:"lastCompleted"`
	Elapsed       string  `json:"elapsed"`
	Percent       float64 `json:"percent"`
	Final         bool    `json:"final"`
}

func NewApp() *App {
	return &App{
		tunnel: newLocalTunnelManager(nil),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.tunnel = newLocalTunnelManager(func(format string, args ...interface{}) {
		a.logMessage(format, args...)
	})
	a.logMessage("startup: clearing old loopback tunnel rules")
	_ = clearLoopbackPortProxy(a.logMessage)
}

func (a *App) shutdown(context.Context) {
	if a.tunnel != nil {
		_ = a.tunnel.Stop()
	}
	a.logMessage("shutdown: stopping local tunnel and clearing loopback rules")
	_ = clearLoopbackPortProxy(a.logMessage)
}

func (a *App) Bootstrap() (bootstrapData, error) {
	cfg, fileCfg, err := loadRuntimeConfigFromFile()
	if err != nil {
		return bootstrapData{}, err
	}

	return a.makeBootstrapData(cfg, fileCfg, a.hasResults()), nil
}

func (a *App) SaveSettings(input settingsPayload) (bootstrapData, error) {
	cfgPath := defaultRuntimeConfig().ConfigPath
	fileCfg, _, _, err := loadFileConfig(cfgPath)
	if err != nil {
		return bootstrapData{}, err
	}
	if len(fileCfg.Games) == 0 {
		fileCfg.Games = defaultFileGames()
	}

	enabledByKey := make(map[string]bool, len(input.Games))
	providerByKey := make(map[string]string, len(input.Games))
	for _, item := range input.Games {
		key := normalizeGameKey(item.Key)
		if key == "" {
			continue
		}
		enabledByKey[key] = item.Enabled
		providerByKey[key] = normalizeProviderPreference(item.PreferredProvider)
	}

	for i := range fileCfg.Games {
		key := normalizeGameKey(fileCfg.Games[i].Key)
		if key == "" {
			key = normalizeGameKey(fileCfg.Games[i].Name)
			fileCfg.Games[i].Key = key
		}
		current := true
		if fileCfg.Games[i].Enabled != nil {
			current = *fileCfg.Games[i].Enabled
		}
		if enabled, ok := enabledByKey[key]; ok {
			current = enabled
		}
		fileCfg.Games[i].Enabled = boolPtr(current)
		if preferred, ok := providerByKey[key]; ok {
			fileCfg.Games[i].PreferredProvider = preferred
		}
	}

	fileCfg.Resolvers = effectiveResolverValues(input.Resolvers)
	fileCfg.Family = strings.TrimSpace(input.Family)
	fileCfg.GeoIP = fileGeoIPConfig{
		PrimaryProvider:  strings.TrimSpace(input.GeoIP.PrimaryProvider),
		FallbackProvider: strings.TrimSpace(input.GeoIP.FallbackProvider),
		IPInfoToken:      strings.TrimSpace(input.GeoIP.IPInfoToken),
		CacheFile:        strings.TrimSpace(input.GeoIP.CacheFile),
		MMDBCityPath:     strings.TrimSpace(input.GeoIP.MMDBCityPath),
		MMDBASNPath:      strings.TrimSpace(input.GeoIP.MMDBASNPath),
	}
	fileCfg.TunnelPort = max(0, input.TunnelPort)

	if err := writeFileConfig(cfgPath, fileCfg); err != nil {
		return bootstrapData{}, err
	}

	cfg, normalizedFileCfg, err := loadRuntimeConfigFromFile()
	if err != nil {
		return bootstrapData{}, err
	}

	return a.makeBootstrapData(cfg, normalizedFileCfg, a.hasResults()), nil
}

func (a *App) Scan() (scanResponse, error) {
	a.mu.Lock()
	if a.scanning {
		a.mu.Unlock()
		return scanResponse{}, fmt.Errorf("a scan is already running")
	}
	a.scanning = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.scanning = false
		a.mu.Unlock()
	}()

	cfg, _, err := loadRuntimeConfigFromFile()
	if err != nil {
		return scanResponse{}, err
	}

	selectedGames := enabledGames(cfg.GameCatalog)
	if len(selectedGames) == 0 {
		return scanResponse{}, fmt.Errorf("no games are enabled in config.json")
	}

	domains, aliases, err := targetsFromGames(selectedGames)
	if err != nil {
		return scanResponse{}, err
	}

	_ = os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755)
	logger, err := newRunLoggerWithHook(cfg.LogFile, func(line string) {
		a.emit("scan:log", map[string]string{
			"time":    time.Now().Format("15:04:05"),
			"message": line,
		})
	})
	if err != nil {
		return scanResponse{}, err
	}
	defer logger.Close()

	cfg.Logger = logger
	cfg.ProgressSink = func(snapshot progressSnapshot) {
		a.emit("scan:progress", progressPayloadFrom(snapshot))
	}

	a.emit("scan:reset")
	a.emit("scan:status", map[string]any{
		"running": true,
		"message": fmt.Sprintf("Scanning %d domains with %d resolvers", len(domains), len(cfg.ResolverSpecs)),
	})

	rows, usedCache, err := executeScan(cfg, domains)
	if err != nil {
		a.emit("scan:status", map[string]any{
			"running": false,
			"message": err.Error(),
		})
		return scanResponse{}, err
	}

	a.rememberResults(rows, aliases)
	response := makeScanResponse(rows, aliases, usedCache)

	a.emit("scan:status", map[string]any{
		"running":   false,
		"usedCache": usedCache,
		"message":   fmt.Sprintf("Scan completed with %d candidates", len(rows)),
	})

	return response, nil
}

func (a *App) TraceCandidate(request candidateRequest) (traceResponse, error) {
	ip := strings.TrimSpace(request.IPAddress)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return traceResponse{}, fmt.Errorf("invalid IP address %q", ip)
	}

	cfg, _, err := loadRuntimeConfigFromFile()
	if err != nil {
		return traceResponse{}, err
	}

	family := familyFromIP(parsed)
	if command, args, commandErr := tracerouteCommand(ip, family, cfg); commandErr == nil {
		a.logMessage("running command: %s %s", command, strings.Join(args, " "))
	}
	result := traceIP(ip, family, cfg)
	a.updateTraceResult(strings.TrimSpace(request.Domain), ip, result)
	a.logMessage("trace finished: %s %s -> %s", strings.TrimSpace(request.Domain), ip, result.Status)

	response := traceResponse{
		Domain:    strings.TrimSpace(request.Domain),
		IPAddress: ip,
		Family:    string(family),
		Status:    result.Status,
		HopCount:  result.HopCount,
		Reached:   result.Reached,
		Note:      result.Note,
		Hops:      result.Hops,
		RawOutput: result.RawOutput,
	}

	a.emit("trace:done", response)
	return response, nil
}

func (a *App) LookupGeoIP(request candidateRequest) (geoIPResponse, error) {
	ip := strings.TrimSpace(request.IPAddress)
	if parsed := net.ParseIP(ip); parsed == nil {
		return geoIPResponse{}, fmt.Errorf("invalid IP address %q", ip)
	}

	cfg, _, err := loadRuntimeConfigFromFile()
	if err != nil {
		return geoIPResponse{}, err
	}

	info := lookupGeoIPInfo(cfg, ip)
	a.logMessage("geoip lookup: %s -> %s%s", ip, valueOrDash(info.Provider), cachedLabel(info.Cached))

	return geoIPResponse{
		Domain:    strings.TrimSpace(request.Domain),
		IPAddress: ip,
		Geo:       valueOrDash(info.Geo),
		Network:   valueOrDash(info.Network),
		Provider:  valueOrDash(info.Provider),
		Cached:    info.Cached,
		Note:      valueOrDash(info.Note),
	}, nil
}

func (a *App) WriteHosts(request candidateRequest) (hostsResponse, error) {
	return a.WriteHostsBatch([]candidateRequest{request})
}

func (a *App) WriteHostsBatch(requests []candidateRequest) (hostsResponse, error) {
	if err := ensureAdminPrivileges("writing hosts entries"); err != nil {
		return hostsResponse{}, err
	}

	rows, aliases, cfg, err := a.resolveRowsForHosts(requests)
	if err != nil {
		return hostsResponse{}, err
	}

	hostnames := collectHostnames(rows, aliases)
	managedHostnames, blockedHostnames := strictManagedHostnamesForRequests(cfg.GameCatalog, requests)
	routeCleanupHostnames := unionHostnames(hostnames, blockedHostnames)

	if _, err := clearTaggedHostsFile("system", hostsTunnelTag, routeCleanupHostnames); err != nil {
		return hostsResponse{}, err
	}
	if _, err := clearTaggedHostsFile("system", hostsTag, blockedHostnames); err != nil {
		return hostsResponse{}, err
	}
	if _, err := clearTaggedHostsFile("system", hostsBlockTag, managedHostnames); err != nil {
		return hostsResponse{}, err
	}

	path, err := upsertHostsFile("system", rows, aliases)
	if err != nil {
		return hostsResponse{}, err
	}
	if len(blockedHostnames) > 0 {
		if _, err := upsertBlockedHostsFile("system", blockedHostnames); err != nil {
			return hostsResponse{}, err
		}
	}

	return hostsResponse{
		Path:      path,
		Hostnames: hostnames,
		Entries:   len(rows),
	}, nil
}

func (a *App) RouteTunnel(request candidateRequest) (tunnelResponse, error) {
	return a.RouteTunnelBatch([]candidateRequest{request})
}

func (a *App) RouteTunnelBatch(requests []candidateRequest) (tunnelResponse, error) {
	if err := ensureAdminPrivileges("enabling local tunnel routing"); err != nil {
		return tunnelResponse{}, err
	}

	rows, aliases, cfg, err := a.resolveRowsForHosts(requests)
	if err != nil {
		return tunnelResponse{}, err
	}

	if a.tunnel == nil {
		a.tunnel = newLocalTunnelManager(nil)
	}

	port, err := a.tunnel.EnsureStarted(cfg.TunnelPort)
	if err != nil {
		return tunnelResponse{}, err
	}
	if err := ensureLoopbackPortProxy(port, a.logMessage); err != nil {
		_ = a.tunnel.Stop()
		return tunnelResponse{}, err
	}

	ruleCount := a.tunnel.SetRoutes(rows, aliases)
	hostnames := collectHostnames(rows, aliases)
	managedHostnames, blockedHostnames := strictManagedHostnamesForRequests(cfg.GameCatalog, requests)
	routeCleanupHostnames := unionHostnames(hostnames, blockedHostnames)

	if _, err := clearTaggedHostsFile("system", hostsTag, routeCleanupHostnames); err != nil {
		_ = clearLoopbackPortProxy(a.logMessage)
		_ = a.tunnel.Stop()
		return tunnelResponse{}, err
	}
	if _, err := clearTaggedHostsFile("system", hostsTunnelTag, blockedHostnames); err != nil {
		_ = clearLoopbackPortProxy(a.logMessage)
		_ = a.tunnel.Stop()
		return tunnelResponse{}, err
	}
	if _, err := clearTaggedHostsFile("system", hostsBlockTag, managedHostnames); err != nil {
		_ = clearLoopbackPortProxy(a.logMessage)
		_ = a.tunnel.Stop()
		return tunnelResponse{}, err
	}
	path, err := upsertTunnelHostsFile("system", rows, aliases)
	if err != nil {
		_ = clearLoopbackPortProxy(a.logMessage)
		_ = a.tunnel.Stop()
		return tunnelResponse{}, err
	}
	if len(blockedHostnames) > 0 {
		if _, err := upsertBlockedHostsFile("system", blockedHostnames); err != nil {
			_ = clearLoopbackPortProxy(a.logMessage)
			_ = a.tunnel.Stop()
			return tunnelResponse{}, err
		}
	}

	snapshot := a.tunnel.Snapshot()
	note := "Loopback hosts and local tunnel routing enabled for HTTP and HTTPS"
	if len(blockedHostnames) > 0 {
		note = fmt.Sprintf("%s; blocked %d non-target download hostnames", note, len(blockedHostnames))
	}
	return tunnelResponse{
		Port:      port,
		Listener:  fmt.Sprintf("%s:%d", tunnelInternalIPv4, port),
		Path:      path,
		Hostnames: hostnames,
		Entries:   len(rows),
		RuleCount: ruleCount,
		Active:    snapshot.Active,
		Note:      note,
	}, nil
}

func (a *App) StopTunnel() (tunnelResponse, error) {
	if err := ensureAdminPrivileges("disabling local tunnel routing"); err != nil {
		return tunnelResponse{}, err
	}

	if a.tunnel != nil {
		_ = a.tunnel.Stop()
	}
	if err := clearLoopbackPortProxy(a.logMessage); err != nil {
		return tunnelResponse{}, err
	}

	path, err := clearTunnelHostsFile("system")
	if err != nil {
		return tunnelResponse{}, err
	}

	return tunnelResponse{
		Path:     path,
		Listener: "-",
		Port:     0,
		Active:   false,
		Note:     "Loopback tunnel routing disabled",
	}, nil
}

func (a *App) ExportLatestCSV() (string, error) {
	a.mu.Lock()
	rows := append([]resultRow(nil), a.latestRows...)
	a.mu.Unlock()

	if len(rows) == 0 {
		return "", fmt.Errorf("no scan results available")
	}

	if err := os.MkdirAll("results", 0o755); err != nil {
		return "", err
	}

	path := filepath.Join("results", fmt.Sprintf("scan-%s.csv", time.Now().Format("20060102-150405")))
	if err := writeCSV(path, rows); err != nil {
		return "", err
	}
	return path, nil
}

func loadRuntimeConfigFromFile() (config, fileConfig, error) {
	cfg := defaultRuntimeConfig()
	fileCfg, _, _, err := loadFileConfig(cfg.ConfigPath)
	if err != nil {
		return config{}, fileConfig{}, err
	}
	if err := applyFileConfig(&cfg, fileCfg); err != nil {
		return config{}, fileConfig{}, err
	}
	return cfg, fileCfg, nil
}

func (a *App) makeBootstrapData(cfg config, fileCfg fileConfig, hasResults bool) bootstrapData {
	tunnelState := tunnelSnapshot{}
	if a.tunnel != nil {
		tunnelState = a.tunnel.Snapshot()
	}

	games := make([]gameView, 0, len(cfg.GameCatalog))
	for _, game := range cfg.GameCatalog {
		games = append(games, gameView{
			ID:                game.ID,
			Key:               game.Key,
			Name:              game.Name,
			Enabled:           game.Enabled,
			DomainCount:       len(game.Domains),
			Domains:           append([]string(nil), game.Domains...),
			PreferredProvider: game.PreferredProvider,
			ProviderOptions:   append([]string(nil), game.ProviderOptions...),
		})
	}

	resolvers := effectiveResolverValues(fileCfg.Resolvers)
	if len(resolvers) == 0 {
		resolvers = resolverValuesFromSpecs(cfg.ResolverSpecs)
	}

	return bootstrapData{
		ConfigPath:      cfg.ConfigPath,
		Games:           games,
		Resolvers:       resolvers,
		Family:          string(cfg.Family),
		GeoIP:           geoIPViewFromConfig(cfg.GeoIP),
		TunnelPort:      cfg.TunnelPort,
		TunnelActive:    tunnelState.Active,
		TunnelRuleCount: tunnelState.RuleCount,
		LogFile:         cfg.LogFile,
		CacheFile:       cfg.CacheFile,
		HasResults:      hasResults,
	}
}

func makeScanResponse(rows []resultRow, aliases map[string][]string, usedCache bool) scanResponse {
	sortedRows := append([]resultRow(nil), rows...)
	sortRows(sortedRows)

	response := scanResponse{
		UsedCache:  usedCache,
		Domains:    buildDomainSummaries(sortedRows),
		Candidates: buildCandidateViews(sortedRows, aliases),
	}
	return response
}

func buildDomainSummaries(rows []resultRow) []domainSummary {
	grouped := make(map[string][]resultRow)
	for _, row := range rows {
		grouped[row.Domain] = append(grouped[row.Domain], row)
	}

	domains := make([]string, 0, len(grouped))
	for domain := range grouped {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	result := make([]domainSummary, 0, len(domains))
	for _, domain := range domains {
		group := grouped[domain]
		bestIP := ""
		bestLatency := "-"
		for _, row := range group {
			if !row.ConnectOK || strings.TrimSpace(row.Address) == "" {
				continue
			}
			bestIP = row.Address
			bestLatency = formatLatency(row.ConnectLatency)
			break
		}
		result = append(result, domainSummary{
			Domain:         domain,
			CandidateCount: len(group),
			BestIP:         bestIP,
			BestLatency:    bestLatency,
		})
	}
	return result
}

func buildCandidateViews(rows []resultRow, aliases map[string][]string) []candidateView {
	views := make([]candidateView, 0, len(rows))
	for _, row := range rows {
		views = append(views, candidateView{
			Domain:       row.Domain,
			IPAddress:    row.Address,
			Resolver:     strings.Join(splitResolverList(row.ResolverList), "\n"),
			Resolvers:    splitResolverList(row.ResolverList),
			Latency:      latencyLabel(row.ConnectLatency, row.ConnectOK),
			Family:       string(row.Family),
			CNAME:        valueOrDash(row.CNAME),
			Note:         valueOrDash(row.Note),
			Aliases:      append([]string(nil), aliases[row.Domain]...),
			ConnectOK:    row.ConnectOK,
			HopCount:     row.HopCount,
			TraceStatus:  valueOrDash(row.TraceStatus),
			TraceReached: row.TraceReached,
		})
	}
	return views
}

func enabledGames(games []gameTarget) []gameTarget {
	selected := make([]gameTarget, 0, len(games))
	for _, game := range games {
		if game.Enabled {
			selected = append(selected, game)
		}
	}
	return selected
}

func aliasesFromGames(games []gameTarget) map[string][]string {
	aliases := make(map[string][]string)
	for _, game := range games {
		for _, domain := range game.Domains {
			aliases[domain] = aliasesForDomain(domain, game.Aliases[domain])
		}
	}
	return aliases
}

func splitResolverList(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	return values
}

func latencyLabel(d time.Duration, ok bool) string {
	if d <= 0 {
		if ok {
			return "0.00 ms"
		}
		return "-"
	}
	return formatLatency(d)
}

func formatLatency(d time.Duration) string {
	return fmt.Sprintf("%.2f ms", float64(d)/float64(time.Millisecond))
}

func progressPayloadFrom(snapshot progressSnapshot) progressPayload {
	percent := 0.0
	if snapshot.Total > 0 {
		percent = float64(snapshot.Completed) / float64(snapshot.Total)
	}
	return progressPayload{
		Total:         snapshot.Total,
		Completed:     snapshot.Completed,
		ActiveCount:   snapshot.ActiveCount,
		ActiveSummary: snapshot.ActiveSummary,
		LastCompleted: snapshot.LastCompleted,
		Elapsed:       formatElapsed(snapshot.Elapsed),
		Percent:       percent,
		Final:         snapshot.Final,
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func dedupeResolverValues(values []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := resolverIdentityKey(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func resolverIdentityKey(value string) string {
	if isSystemResolverValue(value) {
		return systemResolverToken
	}
	server, err := normalizeDNSAddress(value)
	if err == nil {
		return strings.ToLower(server)
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func geoIPViewFromConfig(cfg geoIPConfig) geoIPView {
	return geoIPView{
		PrimaryProvider:  cfg.PrimaryProvider,
		FallbackProvider: cfg.FallbackProvider,
		IPInfoToken:      cfg.IPInfoToken,
		CacheFile:        cfg.CacheFile,
		MMDBCityPath:     cfg.MMDBCityPath,
		MMDBASNPath:      cfg.MMDBASNPath,
	}
}

func cachedLabel(cached bool) string {
	if !cached {
		return ""
	}
	return " (cache)"
}

func (a *App) resolveRowsForHosts(requests []candidateRequest) ([]resultRow, map[string][]string, config, error) {
	if len(requests) == 0 {
		return nil, nil, config{}, fmt.Errorf("no candidates selected")
	}

	cfg, _, err := loadRuntimeConfigFromFile()
	if err != nil {
		return nil, nil, config{}, err
	}

	aliases := aliasesFromGames(cfg.GameCatalog)
	seen := make(map[string]struct{})
	rows := make([]resultRow, 0, len(requests))

	for _, request := range requests {
		domain := strings.TrimSpace(request.Domain)
		ip := strings.TrimSpace(request.IPAddress)
		if domain == "" || ip == "" {
			continue
		}

		key := domain + "|" + ip
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if row, latestAliases, ok := a.lookupLatestCandidate(domain, ip); ok {
			rows = append(rows, row)
			for aliasDomain, values := range latestAliases {
				aliases[aliasDomain] = append([]string(nil), values...)
			}
			continue
		}

		rows = append(rows, resultRow{
			Domain:  domain,
			Address: ip,
			Family:  familyFromIP(net.ParseIP(ip)),
		})
	}

	if len(rows) == 0 {
		return nil, nil, config{}, fmt.Errorf("no valid candidates selected")
	}

	return rows, aliases, cfg, nil
}

func collectHostnames(rows []resultRow, aliases map[string][]string) []string {
	seen := make(map[string]struct{})
	var hostnames []string
	for _, row := range rows {
		for _, hostname := range aliases[row.Domain] {
			if _, ok := seen[hostname]; ok {
				continue
			}
			seen[hostname] = struct{}{}
			hostnames = append(hostnames, hostname)
		}
	}
	sort.Strings(hostnames)
	return hostnames
}

func strictManagedHostnamesForRequests(games []gameTarget, requests []candidateRequest) ([]string, []string) {
	gameSeen := make(map[string]struct{})
	managedSeen := make(map[string]struct{})
	blockedSeen := make(map[string]struct{})
	var managed []string
	var blocked []string

	for _, request := range requests {
		domain := normalizeDomain(request.Domain)
		if domain == "" {
			continue
		}
		for _, game := range games {
			if _, ok := gameSeen[game.Key]; ok {
				continue
			}
			if !gameHasManagedDomain(game, domain) {
				continue
			}
			gameSeen[game.Key] = struct{}{}
			for _, hostname := range allManagedHostnames(game) {
				key := strings.ToLower(hostname)
				if _, ok := managedSeen[key]; ok {
					continue
				}
				managedSeen[key] = struct{}{}
				managed = append(managed, hostname)
			}
			for _, hostname := range blockedManagedHostnames(game) {
				key := strings.ToLower(hostname)
				if _, ok := blockedSeen[key]; ok {
					continue
				}
				blockedSeen[key] = struct{}{}
				blocked = append(blocked, hostname)
			}
		}
	}

	sort.Strings(managed)
	sort.Strings(blocked)
	return managed, blocked
}

func unionHostnames(groups ...[]string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, group := range groups {
		for _, hostname := range group {
			host := strings.TrimSpace(hostname)
			if host == "" {
				continue
			}
			key := strings.ToLower(host)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, host)
		}
	}
	sort.Strings(result)
	return result
}

func (a *App) emit(event string, payload ...interface{}) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, event, payload...)
}

func (a *App) logMessage(format string, args ...interface{}) {
	a.emit("scan:log", map[string]string{
		"time":    time.Now().Format("15:04:05"),
		"message": fmt.Sprintf(format, args...),
	})
}

func (a *App) rememberResults(rows []resultRow, aliases map[string][]string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.latestRows = append([]resultRow(nil), rows...)
	a.latestAliases = cloneAliases(aliases)
}

func (a *App) lookupLatestCandidate(domain string, ip string) (resultRow, map[string][]string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	domain = strings.TrimSpace(domain)
	ip = strings.TrimSpace(ip)
	for _, row := range a.latestRows {
		if row.Domain == domain && row.Address == ip {
			return row, cloneAliases(a.latestAliases), true
		}
	}
	return resultRow{}, nil, false
}

func (a *App) updateTraceResult(domain string, ip string, result traceResult) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for index := range a.latestRows {
		if a.latestRows[index].Domain != domain || a.latestRows[index].Address != ip {
			continue
		}
		a.latestRows[index].HopCount = result.HopCount
		a.latestRows[index].TraceReached = result.Reached
		a.latestRows[index].TraceStatus = result.Status
		a.latestRows[index].Note = mergeNotes(a.latestRows[index].Note, result.Note)
	}
}

func (a *App) hasResults() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.latestRows) > 0
}
