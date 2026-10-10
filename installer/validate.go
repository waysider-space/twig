package installer

import (
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
)

var componentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var authEnvPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// Validate checks a complete installer configuration before generation.
func Validate(config Config) error {
	if err := validateLimits(config.Limits); err != nil {
		return err
	}
	if config.AuthEnv != "" && !authEnvPattern.MatchString(config.AuthEnv) {
		return fmt.Errorf("auth_env must name an uppercase environment variable")
	}
	if len(config.Targets) == 0 || len(config.Targets) > 12 {
		return fmt.Errorf("specify 1 to 12 targets")
	}

	seen := make(map[string]bool)
	for _, target := range config.Targets {
		key := target.OS + "/" + target.Arch
		if seen[key] {
			return fmt.Errorf("duplicate target %s", key)
		}
		seen[key] = true
		if err := ValidateFiles(target.OS, target.Arch, target.Files); err != nil {
			return fmt.Errorf("target %s: %w", key, err)
		}

		u, err := url.Parse(target.URL)
		if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(target.URL, "\x00\r\n") || (u.Scheme != "https" && !(u.Scheme == "http" && loopback(u.Hostname()))) {
			return fmt.Errorf("target %s needs an HTTPS artifact URL", key)
		}
		digest, err := hex.DecodeString(target.SHA256)
		if err != nil || len(digest) != 32 {
			return fmt.Errorf("target %s needs a SHA-256 checksum", key)
		}
		if target.Artifact != "" && (strings.Contains(target.Artifact, "/") || !safePath(target.Artifact, target.OS)) {
			return fmt.Errorf("target %s artifact must be a simple filename", key)
		}
	}

	return nil
}

// ValidateFiles checks platform and file mappings independently of download metadata.
func ValidateFiles(os, arch string, files []File) error {
	if os != "linux" && os != "darwin" && os != "windows" {
		return fmt.Errorf("unsupported OS %q", os)
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture %q", arch)
	}
	if len(files) == 0 {
		return fmt.Errorf("specify at least one file")
	}

	paths := make([]string, 0, len(files))
	for _, file := range files {
		if !safePath(strings.TrimPrefix(file.Source, "./"), os) || !safePath(file.Path, os) {
			return fmt.Errorf("file paths must be safe relative paths")
		}
		if file.Destination != "bin" && file.Destination != "config" && file.Destination != "data" {
			return fmt.Errorf("destination must be bin, config, or data")
		}
		if policy := filePolicy(file); policy != "replace" && policy != "preserve" && policy != "error" {
			return fmt.Errorf("existing must be preserve, replace, or error")
		}
		if os == "windows" && file.Destination == "bin" && (!strings.HasSuffix(strings.ToLower(file.Source), ".exe") || !strings.HasSuffix(strings.ToLower(file.Path), ".exe")) {
			return fmt.Errorf("Windows bin files must have .exe source and destination paths")
		}

		root := file.Destination
		if os == "darwin" && root == "data" {
			root = "config"
		}
		key := root + "/" + file.Path
		if os != "linux" {
			key = strings.ToLower(key)
		}
		for _, previous := range paths {
			if key == previous || strings.HasPrefix(key, previous+"/") || strings.HasPrefix(previous, key+"/") {
				return fmt.Errorf("conflicting file destinations")
			}
		}
		paths = append(paths, key)
	}

	return nil
}

func safePath(value, os string) bool {
	if value == "" || len(value) > 1024 {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." || len(part) > 255 || !componentPattern.MatchString(part) {
			return false
		}
		if os == "windows" {
			base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
			if strings.HasSuffix(part, ".") || base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
				return false
			}
		}
	}

	return true
}

func loopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func validateLimits(limits Limits) error {
	if limits.MaxEntries < 0 || limits.MaxExpandedBytes < 0 || limits.MaxExpandedBytes == math.MaxInt64 {
		return fmt.Errorf("limits must be nonnegative and max_expanded_bytes must be less than %d", int64(math.MaxInt64))
	}

	return nil
}
