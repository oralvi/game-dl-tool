package main

import "testing"

func TestParseTraceHopsRecognizesChineseTimeout(t *testing.T) {
	output := "  1     3 ms     4 ms     3 ms  2001:da8:9000:a436::1\r\n  2     *        *        *     \u8bf7\u6c42\u8d85\u65f6\r\n"

	hops := parseTraceHops(output)
	if len(hops) != 2 {
		t.Fatalf("len(hops) = %d, want 2", len(hops))
	}
	if hops[1].Status != "timeout" {
		t.Fatalf("hops[1].Status = %q, want timeout", hops[1].Status)
	}
}
