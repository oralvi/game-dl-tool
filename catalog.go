package main

import (
	"fmt"
	"sort"
	"strings"
)

type gameTarget struct {
	ID                string
	Key               string
	Name              string
	Enabled           bool
	Domains           []string
	Aliases           map[string][]string
	Groups            []gameDomainGroup
	PreferredProvider string
	ProviderOptions   []string
}

type gameDomainGroup struct {
	Name    string
	Mode    string
	Domains []gameDomain
}

type gameDomain struct {
	Host     string
	Provider string
	Aliases  []string
}

var builtInAliases = map[string][]string{
	"autopatchcn.yuanshen.com": {
		"autopatchcn.yuanshen.com",
		"autopatchhk.yuanshen.com",
		"genshinimpact.mihoyo.com",
	},
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

func defaultGameCatalog() []gameTarget {
	return cloneGameCatalog([]gameTarget{
		{
			ID:      "1",
			Key:     "genshin",
			Name:    "Genshin Impact",
			Enabled: true,
			Domains: []string{"autopatchcn.yuanshen.com"},
		},
		{
			ID:      "2",
			Key:     "hsr",
			Name:    "Honkai: Star Rail",
			Enabled: true,
			Domains: []string{"autopatchcn.bhsr.com"},
		},
		{
			ID:      "3",
			Key:     "bh3",
			Name:    "Honkai Impact 3rd",
			Enabled: true,
			Domains: []string{"autopatchcn.bh3.com"},
		},
		{
			ID:      "4",
			Key:     "zzz",
			Name:    "Zenless Zone Zero",
			Enabled: true,
			Domains: []string{"autopatchcn.juequling.com"},
		},
		{
			ID:      "5",
			Key:     "wuwa-cn",
			Name:    "Wuthering Waves CN",
			Enabled: true,
			Groups: []gameDomainGroup{
				{
					Name: "Download",
					Mode: "manage",
					Domains: []gameDomain{
						{Host: "pcdownload-aliyun.aki-game.com", Provider: "aliyun"},
						{Host: "pcdownload-huoshan.aki-game.com", Provider: "huoshan"},
						{Host: "pcdownload-qcloud.aki-game.com", Provider: "qcloud"},
						{Host: "cdn-aliyun-cn-mc.aki-game.com", Provider: "aliyun"},
						{Host: "cdn-huoshan-cn-mc.aki-game.com", Provider: "huoshan"},
						{Host: "cdn-qcloud-cn-mc.aki-game.com", Provider: "qcloud"},
					},
				},
			},
			PreferredProvider: "auto",
		},
		{
			ID:      "6",
			Key:     "gf2-cn",
			Name:    "Girls' Frontline 2: Exilium CN",
			Enabled: true,
			Domains: []string{"gf2-cn.cdn.sunborngame.com"},
		},
	})
}

func defaultFileGames() []fileGame {
	catalog := defaultGameCatalog()
	games := make([]fileGame, 0, len(catalog))
	for _, game := range catalog {
		enabled := game.Enabled
		games = append(games, fileGame{
			ID:                game.ID,
			Key:               game.Key,
			Name:              game.Name,
			Enabled:           &enabled,
			Domains:           append([]string(nil), game.Domains...),
			Aliases:           cloneAliases(game.Aliases),
			Groups:            cloneFileGroups(game.Groups),
			PreferredProvider: normalizeProviderPreference(game.PreferredProvider),
		})
	}
	return games
}

func gameCatalogFromFileConfig(fileCfg fileConfig) []gameTarget {
	switch {
	case len(fileCfg.Games) > 0:
		return gameCatalogFromFileGames(fileCfg.Games)
	case len(fileCfg.LegacyDomains) > 0:
		return legacyCatalogFromDomains(fileCfg.LegacyDomains, fileCfg.LegacyGameSelection)
	default:
		return defaultGameCatalog()
	}
}

func gameCatalogFromFileGames(fileGames []fileGame) []gameTarget {
	games := make([]gameTarget, 0, len(fileGames))
	for index, item := range fileGames {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}

		groups := gameGroupsFromFileGame(item)
		domains, aliases := managedDomainsFromGroups(groups, item.PreferredProvider)
		if len(domains) == 0 {
			continue
		}

		key := normalizeGameKey(item.Key)
		if key == "" {
			key = normalizeGameKey(name)
		}
		if key == "" {
			key = fmt.Sprintf("game-%d", index+1)
		}

		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = fmt.Sprintf("%d", index+1)
		}

		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}

		games = append(games, gameTarget{
			ID:                id,
			Key:               key,
			Name:              name,
			Enabled:           enabled,
			Domains:           domains,
			Aliases:           aliases,
			Groups:            groups,
			PreferredProvider: normalizeProviderPreference(item.PreferredProvider),
			ProviderOptions:   providerOptionsFromGroups(groups),
		})
	}

	if len(games) == 0 {
		return defaultGameCatalog()
	}
	return cloneGameCatalog(games)
}

