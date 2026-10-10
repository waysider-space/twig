package installer

import (
	"bytes"
	"testing"
)

func TestGenerateDeterministic(t *testing.T) {
	config := validConfig()
	first, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(config)
	if err != nil || !bytes.Equal(first["install.sh"], second["install.sh"]) || len(first["install.ps1"]) != 0 {
		t.Fatalf("deterministic generation: %v", err)
	}
	config.Targets[0].OS = "windows"
	config.Targets[0].Files[0].Source = "grove.exe"
	config.Targets[0].Files[0].Path = "grove.exe"
	if scripts, err := Generate(config); err != nil || len(scripts["install.ps1"]) == 0 || len(scripts["install.sh"]) != 0 {
		t.Fatalf("PowerShell generation: %v", err)
	}
}
