package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

func promptHostRows(reader *bufio.Reader, rows []resultRow) ([]resultRow, bool, error) {
	candidates := hostCandidates(rows)
	if len(candidates) == 0 {
		fmt.Println("\nNo address candidates are available for hosts updates.")
		return nil, false, nil
	}

	fmt.Println("\nHost write candidates:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tDOMAIN\tFAMILY\tADDRESS\tCNAME\tTCP_MS\tTCP_OK\tRESOLVERS")
	for i, row := range candidates {
		fmt.Fprintf(
			tw,
			"%d\t%s\t%s\t%s\t%s\t%s\t%t\t%s\n",
			i+1,
			row.Domain,
			valueOrDash(string(row.Family)),
			row.Address,
			valueOrDash(row.CNAME),
			formatOptionalMillis(row.ConnectLatency, row.ConnectOK || row.ConnectLatency > 0),
			row.ConnectOK,
			valueOrDash(row.ResolverList),
		)
	}
	_ = tw.Flush()

	for {
		answer, err := prompt(reader, "\nChoose candidate IDs to append into system hosts (comma or space separated, Enter to skip): ", "")
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(answer) == "" {
			return nil, false, nil
		}

		indexes, err := parseIndexSelection(answer, len(candidates))
		if err != nil {
			fmt.Printf("Invalid selection: %v\n", err)
			continue
		}

		selected := make([]resultRow, 0, len(indexes))
		for _, index := range indexes {
			selected = append(selected, candidates[index-1])
		}
		return selected, true, nil
	}
}

func hostCandidates(rows []resultRow) []resultRow {
	candidates := make([]resultRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Address) == "" {
			continue
		}
		candidates = append(candidates, row)
	}
	sortRows(candidates)
	return candidates
}

func parseIndexSelection(input string, max int) ([]int, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	var tokens []string
	if max < 10 && !strings.ContainsAny(input, ",; \t") && allDigits(input) {
		for _, r := range input {
			tokens = append(tokens, string(r))
		}
	} else {
		tokens = strings.FieldsFunc(input, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t'
		})
	}

	if len(tokens) == 0 {
		return nil, fmt.Errorf("no candidate IDs found")
	}

	seen := make(map[int]struct{}, len(tokens))
	var indexes []int
	for _, token := range tokens {
		value, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", token)
		}
		if value < 1 || value > max {
			return nil, fmt.Errorf("%d is outside 1-%d", value, max)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		indexes = append(indexes, value)
	}

	sort.Ints(indexes)
	return indexes, nil
}
