package main

import "fmt"

func ensureAdminPrivileges(action string) error {
	elevated, err := isProcessElevated()
	if err != nil {
		return fmt.Errorf("could not determine administrator privileges for %s: %w", action, err)
	}
	if elevated {
		return nil
	}
	return fmt.Errorf("%s requires administrator privileges; restart game-dl-tool as administrator", action)
}
