package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/miekg/dns"
)

var hopLinePattern = regexp.MustCompile(`^\s*(\d+)\s+`)

var defaultResolverSpecs = []resolverSpec{
	{Label: "223.5.5.5", Server: "223.5.5.5"},
	{Label: "119.29.29.29", Server: "119.29.29.29"},
	{Label: "180.76.76.76", Server: "180.76.76.76"},
	{Label: "1.1.1.1", Server: "1.1.1.1"},
	{Label: "8.8.8.8", Server: "8.8.8.8"},
}

type ipFamily string

const (
	family4   ipFamily = "4"
	family6   ipFamily = "6"
	familyAll ipFamily = "all"
)

type resolverSpec struct {
	Label  string
	Server string
}

type resolverClient struct {
	Spec     resolverSpec
	Resolver *net.Resolver
}

type config struct {
	ResolverSpecs  []resolverSpec
	ConfigPath     string
	ConfigDomains  []string
	Family         ipFamily
	HostsOut       string
	GamesSelection string
	UseCache       bool
	CacheFile      string
	LogFile        string
	Port           int
	Rounds         int
	RoundGap       time.Duration
	ResolveTimeout time.Duration
	ConnectTimeout time.Duration
	TraceEnabled   bool
	TraceMaxHops   int
	TraceWait      time.Duration
	TraceTimeout   time.Duration
	Workers        int
	ShowProgress   bool
	Logger         *runLogger
	Interactive    bool
	ManualInput    bool
	GamesWasSet    bool
	FamilyWasSet   bool
	TraceWasSet    bool
	UseCacheWasSet bool
	HostsOutWasSet bool
	ConfigWasSet   bool
	ConfigCreated  bool
}

type discoveryEntry struct {
	Domain         string
	Family         ipFamily
	Address        string
	CNAMEs         map[string]struct{}
	Resolvers      map[string]struct{}
	ResolveLatency time.Duration
}

type traceResult struct {
	HopCount int
	Reached  bool
	Status   string
	Note     string
}

type resultRow struct {
	Domain         string
	Family         ipFamily
	CNAME          string
	Address        string
	ResolverList   string
	ResolverCount  int
	ResolveLatency time.Duration
	ConnectLatency time.Duration
	ConnectOK      bool
	HopCount       int
	TraceReached   bool
	TraceStatus    string
	Note           string
}

type probeBatch struct {
	Domain string
	Rows   []resultRow
}

