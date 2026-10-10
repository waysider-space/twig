package installer

import (
	"bytes"
	"fmt"
	"io"
	"regexp"

	"github.com/goccy/go-yaml"
)

type Manifest struct {
	Schema  int      `yaml:"schema"`
	Name    string   `yaml:"name"`
	Version string   `yaml:"version"`
	AuthEnv string   `yaml:"auth_env,omitempty"`
	Limits  Limits   `yaml:"limits,omitempty"`
	Targets []Target `yaml:"targets"`
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Config returns the generator configuration without manifest-only fields.
func (m Manifest) Config() Config {
	return Config{AuthEnv: m.AuthEnv, Limits: m.Limits, Targets: m.Targets}
}

// ParseManifest decodes and validates one strict YAML installer manifest.
func ParseManifest(reader io.Reader) (Manifest, error) {
	var manifest Manifest
	data, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil {
		return manifest, err
	}
	if len(data) > 1<<20 {
		return manifest, fmt.Errorf("manifest exceeds 1 MiB")
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("parse manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return manifest, fmt.Errorf("manifest must contain one YAML document")
	}
	if manifest.Schema != 1 || !namePattern.MatchString(manifest.Name) || len(manifest.Name) > 100 || manifest.Version == "" {
		return manifest, fmt.Errorf("manifest needs schema 1, an app slug, and a version")
	}
	if err := Validate(manifest.Config()); err != nil {
		return manifest, err
	}

	return manifest, nil
}
