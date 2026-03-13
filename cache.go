package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const defaultCacheFile = "scan_cache.json"

type cachedScan struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Rows        []resultRow `json:"rows"`
}

func writeCache(path string, rows []resultRow) error {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("prepare cache dir %s: %w", dir, err)
		}
	}

	payload, err := json.MarshalIndent(cachedScan{
		GeneratedAt: time.Now(),
		Rows:        rows,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write cache %s: %w", path, err)
	}
	return nil
}

func readCache(path string) ([]resultRow, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read cache %s: %w", path, err)
	}

	var payload cachedScan
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse cache %s: %w", path, err)
	}
	return payload.Rows, nil
}

func filterCachedRows(rows []resultRow, domains []string, family ipFamily) []resultRow {
	domainSet := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domainSet[domain] = struct{}{}
	}

	var filtered []resultRow
	for _, row := range rows {
		if _, ok := domainSet[row.Domain]; !ok {
			continue
		}
		if family != familyAll && row.Family != family {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
