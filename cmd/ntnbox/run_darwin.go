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

	// Parse the args to find --profile, --tle, --tui, --addr, and the command after --.
	parsed, err := parseDarwinArgs(args)
	if err != nil {
		return err
	}

	// Build docker run arguments.
	dockerArgs := []string{
		"run", "--rm",
		"--privileged",
		"--cap-add", "NET_ADMIN",
	}

	// Bind-mount profile or TLE file.
	var containerCmd []string
	if parsed.profilePath != "" {
		absProfile, err := filepath.Abs(parsed.profilePath)
		if err != nil {
			return fmt.Errorf("resolving profile path: %w", err)
		}
		dockerArgs = append(dockerArgs, "-v", absProfile+":/tmp/profile.yaml:ro")
		containerCmd = []string{"run", "--profile", "/tmp/profile.yaml"}
		// Pass through --devices / --phase-sec and any other profile-mode flags.
		containerCmd = append(containerCmd, parsed.extraArgs...)
	} else {
		absTLE, err := filepath.Abs(parsed.tlePath)
		if err != nil {
			return fmt.Errorf("resolving TLE path: %w", err)
		}
		dockerArgs = append(dockerArgs, "-v", absTLE+":/tmp/input.tle:ro")
		containerCmd = []string{"run", "--tle", "/tmp/input.tle"}
		// Pass through extra args, rewriting --link-model path if present.
		for i := 0; i < len(parsed.extraArgs); i++ {
			arg := parsed.extraArgs[i]
			switch {
			case arg == "--link-model" && i+1 < len(parsed.extraArgs):
				absModel, err := filepath.Abs(parsed.extraArgs[i+1])
				if err != nil {
					return fmt.Errorf("resolving link-model path: %w", err)
				}
				dockerArgs = append(dockerArgs, "-v", absModel+":/tmp/linkmodel.yaml:ro")
				containerCmd = append(containerCmd, "--link-model", "/tmp/linkmodel.yaml")
				i++ // skip the value
			case len(arg) > 13 && arg[:13] == "--link-model=":
				modelPath := arg[13:]
				absModel, err := filepath.Abs(modelPath)
				if err != nil {
					return fmt.Errorf("resolving link-model path: %w", err)
				}
				dockerArgs = append(dockerArgs, "-v", absModel+":/tmp/linkmodel.yaml:ro")
				containerCmd = append(containerCmd, "--link-model", "/tmp/linkmodel.yaml")
			default:
				containerCmd = append(containerCmd, arg)
			}
		}
	}

	// Publish the API port if --addr is set.
	if parsed.addr != "" {
		dockerArgs = append(dockerArgs, "-p", dockerHostPublishSpec(parsed.addr))
	}

	// Only attach TTY if stdin is a terminal (required for --tui).
	if fileInfo, _ := os.Stdin.Stat(); fileInfo.Mode()&os.ModeCharDevice != 0 {
		dockerArgs = append(dockerArgs, "-it")
	}

	// Bind-mount host paths referenced by the command (binaries, project dirs).
	// JS projects get a Linux node_modules volume overlay (Darwin modules won't run).
	cmdArgs := parsed.cmdArgs
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
	mounts, rewrittenCmd, err := prepareDarwinCmdMounts(cwd, cmdArgs)
	if err != nil {
		return fmt.Errorf("preparing host mounts: %w", err)
	}
	cmdArgs = rewrittenCmd
	dockerArgs = appendDarwinMountArgs(dockerArgs, mounts)

	// Build the ntnbox run command for inside the container.
	if parsed.tui {
		containerCmd = append(containerCmd, "--tui")
	}
	if parsed.addr != "" {
		// Inside the container, bind 0.0.0.0 so Docker port forwarding works.
		port := parsed.addr
		for i := len(port) - 1; i >= 0; i-- {
			if port[i] == ':' {
				port = port[i+1:]
				break
			}
		}
		containerCmd = append(containerCmd, "--addr", "0.0.0.0:"+port)
	}
	if parsed.record != "" {
		absRecord, err := filepath.Abs(parsed.record)
		if err != nil {
			return fmt.Errorf("resolving record path: %w", err)
		}
		// Create the file only if it doesn't exist (don't truncate).
		if _, err := os.Stat(absRecord); os.IsNotExist(err) {
			f, err := os.Create(absRecord)
			if err != nil {
				return fmt.Errorf("creating record file: %w", err)
			}
			_ = f.Close()
		}
		dockerArgs = append(dockerArgs, "-v", absRecord+":/tmp/recording.jsonl")
		containerCmd = append(containerCmd, "--record", "/tmp/recording.jsonl")
	}
	if parsed.report != "" {
		absReport, err := filepath.Abs(parsed.report)
		if err != nil {
			return fmt.Errorf("resolving report path: %w", err)
		}
		if _, err := os.Stat(absReport); os.IsNotExist(err) {
			f, err := os.Create(absReport)
			if err != nil {
				return fmt.Errorf("creating report file: %w", err)
			}
			_ = f.Close()
		}
		dockerArgs = append(dockerArgs, "-v", absReport+":/tmp/report.json")
		containerCmd = append(containerCmd, "--report", "/tmp/report.json")
	}
	containerCmd = append(containerCmd, "--")
	containerCmd = append(containerCmd, cmdArgs...)

	dockerArgs = append(dockerArgs, "ntnbox:latest")
	dockerArgs = append(dockerArgs, containerCmd...)

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

