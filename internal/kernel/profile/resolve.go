package profile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Resolved is the result of resolving a --profile reference.
type Resolved struct {
	// Profile is the loaded, validated profile.
	Profile *Profile
	// Path is set when the profile was loaded from the filesystem (for
	// Darwin bind-mount). Empty when loaded from the embedded builtins.
	Path string
	// Ref is the original reference string passed to Resolve.
	Ref string
}

// Resolve loads a profile from a filesystem path or an embedded builtin name.
//
// Rules (in order):
//  1. If ref contains a path separator → LoadFile(ref).
//  2. Else if ref exists as a file on disk → LoadFile(ref).
//  3. Else treat as builtin name (optional .yaml suffix) and load from embed.
func Resolve(ref string) (*Resolved, error) {
	if ref == "" {
		return nil, fmt.Errorf("profile: empty reference")
	}

	if strings.ContainsRune(ref, '/') || strings.ContainsRune(ref, filepath.Separator) {
		p, err := LoadFile(ref)
		if err != nil {
			return nil, err
		}
		return &Resolved{Profile: p, Path: ref, Ref: ref}, nil
	}

	if st, err := os.Stat(ref); err == nil && !st.IsDir() {
		p, err := LoadFile(ref)
		if err != nil {
			return nil, err
		}
		return &Resolved{Profile: p, Path: ref, Ref: ref}, nil
	}

	name := strings.TrimSuffix(ref, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	data, err := builtinFS.ReadFile("builtins/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("profile %q: not a file and not a builtin (%s)", ref, builtinNamesHint())
	}
	p, err := LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("profile builtin %s: %w", name, err)
	}
	return &Resolved{Profile: p, Path: "", Ref: ref}, nil
}

// ResolveLoad is Resolve plus returning only the Profile.
func ResolveLoad(ref string) (*Profile, error) {
	r, err := Resolve(ref)
	if err != nil {
		return nil, err
	}
	return r.Profile, nil
}

// Materialize returns a filesystem path for ref suitable for bind-mounts.
// Disk refs are returned as absolute paths with a no-op cleanup. Builtin
// short names are written to a temp file (host embed is the source of
// truth), so Docker proxies work even when the container image is stale.
func Materialize(ref string) (path string, cleanup func(), err error) {
	r, err := Resolve(ref)
	if err != nil {
		return "", nil, err
	}
	if r.Path != "" {
		abs, absErr := filepath.Abs(r.Path)
		if absErr != nil {
			return "", nil, fmt.Errorf("profile: resolving %s: %w", r.Path, absErr)
		}
		return abs, func() {}, nil
	}

	name := strings.TrimSuffix(ref, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	data, err := builtinFS.ReadFile("builtins/" + name + ".yaml")
	if err != nil {
		return "", nil, fmt.Errorf("profile builtin %s: %w", name, err)
	}
	f, err := os.CreateTemp("", "ntnbox-profile-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("profile: temp file: %w", err)
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", nil, fmt.Errorf("profile: writing temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", nil, fmt.Errorf("profile: closing temp: %w", err)
	}
	return tmp, func() { _ = os.Remove(tmp) }, nil
}

// IsBuiltin reports whether ref resolves to an embedded profile without
// needing a filesystem path (no slash, not an existing file, name known).
func IsBuiltin(ref string) bool {
	if ref == "" || strings.ContainsRune(ref, '/') || strings.ContainsRune(ref, filepath.Separator) {
		return false
	}
	if st, err := os.Stat(ref); err == nil && !st.IsDir() {
		return false
	}
	name := strings.TrimSuffix(ref, ".yaml")
	name = strings.TrimSuffix(name, ".yml")
	_, err := builtinFS.ReadFile("builtins/" + name + ".yaml")
	return err == nil
}

func builtinNamesHint() string {
	names, err := ListBuiltins()
	if err != nil || len(names) == 0 {
		return "see testdata/profiles/README.md"
	}
	return "builtins: " + strings.Join(names, ", ")
}

// ListBuiltins returns sorted builtin profile names (without .yaml).
func ListBuiltins() ([]string, error) {
	entries, err := fs.ReadDir(builtinFS, "builtins")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yaml") {
			names = append(names, strings.TrimSuffix(n, ".yaml"))
		}
	}
	sort.Strings(names)
	return names, nil
}
