//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

func replayPlatformGate(args []string) error {
	return replayViaDarwinDocker(args)
}

func replayViaDarwinDocker(args []string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return errors.New("ntnbox replay requires Linux network namespaces; " +
			"on macOS, Docker is required but was not found on PATH")
	}

	// Parse replay flags.
	var filePath, addr, speed string
	var tui bool
	var cmdArgs []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--file" && i+1 < len(args):
			filePath = args[i+1]
			i++
		case args[i] == "--speed" && i+1 < len(args):
			speed = args[i+1]
			i++
		case args[i] == "--addr" && i+1 < len(args):
			addr = args[i+1]
			i++
		case args[i] == "--tui":
			tui = true
		case args[i] == "--":
			cmdArgs = args[i+1:]
		}
		if cmdArgs != nil {
			break
		}
	}

	if filePath == "" {
		return errors.New("--file is required\n\nUsage: ntnbox replay --file <path> [--speed <N>] -- <cmd> [args...]")
	}
	if len(cmdArgs) == 0 {
		return errors.New("no command specified after --")
	}

	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolving file path: %w", err)
	}

	dockerArgs := []string{
		"run", "--rm",
		"--privileged",
		"--cap-add", "NET_ADMIN",
		"-v", absFile + ":/tmp/recording.jsonl:ro",
	}

	if addr != "" {
		dockerArgs = append(dockerArgs, "-p", dockerHostPublishSpec(addr))
	}

	if fileInfo, _ := os.Stdin.Stat(); fileInfo.Mode()&os.ModeCharDevice != 0 {
		dockerArgs = append(dockerArgs, "-it")
	}

	// Bind-mount local command binaries.
	if len(cmdArgs) > 0 {
		cmdBin := cmdArgs[0]
		if len(cmdBin) > 0 && (cmdBin[0] == '/' || (len(cmdBin) > 1 && cmdBin[:2] == "./")) {
			if absBin, err := filepath.Abs(cmdBin); err == nil {
				if info, err := os.Stat(absBin); err == nil && !info.IsDir() {
					dockerArgs = append(dockerArgs, "-v", absBin+":/app/"+filepath.Base(absBin)+":ro")
					cmdArgs[0] = "/app/" + filepath.Base(absBin)
				}
			}
		}
	}

	// Container command.
	containerCmd := []string{"replay", "--file", "/tmp/recording.jsonl"}
	if tui {
		containerCmd = append(containerCmd, "--tui")
	}
	if speed != "" {
		containerCmd = append(containerCmd, "--speed", speed)
	}
	if addr != "" {
		port := addr
		for i := len(port) - 1; i >= 0; i-- {
			if port[i] == ':' {
				port = port[i+1:]
				break
			}
		}
		containerCmd = append(containerCmd, "--addr", "0.0.0.0:"+port)
	}
	containerCmd = append(containerCmd, "--")
	containerCmd = append(containerCmd, cmdArgs...)

	dockerArgs = append(dockerArgs, "ntnbox:latest")
	dockerArgs = append(dockerArgs, containerCmd...)

	fmt.Fprintf(os.Stderr, "ntnbox: detected macOS, replaying via Docker...\n")
	fmt.Fprintf(os.Stderr, "ntnbox: %s %v\n", dockerPath, dockerArgs)

	cmd := exec.Command(dockerPath, dockerArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting docker: %w", err)
	}

	go func() {
		for sig := range sigCh {
			_ = cmd.Process.Signal(sig)
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	return errProxyComplete
}
