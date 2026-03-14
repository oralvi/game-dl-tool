package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileConfigUnmarshalLegacyGamesString(t *testing.T) {
	data := []byte(`{
  "domains": ["autopatchcn.yuanshen.com", "autopatchcn.bhsr.com"],
  "games": "12",
  "family": "6",
  "trace": false,
  "use_cache": true
}`)

	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if cfg.LegacyGameSelection != "12" {
		t.Fatalf("LegacyGameSelection = %q, want %q", cfg.LegacyGameSelection, "12")
	}
	if len(cfg.LegacyDomains) != 2 {
		t.Fatalf("LegacyDomains length = %d, want 2", len(cfg.LegacyDomains))
	}
}

func TestGameCatalogFromFileConfigNewGamesArray(t *testing.T) {
	enabledTrue := true
	enabledFalse := false
	fileCfg := fileConfig{
		Games: []fileGame{
			{
				Name:    "Alpha",
				Key:     "alpha",
				Enabled: &enabledTrue,
				Domains: []string{"a.example.com", "b.example.com"},
			},
			{
				Name:    "Beta",
				Key:     "beta",
				Enabled: &enabledFalse,
				Domains: []string{"c.example.com"},
			},
		},
	}

	catalog := gameCatalogFromFileConfig(fileCfg)
	if len(catalog) != 2 {
		t.Fatalf("catalog length = %d, want 2", len(catalog))
	}
	if catalog[0].Name != "Alpha" || !catalog[0].Enabled {
		t.Fatalf("catalog[0] = %+v, want enabled Alpha", catalog[0])
	}
	if len(catalog[0].Domains) != 2 {
		t.Fatalf("catalog[0].Domains length = %d, want 2", len(catalog[0].Domains))
	}
	if catalog[1].Name != "Beta" || catalog[1].Enabled {
		t.Fatalf("catalog[1] = %+v, want disabled Beta", catalog[1])
	}
}

func TestApplyFileConfigResolvers(t *testing.T) {
	cfg := defaultRuntimeConfig()
	fileCfg := fileConfig{
		Resolvers: []string{"223.5.5.5", "1.1.1.1"},
	}

	if err := applyFileConfig(&cfg, fileCfg); err != nil {
		t.Fatalf("applyFileConfig() error = %v", err)
	}
	expected := effectiveResolverValues(fileCfg.Resolvers)
	if len(cfg.ResolverSpecs) != len(expected) {
		t.Fatalf("len(cfg.ResolverSpecs) = %d, want %d", len(cfg.ResolverSpecs), len(expected))
	}
	for index, resolver := range expected {
		if cfg.ResolverSpecs[index].Label != resolver {
			t.Fatalf("cfg.ResolverSpecs[%d].Label = %q, want %q", index, cfg.ResolverSpecs[index].Label, resolver)
		}
	}
}

func TestGameCatalogFromFileConfigPreferredProvider(t *testing.T) {
	enabledTrue := true
	fileCfg := fileConfig{
		Games: []fileGame{
			{
				Name:              "Wuthering Waves CN",
				Key:               "wuwa-cn",
				Enabled:           &enabledTrue,
				PreferredProvider: "aliyun",
				Groups: []fileGameGroup{
					{
						Name: "Download",
						Mode: "manage",
						Domains: []fileGameDomain{
							{Host: "cdn-aliyun-cn-mc.aki-game.com", Provider: "aliyun"},
							{Host: "cdn-huoshan-cn-mc.aki-game.com", Provider: "huoshan"},
							{Host: "cdn-qcloud-cn-mc.aki-game.com", Provider: "qcloud"},
						},
					},
				},
			},
		},
	}

	catalog := gameCatalogFromFileConfig(fileCfg)
	if len(catalog) != 1 {
		t.Fatalf("catalog length = %d, want 1", len(catalog))
	}
	if got, want := catalog[0].PreferredProvider, "aliyun"; got != want {
		t.Fatalf("PreferredProvider = %q, want %q", got, want)
	}
	if len(catalog[0].Domains) != 1 || catalog[0].Domains[0] != "cdn-aliyun-cn-mc.aki-game.com" {
		t.Fatalf("catalog[0].Domains = %#v, want only aliyun domain", catalog[0].Domains)
	}
	if len(catalog[0].ProviderOptions) != 3 {
		t.Fatalf("ProviderOptions = %#v, want 3 providers", catalog[0].ProviderOptions)
	}
}

func TestLoadFileConfigInitializesGamesConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	cfg, found, created, err := loadFileConfig(configPath)
	if err != nil {
		t.Fatalf("loadFileConfig() error = %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if !created {
		t.Fatalf("created = false, want true")
	}
	if len(cfg.Games) == 0 {
		t.Fatalf("cfg.Games length = 0, want > 0")
	}
	if cfg.TunnelPort != 0 {
		t.Fatalf("cfg.TunnelPort = %d, want 0", cfg.TunnelPort)
	}
	if cfg.GeoIP.PrimaryProvider != "ipwho.is" {
		t.Fatalf("cfg.GeoIP.PrimaryProvider = %q, want %q", cfg.GeoIP.PrimaryProvider, "ipwho.is")
	}
	foundGF2 := false
	for _, game := range cfg.Games {
		if game.Key == "gf2-cn" {
			foundGF2 = true
			if len(game.Domains) != 1 || game.Domains[0] != "gf2-cn.cdn.sunborngame.com" {
				t.Fatalf("gf2 game domains = %#v, want gf2-cn.cdn.sunborngame.com", game.Domains)
			}
		}
	}
	if !foundGF2 {
		t.Fatalf("default config missing gf2-cn target")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("written config is not valid JSON")
	}
	if string(data) == "" {
		t.Fatalf("written config is empty")
	}
}
