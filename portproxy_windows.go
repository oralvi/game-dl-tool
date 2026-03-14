//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func ensureLoopbackPortProxy(port int, logf func(string, ...interface{})) error {
	if port <= 0 {
		return fmt.Errorf("invalid tunnel port %d", port)
	}

	commands := buildPortProxyCommands(port, true)

	for _, args := range commands {
		if logf != nil {
			logf("running command: netsh %s", strings.Join(args, " "))
		}

		output, err := runNetsh(args)
		if err != nil {
			if isPortProxyDeleteCommand(args) {
				if logf != nil {
					if strings.TrimSpace(output) == "" {
						logf("delete command skipped: netsh %s", strings.Join(args, " "))
					} else {
						logf("delete command skipped: netsh %s (%s)", strings.Join(args, " "), strings.TrimSpace(output))
					}
				}
				continue
			}

			if logf != nil {
				logf("command failed: netsh %s (%s)", strings.Join(args, " "), strings.TrimSpace(output))
			}
			return fmt.Errorf("netsh %v failed: %w: %s", args, err, output)
		}

		if logf != nil {
			logf("command completed: netsh %s", strings.Join(args, " "))
		}
	}

	return nil
}

func clearLoopbackPortProxy(logf func(string, ...interface{})) error {
	commands := buildPortProxyCommands(0, false)

	for _, args := range commands {
		if logf != nil {
			logf("running command: netsh %s", strings.Join(args, " "))
		}
		_, _ = runNetsh(args)
	}
	return nil
}

func runNetsh(args []string) (string, error) {
	cmd := exec.Command("netsh", args...)
	prepareBackgroundCommand(cmd)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(decodeCommandOutput(output)), err
}

func isPortProxyDeleteCommand(args []string) bool {
	if len(args) < 3 {
		return false
	}
	return strings.EqualFold(args[0], "interface") &&
		strings.EqualFold(args[1], "portproxy") &&
		strings.EqualFold(args[2], "delete")
}

func buildPortProxyCommands(port int, includeAdd bool) [][]string {
	ports := []int{tunnelHTTPPort, tunnelHTTPSPort}
	commands := make([][]string, 0, len(ports)*4)
	for _, listenPort := range ports {
		commands = append(commands,
			[]string{"interface", "portproxy", "delete", "v4tov4", "listenaddress=" + tunnelLoopbackIPv4, fmt.Sprintf("listenport=%d", listenPort)},
			[]string{"interface", "portproxy", "delete", "v6tov4", "listenaddress=" + tunnelLoopbackIPv6, fmt.Sprintf("listenport=%d", listenPort)},
		)
		if includeAdd {
			commands = append(commands,
				[]string{"interface", "portproxy", "add", "v4tov4", "listenaddress=" + tunnelLoopbackIPv4, fmt.Sprintf("listenport=%d", listenPort), "connectaddress=" + tunnelInternalIPv4, fmt.Sprintf("connectport=%d", port)},
				[]string{"interface", "portproxy", "add", "v6tov4", "listenaddress=" + tunnelLoopbackIPv6, fmt.Sprintf("listenport=%d", listenPort), "connectaddress=" + tunnelInternalIPv4, fmt.Sprintf("connectport=%d", port)},
			)
		}
	}
	return commands
}
