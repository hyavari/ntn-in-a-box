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

// runPlatformGate on macOS re-invokes ntnbox run inside a Docker
// container. Returns nil only if it should fall through to native
// execution (never on macOS — it either proxies or errors).
// The returned error is always non-nil: either the proxy ran
// successfully (returning the container's exit status as an error) or
// it failed to start.
func runPlatformGate(originalArgs []string) error {
	return runViaDarwinDocker(originalArgs)
}

func runViaDarwinDocker(args []string) error {
	// Check Docker is available.
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return errors.New("ntnbox run requires Linux network namespaces.\n" +
			"On macOS, Docker is required but was not found on PATH.\n" +
			"Install Docker Desktop: https://www.docker.com/products/docker-desktop/")
	}

	// Parse the args to find --profile and the command after --.
	profilePath, cmdArgs, err := parseDarwinArgs(args)
	if err != nil {
		return err
	}

	// Resolve profile to absolute path for bind-mounting.
	absProfile, err := filepath.Abs(profilePath)
	if err != nil {
		return fmt.Errorf("resolving profile path: %w", err)
	}

	// Build docker run arguments.
	dockerArgs := []string{
		"run", "--rm",
		"--privileged",
		"--cap-add", "NET_ADMIN",
		"-v", absProfile + ":/tmp/profile.yaml:ro",
	}

	// Only attach TTY if stdin is a terminal.
	if fileInfo, _ := os.Stdin.Stat(); fileInfo.Mode()&os.ModeCharDevice != 0 {
		dockerArgs = append(dockerArgs, "-it")
	}

	// If the command is a local file, bind-mount it too.
	if len(cmdArgs) > 0 {
		cmdBin := cmdArgs[0]
		if absBin, err := filepath.Abs(cmdBin); err == nil {
			if info, err := os.Stat(absBin); err == nil && !info.IsDir() {
				dockerArgs = append(dockerArgs,
					"-v", absBin+":/app/"+filepath.Base(absBin)+":ro",
				)
				cmdArgs[0] = "/app/" + filepath.Base(absBin)
			}
		}
	}

	// Image + ntnbox run command inside the container.
	dockerArgs = append(dockerArgs, "ntnbox:latest",
		"run", "--profile", "/tmp/profile.yaml", "--",
	)
	dockerArgs = append(dockerArgs, cmdArgs...)

	fmt.Fprintf(os.Stderr, "ntnbox: detected macOS, running via Docker...\n")
	fmt.Fprintf(os.Stderr, "ntnbox: %s %v\n", dockerPath, dockerArgs)

	// Execute docker, forwarding stdio.
	cmd := exec.Command(dockerPath, dockerArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Forward signals to the docker process.
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

// parseDarwinArgs extracts --profile value and the command after "--"
// from the original args slice without modifying them.
func parseDarwinArgs(args []string) (profilePath string, cmdArgs []string, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--profile" && i+1 < len(args):
			profilePath = args[i+1]
			i++
		case len(args[i]) > len("--profile=") && args[i][:10] == "--profile=":
			profilePath = args[i][10:]
		case args[i] == "--":
			cmdArgs = args[i+1:]
			if profilePath == "" {
				return "", nil, errors.New("--profile is required")
			}
			if len(cmdArgs) == 0 {
				return "", nil, errors.New("no command specified after --")
			}
			return profilePath, cmdArgs, nil
		}
	}

	if profilePath == "" {
		return "", nil, errors.New("--profile is required\n\nUsage: ntnbox run --profile <path> [--addr <host:port>] -- <cmd> [args...]")
	}
	return "", nil, errors.New("no command specified after --\n\nUsage: ntnbox run --profile <path> [--addr <host:port>] -- <cmd> [args...]")
}
