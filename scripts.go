//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
)

var swaggerArgs = []string{"swag", "init", "-g", "cmd/api/main.go", "-o", "docs"}

var commands = map[string][]string{
	"run":          {"go", "run", "./cmd/api"},
	"test":         {"go", "test", "./tests/integration/...", "-v"},
	"build":        {"go", "build", "-o", "bin/server", "./cmd/api"},
	"docker-build": {"docker", "build", "-t", "gateway-microcontroller", "."},
	"swagger":      swaggerArgs,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run scripts.go <command>")
		fmt.Println("Commands:")
		for name := range commands {
			fmt.Printf("  %s\n", name)
		}
		os.Exit(1)
	}

	name := os.Args[1]
	args, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", name)
		os.Exit(1)
	}

	if name == "run" {
		runCmd(swaggerArgs)
	}

	runCmd(args)
}

func runCmd(args []string) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