func main() {
	cfg, inputFile, csvFile, cliDomains := parseFlags()
	reader := bufio.NewReader(os.Stdin)
	logger, err := newRunLogger(cfg.LogFile)
	if err != nil {
		exitWithError(err.Error())
	}
	defer logger.Close()
	cfg.Logger = logger
	cfg.Logger.Printf("session started")
	if cfg.ConfigCreated {
		fmt.Printf("Initialized default config at %s\n", cfg.ConfigPath)
		cfg.Logger.Printf("initialized default config at %s", cfg.ConfigPath)
	}

	domains, aliases, err := resolveDomainsAndAliases(&cfg, inputFile, cliDomains, reader)
	if err != nil {
		exitWithError(err.Error())
	}
	if len(domains) == 0 {
		exitWithError("no valid domains found")
	}

	fmt.Println("Selected domains:")
	for _, domain := range domains {
		fmt.Println(" ", domain)
	}
	cfg.Logger.Printf("selected domains: %s", strings.Join(domains, ", "))

	var rows []resultRow
	if cfg.UseCache {
		cachedRows, err := readCache(cfg.CacheFile)
		if err != nil {
			exitWithError(err.Error())
		}
		rows = filterCachedRows(cachedRows, domains, cfg.Family)
		if len(rows) > 0 {
			fmt.Printf("\nUsing cached results from %s\n", cfg.CacheFile)
			cfg.Logger.Printf("using cached results from %s", cfg.CacheFile)
		} else {
			fmt.Printf("\nCache %s had no matching rows, running a live scan instead.\n", cfg.CacheFile)
			cfg.Logger.Printf("cache %s had no matching rows; falling back to live scan", cfg.CacheFile)
		}
	}

	if len(rows) == 0 {
		resolvers, err := newResolverClients(cfg.ResolverSpecs)
		if err != nil {
			exitWithError(err.Error())
		}

		fmt.Printf(
			"\nStarting live scan for %d domains with %d resolvers. Trace: %t. Log: %s\n",
			len(domains),
			len(cfg.ResolverSpecs),
			cfg.TraceEnabled,
			cfg.LogFile,
		)
		cfg.Logger.Printf(
			"live scan started: domains=%d resolvers=%d family=%s trace=%t progress=%t",
			len(domains),
			len(cfg.ResolverSpecs),
			cfg.Family,
			cfg.TraceEnabled,
			cfg.ShowProgress,
		)

		rows = run(cfg, resolvers, domains)
		if err := writeCache(cfg.CacheFile, rows); err != nil {
			exitWithError(err.Error())
		}
		fmt.Printf("\nCache saved to %s\n", cfg.CacheFile)
		cfg.Logger.Printf("cache saved to %s", cfg.CacheFile)
	}

	sortRows(rows)
	printTable(rows)
	printBestByDomain(rows)
	cfg.Logger.Printf("scan completed with %d result rows", len(rows))

	rowsForHosts := bestRowsByDomainAndFamily(rows)
	if cfg.HostsOut == "" && cfg.Interactive && !cfg.HostsOutWasSet && !cfg.ManualInput && !cfg.GamesWasSet {
		selectedRows, writeHosts, err := promptHostRows(reader, rows)
		if err != nil {
			exitWithError(err.Error())
		}
		if writeHosts {
			cfg.HostsOut = "system"
			rowsForHosts = selectedRows
		}
	}

	if cfg.HostsOut != "" {
		writtenPath, err := upsertHostsFile(cfg.HostsOut, rowsForHosts, aliases)
		if err != nil {
			exitWithError(err.Error())
		}
		fmt.Printf("\nHosts written to %s\n", writtenPath)
		cfg.Logger.Printf("hosts written to %s", writtenPath)
	}

	if csvFile != "" {
		if err := writeCSV(csvFile, rows); err != nil {
			exitWithError(err.Error())
		}
		fmt.Printf("\nCSV written to %s\n", csvFile)
		cfg.Logger.Printf("csv written to %s", csvFile)
	}
}

