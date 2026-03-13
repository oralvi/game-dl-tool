package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fileConfig struct {
	Domains  []string `json:"domains"`
	Games    string   `json:"games"`
	Family   string   `json:"family"`
	Trace    *bool    `json:"trace"`
	UseCache *bool    `json:"use_cache"`
	HostsOut string   `json:"hosts_out"`
}

func defaultFileConfig() fileConfig {
	traceDisabled := false
	useCacheDisabled := false
	return fileConfig{
		Domains: []string{
			"autopatchcn.bh3.com",
			"autopatchcn.bhsr.com",
			"autopatchcn.yuanshen.com",
			"autopatchcn.juequling.com",
			"prod-cn-alicdn-gamestarter.kurogame.com",
			"pcdownload-aliyun.aki-game.com",
			"pcdownload-huoshan.aki-game.com",
			"pcdownload-qcloud.aki-game.com",
		},
		Family:   "6",
		Trace:    &traceDisabled,
		UseCache: &useCacheDisabled,
	}
}

func loadFileConfig(path string, required bool) (fileConfig, bool, bool, error) {
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

func applyFileConfig(cfg *config, fileCfg fileConfig, allowDomains bool) error {
	if !cfg.GamesWasSet && strings.TrimSpace(cfg.GamesSelection) == "" && strings.TrimSpace(fileCfg.Games) != "" {
		cfg.GamesSelection = strings.TrimSpace(fileCfg.Games)
	}

	if !cfg.FamilyWasSet && strings.TrimSpace(fileCfg.Family) != "" {
		family, err := parseFamily(fileCfg.Family)
		if err != nil {
			return fmt.Errorf("config family: %w", err)
		}
		cfg.Family = family
	}

	if !cfg.TraceWasSet && fileCfg.Trace != nil {
		cfg.TraceEnabled = *fileCfg.Trace
	}

	if !cfg.UseCacheWasSet && fileCfg.UseCache != nil {
		cfg.UseCache = *fileCfg.UseCache
	}

	if !cfg.HostsOutWasSet && strings.TrimSpace(fileCfg.HostsOut) != "" {
		cfg.HostsOut = strings.TrimSpace(fileCfg.HostsOut)
	}

	if allowDomains && len(fileCfg.Domains) > 0 {
		domains, err := loadDomains("", fileCfg.Domains)
		if err != nil {
			return fmt.Errorf("config domains: %w", err)
		}
		cfg.ConfigDomains = domains
	}

	return nil
}
