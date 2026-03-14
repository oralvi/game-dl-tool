package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var spinnerFrames = []rune{'|', '/', '-', '\\'}

type progressSnapshot struct {
	Total         int
	Completed     int
	ActiveCount   int
	ActiveSummary string
	LastCompleted string
	Elapsed       time.Duration
	Final         bool
}

type progressTracker struct {
	total    int
	started  time.Time
	stopCh   chan struct{}
	doneCh   chan struct{}
	onUpdate func(progressSnapshot)

	mu            sync.Mutex
	completed     int
	activeDomains map[string]time.Time
	lastCompleted string
	spinnerIndex  int
}

func newProgressTracker(total int, _ bool, onUpdate func(progressSnapshot)) *progressTracker {
	tracker := &progressTracker{
		total:         total,
		started:       time.Now(),
		activeDomains: make(map[string]time.Time),
		onUpdate:      onUpdate,
	}
	if !tracker.enabled() {
		return tracker
	}

	tracker.stopCh = make(chan struct{})
	tracker.doneCh = make(chan struct{})
	go tracker.loop()
	return tracker
}

func (p *progressTracker) loop() {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	p.render()
	for {
		select {
		case <-ticker.C:
			p.render()
		case <-p.stopCh:
			p.renderFinal()
			close(p.doneCh)
			return
		}
	}
}

func (p *progressTracker) startDomain(domain string) {
	if !p.enabled() {
		return
	}

	p.mu.Lock()
	p.activeDomains[domain] = time.Now()
	p.mu.Unlock()
}

func (p *progressTracker) finishDomain(domain string) {
	if !p.enabled() {
		return
	}

	p.mu.Lock()
	delete(p.activeDomains, domain)
	p.completed++
	p.lastCompleted = domain
	p.mu.Unlock()
}

func (p *progressTracker) stop() {
	if !p.enabled() {
		return
	}
	close(p.stopCh)
	<-p.doneCh
}

func (p *progressTracker) render() {
	if !p.enabled() {
		return
	}

	p.mu.Lock()
	line := p.buildLineLocked(false)
	snapshot := p.snapshotLocked(false)
	p.mu.Unlock()
	p.publish(line, snapshot)
}

func (p *progressTracker) renderFinal() {
	if !p.enabled() {
		return
	}

	p.mu.Lock()
	line := p.buildLineLocked(true)
	snapshot := p.snapshotLocked(true)
	p.mu.Unlock()
	p.publish(line, snapshot)
}

func (p *progressTracker) buildLineLocked(final bool) string {
	frame := spinnerFrames[p.spinnerIndex%len(spinnerFrames)]
	if final {
		frame = '='
	} else {
		p.spinnerIndex++
	}

	bar := buildProgressBar(p.completed, p.total, 24)
	line := fmt.Sprintf(
		"%c Scanning [%s] %d/%d domains | active %d | elapsed %s",
		frame,
		bar,
		p.completed,
		p.total,
		len(p.activeDomains),
		formatElapsed(time.Since(p.started)),
	)

	activeSummary := summarizeActiveDomains(p.activeDomains, 2)
	switch {
	case activeSummary != "":
		line += " | current: " + activeSummary
	case p.lastCompleted != "":
		line += " | last: " + truncateLabel(p.lastCompleted, 36)
	}
	return line
}

func (p *progressTracker) enabled() bool {
	return p.total > 0 && p.onUpdate != nil
}

func (p *progressTracker) publish(line string, snapshot progressSnapshot) {
	if p.onUpdate != nil {
		p.onUpdate(snapshot)
	}
	_ = line
}

func (p *progressTracker) snapshotLocked(final bool) progressSnapshot {
	return progressSnapshot{
		Total:         p.total,
		Completed:     p.completed,
		ActiveCount:   len(p.activeDomains),
		ActiveSummary: summarizeActiveDomains(p.activeDomains, 2),
		LastCompleted: p.lastCompleted,
		Elapsed:       time.Since(p.started),
		Final:         final,
	}
}

func buildProgressBar(done int, total int, width int) string {
	if width < 1 {
		width = 1
	}
	if total < 1 {
		return strings.Repeat("-", width)
	}

	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}

	filled := done * width / total
	if filled > width {
		filled = width
	}
	return strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
}

func summarizeActiveDomains(active map[string]time.Time, limit int) string {
	if len(active) == 0 {
		return ""
	}
	if limit < 1 {
		limit = 1
	}

	domains := make([]string, 0, len(active))
	for domain := range active {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	if len(domains) > limit {
		visible := make([]string, 0, limit+1)
		for _, domain := range domains[:limit] {
			visible = append(visible, truncateLabel(domain, 28))
		}
		visible = append(visible, fmt.Sprintf("+%d more", len(domains)-limit))
		return strings.Join(visible, ", ")
	}

	for i, domain := range domains {
		domains[i] = truncateLabel(domain, 28)
	}
	return strings.Join(domains, ", ")
}

func truncateLabel(label string, max int) string {
	if max < 4 || len(label) <= max {
		return label
	}
	return label[:max-3] + "..."
}

func formatElapsed(d time.Duration) string {
	seconds := int(d.Round(time.Second) / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	minutes := seconds / 60
	seconds = seconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
