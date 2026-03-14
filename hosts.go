package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	hostsTag                  = "#DLTOOL"
	hostsTunnelTag            = "#DLTOOL-TUNNEL"
	hostsBlockTag             = "#DLTOOL-BLOCK"
	hostsBackupSuffix         = ".game-dl-tool.bak"
	hostsOriginalBackupSuffix = ".game-dl-tool.original.bak"
)

type hostEntry struct {
	Address  string
	Hostname string
	Tag      string
}

func upsertHostsFile(target string, rows []resultRow, aliases map[string][]string) (string, error) {
	return upsertTaggedHostsFile(target, buildHostEntries(rows, aliases, hostsTag), hostsTag)
}

func upsertTunnelHostsFile(target string, rows []resultRow, aliases map[string][]string) (string, error) {
	return upsertTaggedHostsFile(target, buildTunnelHostEntries(rows, aliases), hostsTunnelTag)
}

func upsertBlockedHostsFile(target string, hostnames []string) (string, error) {
	return upsertTaggedHostsFile(target, buildBlockedHostEntries(hostnames), hostsBlockTag)
}

func clearTunnelHostsFile(target string) (string, error) {
	return upsertTaggedHostsFile(target, nil, hostsTunnelTag)
}

func clearTaggedHostsFile(target string, tag string, hostnames []string) (string, error) {
	entries := make([]hostEntry, 0, len(hostnames))
	for _, hostname := range hostnames {
		entries = append(entries, hostEntry{
			Hostname: hostname,
			Tag:      tag,
		})
	}
	return upsertTaggedHostsFile(target, entries, tag)
}

func upsertTaggedHostsFile(target string, entries []hostEntry, tag string) (string, error) {
	path, err := resolveHostsPath(target)
	if err != nil {
		return "", err
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read hosts %s: %w", path, err)
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
	}

	if len(contentBytes) > 0 {
		if err := ensureOriginalHostsBackup(path, contentBytes, mode); err != nil {
			return "", err
		}

		backupPath := path + hostsBackupSuffix
		if err := os.WriteFile(backupPath, contentBytes, mode); err != nil {
			return "", fmt.Errorf("write hosts backup %s: %w", backupPath, err)
		}
	}

	rebuilt := rebuildHostsContent(string(contentBytes), entries, tag)
	if err := os.WriteFile(path, []byte(rebuilt), mode); err != nil {
		return "", fmt.Errorf("write hosts %s: %w", path, err)
	}
	return path, nil
}

func ensureOriginalHostsBackup(path string, content []byte, mode os.FileMode) error {
	backupPath := path + hostsOriginalBackupSuffix
	if _, err := os.Stat(backupPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat original hosts backup %s: %w", backupPath, err)
	}

	if err := os.WriteFile(backupPath, content, mode); err != nil {
		return fmt.Errorf("write original hosts backup %s: %w", backupPath, err)
	}
	return nil
}

func resolveHostsPath(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("empty hosts target")
	}
	if strings.EqualFold(target, "system") {
		if runtime.GOOS == "windows" {
			root := os.Getenv("SystemRoot")
			if root == "" {
				root = os.Getenv("WINDIR")
			}
			if root == "" {
				root = `C:\Windows`
			}
			return filepath.Join(root, "System32", "drivers", "etc", "hosts"), nil
		}
		return "/etc/hosts", nil
	}
	return filepath.Clean(target), nil
}

func buildHostEntries(bestRows []resultRow, aliases map[string][]string, tag string) []hostEntry {
	seen := make(map[string]struct{})
	var lines []hostEntry
	for _, row := range bestRows {
		if strings.TrimSpace(row.Address) == "" {
			continue
		}
		hostnames := aliases[row.Domain]
		if len(hostnames) == 0 {
			hostnames = []string{row.Domain}
		}
		for _, host := range hostnames {
			key := strings.ToLower(strings.TrimSpace(row.Address)) + "|" + strings.ToLower(strings.TrimSpace(host)) + "|" + tag
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			lines = append(lines, hostEntry{
				Address:  row.Address,
				Hostname: host,
				Tag:      tag,
			})
		}
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Hostname != lines[j].Hostname {
			return lines[i].Hostname < lines[j].Hostname
		}
		if lines[i].Address != lines[j].Address {
			return lines[i].Address < lines[j].Address
		}
		return lines[i].Tag < lines[j].Tag
	})
	return lines
}

