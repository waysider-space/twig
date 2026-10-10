package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cliManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "twig.yml")
	data := "schema: 1\nname: grove\nversion: 1.0.0\ntargets:\n  - os: linux\n    arch: amd64\n    url: https://example.com/grove.tar.gz\n    sha256: " + strings.Repeat("a", 64) + "\n    files:\n      - source: grove\n        destination: bin\n        path: grove\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestCLIValidate(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"validate", cliManifest(t)}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Valid") {
		t.Fatalf("validation output: %s", output.String())
	}
}

func TestCLIGenerate(t *testing.T) {
	var output bytes.Buffer
	manifest, directory := cliManifest(t), t.TempDir()
	args := []string{"generate", manifest, "--output", directory}
	if err := Run(context.Background(), args, &output, &output); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "install.sh")
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), args, &output, &output); err == nil {
		t.Fatal("overwrote existing output")
	}
	if data, _ := os.ReadFile(path); string(data) != "existing" {
		t.Fatal("changed existing output after refusal")
	}
	if err := Run(context.Background(), append(args, "--force"), &output, &output); err != nil {
		t.Fatal(err)
	}
}

func TestCLIValidateArtifacts(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"validate", cliManifest(t), "--artifacts", t.TempDir()}, &output, &output); err == nil || !strings.Contains(err.Error(), "artifact") {
		t.Fatalf("missing artifact filename: %v", err)
	}
}