func parseFlags() (config, string, string, []string) {
	var (
		configPath     = flag.String("config", "config.json", "Optional JSON config file for domains and default scan options")
		inputFile      = flag.String("input", "", "Path to a file containing domains, one per line")
		csvFile        = flag.String("csv", "", "Optional CSV output path")
		dnsList        = flag.String("dns", "", "Comma-separated DNS resolvers. Use 'system' to include the OS resolver")
		gamesFlag      = flag.String("games", "", "Game selection like 12, 135, or all")
		familyFlag     = flag.String("family", string(family6), "IP family to scan: 4, 6, or all")
		hostsOut       = flag.String("hosts-out", "", "Write best results into a hosts file. Use 'system' for the OS hosts file")
		useCache       = flag.Bool("use-cache", false, "Use cached results instead of running a live scan")
		cacheFile      = flag.String("cache-file", defaultCacheFile, "Cache file path used for saving and loading scan results")
		logFile        = flag.String("log-file", "cache/latest_scan.log", "Log file path for scan progress and run details; use empty string to disable")
		port           = flag.Int("port", 443, "TCP port used for latency probing; set 0 to skip")
		rounds         = flag.Int("rounds", 3, "How many DNS sampling rounds to run per resolver")
		roundGap       = flag.Duration("round-gap", 300*time.Millisecond, "Pause between DNS rounds for the same resolver")
		resolveTimeout = flag.Duration("resolve-timeout", 5*time.Second, "Timeout for each AAAA lookup")
		connectTimeout = flag.Duration("connect-timeout", 3*time.Second, "Timeout for each TCP latency probe")
		traceEnabled   = flag.Bool("trace", false, "Run traceroute/tracert hop estimation")
		traceMaxHops   = flag.Int("trace-max-hops", 16, "Maximum hop count used by traceroute")
		traceWait      = flag.Duration("trace-wait", 400*time.Millisecond, "Per-hop wait budget used by traceroute")
		traceTimeout   = flag.Duration("trace-timeout", 20*time.Second, "Overall timeout for traceroute")
		workers        = flag.Int("workers", 4, "Number of concurrent domains to probe")
		progress       = flag.Bool("progress", true, "Show a live progress bar while scanning when stdout is a terminal")
	)

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] [domain1 domain2 ...]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Games:")
		for _, game := range knownGames {
			fmt.Fprintf(flag.CommandLine.Output(), "  %s. %s\n", game.ID, game.Name)
		}
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Examples:")
		fmt.Fprintf(flag.CommandLine.Output(), "  %s\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -games 12 -family all\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -games 5 -family 6 -csv wuwa.csv\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -family all -hosts-out system\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Default resolvers when -dns is omitted:")
		for _, spec := range defaultResolverSpecs {
			if spec.Server == "" {
				fmt.Fprintf(flag.CommandLine.Output(), "  %s\n", spec.Label)
				continue
			}
			fmt.Fprintf(flag.CommandLine.Output(), "  %s\n", spec.Server)
		}
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()
	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	if *workers < 1 {
		exitWithError("-workers must be at least 1")
	}
	if *rounds < 1 {
		exitWithError("-rounds must be at least 1")
	}
	if *traceMaxHops < 1 {
		exitWithError("-trace-max-hops must be at least 1")
	}
	if *port < 0 {
		exitWithError("-port cannot be negative")
	}
	if *roundGap < 0 {
		exitWithError("-round-gap cannot be negative")
	}
	if *resolveTimeout <= 0 || *connectTimeout <= 0 || *traceWait <= 0 || *traceTimeout <= 0 {
		exitWithError("all timeout values must be positive")
	}

	resolverSpecs, err := parseResolverSpecs(*dnsList)
	if err != nil {
		exitWithError(err.Error())
	}

	family, err := parseFamily(*familyFlag)
	if err != nil {
		exitWithError(err.Error())
	}

	cfg := config{
		ResolverSpecs:  resolverSpecs,
		ConfigPath:     strings.TrimSpace(*configPath),
		Family:         family,
		HostsOut:       strings.TrimSpace(*hostsOut),
		GamesSelection: strings.TrimSpace(*gamesFlag),
		UseCache:       *useCache,
		CacheFile:      strings.TrimSpace(*cacheFile),
		LogFile:        strings.TrimSpace(*logFile),
		Port:           *port,
		Rounds:         *rounds,
		RoundGap:       *roundGap,
		ResolveTimeout: *resolveTimeout,
		ConnectTimeout: *connectTimeout,
		TraceEnabled:   *traceEnabled,
		TraceMaxHops:   *traceMaxHops,
		TraceWait:      *traceWait,
		TraceTimeout:   *traceTimeout,
		Workers:        *workers,
		ShowProgress:   *progress && isTerminalFile(os.Stdout),
		Interactive:    isInteractiveTerminal(),
		ManualInput:    strings.TrimSpace(*inputFile) != "" || len(flag.Args()) > 0,
		GamesWasSet:    setFlags["games"],
		FamilyWasSet:   setFlags["family"],
		TraceWasSet:    setFlags["trace"],
		UseCacheWasSet: setFlags["use-cache"],
		HostsOutWasSet: setFlags["hosts-out"],
		ConfigWasSet:   setFlags["config"],
	}
	if cfg.CacheFile == "" {
		cfg.CacheFile = defaultCacheFile
	}

	fileCfg, foundConfig, createdConfig, err := loadFileConfig(cfg.ConfigPath, cfg.ConfigWasSet)
	if err != nil {
		exitWithError(err.Error())
	}
	cfg.ConfigCreated = createdConfig
	if foundConfig {
		allowDomains := strings.TrimSpace(*inputFile) == "" && len(flag.Args()) == 0
		if err := applyFileConfig(&cfg, fileCfg, allowDomains); err != nil {
			exitWithError(err.Error())
		}
	}

	return cfg, *inputFile, *csvFile, flag.Args()
}

func exitWithError(message string) {
	fmt.Fprintln(os.Stderr, "Error:", message)
	os.Exit(1)
}

func parseResolverSpecs(input string) ([]resolverSpec, error) {
	if strings.TrimSpace(input) == "" {
		return append([]resolverSpec(nil), defaultResolverSpecs...), nil
	}

	seen := make(map[string]struct{})
	var specs []resolverSpec
	for _, token := range strings.Split(input, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		if strings.EqualFold(token, "system") {
			if _, ok := seen["system"]; ok {
				continue
			}
			seen["system"] = struct{}{}
			specs = append(specs, resolverSpec{Label: "system"})
			continue
		}

		server, err := normalizeDNSAddress(token)
		if err != nil {
			return nil, fmt.Errorf("invalid -dns value %q: %w", token, err)
		}
		if _, ok := seen[server]; ok {
			continue
		}
		seen[server] = struct{}{}
		specs = append(specs, resolverSpec{
			Label:  token,
			Server: server,
		})
	}

	if len(specs) == 0 {
		return nil, errors.New("no usable resolvers found in -dns")
	}
	return specs, nil
}

