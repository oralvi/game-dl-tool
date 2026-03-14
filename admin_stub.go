//go:build !windows

package main

func isProcessElevated() (bool, error) {
	return true, nil
}
