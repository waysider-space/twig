package installer

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templates embed.FS

type scriptTarget struct {
	Pattern string
	Arch    string
	URL     string
	SHA256  string
	Files   []scriptFile
	Entries string
	Roots   []scriptRoot
}

type scriptFile struct {
	Source      string
	Destination string
	Path        string
	Policy      string
	Mode        string
	Executable  bool
}

// Generate renders shell and PowerShell installers from Go configuration.
func Generate(config Config) (map[string][]byte, error) {
	if err := Validate(config); err != nil {
		return nil, err
	}
	var unix, windows []scriptTarget
	for _, target := range config.Targets {
		item := scriptTarget{Arch: target.Arch, URL: target.URL, SHA256: strings.ToLower(target.SHA256), Roots: directoryDefaults(target.OS)}
		os := "Linux"
		if target.OS == "darwin" {
			os = "Darwin"
		}
		item.Pattern = os + "/x86_64|" + os + "/amd64"
		if target.Arch == "arm64" {
			item.Pattern = os + "/arm64|" + os + "/aarch64"
		}
		var lines []string
		for _, file := range target.Files {
			mode := "600"
			if file.Destination == "bin" || file.Executable {
				mode = "755"
			}
			item.Files = append(item.Files, scriptFile{Source: file.Source, Destination: file.Destination, Path: file.Path, Policy: filePolicy(file), Mode: mode, Executable: file.Destination == "bin"})
			lines = append(lines, strings.Join([]string{file.Source, file.Destination, file.Path, filePolicy(file), mode}, " "))
		}
		item.Entries = strings.Join(lines, "\n")
		if target.OS == "windows" {
			if item.Arch == "amd64" {
				item.Arch = "x64"
			}
			windows = append(windows, item)
		} else {
			unix = append(unix, item)
		}
	}

	output := make(map[string][]byte)
	for _, kind := range []struct {
		name    string
		targets []scriptTarget
	}{{"install.sh", unix}, {"install.ps1", windows}} {
		if len(kind.targets) == 0 {
			continue
		}
		tmpl, err := template.New(kind.name+".tmpl").Funcs(template.FuncMap{"shq": shellQuote, "psq": powerShellQuote}).ParseFS(templates, "templates/"+kind.name+".tmpl", "templates/windows.cs.tmpl")
		if err != nil {
			return nil, fmt.Errorf("parse installer template: %w", err)
		}
		authExpr := "''"
		if config.AuthEnv != "" {
			authExpr = "${" + config.AuthEnv + ":-}"
		}
		data := struct {
			Targets  []scriptTarget
			AuthEnv  string
			AuthExpr string
			Roots    []scriptRoot
			Limits   Limits
		}{kind.targets, config.AuthEnv, authExpr, kind.targets[0].Roots, config.Limits}
		var script bytes.Buffer
		if err := tmpl.Execute(&script, data); err != nil {
			return nil, fmt.Errorf("render installer: %w", err)
		}
		output[kind.name] = script.Bytes()
	}

	return output, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
