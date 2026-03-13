package main

import "os"

func main() {
	if shouldLaunchGUI(os.Args[1:]) {
		if err := runGUI(); err != nil {
			exitWithError(err.Error())
		}
		return
	}
	runCLI()
}

func shouldLaunchGUI(args []string) bool {
	if len(args) == 0 {
		return true
	}
	for _, arg := range args {
		switch arg {
		case "-gui", "--gui":
			return true
		}
	}
	return false
}
