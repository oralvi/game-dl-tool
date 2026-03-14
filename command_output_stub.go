//go:build !windows

package main

import "strings"

func decodeCommandOutput(output []byte) string {
	return string(output)
}

func containsTraceTimeoutText(line string) bool {
	return strings.Contains(strings.ToLower(line), "timed out")
}