func loadDomains(inputFile string, cliDomains []string) ([]string, error) {
	seen := make(map[string]struct{})
	var domains []string

	appendDomain := func(raw string) {
		normalized := normalizeDomain(raw)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		domains = append(domains, normalized)
	}

	if inputFile != "" {
		file, err := os.Open(inputFile)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", inputFile, err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			appendDomain(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read %s: %w", inputFile, err)
		}
	}

	for _, domain := range cliDomains {
		appendDomain(domain)
	}

	return domains, nil
}

func normalizeDomain(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}

	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	s = fields[0]

	if strings.Contains(s, "://") {
		if parsed, err := url.Parse(s); err == nil && parsed.Host != "" {
			s = parsed.Host
		}
	}

	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}

	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSuffix(s, ".")
	return strings.ToLower(s)
}

func newResolverClients(specs []resolverSpec) ([]resolverClient, error) {
	clients := make([]resolverClient, 0, len(specs))
	for _, spec := range specs {
		if spec.Server != "" {
			server, err := normalizeDNSAddress(spec.Server)
			if err != nil {
				return nil, fmt.Errorf("invalid resolver %q: %w", spec.Label, err)
			}
			spec.Server = server
		}

		resolver, err := newResolver(spec)
		if err != nil {
			return nil, err
		}
		clients = append(clients, resolverClient{
			Spec:     spec,
			Resolver: resolver,
		})
	}
	return clients, nil
}

func newResolver(spec resolverSpec) (*net.Resolver, error) {
	if spec.Server == "" {
		return net.DefaultResolver, nil
	}

	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := &net.Dialer{}
			return dialer.DialContext(ctx, network, spec.Server)
		},
	}, nil
}

func normalizeDNSAddress(server string) (string, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return "", errors.New("empty dns server")
	}

	if _, _, err := net.SplitHostPort(server); err == nil {
		return server, nil
	}

	if strings.HasPrefix(server, "[") && strings.HasSuffix(server, "]") {
		return net.JoinHostPort(strings.Trim(server, "[]"), "53"), nil
	}

	if ip := net.ParseIP(server); ip != nil {
		return net.JoinHostPort(server, "53"), nil
	}

	if strings.Contains(server, ":") {
		return "", fmt.Errorf("missing brackets or port for %s", server)
	}

	return net.JoinHostPort(server, "53"), nil
}

