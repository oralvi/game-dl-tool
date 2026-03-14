package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type runLogger struct {
	mu   sync.Mutex
	file *os.File
	hook func(string)
}

func newRunLoggerWithHook(path string, hook func(string)) (*runLogger, error) {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return &runLogger{hook: hook}, nil
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("prepare log dir %s: %w", dir, err)
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	return &runLogger{file: file, hook: hook}, nil
}

func (l *runLogger) Printf(format string, args ...any) {
	if l == nil {
		return
	}

	line := fmt.Sprintf(format, args...)
	if l.hook != nil {
		l.hook(line)
	}

	if l.file == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.file, "[%s] %s\n", timestamp, line)
}

func (l *runLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}
