package installer

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{Targets: []Target{{OS: "linux", Arch: "amd64", URL: "https://example.com/grove.tar.gz", SHA256: strings.Repeat("a", 64), Files: []File{{Source: "bin/grove", Destination: "bin", Path: "grove"}, {Source: "defaults/config.toml", Destination: "config", Path: "grove/config.toml"}}}}}
}

func TestValidateConfig(t *testing.T) {
	if err := Validate(validConfig()); err != nil {
		t.Fatal(err)
	}

	for _, change := range []func(*Config){
		func(c *Config) { c.Targets = nil },
		func(c *Config) { c.Targets = append(c.Targets, c.Targets[0]) },
		func(c *Config) { c.Targets[0].SHA256 = strings.Repeat("z", 64) },
		func(c *Config) { c.Targets[0].URL = "http://example.com/file" },
		func(c *Config) { c.Targets[0].URL = "https://user:secret@example.com/file" },
		func(c *Config) { c.AuthEnv = "TOKEN;exit" },
	} {
		config := validConfig()
		change(&config)
		if err := Validate(config); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestValidateFiles(t *testing.T) {
	for _, source := range []string{"../grove", "/grove", "bin/../grove", "bin//grove", "bin\\grove", "bin/$(id)", "././grove"} {
		if err := ValidateFiles("linux", "amd64", []File{{Source: source, Destination: "bin", Path: "grove"}}); err == nil {
			t.Fatalf("accepted %q", source)
		}
	}

	if err := ValidateFiles("linux", "amd64", []File{{Source: "./bin/grove", Destination: "bin", Path: "grove"}}); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"CON.exe", "aux.txt", "bin/NUL", "grove.", "C:/grove.exe"} {
		if err := ValidateFiles("windows", "amd64", []File{{Source: "grove.exe", Destination: "bin", Path: path}}); err == nil {
			t.Fatalf("accepted Windows path %q", path)
		}
	}

	for _, files := range [][]File{
		{{Source: "one", Destination: "config", Path: "grove/a"}, {Source: "two", Destination: "data", Path: "grove/A"}},
		{{Source: "one", Destination: "bin", Path: "grove"}, {Source: "two", Destination: "bin", Path: "grove/helper"}},
	} {
		if err := ValidateFiles("darwin", "arm64", files); err == nil {
			t.Fatal("accepted destination collision")
		}
	}
}