func run(cfg config, resolvers []resolverClient, domains []string) []resultRow {
	jobs := make(chan string)
	results := make(chan probeBatch)
	progress := newProgressTracker(len(domains), cfg.ShowProgress)
	defer progress.stop()

	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for domain := range jobs {
				progress.startDomain(domain)
				cfg.Logger.Printf("domain started: %s", domain)
				rows := probeDomain(cfg, resolvers, domain)
				progress.finishDomain(domain)
				cfg.Logger.Printf("domain finished: %s rows=%d", domain, len(rows))
				results <- probeBatch{
					Domain: domain,
					Rows:   rows,
				}
			}
		}()
	}

	go func() {
		for _, domain := range domains {
			jobs <- domain
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var rows []resultRow
	for batch := range results {
		rows = append(rows, batch.Rows...)
	}

	return rows
}

func probeDomain(cfg config, resolvers []resolverClient, domain string) []resultRow {
	discovered := make(map[string]*discoveryEntry)
	var errorNotes []string
	hadSuccessfulLookup := false

	for _, family := range cfg.Family.scanFamilies() {
		for _, client := range resolvers {
			resolverIPs := make(map[string]net.IP)
			resolverCNAMEs := make(map[string]struct{})
			var bestLatency time.Duration
			var lastErr error
			successThisResolver := false

			for round := 0; round < cfg.Rounds; round++ {
				ips, cnames, resolveLatency, err := lookupForResolver(client, domain, family, cfg.ResolveTimeout)
				if err != nil {
					lastErr = err
				} else {
					successThisResolver = true
					if bestLatency == 0 || resolveLatency < bestLatency {
						bestLatency = resolveLatency
					}
					for _, ip := range ips {
						resolverIPs[ip.String()] = ip
					}
					for _, cname := range cnames {
						if cname != "" {
							resolverCNAMEs[cname] = struct{}{}
						}
					}
				}

				if round+1 < cfg.Rounds && cfg.RoundGap > 0 {
					time.Sleep(cfg.RoundGap)
				}
			}

			if !successThisResolver {
				if lastErr != nil {
					errorNotes = append(errorNotes, fmt.Sprintf("%s/%s:%v", client.Spec.Label, family, lastErr))
				}
				continue
			}

			hadSuccessfulLookup = true
			for _, ip := range resolverIPs {
				key := fmt.Sprintf("%s|%s", family, ip.String())
				entry, ok := discovered[key]
				if !ok {
					entry = &discoveryEntry{
						Domain:         domain,
						Family:         family,
						Address:        ip.String(),
						CNAMEs:         make(map[string]struct{}),
						Resolvers:      make(map[string]struct{}),
						ResolveLatency: bestLatency,
					}
					discovered[key] = entry
				}
				if bestLatency > 0 && (entry.ResolveLatency == 0 || bestLatency < entry.ResolveLatency) {
					entry.ResolveLatency = bestLatency
				}
				entry.Resolvers[client.Spec.Label] = struct{}{}
				for cname := range resolverCNAMEs {
					entry.CNAMEs[cname] = struct{}{}
				}
			}
		}
	}

	if len(discovered) == 0 {
		status := missingStatus(cfg.Family)
		if !hadSuccessfulLookup {
			status = "resolve_failed"
		}

		return []resultRow{{
			Domain:       domain,
			ResolverList: joinResolverLabels(cfg.ResolverSpecs),
			TraceStatus:  status,
			Note:         joinNotes(errorNotes),
		}}
	}

	keys := make([]string, 0, len(discovered))
	for key := range discovered {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rows := make([]resultRow, 0, len(keys))
	for _, key := range keys {
		entry := discovered[key]
		row := resultRow{
			Domain:         entry.Domain,
			Family:         entry.Family,
			CNAME:          joinSet(entry.CNAMEs),
			Address:        entry.Address,
			ResolverList:   joinSet(entry.Resolvers),
			ResolverCount:  len(entry.Resolvers),
			ResolveLatency: entry.ResolveLatency,
			TraceStatus:    "disabled",
		}

		if cfg.Port > 0 {
			latency, err := measureTCP(net.ParseIP(entry.Address), cfg.Port, cfg.ConnectTimeout)
			row.ConnectLatency = latency
			row.ConnectOK = err == nil
			if err != nil {
				row.Note = mergeNotes(row.Note, fmt.Sprintf("tcp:%v", err))
			}
		}

		if cfg.TraceEnabled {
			trace := traceIP(entry.Address, entry.Family, cfg)
			row.HopCount = trace.HopCount
			row.TraceReached = trace.Reached
			row.TraceStatus = trace.Status
			row.Note = mergeNotes(row.Note, trace.Note)
		}

		rows = append(rows, row)
	}

	return rows
}

func lookupForResolver(client resolverClient, domain string, family ipFamily, timeout time.Duration) ([]net.IP, []string, time.Duration, error) {
	if client.Spec.Server != "" {
		return lookupWithDNSClient(client.Spec.Server, domain, family, timeout)
	}

	resolver := client.Resolver
	resolveCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	ips, err := resolver.LookupIP(resolveCtx, family.lookupNetwork(), domain)
	latency := time.Since(start)
	if err != nil {
		return nil, nil, latency, err
	}

	cname := lookupCanonicalName(resolver, domain, timeout)
	filtered := filterIPs(ips, family)
	if cname == "" {
		return filtered, nil, latency, nil
	}
	return filtered, []string{cname}, latency, nil
}

func lookupCanonicalName(resolver *net.Resolver, domain string, timeout time.Duration) string {
	cnameCtx, cancel := context.WithTimeout(context.Background(), minDuration(timeout, 2*time.Second))
	defer cancel()

	cname, err := resolver.LookupCNAME(cnameCtx, domain)
	if err != nil {
		return ""
	}

	cname = strings.TrimSuffix(strings.ToLower(cname), ".")
	if cname == strings.ToLower(domain) {
		return ""
	}
	return cname
}

func lookupWithDNSClient(server string, domain string, family ipFamily, timeout time.Duration) ([]net.IP, []string, time.Duration, error) {
	start := time.Now()
	cnameSet := make(map[string]struct{})
	ipSet := make(map[string]net.IP)
	current := strings.ToLower(strings.TrimSuffix(domain, "."))
	seenNames := make(map[string]struct{})
	var lastErr error

	for depth := 0; depth < 8; depth++ {
		if _, ok := seenNames[current]; ok {
			break
		}
		seenNames[current] = struct{}{}

		ips, cnames, nextName, err := lookupOneHop(server, current, family, timeout)
		if err != nil {
			lastErr = err
			break
		}

		for _, ip := range ips {
			ipSet[ip.String()] = ip
		}
		for _, cname := range cnames {
			cnameSet[cname] = struct{}{}
		}

		if len(ipSet) > 0 || nextName == "" {
			break
		}
		current = nextName
	}

	latency := time.Since(start)
	if len(ipSet) == 0 && lastErr != nil {
		return nil, nil, latency, lastErr
	}

	var ips []net.IP
	for _, ip := range ipSet {
		ips = append(ips, ip)
	}

	filtered := filterIPs(ips, family)
	var cnames []string
	for cname := range cnameSet {
		cnames = append(cnames, cname)
	}
	sort.Strings(cnames)
	return filtered, cnames, latency, nil
}

func lookupOneHop(server string, domain string, family ipFamily, timeout time.Duration) ([]net.IP, []string, string, error) {
	query := new(dns.Msg)
	query.SetQuestion(dns.Fqdn(domain), family.dnsType())
	query.RecursionDesired = true

	client := &dns.Client{
		Net:     "udp",
		Timeout: timeout,
	}

	response, _, err := client.Exchange(query, server)
	if err != nil {
		return nil, nil, "", err
	}
	if response == nil {
		return nil, nil, "", errors.New("empty DNS response")
	}

	if response.Truncated {
		client.Net = "tcp"
		response, _, err = client.Exchange(query, server)
		if err != nil {
			return nil, nil, "", err
		}
		if response == nil {
			return nil, nil, "", errors.New("empty DNS response")
		}
	}

	switch response.Rcode {
	case dns.RcodeSuccess:
	case dns.RcodeNameError:
		return nil, nil, "", errors.New("no such host")
	default:
		return nil, nil, "", fmt.Errorf("dns rcode %s", dns.RcodeToString[response.Rcode])
	}

	cnameMap := make(map[string]string)
	cnameSet := make(map[string]struct{})
	var ips []net.IP
	for _, answer := range response.Answer {
		switch rr := answer.(type) {
		case *dns.CNAME:
			from := strings.TrimSuffix(strings.ToLower(rr.Hdr.Name), ".")
			target := strings.TrimSuffix(strings.ToLower(rr.Target), ".")
			if from != "" && target != "" && from != target {
				cnameMap[from] = target
				cnameSet[target] = struct{}{}
			}
		case *dns.A:
			if family == family4 && rr.A != nil {
				ips = append(ips, rr.A)
			}
		case *dns.AAAA:
			if family == family6 && rr.AAAA != nil {
				ips = append(ips, rr.AAAA)
			}
		}
	}

	var cnames []string
	for cname := range cnameSet {
		cnames = append(cnames, cname)
	}
	sort.Strings(cnames)

	nextName := followCNAMEChain(domain, cnameMap)
	return filterIPs(ips, family), cnames, nextName, nil
}

func followCNAMEChain(domain string, cnameMap map[string]string) string {
	current := strings.ToLower(strings.TrimSuffix(domain, "."))
	seen := make(map[string]struct{})
	for {
		next, ok := cnameMap[current]
		if !ok || next == "" {
			break
		}
		if _, ok := seen[next]; ok {
			break
		}
		seen[next] = struct{}{}
		current = next
	}

	if current == strings.ToLower(strings.TrimSuffix(domain, ".")) {
		return ""
	}
	return current
}

func filterIPs(ips []net.IP, family ipFamily) []net.IP {
	seen := make(map[string]struct{})
	var filtered []net.IP
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		actualFamily := familyFromIP(ip)
		if actualFamily == "" {
			continue
		}
		if actualFamily != family {
			continue
		}
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, ip)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].String() < filtered[j].String()
	})

	return filtered
}

