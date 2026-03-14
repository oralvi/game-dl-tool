package main

import (
	"fmt"
	"time"
)

func defaultRuntimeConfig() config {
	return config{
		ResolverSpecs:  append([]resolverSpec(nil), defaultResolverSpecs...),
		ConfigPath:     "config.json",
		GameCatalog:    defaultGameCatalog(),
		Family:         family6,
		GeoIP:          defaultGeoIPConfig(),
		TunnelPort:     0,
		CacheFile:      defaultCacheFile,
		LogFile:        "latest_scan.log",
		Port:           443,
		Rounds:         3,
		RoundGap:       300 * time.Millisecond,
		ResolveTimeout: 5 * time.Second,
		ConnectTimeout: 3 * time.Second,
		TraceMaxHops:   16,
		TraceWait:      400 * time.Millisecond,
		TraceTimeout:   20 * time.Second,
		Workers:        4,
		ShowProgress:   false,
	}
}

func executeScan(cfg config, domains []string) ([]resultRow, bool, error) {
	if len(domains) == 0 {
		return nil, false, fmt.Errorf("no valid domains found")
	}
	if cfg.Logger == nil {
		cfg.Logger = &runLogger{}
	}
	if cfg.CacheFile == "" {
		cfg.CacheFile = defaultCacheFile
	}

	var rows []resultRow
	usedCache := false
	cachedRows, found, err := readCacheIfPresent(cfg.CacheFile)
	switch {
	case err != nil:
		cfg.Logger.Printf("cache %s could not be read; running live scan: %v", cfg.CacheFile, err)
	case !found:
		cfg.Logger.Printf("cache %s not found; running live scan", cfg.CacheFile)
	default:
		filtered := filterCachedRows(cachedRows, domains, cfg.Family)
		if len(filtered) > 0 {
			cfg.Logger.Printf("cache %s loaded with %d matching rows; live scan will refresh it", cfg.CacheFile, len(filtered))
		} else {
			cfg.Logger.Printf("cache %s had no matching rows; running live scan", cfg.CacheFile)
		}
	}

	if len(rows) == 0 {
		resolvers, err := newResolverClients(cfg.ResolverSpecs)
		if err != nil {
			return nil, false, err
		} else {
			cfg.Logger.Printf(
				"live scan started: domains=%d resolvers=%d family=%s progress=%t",
				len(domains),
				len(cfg.ResolverSpecs),
				cfg.Family,
				cfg.ShowProgress,
			)

			rows = run(cfg, resolvers, domains)
			if err := writeCache(cfg.CacheFile, rows); err != nil {
				return nil, false, err
			}
			cfg.Logger.Printf("cache saved to %s", cfg.CacheFile)
		}
	}

	sortRows(rows)
	cfg.Logger.Printf("scan completed with %d result rows", len(rows))
	return rows, usedCache, nil
}