func buildTunnelHostEntries(bestRows []resultRow, aliases map[string][]string) []hostEntry {
	base := buildHostEntries(bestRows, aliases, hostsTunnelTag)
	seen := make(map[string]struct{})
	var entries []hostEntry
	for _, item := range base {
		for _, address := range []string{tunnelLoopbackIPv4, tunnelLoopbackIPv6} {
			key := address + "|" + item.Hostname
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			entries = append(entries, hostEntry{
				Address:  address,
				Hostname: item.Hostname,
				Tag:      hostsTunnelTag,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Hostname != entries[j].Hostname {
			return entries[i].Hostname < entries[j].Hostname
		}
		if entries[i].Address != entries[j].Address {
			return entries[i].Address < entries[j].Address
		}
		return entries[i].Tag < entries[j].Tag
	})
	return entries
}

func buildBlockedHostEntries(hostnames []string) []hostEntry {
	seen := make(map[string]struct{})
	var entries []hostEntry
	for _, hostname := range hostnames {
		host := strings.TrimSpace(hostname)
		if host == "" {
			continue
		}
		for _, address := range []string{"0.0.0.0", "::"} {
			key := strings.ToLower(address + "|" + host)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			entries = append(entries, hostEntry{
				Address:  address,
				Hostname: host,
				Tag:      hostsBlockTag,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Hostname != entries[j].Hostname {
			return entries[i].Hostname < entries[j].Hostname
		}
		if entries[i].Address != entries[j].Address {
			return entries[i].Address < entries[j].Address
		}
		return entries[i].Tag < entries[j].Tag
	})
	return entries
}

func rebuildHostsContent(existing string, entries []hostEntry, tag string) string {
	eol := detectLineEnding(existing)
	managedHosts := managedHostnames(entries)
	scanner := bufio.NewScanner(strings.NewReader(existing))
	var kept []string
	for scanner.Scan() {
		line := scanner.Text()
		if host, lineTag, ok := taggedHost(line); ok && lineTag == tag {
			if _, managed := managedHosts[host]; managed {
				continue
			}
		}
		if strings.Contains(line, tag) && len(managedHosts) == 0 {
			continue
		}
		kept = append(kept, line)
	}

	kept = trimTrailingEmptyLines(kept)
	validEntries := 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.Address) != "" && strings.TrimSpace(entry.Hostname) != "" {
			validEntries++
		}
	}
	if validEntries > 0 {
		if len(kept) > 0 {
			kept = append(kept, "")
		}
		for _, entry := range entries {
			if strings.TrimSpace(entry.Address) == "" || strings.TrimSpace(entry.Hostname) == "" {
				continue
			}
			kept = append(kept, fmt.Sprintf("%s %s %s", entry.Address, entry.Hostname, entry.Tag))
		}
	}

	result := strings.Join(kept, eol)
	if result != "" {
		result += eol
	}
	return result
}

func detectLineEnding(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func trimTrailingEmptyLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

func managedHostnames(entries []hostEntry) map[string]struct{} {
	hosts := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		host := strings.ToLower(strings.TrimSpace(entry.Hostname))
		if host == "" {
			continue
		}
		hosts[host] = struct{}{}
	}
	return hosts
}

func taggedHost(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", "", false
	}
	tag := fields[len(fields)-1]
	if !strings.HasPrefix(tag, "#DLTOOL") {
		return "", "", false
	}
	return strings.ToLower(fields[1]), tag, true
}
