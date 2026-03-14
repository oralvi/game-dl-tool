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

	for index, args := range commands {
		if logf != nil {
			logf("running command: netsh %s", strings.Join(args, " "))
		}
		cmd := exec.Command("netsh", args...)
		prepareBackgroundCommand(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if index < 2 {
				if logf != nil {
					logf("command finished with cleanup miss: netsh %s", strings.Join(args, " "))
				}
				continue
			}
			if logf != nil {
				logf("command failed: netsh %s", strings.Join(args, " "))
			}
			return fmt.Errorf("netsh %v failed: %w: %s", args, err, string(output))
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
		cmd := exec.Command("netsh", args...)
		prepareBackgroundCommand(cmd)
		_, _ = cmd.CombinedOutput()
	}
	return nil
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
