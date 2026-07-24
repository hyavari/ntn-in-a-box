package profile

import "embed"

// Builtin YAML profiles shipped inside the ntnbox binary. Keep in sync with
// testdata/profiles/*.yaml (enforced by TestBuiltinMatchesTestdata).
//
//go:embed builtins/*.yaml
var builtinFS embed.FS
