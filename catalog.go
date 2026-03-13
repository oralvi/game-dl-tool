package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type gameTarget struct {
	ID      string
	Key     string
	Name    string
	Domains []string
	Aliases map[string][]string
}

var knownGames = []gameTarget{
	{
		ID:      "1",
		Key:     "genshin",
		Name:    "原神",
		Domains: []string{"autopatchcn.yuanshen.com"},
		Aliases: map[string][]string{
			"autopatchcn.yuanshen.com": {
				"autopatchcn.yuanshen.com",
				"autopatchhk.yuanshen.com",
				"genshinimpact.mihoyo.com",
			},
		},
	},
	{
		ID:      "2",
		Key:     "hsr",
		Name:    "星铁",
		Domains: []string{"autopatchcn.bhsr.com"},
		Aliases: map[string][]string{
			"autopatchcn.bhsr.com": {"autopatchcn.bhsr.com"},
		},
	},
	{
		ID:      "3",
		Key:     "bh3",
		Name:    "崩坏3",
		Domains: []string{"autopatchcn.bh3.com"},
		Aliases: map[string][]string{
			"autopatchcn.bh3.com": {"autopatchcn.bh3.com"},
		},
	},
	{
		ID:      "4",
		Key:     "zzz",
		Name:    "绝区零",
		Domains: []string{"autopatchcn.juequling.com"},
		Aliases: map[string][]string{
			"autopatchcn.juequling.com": {"autopatchcn.juequling.com"},
		},
	},
	{
		ID:   "5",
		Key:  "wuwa-cn",
		Name: "鸣潮国服",
		Domains: []string{
			"prod-cn-alicdn-gamestarter.kurogame.com",
			"pcdownload-aliyun.aki-game.com",
			"pcdownload-huoshan.aki-game.com",
			"pcdownload-qcloud.aki-game.com",
		},
		Aliases: map[string][]string{
			"prod-cn-alicdn-gamestarter.kurogame.com": {"prod-cn-alicdn-gamestarter.kurogame.com"},
			"pcdownload-aliyun.aki-game.com":          {"pcdownload-aliyun.aki-game.com"},
			"pcdownload-huoshan.aki-game.com":         {"pcdownload-huoshan.aki-game.com"},
			"pcdownload-qcloud.aki-game.com":          {"pcdownload-qcloud.aki-game.com"},
		},
	},
}

func resolveDomainsAndAliases(cfg *config, inputFile string, cliDomains []string, reader *bufio.Reader) ([]string, map[string][]string, error) {
	if inputFile != "" || len(cliDomains) > 0 {
		domains, err := loadDomains(inputFile, cliDomains)
		if err != nil {
			return nil, nil, err
		}
		return domains, identityAliases(domains), nil
	}

	if len(cfg.ConfigDomains) > 0 {
		return cfg.ConfigDomains, identityAliases(cfg.ConfigDomains), nil
	}

	if cfg.Interactive && !cfg.GamesWasSet {
		selection, err := promptGames(reader)
		if err != nil {
			return nil, nil, err
		}
		cfg.GamesSelection = selection
	}

	if cfg.GamesSelection == "" {
		cfg.GamesSelection = "all"
	}

	domains, aliases, err := targetsFromGameSelection(cfg.GamesSelection)
	if err != nil {
		return nil, nil, err
	}

	if cfg.Interactive && !cfg.GamesWasSet && !cfg.FamilyWasSet {
		family, err := promptFamily(reader, cfg.Family)
		if err != nil {
			return nil, nil, err
		}
		cfg.Family = family
	}

	if cfg.Interactive && !cfg.GamesWasSet && !cfg.TraceWasSet {
		cfg.TraceEnabled = askYesNo(reader, "Enable trace hop probing? [y/N]: ", cfg.TraceEnabled)
	}

	if cfg.Interactive && !cfg.GamesWasSet && !cfg.UseCacheWasSet && fileExists(cfg.CacheFile) {
		cfg.UseCache = askYesNo(reader, fmt.Sprintf("Use cached results from %s? [y/N]: ", cfg.CacheFile), false)
	}

	return domains, aliases, nil
}

