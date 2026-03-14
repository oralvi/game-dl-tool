package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
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
	"time"

	"github.com/miekg/dns"
)

var hopLinePattern = regexp.MustCompile(`^\s*(\d+)\s+`)

const systemResolverToken = "system"

var defaultResolverSpecs = []resolverSpec{
	{Label: "223.5.5.5", Server: "223.5.5.5"},
	{Label: "223.6.6.6", Server: "223.6.6.6"},
	{Label: "119.29.29.29", Server: "119.29.29.29"},
	{Label: "180.76.76.76", Server: "180.76.76.76"},
	{Label: "114.114.114.114", Server: "114.114.114.114"},
	{Label: "114.114.115.115", Server: "114.114.115.115"},
	{Label: "1.1.1.1", Server: "1.1.1.1"},
	{Label: "8.8.8.8", Server: "8.8.8.8"},
	{Label: "9.9.9.9", Server: "9.9.9.9"},
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

type geoIPConfig struct {
	PrimaryProvider  string
	FallbackProvider string
	IPInfoToken      string
	CacheFile        string
	MMDBCityPath     string
	MMDBASNPath      string
}

type config struct {
	ResolverSpecs  []resolverSpec
	ConfigPath     string
	GameCatalog    []gameTarget
	Family         ipFamily
	GeoIP          geoIPConfig
	TunnelPort     int
	CacheFile      string
	LogFile        string
	Port           int
	Rounds         int
	RoundGap       time.Duration
	ResolveTimeout time.Duration
	ConnectTimeout time.Duration
	TraceMaxHops   int
	TraceWait      time.Duration
	TraceTimeout   time.Duration
	Workers        int
	ShowProgress   bool
	Logger         *runLogger
	ProgressSink   func(progressSnapshot)
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
	HopCount  int
	Reached   bool
	Status    string
	Note      string
	Hops      []traceHop
	RawOutput string
}

type traceHop struct {
	Hop       int    `json:"hop"`
	IPAddress string `json:"ipAddress"`
	Hostname  string `json:"hostname"`
	Geo       string `json:"geo"`
	Network   string `json:"network"`
	RTT       string `json:"rtt"`
	Status    string `json:"status"`
	RawLine   string `json:"rawLine"`
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
		if spec.Server != "" && !isSystemResolverValue(spec.Server) {
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
	if spec.Server == "" || isSystemResolverValue(spec.Server) {
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
	if isSystemResolverValue(server) {
		return systemResolverToken, nil
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

func isSystemResolverValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case systemResolverToken, "local", "localdns", "os":
		return true
	default:
		return false
	}
}

func run(cfg config, resolvers []resolverClient, domains []string) []resultRow {
	jobs := make(chan string)
	results := make(chan probeBatch)
	progress := newProgressTracker(len(domains), cfg.ShowProgress, cfg.ProgressSink)
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

	cmd := exec.CommandContext(ctx, command, args...)
	prepareBackgroundCommand(cmd)
	output, err := cmd.CombinedOutput()
	rawOutput := strings.TrimSpace(decodeCommandOutput(output))
	hops := parseTraceHops(rawOutput)
	hops = enrichTraceHops(cfg, hops)
	hopCount, reached := summarizeTraceHops(hops, ip)
	if reached {
		return traceResult{
			HopCount:  hopCount,
			Reached:   true,
			Status:    "ok",
			Hops:      hops,
			RawOutput: rawOutput,
		}
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if hopCount > 0 {
			return traceResult{
				HopCount:  hopCount,
				Reached:   false,
				Status:    "partial",
				Note:      "traceroute timed out",
				Hops:      hops,
				RawOutput: rawOutput,
			}
		}
		return traceResult{
			Status:    "timeout",
			Note:      "traceroute timed out",
			Hops:      hops,
			RawOutput: rawOutput,
		}
	}

	if hopCount > 0 {
		note := ""
		if err != nil {
			note = err.Error()
		}
		return traceResult{
			HopCount:  hopCount,
			Reached:   false,
			Status:    "partial",
			Note:      note,
			Hops:      hops,
			RawOutput: rawOutput,
		}
	}

	if err != nil {
		return traceResult{
			Status:    "failed",
			Note:      err.Error(),
			Hops:      hops,
			RawOutput: rawOutput,
		}
	}

	return traceResult{
		Status:    "no_route",
		Note:      "no hops detected",
		Hops:      hops,
		RawOutput: rawOutput,
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

func parseTraceHops(output string) []traceHop {
	if strings.TrimSpace(output) == "" {
		return nil
	}

	var hops []traceHop
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

		tail := strings.TrimSpace(line[len(matches[0]):])
		ip := findLastIPToken(line)
		rtt := tail
		status := "ok"
		if ip != "" {
			if index := strings.LastIndex(tail, ip); index >= 0 {
				rtt = strings.TrimSpace(tail[:index])
			}
		} else {
			status = "timeout"
		}
		if containsTraceTimeoutText(line) {
			status = "timeout"
		}
		if strings.TrimSpace(rtt) == "" {
			rtt = "-"
		}

		hops = append(hops, traceHop{
			Hop:       hop,
			IPAddress: ip,
			RTT:       rtt,
			Status:    status,
			RawLine:   strings.TrimSpace(line),
		})
	}
	return hops
}

func summarizeTraceHops(hops []traceHop, targetIP string) (int, bool) {
	maxHop := 0
	reachedHop := 0
	for _, hop := range hops {
		if hop.Hop > maxHop {
			maxHop = hop.Hop
		}
		if strings.TrimSpace(targetIP) != "" && hop.IPAddress == strings.TrimSpace(targetIP) {
			reachedHop = hop.Hop
		}
	}
	if reachedHop > 0 {
		return reachedHop, true
	}
	return maxHop, false
}

func findLastIPToken(line string) string {
	fields := strings.Fields(line)
	for index := len(fields) - 1; index >= 0; index-- {
		token := strings.Trim(fields[index], "[](),")
		if net.ParseIP(token) != nil {
			return token
		}
	}
	return ""
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