func measureTCP(ip net.IP, port int, timeout time.Duration) (time.Duration, error) {
	if ip == nil {
		return 0, errors.New("invalid IP address")
	}

	network := "tcp6"
	if familyFromIP(ip) == family4 {
		network = "tcp4"
	}

	target := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, network, target)
	latency := time.Since(start)
	if err != nil {
		return latency, err
	}
	_ = conn.Close()
	return latency, nil
}

func traceIP(ip string, family ipFamily, cfg config) traceResult {
	command, args, err := tracerouteCommand(ip, family, cfg)
	if err != nil {
		return traceResult{
			Status: "unsupported",
			Note:   err.Error(),
		}
	}

	if _, err := exec.LookPath(command); err != nil {
		return traceResult{
			Status: "command_missing",
			Note:   err.Error(),
		}
	}

	timeout := maxDuration(
		cfg.TraceTimeout,
		time.Duration(cfg.TraceMaxHops)*cfg.TraceWait*3+2*time.Second,
	)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	hopCount, reached := parseTraceOutput(string(output), ip)
	if reached {
		return traceResult{
			HopCount: hopCount,
			Reached:  true,
			Status:   "ok",
		}
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if hopCount > 0 {
			return traceResult{
				HopCount: hopCount,
				Reached:  false,
				Status:   "partial",
				Note:     "traceroute timed out",
			}
		}
		return traceResult{
			Status: "timeout",
			Note:   "traceroute timed out",
		}
	}

	if hopCount > 0 {
		note := ""
		if err != nil {
			note = err.Error()
		}
		return traceResult{
			HopCount: hopCount,
			Reached:  false,
			Status:   "partial",
			Note:     note,
		}
	}

	if err != nil {
		return traceResult{
			Status: "failed",
			Note:   err.Error(),
		}
	}

	return traceResult{
		Status: "no_route",
		Note:   "no hops detected",
	}
}