func legacyCatalogFromDomains(domains []string, selection string) []gameTarget {
	base := defaultGameCatalog()
	domainSet := make(map[string]struct{})
	for _, domain := range normalizeDomains(domains) {
		domainSet[domain] = struct{}{}
	}
	if len(domainSet) == 0 {
		return defaultGameCatalog()
	}

	var games []gameTarget
	usedDomains := make(map[string]struct{})
	for _, game := range base {
		var matched []string
		for _, domain := range game.Domains {
			if _, ok := domainSet[domain]; ok {
				matched = append(matched, domain)
				usedDomains[domain] = struct{}{}
			}
		}
		if len(matched) == 0 {
			continue
		}
		game.Domains = matched
		game.Aliases = make(map[string][]string, len(matched))
		for _, domain := range matched {
			game.Aliases[domain] = aliasesForDomain(domain, nil)
		}
		games = append(games, game)
	}

	var customDomains []string
	for domain := range domainSet {
		if _, ok := usedDomains[domain]; ok {
			continue
		}
		customDomains = append(customDomains, domain)
	}
	sort.Strings(customDomains)
	if len(customDomains) > 0 {
		customAliases := make(map[string][]string, len(customDomains))
		for _, domain := range customDomains {
			customAliases[domain] = aliasesForDomain(domain, nil)
		}
		games = append(games, gameTarget{
			ID:      fmt.Sprintf("%d", len(games)+1),
			Key:     "custom",
			Name:    "Custom Targets",
			Enabled: true,
			Domains: customDomains,
			Aliases: customAliases,
		})
	}

	applyLegacySelection(games, selection)
	if len(games) == 0 {
		return defaultGameCatalog()
	}
	return cloneGameCatalog(games)
}

func applyLegacySelection(games []gameTarget, selection string) {
	if strings.TrimSpace(selection) == "" {
		for i := range games {
			games[i].Enabled = true
		}
		return
	}

	selected := make(map[string]struct{})
	for _, token := range splitSelectionTokens(selection) {
		if game, ok := lookupGame(games, token); ok {
			selected[game.ID] = struct{}{}
		}
	}
	if len(selected) == 0 {
		for i := range games {
			games[i].Enabled = true
		}
		return
	}
	for i := range games {
		_, games[i].Enabled = selected[games[i].ID]
	}
}

func targetsFromGames(games []gameTarget) ([]string, map[string][]string, error) {
	seenDomains := make(map[string]struct{})
	aliases := make(map[string][]string)
	var domains []string

	for _, game := range games {
		for _, domain := range normalizeDomains(game.Domains) {
			if _, ok := seenDomains[domain]; !ok {
				seenDomains[domain] = struct{}{}
				domains = append(domains, domain)
			}
			aliases[domain] = aliasesForDomain(domain, game.Aliases[domain])
		}
	}

	sort.Strings(domains)
	if len(domains) == 0 {
		return nil, nil, fmt.Errorf("selected games have no valid domains")
	}
	return domains, aliases, nil
}

