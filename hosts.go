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

const hostsTag = "#DLTOOL"

func upsertHostsFile(target string, rows []resultRow, aliases map[string][]string) (string, error) {
	path, err := resolveHostsPath(target)
	if err != nil {
		return "", err
	}

	lines := buildHostEntries(rows, aliases)

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
		backupPath := path + ".game-dl-tool.bak"
		if err := os.WriteFile(backupPath, contentBytes, mode); err != nil {
			return "", fmt.Errorf("write hosts backup %s: %w", backupPath, err)
		}
	}

	rebuilt := rebuildHostsContent(string(contentBytes), lines)
	if err := os.WriteFile(path, []byte(rebuilt), mode); err != nil {
		return "", fmt.Errorf("write hosts %s: %w", path, err)
	}
	return path, nil
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

func buildHostEntries(bestRows []resultRow, aliases map[string][]string) []string {
	seen := make(map[string]struct{})
	var lines []string
	for _, row := range bestRows {
		if strings.TrimSpace(row.Address) == "" {
			continue
		}
		hostnames := aliases[row.Domain]
		if len(hostnames) == 0 {
			hostnames = []string{row.Domain}
		}
		for _, host := range hostnames {
			line := fmt.Sprintf("%s %s %s", row.Address, host, hostsTag)
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return lines
}

func rebuildHostsContent(existing string, entries []string) string {
	eol := detectLineEnding(existing)
	managedHosts := managedHostnames(entries)
	scanner := bufio.NewScanner(strings.NewReader(existing))
	var kept []string
	for scanner.Scan() {
		line := scanner.Text()
		if host, ok := taggedHost(line); ok {
			if _, managed := managedHosts[host]; managed {
				continue
			}
		}
		if strings.Contains(line, hostsTag) && len(managedHosts) == 0 {
			continue
		}
		kept = append(kept, line)
	}

	kept = trimTrailingEmptyLines(kept)
	if len(entries) > 0 {
		if len(kept) > 0 {
			kept = append(kept, "")
		}
		kept = append(kept, entries...)
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

func managedHostnames(entries []string) map[string]struct{} {
	hosts := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		fields := strings.Fields(entry)
		if len(fields) < 3 {
			continue
		}
		hosts[strings.ToLower(fields[1])] = struct{}{}
	}
	return hosts
}

func taggedHost(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", false
	}
	if fields[len(fields)-1] != hostsTag {
		return "", false
	}
	return strings.ToLower(fields[1]), true
}