func tracerouteCommand(ip string, family ipFamily, cfg config) (string, []string, error) {
	switch runtime.GOOS {
	case "windows":
		waitMillis := max(1, int(cfg.TraceWait/time.Millisecond))
		return "tracert", []string{
			family.traceFlag(),
			"-d",
			"-h", strconv.Itoa(cfg.TraceMaxHops),
			"-w", strconv.Itoa(waitMillis),
			ip,
		}, nil
	case "linux", "darwin":
		waitSeconds := fmt.Sprintf("%.3f", cfg.TraceWait.Seconds())
		return "traceroute", []string{
			family.traceFlag(),
			"-n",
			"-m", strconv.Itoa(cfg.TraceMaxHops),
			"-w", waitSeconds,
			"-q", "1",
			ip,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported platform for traceroute: %s", runtime.GOOS)
	}
}

func parseTraceOutput(output string, targetIP string) (int, bool) {
	maxHop := 0
	reachedHop := 0

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		matches := hopLinePattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}

		hop, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		if hop > maxHop {
			maxHop = hop
		}
		if strings.Contains(line, targetIP) {
			reachedHop = hop
		}
	}

	if reachedHop > 0 {
		return reachedHop, true
	}
	return maxHop, false
}

func sortRows(rows []resultRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Domain != rows[j].Domain {
			return rows[i].Domain < rows[j].Domain
		}
		if familyOrder(rows[i].Family) != familyOrder(rows[j].Family) {
			return familyOrder(rows[i].Family) < familyOrder(rows[j].Family)
		}
		if rows[i].ConnectOK != rows[j].ConnectOK {
			return rows[i].ConnectOK
		}
		if rows[i].ConnectLatency != rows[j].ConnectLatency {
			switch {
			case rows[i].ConnectLatency == 0:
				return false
			case rows[j].ConnectLatency == 0:
				return true
			default:
				return rows[i].ConnectLatency < rows[j].ConnectLatency
			}
		}
		return rows[i].Address < rows[j].Address
	})
}

func printTable(rows []resultRow) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DOMAIN\tFAMILY\tCNAME\tADDRESS\tRESOLVERS\tDNS_MS\tTCP_MS\tTCP_OK\tHOPS\tTRACE\tNOTE")
	for _, row := range rows {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\t%s\t%s\n",
			row.Domain,
			valueOrDash(string(row.Family)),
			valueOrDash(row.CNAME),
			valueOrDash(row.Address),
			valueOrDash(row.ResolverList),
			formatMillis(row.ResolveLatency),
			formatOptionalMillis(row.ConnectLatency, row.ConnectOK || row.ConnectLatency > 0),
			row.ConnectOK,
			formatOptionalInt(row.HopCount),
			row.TraceStatus,
			valueOrDash(row.Note),
		)
	}
	_ = tw.Flush()
}

