//go:build !windows

package main

import "fmt"

func ensureLoopbackPortProxy(port int, _ func(string, ...interface{})) error {
	return fmt.Errorf("loopback portproxy is only supported on Windows")
}

func clearLoopbackPortProxy(_ func(string, ...interface{})) error {
	return nil
}
