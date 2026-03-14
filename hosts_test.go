package main

import (
	"os"
	"path/filepath"
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

	entries := []hostEntry{
		{Address: "2408:bbbb::2", Hostname: "autopatchcn.yuanshen.com", Tag: hostsTag},
		{Address: "2408:bbbb::2", Hostname: "autopatchhk.yuanshen.com", Tag: hostsTag},
	}

	got := rebuildHostsContent(existing, entries, hostsTag)

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

func TestRebuildHostsContentByTagDoesNotTouchDirectHosts(t *testing.T) {
	existing := strings.Join([]string{
		"127.0.0.1 localhost",
		"203.0.113.10 autopatchcn.bh3.com #DLTOOL",
		"127.0.0.1 autopatchcn.bh3.com #DLTOOL-TUNNEL",
		"",
	}, "\n")

	got := rebuildHostsContent(existing, nil, hostsTunnelTag)
	if !strings.Contains(got, "203.0.113.10 autopatchcn.bh3.com #DLTOOL") {
		t.Fatalf("expected direct hosts line to remain, got:\n%s", got)
	}
	if strings.Contains(got, "127.0.0.1 autopatchcn.bh3.com #DLTOOL-TUNNEL") {
		t.Fatalf("expected tunnel tagged line to be removed, got:\n%s", got)
	}
}

func TestEnsureOriginalHostsBackupOnlyCreatedOnce(t *testing.T) {
	tempDir := t.TempDir()
	hostsPath := filepath.Join(tempDir, "hosts")
	originalContent := []byte("127.0.0.1 localhost\n")
	if err := ensureOriginalHostsBackup(hostsPath, originalContent, 0o644); err != nil {
		t.Fatalf("ensureOriginalHostsBackup() first call error = %v", err)
	}

	updatedContent := []byte("203.0.113.10 example.test\n")
	if err := ensureOriginalHostsBackup(hostsPath, updatedContent, 0o644); err != nil {
		t.Fatalf("ensureOriginalHostsBackup() second call error = %v", err)
	}

	data, err := os.ReadFile(hostsPath + hostsOriginalBackupSuffix)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(data) != string(originalContent) {
		t.Fatalf("original backup content = %q, want %q", string(data), string(originalContent))
	}
}
