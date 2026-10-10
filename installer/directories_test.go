package installer

import "testing"

func TestDirectoryDefaults(t *testing.T) {
	for _, test := range []struct {
		os          string
		category    string
		environment string
		folder      string
		suffix      string
	}{
		{"linux", "bin", "", "", ".local/bin"},
		{"linux", "config", "XDG_CONFIG_HOME", "", ".config"},
		{"linux", "data", "XDG_DATA_HOME", "", ".local/share"},
		{"darwin", "bin", "", "", ".local/bin"},
		{"darwin", "config", "", "", "Library/Application Support"},
		{"darwin", "data", "", "", "Library/Application Support"},
		{"windows", "bin", "", "LocalApplicationData", `Programs\Twig\bin`},
		{"windows", "config", "", "ApplicationData", ""},
		{"windows", "data", "", "LocalApplicationData", ""},
	} {
		t.Run(test.os+"/"+test.category, func(t *testing.T) {
			for _, root := range directoryDefaults(test.os) {
				if root.Category == test.category {
					if root.Environment != test.environment || root.Folder != test.folder || root.Suffix != test.suffix {
						t.Fatalf("directory rule: %+v", root)
					}

					return
				}
			}
			t.Fatal("missing directory rule")
		})
	}
}
