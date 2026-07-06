package profile

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile reads a profile from a YAML file and validates it.
func LoadFile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile: reading %s: %w", path, err)
	}
	p, err := LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("profile: %s: %w", path, err)
	}
	return p, nil
}

// LoadBytes parses and validates a profile from raw YAML.
func LoadBytes(data []byte) (*Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid profile: %w", err)
	}
	return &p, nil
}
