package main

import (
	"testing"
	"time"
)

func TestBuildProgressBar(t *testing.T) {
	got := buildProgressBar(2, 4, 8)
	want := "####----"
	if got != want {
		t.Fatalf("buildProgressBar = %q, want %q", got, want)
	}
}

func TestSummarizeActiveDomains(t *testing.T) {
	active := map[string]time.Time{
		"autopatchcn.yuanshen.com":       {},
		"pcdownload-aliyun.aki-game.com": {},
		"autopatchcn.bhsr.com":           {},
	}

	got := summarizeActiveDomains(active, 2)
	want := "autopatchcn.bhsr.com, autopatchcn.yuanshen.com, +1 more"
	if got != want {
		t.Fatalf("summarizeActiveDomains = %q, want %q", got, want)
	}
}
