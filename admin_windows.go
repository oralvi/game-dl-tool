//go:build windows

package main

import "golang.org/x/sys/windows"

func isProcessElevated() (bool, error) {
	return windows.GetCurrentProcessToken().IsElevated(), nil
}