func lookupGame(games []gameTarget, token string) (gameTarget, bool) {
	token = strings.ToLower(strings.TrimSpace(token))
	for _, game := range games {
		if token == strings.ToLower(game.ID) || token == strings.ToLower(game.Key) {
			return game, true
		}
	}
	return gameTarget{}, false
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

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{})
	var normalized []string
	for _, raw := range domains {
		domain := normalizeDomain(raw)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		normalized = append(normalized, domain)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeProviderPreference(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	switch value {
	case "", "auto":
		return "auto"
	default:
		return value
	}
}

func normalizeGroupMode(input string) string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "observe", "observer", "watch":
		return "observe"
	default:
		return "manage"
	}
}

func gameGroupsFromFileGame(item fileGame) []gameDomainGroup {
	if len(item.Groups) == 0 {
		if len(item.Domains) == 0 {
			return nil
		}
		group := gameDomainGroup{
			Name: "Default",
			Mode: "manage",
		}
		for _, host := range normalizeDomains(item.Domains) {
			group.Domains = append(group.Domains, gameDomain{
				Host:    host,
				Aliases: aliasesForDomain(host, item.Aliases[host]),
			})
		}
		return []gameDomainGroup{group}
	}

	groups := make([]gameDomainGroup, 0, len(item.Groups))
	for index, rawGroup := range item.Groups {
		group := gameDomainGroup{
			Name: strings.TrimSpace(rawGroup.Name),
			Mode: normalizeGroupMode(rawGroup.Mode),
		}
		if group.Name == "" {
			group.Name = fmt.Sprintf("Group %d", index+1)
		}
		seen := make(map[string]struct{})
		for _, rawDomain := range rawGroup.Domains {
			host := normalizeDomain(rawDomain.Host)
			if host == "" {
				continue
			}
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			group.Domains = append(group.Domains, gameDomain{
				Host:     host,
				Provider: normalizeProviderPreference(rawDomain.Provider),
				Aliases:  aliasesForDomain(host, firstNonEmptySlice(rawDomain.Aliases, item.Aliases[host])),
			})
		}
		if len(group.Domains) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func managedDomainsFromGroups(groups []gameDomainGroup, preferredProvider string) ([]string, map[string][]string) {
	preferred := normalizeProviderPreference(preferredProvider)
	aliases := make(map[string][]string)
	var domains []string
	seen := make(map[string]struct{})

	for _, group := range groups {
		if normalizeGroupMode(group.Mode) != "manage" {
			continue
		}
		for _, domain := range group.Domains {
			if preferred != "auto" && domain.Provider != "" && domain.Provider != preferred {
				continue
			}
			if _, ok := seen[domain.Host]; ok {
				continue
			}
			seen[domain.Host] = struct{}{}
			domains = append(domains, domain.Host)
			aliases[domain.Host] = aliasesForDomain(domain.Host, domain.Aliases)
		}
	}

	sort.Strings(domains)
	return domains, aliases
}

func providerOptionsFromGroups(groups []gameDomainGroup) []string {
	seen := make(map[string]struct{})
	var providers []string
	for _, group := range groups {
		if normalizeGroupMode(group.Mode) != "manage" {
			continue
		}
		for _, domain := range group.Domains {
			if domain.Provider == "" || domain.Provider == "auto" {
				continue
			}
			if _, ok := seen[domain.Provider]; ok {
				continue
			}
			seen[domain.Provider] = struct{}{}
			providers = append(providers, domain.Provider)
		}
	}
	sort.Strings(providers)
	return providers
}

func gameHasManagedDomain(game gameTarget, hostname string) bool {
	hostname = normalizeDomain(hostname)
	if hostname == "" {
		return false
	}
	if len(game.Groups) == 0 {
		return containsFold(game.Domains, hostname)
	}
	for _, group := range game.Groups {
		if normalizeGroupMode(group.Mode) != "manage" {
			continue
		}
		for _, domain := range group.Domains {
			if strings.EqualFold(domain.Host, hostname) {
				return true
			}
		}
	}
	return false
}

func allManagedHostnames(game gameTarget) []string {
	seen := make(map[string]struct{})
	var hostnames []string
	if len(game.Groups) == 0 {
		for _, host := range game.Domains {
			for _, alias := range aliasesForDomain(host, game.Aliases[host]) {
				key := strings.ToLower(alias)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				hostnames = append(hostnames, alias)
			}
		}
		sort.Strings(hostnames)
		return hostnames
	}
	for _, group := range game.Groups {
		if normalizeGroupMode(group.Mode) != "manage" {
			continue
		}
		for _, domain := range group.Domains {
			for _, alias := range aliasesForDomain(domain.Host, domain.Aliases) {
				key := strings.ToLower(alias)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				hostnames = append(hostnames, alias)
			}
		}
	}
	sort.Strings(hostnames)
	return hostnames
}

func blockedManagedHostnames(game gameTarget) []string {
	preferred := normalizeProviderPreference(game.PreferredProvider)
	if preferred == "" || preferred == "auto" {
		return nil
	}

	seen := make(map[string]struct{})
	var hostnames []string
	for _, group := range game.Groups {
		if normalizeGroupMode(group.Mode) != "manage" {
			continue
		}
		for _, domain := range group.Domains {
			if domain.Provider == "" || normalizeProviderPreference(domain.Provider) == preferred {
				continue
			}
			for _, alias := range aliasesForDomain(domain.Host, domain.Aliases) {
				key := strings.ToLower(alias)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				hostnames = append(hostnames, alias)
			}
		}
	}
	sort.Strings(hostnames)
	return hostnames
}

func aliasesForDomain(domain string, configured []string) []string {
	values := configured
	if len(values) == 0 {
		values = builtInAliases[domain]
	}
	if len(values) == 0 {
		values = []string{domain}
	}
	if !containsFold(values, domain) {
		values = append([]string{domain}, values...)
	}
	return normalizeDomains(values)
}

func normalizeGameKey(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func cloneGameCatalog(games []gameTarget) []gameTarget {
	cloned := make([]gameTarget, 0, len(games))
	for _, game := range games {
		cloned = append(cloned, gameTarget{
			ID:                game.ID,
			Key:               game.Key,
			Name:              game.Name,
			Enabled:           game.Enabled,
			Domains:           append([]string(nil), game.Domains...),
			Aliases:           cloneAliases(game.Aliases),
			Groups:            cloneGameGroups(game.Groups),
			PreferredProvider: game.PreferredProvider,
			ProviderOptions:   append([]string(nil), game.ProviderOptions...),
		})
	}
	return cloned
}

func cloneAliases(aliases map[string][]string) map[string][]string {
	if len(aliases) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(aliases))
	for domain, values := range aliases {
		cloned[domain] = append([]string(nil), values...)
	}
	return cloned
}

func cloneGameGroups(groups []gameDomainGroup) []gameDomainGroup {
	if len(groups) == 0 {
		return nil
	}
	cloned := make([]gameDomainGroup, 0, len(groups))
	for _, group := range groups {
		next := gameDomainGroup{
			Name: group.Name,
			Mode: group.Mode,
		}
		for _, domain := range group.Domains {
			next.Domains = append(next.Domains, gameDomain{
				Host:     domain.Host,
				Provider: domain.Provider,
				Aliases:  append([]string(nil), domain.Aliases...),
			})
		}
		cloned = append(cloned, next)
	}
	return cloned
}

func cloneFileGroups(groups []gameDomainGroup) []fileGameGroup {
	if len(groups) == 0 {
		return nil
	}
	cloned := make([]fileGameGroup, 0, len(groups))
	for _, group := range groups {
		next := fileGameGroup{
			Name: group.Name,
			Mode: group.Mode,
		}
		for _, domain := range group.Domains {
			next.Domains = append(next.Domains, fileGameDomain{
				Host:     domain.Host,
				Provider: domain.Provider,
				Aliases:  append([]string(nil), domain.Aliases...),
			})
		}
		cloned = append(cloned, next)
	}
	return cloned
}

func firstNonEmptySlice(primary, fallback []string) []string {
	if len(primary) > 0 {
		return primary
	}
	return fallback
}
