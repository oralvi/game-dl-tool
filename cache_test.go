package main

import (
	"path/filepath"
	"testing"
)

func TestReadCacheIfPresentMissingFile(t *testing.T) {
	rows, found, err := readCacheIfPresent(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("readCacheIfPresent() error = %v, want nil", err)
	}
	if found {
		t.Fatalf("found = true, want false")
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestReadCacheIfPresentRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan_cache.json")
	input := []resultRow{{
		Domain:    "example.com",
		Family:    family6,
		Address:   "2001:db8::1",
		ConnectOK: true,
	}}

	if err := writeCache(path, input); err != nil {
		t.Fatalf("writeCache() error = %v", err)
	}

	rows, found, err := readCacheIfPresent(path)
	if err != nil {
		t.Fatalf("readCacheIfPresent() error = %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Domain != input[0].Domain || rows[0].Address != input[0].Address {
		t.Fatalf("rows[0] = %+v, want domain=%q address=%q", rows[0], input[0].Domain, input[0].Address)
	}
}
