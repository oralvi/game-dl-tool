package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func defaultRuntimeConfig() config {
	return config{
		ResolverSpecs:  append([]resolverSpec(nil), defaultResolverSpecs...),
		ConfigPath:     "config.json",
		Family:         family6,
		CacheFile:      defaultCacheFile,
		LogFile:        "cache/latest_scan.log",
		Port:           443,
		Rounds:         3,
		RoundGap:       300 * time.Millisecond,
		ResolveTimeout: 5 * time.Second,
		ConnectTimeout: 3 * time.Second,
		TraceEnabled:   false,
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
	if cfg.UseCache {
		cachedRows, err := readCache(cfg.CacheFile)
		if err != nil {
			return nil, false, err
		}
		rows = filterCachedRows(cachedRows, domains, cfg.Family)
		if len(rows) > 0 {
			cfg.Logger.Printf("using cached results from %s", cfg.CacheFile)
			usedCache = true
		} else {
			cfg.Logger.Printf("cache %s had no matching rows; falling back to live scan", cfg.CacheFile)
		}
	}

	if len(rows) == 0 {
		resolvers, err := newResolverClients(cfg.ResolverSpecs)
		if err != nil {
			return nil, false, err
		}

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
			return nil, false, err
		}
		cfg.Logger.Printf("cache saved to %s", cfg.CacheFile)
	}

	sortRows(rows)
	cfg.Logger.Printf("scan completed with %d result rows", len(rows))
	return rows, usedCache, nil
}

func selectedGameSummary(selection string) string {
	if strings.TrimSpace(selection) == "" {
		return "all"
	}
	return selection
}

func fastestSuccessfulRow(rows []resultRow, family ipFamily) (resultRow, bool) {
	var best resultRow
	found := false
	for _, row := range rows {
		if row.Family != family || !row.ConnectOK || row.ConnectLatency <= 0 {
			continue
		}
		if !found || betterRow(row, best) {
			best = row
			found = true
		}
	}
	return best, found
}

func selectedGamesFromConfig(fileCfg fileConfig) map[string]bool {
	selected := make(map[string]bool, len(knownGames))
	if strings.TrimSpace(fileCfg.Games) == "" {
		for _, game := range knownGames {
			selected[game.ID] = true
		}
		return selected
	}

	for _, token := range splitSelectionTokens(fileCfg.Games) {
		if game, ok := lookupGame(token); ok {
			selected[game.ID] = true
		}
	}
	if len(selected) == 0 {
		for _, game := range knownGames {
			selected[game.ID] = true
		}
	}
	return selected
}

func allGameIDs() []string {
	ids := make([]string, 0, len(knownGames))
	for _, game := range knownGames {
		ids = append(ids, game.ID)
	}
	sort.Strings(ids)
	return ids
}