func printBestByDomain(rows []resultRow) {
	bestRows := bestRowsByDomainAndFamily(rows)
	if len(bestRows) == 0 {
		return
	}

	fmt.Println()

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "BEST_DOMAIN\tFAMILY\tBEST_CNAME\tBEST_ADDRESS\tRESOLVERS\tTCP_MS\tHOPS\tTRACE\tNOTE")
	for _, row := range bestRows {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Domain,
			valueOrDash(string(row.Family)),
			valueOrDash(row.CNAME),
			valueOrDash(row.Address),
			valueOrDash(row.ResolverList),
			formatOptionalMillis(row.ConnectLatency, row.ConnectOK || row.ConnectLatency > 0),
			formatOptionalInt(row.HopCount),
			row.TraceStatus,
			valueOrDash(row.Note),
		)
	}
	_ = tw.Flush()
}

func bestRowsByDomainAndFamily(rows []resultRow) []resultRow {
	best := make(map[string]resultRow)
	for _, row := range rows {
		key := row.Domain + "|" + string(row.Family)
		current, ok := best[key]
		if !ok || betterRow(row, current) {
			best[key] = row
		}
	}

	keys := make([]string, 0, len(best))
	for key := range best {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]resultRow, 0, len(keys))
	for _, key := range keys {
		result = append(result, best[key])
	}
	sortRows(result)
	return result
}

func betterRow(left resultRow, right resultRow) bool {
	leftHasAddress := strings.TrimSpace(left.Address) != ""
	rightHasAddress := strings.TrimSpace(right.Address) != ""
	if leftHasAddress != rightHasAddress {
		return leftHasAddress
	}
	if left.ConnectOK != right.ConnectOK {
		return left.ConnectOK
	}
	if left.ConnectLatency != right.ConnectLatency {
		switch {
		case left.ConnectLatency == 0:
			return false
		case right.ConnectLatency == 0:
			return true
		default:
			return left.ConnectLatency < right.ConnectLatency
		}
	}
	if left.ResolverCount != right.ResolverCount {
		return left.ResolverCount > right.ResolverCount
	}
	if left.TraceReached != right.TraceReached {
		return left.TraceReached
	}
	if left.HopCount != right.HopCount {
		switch {
		case left.HopCount == 0:
			return false
		case right.HopCount == 0:
			return true
		default:
			return left.HopCount < right.HopCount
		}
	}
	return left.Address < right.Address
}

func writeCSV(path string, rows []resultRow) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{
		"domain",
		"family",
		"cname",
		"address",
		"resolvers",
		"dns_ms",
		"tcp_ms",
		"tcp_ok",
		"hop_count",
		"trace_reached",
		"trace_status",
		"note",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for _, row := range rows {
		record := []string{
			row.Domain,
			string(row.Family),
			row.CNAME,
			row.Address,
			row.ResolverList,
			formatMillis(row.ResolveLatency),
			formatOptionalMillis(row.ConnectLatency, row.ConnectOK || row.ConnectLatency > 0),
			strconv.FormatBool(row.ConnectOK),
			formatOptionalInt(row.HopCount),
			strconv.FormatBool(row.TraceReached),
			row.TraceStatus,
			row.Note,
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}

	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}

	return nil
}

func formatMillis(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond))
}

func formatOptionalMillis(d time.Duration, enabled bool) string {
	if !enabled {
		return ""
	}
	return formatMillis(d)
}

func formatOptionalInt(v int) string {
	if v <= 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func joinSet(values map[string]struct{}) string {
	if len(values) == 0 {
		return ""
	}

	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return strings.Join(items, ",")
}

func joinResolverLabels(specs []resolverSpec) string {
	items := make([]string, 0, len(specs))
	for _, spec := range specs {
		items = append(items, spec.Label)
	}
	return strings.Join(items, ",")
}

func joinNotes(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	sort.Strings(notes)
	return strings.Join(notes, " | ")
}

func mergeNotes(left string, right string) string {
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + "; " + right
	}
}

func minDuration(left time.Duration, right time.Duration) time.Duration {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}

func maxDuration(left time.Duration, right time.Duration) time.Duration {
	if left <= 0 {
		return right
	}
	if right > left {
		return right
	}
	return left
}

func valueOrDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
