package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBuiltinShortNames(t *testing.T) {
	for _, name := range []string{"nbiot_ntn", "lband_geo", "leo_d2c"} {
		r, err := Resolve(name)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", name, err)
		}
		if r.Path != "" {
			t.Fatalf("Resolve(%q): expected embedded (empty Path), got %q", name, r.Path)
		}
		if r.Profile.Name != name {
			t.Fatalf("Resolve(%q): profile name %q", name, r.Profile.Name)
		}
		if !IsBuiltin(name) {
			t.Fatalf("IsBuiltin(%q) = false", name)
		}
	}
}

func TestResolveBuiltinWithYamlSuffix(t *testing.T) {
	r, err := Resolve("geo_steady.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "" || r.Profile.Name != "geo_steady" {
		t.Fatalf("got path=%q name=%q", r.Path, r.Profile.Name)
	}
}

func TestResolveFilesystemPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	const yaml = `
name: custom
schedule:
  mode: continuous
  period_sec: 30
curves:
  delay_ms: [{offset_sec: 0, value: 10}]
  jitter_ms: [{offset_sec: 0, value: 1}]
  loss_pct: [{offset_sec: 0, value: 0}]
  bandwidth_kbps: [{offset_sec: 0, value: 100}]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != path || r.Profile.Name != "custom" {
		t.Fatalf("got path=%q name=%q", r.Path, r.Profile.Name)
	}
	if IsBuiltin(path) {
		t.Fatal("path should not be builtin")
	}
}

func TestResolveBareFilenamePrefersDisk(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	const yaml = `
name: nbiot_ntn
schedule:
  mode: continuous
  period_sec: 30
curves:
  delay_ms: [{offset_sec: 0, value: 1}]
  jitter_ms: [{offset_sec: 0, value: 1}]
  loss_pct: [{offset_sec: 0, value: 0}]
  bandwidth_kbps: [{offset_sec: 0, value: 1}]
`
	if err := os.WriteFile("nbiot_ntn.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve("nbiot_ntn.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "nbiot_ntn.yaml" {
		t.Fatalf("expected disk path, got %#v", r)
	}
	if r.Profile.Curves.DelayMs[0].Value != 1 {
		t.Fatalf("expected disk override delay=1, got %v", r.Profile.Curves.DelayMs[0].Value)
	}
}

func TestResolveUnknownShortName(t *testing.T) {
	_, err := Resolve("not_a_real_bearer")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterializeBuiltinTempFile(t *testing.T) {
	path, cleanup, err := Materialize("nbiot_ntn")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "nbiot_ntn" {
		t.Fatalf("name %q", p.Name)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, stat err=%v", err)
	}
}

func TestMaterializeDiskPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "custom.yaml")
	const yaml = `
name: custom
schedule:
  mode: continuous
  period_sec: 30
curves:
  delay_ms: [{offset_sec: 0, value: 10}]
  jitter_ms: [{offset_sec: 0, value: 1}]
  loss_pct: [{offset_sec: 0, value: 0}]
  bandwidth_kbps: [{offset_sec: 0, value: 100}]
`
	if err := os.WriteFile(src, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := Materialize(src)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if path != src && path != filepath.Clean(src) {
		// Absolute form of src.
		abs, _ := filepath.Abs(src)
		if path != abs {
			t.Fatalf("expected abs disk path, got %q", path)
		}
	}
}

func TestBuiltinMatchesTestdata(t *testing.T) {
	names, err := ListBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "..", "testdata", "profiles")
	for _, name := range names {
		wantPath := filepath.Join(root, name+".yaml")
		want, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatalf("testdata missing for builtin %s: %v", name, err)
		}
		got, err := builtinFS.ReadFile("builtins/" + name + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("builtin %s.yaml drifted from testdata/profiles/%s.yaml", name, name)
		}
	}
}
