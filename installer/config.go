package installer

type Config struct {
	AuthEnv string
	Limits  Limits
	Targets []Target
}

type Limits struct {
	MaxEntries       int   `yaml:"max_entries,omitempty"`
	MaxExpandedBytes int64 `yaml:"max_expanded_bytes,omitempty"`
}

type Target struct {
	OS       string `yaml:"os"`
	Arch     string `yaml:"arch"`
	URL      string `yaml:"url"`
	SHA256   string `yaml:"sha256"`
	Artifact string `yaml:"artifact,omitempty"`
	Files    []File `yaml:"files"`
}

type File struct {
	Source      string `json:"source" yaml:"source"`
	Destination string `json:"destination" yaml:"destination"`
	Path        string `json:"path" yaml:"path"`
	Existing    string `json:"existing,omitempty" yaml:"existing,omitempty"`
	Executable  bool   `json:"executable,omitempty" yaml:"executable,omitempty"`
}

func filePolicy(file File) string {
	if file.Existing != "" {
		return file.Existing
	}
	if file.Destination == "config" {
		return "preserve"
	}

	return "replace"
}
