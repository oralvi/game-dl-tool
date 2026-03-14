//go:build windows

package main

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func decodeCommandOutput(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	if utf8.Valid(output) {
		return string(output)
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().String(string(output))
	if err == nil {
		return decoded
	}
	return string(output)
}

func containsTraceTimeoutText(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "timed out") || strings.Contains(line, "\u8bf7\u6c42\u8d85\u65f6")
}
