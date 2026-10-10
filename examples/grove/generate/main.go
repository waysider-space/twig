package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/waysider-space/twig/installer"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "serve" {
		fmt.Println("Serving Grove at http://127.0.0.1:8090")
		if err := http.ListenAndServe("127.0.0.1:8090", http.FileServer(http.Dir("dist/grove"))); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		return
	}
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	if err := os.MkdirAll("dist/grove", 0o755); err != nil {
		return err
	}
	manifest := installer.Manifest{Schema: 1, Name: "grove", Version: "1.0.0"}
	for _, platform := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			binary := "grove"
			if platform == "windows" {
				binary += ".exe"
			}
			path := filepath.Join("dist/grove", "grove-"+platform+"-"+arch)
			command := exec.Command("go", "build", "-o", path, "./examples/grove/app")
			command.Env = append(os.Environ(), "GOOS="+platform, "GOARCH="+arch, "CGO_ENABLED=0")
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("build Grove: %w: %s", err, output)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			files := []installer.File{{Source: "bin/" + binary, Destination: "bin", Path: binary}, {Source: "defaults/config.toml", Destination: "config", Path: "grove/config.toml"}, {Source: "assets/theme.json", Destination: "data", Path: "grove/theme.json"}}
			contents := [][]byte{body, []byte("theme = \"default\"\n"), []byte("{\"name\":\"default\"}\n")}
			archive, extension, err := packageArchive(platform, files, contents)
			if err != nil {
				return err
			}
			name := "grove-" + platform + "-" + arch + extension
			if err := os.WriteFile(filepath.Join("dist/grove", name), archive, 0o644); err != nil {
				return err
			}
			digest := sha256.Sum256(archive)
			manifest.Targets = append(manifest.Targets, installer.Target{OS: platform, Arch: arch, URL: "http://127.0.0.1:8090/" + name, SHA256: hex.EncodeToString(digest[:]), Artifact: name, Files: files})
		}
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}

	return os.WriteFile("dist/grove/twig.yml", data, 0o644)
}

func packageArchive(platform string, files []installer.File, contents [][]byte) ([]byte, string, error) {
	var output bytes.Buffer
	if platform == "windows" {
		archive := zip.NewWriter(&output)
		for index, file := range files {
			writer, err := archive.Create(file.Source)
			if err != nil {
				return nil, "", err
			}
			if _, err := writer.Write(contents[index]); err != nil {
				return nil, "", err
			}
		}
		if err := archive.Close(); err != nil {
			return nil, "", err
		}

		return output.Bytes(), ".zip", nil
	}

	gz := gzip.NewWriter(&output)
	archive := tar.NewWriter(gz)
	for index, file := range files {
		if err := archive.WriteHeader(&tar.Header{Name: file.Source, Mode: 0o644, Size: int64(len(contents[index]))}); err != nil {
			return nil, "", err
		}
		if _, err := archive.Write(contents[index]); err != nil {
			return nil, "", err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, "", err
	}

	return output.Bytes(), ".tar.gz", nil
}
