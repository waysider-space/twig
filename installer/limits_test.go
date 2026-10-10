package installer

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
)

func TestOptionalArchiveLimits(t *testing.T) {
	entries := []archiveEntry{{name: "config.toml", body: bytes.Repeat([]byte("a"), 6000)}, {name: "theme.json", body: []byte("{}")}}
	files := []File{{Source: "config.toml", Destination: "config", Path: "grove/config.toml"}, {Source: "config.toml", Destination: "data", Path: "grove/copy.toml"}}
	for _, format := range []string{"tar", "zip"} {
		for _, test := range []struct {
			name    string
			limits  Limits
			wantErr bool
		}{
			{name: "unlimited"},
			{name: "entry limit", limits: Limits{MaxEntries: 1}, wantErr: true},
			{name: "expanded limit", limits: Limits{MaxExpandedBytes: 1000}, wantErr: true},
			{name: "repeated source", limits: Limits{MaxExpandedBytes: 9000}, wantErr: true},
			{name: "within limits", limits: Limits{MaxEntries: 2, MaxExpandedBytes: 20000}},
		} {
			t.Run(format+"/"+test.name, func(t *testing.T) {
				var err error
				if format == "tar" {
					err = ValidateTarGz(context.Background(), bytes.NewReader(tarArchive(t, entries...)), files, test.limits)
				} else {
					data := zipArchive(t, entries...)
					err = ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "amd64", files, test.limits)
				}
				if (err != nil) != test.wantErr {
					t.Fatalf("validation: %v, want error %t", err, test.wantErr)
				}
			})
		}
	}
}

func TestLimitsValidation(t *testing.T) {
	for _, limits := range []Limits{{MaxEntries: -1}, {MaxExpandedBytes: -1}, {MaxExpandedBytes: 1<<63 - 1}} {
		config := validConfig()
		config.Limits = limits
		if err := Validate(config); err == nil {
			t.Fatalf("accepted invalid limits: %+v", limits)
		}
	}
}

func TestManifestLimits(t *testing.T) {
	data := "schema: 1\nname: grove\nversion: 1.0.0\nlimits:\n  max_entries: 50\n  max_expanded_bytes: 1048576\ntargets:\n  - os: linux\n    arch: amd64\n    url: https://example.com/grove.tar.gz\n    sha256: " + strings.Repeat("a", 64) + "\n    files:\n      - source: grove\n        destination: bin\n        path: grove\n"
	manifest, err := ParseManifest(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Config().Limits != (Limits{MaxEntries: 50, MaxExpandedBytes: 1048576}) {
		t.Fatalf("limits lost: %+v", manifest.Config().Limits)
	}
}

func TestExactArchiveByteLimits(t *testing.T) {
	entries := []archiveEntry{{name: "config.toml", body: []byte("config")}}
	files := []File{{Source: "config.toml", Destination: "config", Path: "grove/config.toml"}}
	data := tarArchive(t, entries...)
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTarGz(context.Background(), bytes.NewReader(data), files, Limits{MaxExpandedBytes: int64(len(raw))}); err != nil {
		t.Fatalf("exact tar limit: %v", err)
	}
	if err := ValidateTarGz(context.Background(), bytes.NewReader(data), files, Limits{MaxExpandedBytes: int64(len(raw) - 1)}); err == nil {
		t.Fatal("accepted tar one byte over limit")
	}
	data = zipArchive(t, entries...)
	if err := ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "amd64", files, Limits{MaxExpandedBytes: 6}); err != nil {
		t.Fatalf("exact ZIP limit: %v", err)
	}
	if err := ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "amd64", files, Limits{MaxExpandedBytes: 5}); err == nil {
		t.Fatal("accepted ZIP one byte over limit")
	}
}

func TestUnlimitedArchiveEntries(t *testing.T) {
	entries := make([]archiveEntry, 10001)
	for index := range entries {
		entries[index] = archiveEntry{name: "unused", body: []byte("a")}
	}
	entries = append(entries, archiveEntry{name: "config.toml", body: []byte("config")})
	files := []File{{Source: "config.toml", Destination: "config", Path: "grove/config.toml"}}
	if err := ValidateTarGz(context.Background(), bytes.NewReader(tarArchive(t, entries...)), files, Limits{}); err != nil {
		t.Fatalf("implicit tar entry limit: %v", err)
	}
	data := zipArchive(t, entries...)
	if err := ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "amd64", files, Limits{}); err != nil {
		t.Fatalf("implicit ZIP entry limit: %v", err)
	}
	config, env, _ := installerConfig(t, entries, files)
	if output, err := runInstaller(t, config, env); err != nil {
		t.Fatalf("implicit installer entry limit: %v: %s", err, output)
	}
}
