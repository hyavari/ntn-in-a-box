package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareDarwinCmdMounts_RewritesDirAndOverlaysNodeModules(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "ntnkit")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := []string{
		"env",
		"NTNBOX_API_BASE=http://10.200.0.1:18080",
		"pnpm",
		"--dir",
		"ntnkit",
		"--filter",
		"@ntnkit/example-ci-smoke",
		"start",
	}

	mounts, rewritten, err := prepareDarwinCmdMounts(root, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mounts=%d want 1: %+v", len(mounts), mounts)
	}
	if mounts[0].HostPath != proj {
		t.Fatalf("HostPath=%q want %q", mounts[0].HostPath, proj)
	}
	if mounts[0].NodeModulesVolume == "" {
		t.Fatal("expected node_modules volume for package.json project")
	}
	if rewritten[4] != mounts[0].ContainerPath {
		t.Fatalf("--dir value=%q want %q", rewritten[4], mounts[0].ContainerPath)
	}
	if rewritten[1] != "NTNBOX_API_BASE=http://10.200.0.1:18080" {
		t.Fatalf("env arg rewritten unexpectedly: %q", rewritten[1])
	}
}

func TestPrepareDarwinCmdMounts_ScriptMountsProjectRoot(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "ntnkit")
	scriptDir := filepath.Join(proj, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(scriptDir, "ntnbox-ci-smoke.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	mounts, rewritten, err := prepareDarwinCmdMounts(root, []string{
		"env", "FOO=1", "../ntnkit/scripts/ntnbox-ci-smoke.sh",
	})
	// cwd is root but script path is relative to ntn-in-a-box style; use abs via join
	_ = mounts
	// Re-run with cwd = a sibling folder like ntn-in-a-box
	sib := filepath.Join(root, "ntn-in-a-box")
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	mounts, rewritten, err = prepareDarwinCmdMounts(sib, []string{
		"env", "FOO=1", "../ntnkit/scripts/ntnbox-ci-smoke.sh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 || mounts[0].HostPath != proj {
		t.Fatalf("expected project mount, got %+v", mounts)
	}
	wantSuffix := "/scripts/ntnbox-ci-smoke.sh"
	if !strings.HasSuffix(rewritten[2], wantSuffix) {
		t.Fatalf("script path=%q want suffix %q", rewritten[2], wantSuffix)
	}
	if !strings.HasPrefix(rewritten[2], mounts[0].ContainerPath) {
		t.Fatalf("script path=%q want under %q", rewritten[2], mounts[0].ContainerPath)
	}
}

func TestPrepareDarwinCmdMounts_LocalBinary(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "poller")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	mounts, rewritten, err := prepareDarwinCmdMounts(root, []string{"./poller", "--url", "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mounts=%d want 1", len(mounts))
	}
	if rewritten[0] != mounts[0].ContainerPath {
		t.Fatalf("bin=%q want %q", rewritten[0], mounts[0].ContainerPath)
	}
	if mounts[0].NodeModulesVolume != "" {
		t.Fatal("binary should not get node_modules volume")
	}
}

func TestLooksLikePath(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"../ntnkit", true},
		{"./poller", true},
		{"/tmp/x", true},
		{"samples/node-retry/index.js", true},
		{"pnpm", false},
		{"--filter", false},
		{"NTNBOX_API_BASE=http://10.200.0.1:18080", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikePath(tt.in); got != tt.want {
			t.Errorf("looksLikePath(%q)=%v want %v", tt.in, got, tt.want)
		}
	}
}