// darwinParsed holds the parsed flags from the run command args.
type darwinParsed struct {
	profilePath string
	tlePath     string
	tui         bool
	addr        string
	record      string
	report      string
	cmdArgs     []string
	extraArgs   []string // All other flags to pass through (TLE flags, etc.)
}

// parseDarwinArgs extracts --profile, --tle, --tui, --addr values and the
// command after "--" from the original args slice.
func parseDarwinArgs(args []string) (darwinParsed, error) {
	var p darwinParsed
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--profile" && i+1 < len(args):
			p.profilePath = args[i+1]
			i++
		case len(args[i]) > 10 && args[i][:10] == "--profile=":
			p.profilePath = args[i][10:]
		case args[i] == "--tle" && i+1 < len(args):
			p.tlePath = args[i+1]
			i++
		case len(args[i]) > 6 && args[i][:6] == "--tle=":
			p.tlePath = args[i][6:]
		case args[i] == "--tui":
			p.tui = true
		case args[i] == "--record" && i+1 < len(args):
			p.record = args[i+1]
			i++
		case args[i] == "--report" && i+1 < len(args):
			p.report = args[i+1]
			i++
		case args[i] == "--addr" && i+1 < len(args):
			p.addr = args[i+1]
			i++
		case len(args[i]) > 7 && args[i][:7] == "--addr=":
			p.addr = args[i][7:]
		case args[i] == "--":
			p.cmdArgs = args[i+1:]
			if p.profilePath == "" && p.tlePath == "" {
				return p, errors.New("--profile or --tle is required")
			}
			if p.profilePath != "" && p.tlePath != "" {
				return p, errors.New("flags --tle and --profile are mutually exclusive")
			}
			if len(p.cmdArgs) == 0 {
				return p, errors.New("no command specified after --")
			}
			return p, nil
		default:
			// Collect other flags to pass through to the container.
			p.extraArgs = append(p.extraArgs, args[i])
		}
	}

	if p.profilePath == "" && p.tlePath == "" {
		return p, errors.New("--profile or --tle is required\n\nUsage: ntnbox run --profile <path> -- <cmd> [args...]\n       ntnbox run --tle <path> --lat <deg> --lon <deg> -- <cmd> [args...]")
	}
	if p.profilePath != "" && p.tlePath != "" {
		return p, errors.New("flags --tle and --profile are mutually exclusive")
	}
	return p, errors.New("no command specified after --\n\nUsage: ntnbox run --profile <path> -- <cmd> [args...]\n       ntnbox run --tle <path> --lat <deg> --lon <deg> -- <cmd> [args...]")
}
