package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hostMount describes a host path bind-mounted into the Darwin Docker proxy.
type hostMount struct {
	HostPath      string
	ContainerPath string
	// NodeModulesVolume, when non-empty, is a named Docker volume mounted over
	// ContainerPath/node_modules so Linux deps are used instead of Darwin ones.
	NodeModulesVolume string
}

// prepareDarwinCmdMounts finds host paths referenced by cmdArgs, prepares
// bind mounts under /host/*, and returns rewritten command args.
//
// Files under a JS project (nearest package.json ancestor) mount the project
// root so relative scripts keep a usable tree. Those roots also get a named
// volume overlay on node_modules.
func prepareDarwinCmdMounts(cwd string, cmdArgs []string) ([]hostMount, []string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, nil, err
		}
	}

	mountByHost := map[string]hostMount{}
	rewritten := make([]string, len(cmdArgs))
	copy(rewritten, cmdArgs)

	for i, arg := range cmdArgs {
		hostPath, ok := resolveHostPathArg(cwd, arg)
		if !ok {
			continue
		}
		containerArg, err := mountAndRewrite(mountByHost, hostPath)
		if err != nil {
			return nil, nil, err
		}
		rewritten[i] = containerArg
	}

	// Also rewrite --dir / -C values (flag + separate argv).
	for i := 0; i+1 < len(cmdArgs); i++ {
		if cmdArgs[i] != "--dir" && cmdArgs[i] != "-C" {
			continue
		}
		hostPath, ok := resolveExistingPath(cwd, cmdArgs[i+1])
		if !ok {
			continue
		}
		containerArg, err := mountAndRewrite(mountByHost, hostPath)
		if err != nil {
			return nil, nil, err
		}
		rewritten[i+1] = containerArg
	}

	mounts := make([]hostMount, 0, len(mountByHost))
	for _, m := range mountByHost {
		mounts = append(mounts, m)
	}
	return mounts, rewritten, nil
}

func mountAndRewrite(mountByHost map[string]hostMount, hostPath string) (string, error) {
	mountRoot, rel, err := mountRootForPath(hostPath)
	if err != nil {
		return "", err
	}
	mount, err := ensureHostMount(mountByHost, mountRoot)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == "" {
		return mount.ContainerPath, nil
	}
	return filepath.Join(mount.ContainerPath, rel), nil
}

func mountRootForPath(hostPath string) (root string, rel string, err error) {
	info, err := os.Stat(hostPath)
	if err != nil {
		return "", "", err
	}
	start := hostPath
	if !info.IsDir() {
		start = filepath.Dir(hostPath)
	}
	if proj, ok := findPackageRoot(start); ok {
		rel, err := filepath.Rel(proj, hostPath)
		if err != nil {
			return "", "", err
		}
		return proj, rel, nil
	}
	// Non-JS path: mount the exact file or directory.
	return hostPath, ".", nil
}

func findPackageRoot(dir string) (string, bool) {
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, "package.json")); err == nil {
			return cur, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

func ensureHostMount(mountByHost map[string]hostMount, hostPath string) (hostMount, error) {
	if m, ok := mountByHost[hostPath]; ok {
		return m, nil
	}
	info, err := os.Stat(hostPath)
	if err != nil {
		return hostMount{}, err
	}
	containerPath := containerPathForHost(hostPath)
	m := hostMount{
		HostPath:      hostPath,
		ContainerPath: containerPath,
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(hostPath, "package.json")); err == nil {
			m.NodeModulesVolume = nodeModulesVolumeName(hostPath)
		}
	}
	mountByHost[hostPath] = m
	return m, nil
}

func resolveHostPathArg(cwd, arg string) (string, bool) {
	if !looksLikePath(arg) {
		return "", false
	}
	return resolveExistingPath(cwd, arg)
}

func resolveExistingPath(cwd, arg string) (string, bool) {
	if arg == "" || strings.HasPrefix(arg, "-") {
		return "", false
	}
	if strings.Contains(arg, "=") {
		return "", false
	}
	candidate := arg
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(cwd, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(abs); err != nil {
		return "", false
	}
	return abs, true
}

func looksLikePath(arg string) bool {
	if arg == "" || strings.HasPrefix(arg, "-") {
		return false
	}
	if strings.Contains(arg, "=") {
		return false
	}
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return true
	}
	return strings.Contains(arg, "/")
}

func containerPathForHost(hostPath string) string {
	sum := sha256.Sum256([]byte(hostPath))
	base := filepath.Base(hostPath)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "path"
	}
	return fmt.Sprintf("/host/%s-%x", sanitizeName(base), sum[:4])
}

func nodeModulesVolumeName(hostPath string) string {
	sum := sha256.Sum256([]byte(hostPath))
	return fmt.Sprintf("ntnbox-nm-%x", sum[:8])
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "path"
	}
	return out
}