func promptGames(reader *bufio.Reader) (string, error) {
	fmt.Println("Available games:")
	for _, game := range knownGames {
		fmt.Printf("  %s. %s\n", game.ID, game.Name)
	}
	return prompt(reader, "Choose games (e.g. 12, 135, or Enter for all): ", "")
}

func promptFamily(reader *bufio.Reader, fallback ipFamily) (ipFamily, error) {
	answer, err := prompt(reader, fmt.Sprintf("Choose family [4/6/all] (default %s): ", fallback), string(fallback))
	if err != nil {
		return fallback, err
	}
	return parseFamily(answer)
}

func prompt(reader *bufio.Reader, label string, fallback string) (string, error) {
	fmt.Print(label)
	text, err := reader.ReadString('\n')
	if err != nil && !errorsIsEOF(err) {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback, nil
	}
	return text, nil
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}

func askYesNo(reader *bufio.Reader, label string, fallback bool) bool {
	answer, err := prompt(reader, label, "")
	if err != nil {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "1":
		return true
	case "n", "no", "0":
		return false
	case "":
		return fallback
	default:
		return fallback
	}
}

func parseFamily(input string) (ipFamily, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "6", "ipv6":
		return family6, nil
	case "4", "ipv4":
		return family4, nil
	case "all", "a":
		return familyAll, nil
	default:
		return "", fmt.Errorf("invalid family %q: use 4, 6, or all", input)
	}
}

func isInteractiveTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func targetsFromGameSelection(selection string) ([]string, map[string][]string, error) {
	if strings.TrimSpace(selection) == "" || strings.EqualFold(strings.TrimSpace(selection), "all") {
		return allKnownDomainsAndAliases()
	}

	selected := make(map[string]gameTarget)
	for _, token := range splitSelectionTokens(selection) {
		game, ok := lookupGame(token)
		if !ok {
			return nil, nil, fmt.Errorf("unknown game selection %q", token)
		}
		selected[game.ID] = game
	}

	if len(selected) == 0 {
		return allKnownDomainsAndAliases()
	}

	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var domains []string
	aliases := make(map[string][]string)
	for _, id := range ids {
		for _, domain := range selected[id].Domains {
			domains = append(domains, domain)
			aliases[domain] = append([]string(nil), selected[id].Aliases[domain]...)
		}
	}
	return domains, aliases, nil
}

func allKnownDomainsAndAliases() ([]string, map[string][]string, error) {
	var domains []string
	aliases := make(map[string][]string)
	for _, game := range knownGames {
		domains = append(domains, game.Domains...)
		for domain, items := range game.Aliases {
			aliases[domain] = append([]string(nil), items...)
		}
	}
	sort.Strings(domains)
	return domains, aliases, nil
}

func identityAliases(domains []string) map[string][]string {
	aliases := make(map[string][]string, len(domains))
	for _, domain := range domains {
		aliases[domain] = []string{domain}
	}
	return aliases
}

func splitSelectionTokens(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	if !strings.ContainsAny(input, ",; \t") && allDigits(input) {
		tokens := make([]string, 0, len(input))
		for _, r := range input {
			tokens = append(tokens, string(r))
		}
		return tokens
	}
	return strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
}

func allDigits(input string) bool {
	for _, r := range input {
		if r < '0' || r > '9' {
			return false
		}
	}
	return input != ""
}

func lookupGame(token string) (gameTarget, bool) {
	token = strings.ToLower(strings.TrimSpace(token))
	for _, game := range knownGames {
		if token == strings.ToLower(game.ID) || token == strings.ToLower(game.Key) {
			return game, true
		}
	}
	return gameTarget{}, false
}
