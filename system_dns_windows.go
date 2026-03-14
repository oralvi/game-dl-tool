//go:build windows

package main

import (
	"bufio"
	"net"
	"os/exec"
	"strings"
	"sync"
)

var (
	localResolverValuesOnce sync.Once
	localResolverValues     []string
)

func discoverLocalResolverValues() []string {
	localResolverValuesOnce.Do(func() {
		localResolverValues = lookupLocalResolverValues()
	})
	return append([]string(nil), localResolverValues...)
}

func lookupLocalResolverValues() []string {
	values := runLocalResolverDiscovery("Get-NetIPConfiguration | Where-Object { $_.NetAdapter.Status -eq 'Up' -and $_.DNSServer.ServerAddresses } | ForEach-Object { $_.DNSServer.ServerAddresses }")
	if len(values) > 0 {
		return values
	}

	return runLocalResolverDiscovery("Get-DnsClientServerAddress -AddressFamily IPv4,IPv6 | Where-Object { $_.ServerAddresses } | ForEach-Object { $_.ServerAddresses }")
}

func runLocalResolverDiscovery(script string) []string {
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	)
	prepareBackgroundCommand(cmd)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	var values []string
	scanner := bufio.NewScanner(strings.NewReader(decodeCommandOutput(output)))
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}

		value, ok := normalizeLocalResolverValue(raw)
		if !ok {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func normalizeLocalResolverValue(raw string) (string, bool) {
	host := strings.TrimSpace(raw)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if zoneIndex := strings.Index(host, "%"); zoneIndex >= 0 {
		host = host[:zoneIndex]
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return "", false
	}
	if ip.IsLoopback() || ip.IsUnspecified() {
		return "", false
	}
	if ip.To4() == nil {
		if ip.IsLinkLocalUnicast() {
			return "", false
		}
		if strings.HasPrefix(strings.ToLower(ip.String()), "fec0:0:0:ffff:") {
			return "", false
		}
	}
	return host, true
}
