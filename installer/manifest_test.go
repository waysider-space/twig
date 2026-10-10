package installer

import (
	"strings"
	"testing"
)

func TestParseManifestStrict(t *testing.T) {
	data := "schema: 1\nname: grove\nversion: 1.2.3\ntargets:\n  - os: linux\n    arch: amd64\n    url: https://example.com/grove.tar.gz\n    sha256: " + strings.Repeat("a", 64) + "\n    files:\n      - source: bin/grove\n        destination: bin\n        path: grove\n"
	manifest, err := ParseManifest(strings.NewReader(data))
	if err != nil || manifest.Name != "grove" || len(manifest.Config().Targets) != 1 {
		t.Fatalf("parse: %v, %v", manifest, err)
	}

	for _, invalid := range []string{data + "unknown: true\n", data + "name: other\n", data + "---\nschema: 1\n", strings.Replace(data, "schema: 1", "schema: 2", 1)} {
		if _, err := ParseManifest(strings.NewReader(invalid)); err == nil {
			t.Fatal("accepted invalid manifest")
		}
	}
}
