package main

import (
	"strings"
	"testing"
)

func TestRebuildHostsContentKeepsUnmanagedTaggedLines(t *testing.T) {
	existing := strings.Join([]string{
		"127.0.0.1 localhost",
		"240e:b1:9801:40c:3::e autopatchcn.yuanshen.com #DLTOOL",
		"240e:b1:9801:40c:3::e autopatchhk.yuanshen.com #DLTOOL",
		"2408:aaaa::1 autopatchcn.bhsr.com #DLTOOL",
		"",
	}, "\n")

	entries := []string{
		"2408:bbbb::2 autopatchcn.yuanshen.com #DLTOOL",
		"2408:bbbb::2 autopatchhk.yuanshen.com #DLTOOL",
	}

	got := rebuildHostsContent(existing, entries)

	if !strings.Contains(got, "2408:aaaa::1 autopatchcn.bhsr.com #DLTOOL") {
		t.Fatalf("expected unmanaged tagged line to remain, got:\n%s", got)
	}
	if strings.Contains(got, "240e:b1:9801:40c:3::e autopatchcn.yuanshen.com #DLTOOL") {
		t.Fatalf("expected managed tagged line to be replaced, got:\n%s", got)
	}
	if !strings.Contains(got, "2408:bbbb::2 autopatchcn.yuanshen.com #DLTOOL") {
		t.Fatalf("expected replacement tagged line, got:\n%s", got)
	}
}
