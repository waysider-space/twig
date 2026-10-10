package installer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func runInstaller(t *testing.T, config Config, env []string) (string, error) {
	t.Helper()
	scripts, err := Generate(config)
	if err != nil {
		t.Fatal(err)
	}
	name, shell := "install.sh", "sh"
	if runtime.GOOS == "windows" {
		name, shell = "install.ps1", "powershell.exe"
	}
	if _, err := exec.LookPath(shell); err != nil {
		t.Fatalf("installer test requires %s", shell)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, scripts[name], 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args := []string{path}
	if runtime.GOOS == "windows" {
		args = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path}
	}
	command := exec.CommandContext(ctx, shell, args...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TWIG_") && !strings.HasPrefix(value, "XDG_") && !strings.HasPrefix(value, "GROVE_TOKEN=") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, env...)
	output, err := command.CombinedOutput()

	return string(output), err
}

func installerConfig(t *testing.T, entries []archiveEntry, files []File) (Config, []string, string) {
	t.Helper()
	var data []byte
	if runtime.GOOS == "windows" {
		data = zipArchive(t, entries...)
	} else {
		data = tarArchive(t, entries...)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	digest := sha256.Sum256(data)
	config := Config{Targets: []Target{{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: server.URL + "/archive", SHA256: hex.EncodeToString(digest[:]), Files: files}}}
	root := t.TempDir()
	env := []string{"TWIG_BIN_DIR=" + filepath.Join(root, "bin space"), "TWIG_CONFIG_DIR=" + filepath.Join(root, "config space"), "TWIG_DATA_DIR=" + filepath.Join(root, "data space")}

	return config, env, root
}

func fixtureFiles() ([]archiveEntry, []File) {
	bin, body := "grove", []byte("#!/bin/sh\necho grove\n")
	if runtime.GOOS == "windows" {
		bin, body = "grove.exe", peBytes(runtime.GOARCH)
	}

	return []archiveEntry{{name: "bin/" + bin, body: body}, {name: "config.toml", body: []byte("default=true\n")}, {name: "theme.json", body: []byte("{}\n")}}, []File{{Source: "bin/" + bin, Destination: "bin", Path: bin}, {Source: "config.toml", Destination: "config", Path: "grove/config.toml"}, {Source: "theme.json", Destination: "data", Path: "grove/theme.json"}}
}

func TestInstallerDestinations(t *testing.T) {
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries, files)
	if output, err := runInstaller(t, config, env); err != nil {
		t.Fatalf("install: %v: %s", err, output)
	}
	for index, file := range files {
		path := filepath.Join(root, file.Destination+" space", filepath.FromSlash(file.Path))
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(entries[index].body) {
			t.Fatalf("%s: %q, %v", path, data, err)
		}
	}
}

func TestInstallerPreservesConfig(t *testing.T) {
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries, files)
	if output, err := runInstaller(t, config, env); err != nil {
		t.Fatalf("first install: %v: %s", err, output)
	}
	path := filepath.Join(root, "config space", "grove", "config.toml")
	if err := os.WriteFile(path, []byte("edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := runInstaller(t, config, env); err != nil {
		t.Fatalf("reinstall: %v: %s", err, output)
	}
	if data, _ := os.ReadFile(path); string(data) != "edited\n" {
		t.Fatalf("config replaced: %q", data)
	}
	config.Targets[0].Files[1].Existing = "error"
	if _, err := runInstaller(t, config, env); err == nil {
		t.Fatal("existing:error accepted an existing destination")
	}
}

func TestInstallerRejectsUnsafeEntries(t *testing.T) {
	entries, files := fixtureFiles()
	for _, bad := range []archiveEntry{{name: files[0].Source, body: entries[0].body}, {name: "./" + files[0].Source, body: entries[0].body}, {name: "other", kind: tar.TypeSymlink}} {
		badEntries := append(append([]archiveEntry{}, entries...), bad)
		if bad.kind != 0 {
			badEntries[0] = archiveEntry{name: files[0].Source, kind: tar.TypeSymlink}
		}
		config, env, root := installerConfig(t, badEntries, files)
		if output, err := runInstaller(t, config, env); err == nil {
			t.Fatalf("unsafe archive accepted: %s", output)
		}
		if _, err := os.Stat(filepath.Join(root, "bin space", files[0].Path)); !os.IsNotExist(err) {
			t.Fatal("failed validation changed a destination")
		}
	}
}

func TestInstallerRejectsRootCollisions(t *testing.T) {
	entries, _ := fixtureFiles()
	files := []File{{Source: "config.toml", Destination: "config", Path: "grove/config.toml"}, {Source: "theme.json", Destination: "data", Path: "config.toml"}}
	config, _, root := installerConfig(t, entries, files)
	env := []string{"TWIG_CONFIG_DIR=" + root, "TWIG_DATA_DIR=" + filepath.Join(root, "grove")}
	if _, err := runInstaller(t, config, env); err == nil {
		t.Fatal("aliased destination roots accepted")
	}
}

func TestInstallerRejectsLinkedDestinations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs extra Windows privileges")
	}
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries, files)
	if err := os.MkdirAll(filepath.Join(root, "config space"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "config space", "grove")); err != nil {
		t.Fatal(err)
	}
	if _, err := runInstaller(t, config, env); err == nil {
		t.Fatal("symlink descendant accepted")
	}
}

func TestInstallerAuthentication(t *testing.T) {
	entries, files := fixtureFiles()
	config, env, _ := installerConfig(t, entries, files)
	var data []byte
	if runtime.GOOS == "windows" {
		data = zipArchive(t, entries...)
	} else {
		data = tarArchive(t, entries...)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer secret-token")) != 1 {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
		w.Write(data)
	}))
	defer server.Close()
	config.AuthEnv = "GROVE_TOKEN"
	config.Targets[0].URL = server.URL
	if output, err := runInstaller(t, config, append(env, "GROVE_TOKEN=secret-token")); err != nil || strings.Contains(output, "secret-token") {
		t.Fatalf("authentication: %v: %s", err, output)
	}
	if output, err := runInstaller(t, config, append(env, "GROVE_TOKEN=invalid\nheader")); err == nil || strings.Contains(output, "invalid\nheader") {
		t.Fatalf("invalid token accepted/exposed: %v: %s", err, output)
	}
}

func TestInstallerFollowsRedirectsWithoutLeakingCredentials(t *testing.T) {
	for _, test := range []struct {
		name string
		auth bool
	}{
		{name: "public"},
		{name: "authenticated", auth: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			testInstallerRedirect(t, test.auth)
		})
	}
}

func testInstallerRedirect(t *testing.T, auth bool) {
	t.Helper()
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries, files)
	var data []byte
	if runtime.GOOS == "windows" {
		data = zipArchive(t, entries...)
	} else {
		data = tarArchive(t, entries...)
	}
	var reached, leaked atomic.Bool
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Twig-Config") != "" {
			leaked.Store(true)
		}
		w.Write(data)
	}))
	defer storage.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth && r.URL.Path == "/archive" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer secret-token")) != 1 {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
		if r.URL.Path == "/archive" {
			http.Redirect(w, r, "/storage", http.StatusTemporaryRedirect)

			return
		}
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		http.Redirect(w, r, storage.URL, http.StatusFound)
	}))
	defer redirect.Close()
	digest := sha256.Sum256(data)
	config.Targets[0].URL = redirect.URL + "/archive"
	config.Targets[0].SHA256 = hex.EncodeToString(digest[:])
	if auth {
		config.AuthEnv = "GROVE_TOKEN"
		env = append(env, "GROVE_TOKEN=secret-token")
	}
	if output, err := runInstaller(t, config, env); err != nil || strings.Contains(output, "secret-token") || !reached.Load() || leaked.Load() {
		t.Fatalf("redirect download: %v, reached=%t, leaked=%t: %s", err, reached.Load(), leaked.Load(), output)
	}
	installed, err := os.ReadFile(filepath.Join(root, "bin space", files[0].Path))
	if err != nil || !bytes.Equal(installed, entries[0].body) {
		t.Fatalf("installed file: %v", err)
	}
}

func TestInstallerRejectsRedirectLoops(t *testing.T) {
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries, files)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "/archive", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	config.Targets[0].URL = server.URL + "/archive"
	if output, err := runInstaller(t, config, env); err == nil || !strings.Contains(output, "Artifact download failed") {
		t.Fatalf("redirect loop accepted: %v: %s", err, output)
	}
	if requests.Load() != 6 {
		t.Fatalf("redirect requests = %d, want 6", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(root, "bin space", files[0].Path)); !os.IsNotExist(err) {
		t.Fatal("redirect failure changed a destination")
	}
}

func TestInstallerRejectsRedirectProtocols(t *testing.T) {
	entries, files := fixtureFiles()
	config, env, _ := installerConfig(t, entries, files)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///missing/archive", http.StatusFound)
	}))
	defer server.Close()
	config.Targets[0].URL = server.URL
	if output, err := runInstaller(t, config, env); err == nil || !strings.Contains(output, "Artifact download failed") {
		t.Fatalf("unsupported redirect protocol: %v: %s", err, output)
	}
}

func TestInstallerRejectsHTTPSDowngrades(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix curl certificate bundle")
	}
	entries, files := fixtureFiles()
	config, env, _ := installerConfig(t, entries, files)
	var reached atomic.Bool
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
	}))
	defer storage.Close()
	var redirected atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		http.Redirect(w, r, storage.URL, http.StatusFound)
	}))
	defer server.Close()
	certificate := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	env = append(env, "CURL_CA_BUNDLE="+certificate)
	config.Targets[0].URL = server.URL
	if output, err := runInstaller(t, config, env); err == nil || !redirected.Load() || reached.Load() || !strings.Contains(output, "Artifact download failed") {
		t.Fatalf("HTTPS downgrade: %v, redirected=%t, reached=%t: %s", err, redirected.Load(), reached.Load(), output)
	}
}

func TestInstallerChecksumMismatch(t *testing.T) {
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries, files)
	config.Targets[0].SHA256 = strings.Repeat("0", 64)
	if output, err := runInstaller(t, config, env); err == nil || !strings.Contains(output, "checksum mismatch") {
		t.Fatalf("checksum failure: %v: %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "bin space", files[0].Path)); !os.IsNotExist(err) {
		t.Fatal("checksum failure changed a destination")
	}
}

func TestInstallerConfigOnly(t *testing.T) {
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries[1:2], files[1:2])
	if output, err := runInstaller(t, config, env); err != nil {
		t.Fatalf("config-only install: %v: %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "bin space")); !os.IsNotExist(err) {
		t.Fatal("config-only install created a bin root")
	}
}

func TestInstallerUnixDefaults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix destination defaults")
	}
	entries, files := fixtureFiles()
	config, _, root := installerConfig(t, entries, files)
	env := []string{"HOME=" + root, "XDG_CONFIG_HOME=relative", "XDG_DATA_HOME=relative"}
	if output, err := runInstaller(t, config, env); err != nil {
		t.Fatalf("default-root install: %v: %s", err, output)
	}
	configRoot, dataRoot := filepath.Join(root, ".config"), filepath.Join(root, ".local/share")
	if runtime.GOOS == "darwin" {
		configRoot, dataRoot = filepath.Join(root, "Library/Application Support"), filepath.Join(root, "Library/Application Support")
	}
	for _, path := range []string{filepath.Join(root, ".local/bin", files[0].Path), filepath.Join(configRoot, files[1].Path), filepath.Join(dataRoot, files[2].Path)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	configInfo, _ := os.Stat(filepath.Join(configRoot, files[1].Path))
	binInfo, _ := os.Stat(filepath.Join(root, ".local/bin", files[0].Path))
	if configInfo.Mode().Perm() != 0o600 || binInfo.Mode().Perm() != 0o755 {
		t.Fatal("incorrect config or executable permissions")
	}
}

func TestInstallerOverridesWithoutHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix HOME environment")
	}
	entries, files := fixtureFiles()
	config, env, _ := installerConfig(t, entries, files)
	if output, err := runInstaller(t, config, append(env, "HOME=")); err != nil {
		t.Fatalf("explicit roots require HOME: %v: %s", err, output)
	}
}

func TestInstallerLimits(t *testing.T) {
	entries := []archiveEntry{{name: "config.toml", body: bytes.Repeat([]byte("a"), 6000)}, {name: "theme.json", body: []byte("{}")}}
	files := []File{{Source: "config.toml", Destination: "config", Path: "grove/config.toml"}, {Source: "config.toml", Destination: "data", Path: "grove/copy.toml"}}
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
		t.Run(test.name, func(t *testing.T) {
			config, env, root := installerConfig(t, entries, files)
			config.Limits = test.limits
			output, err := runInstaller(t, config, env)
			if (err != nil) != test.wantErr {
				t.Fatalf("installation: %v: %s", err, output)
			}
			if test.wantErr {
				if _, err := os.Stat(filepath.Join(root, "config space", "grove", "config.toml")); !os.IsNotExist(err) {
					t.Fatal("limit failure changed a destination")
				}
			}
		})
	}
}

func TestInstallerWindowsZIPChecksum(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell ZIP validation")
	}
	entries, files := fixtureFiles()
	data := zipArchive(t, entries...)
	index := bytes.Index(data, []byte{'P', 'K', 1, 2})
	if index < 0 {
		t.Fatal("missing ZIP directory")
	}
	binary.LittleEndian.PutUint32(data[index+16:], binary.LittleEndian.Uint32(data[index+16:])^1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer server.Close()
	config, env, root := installerConfig(t, entries, files)
	digest := sha256.Sum256(data)
	config.Targets[0].URL = server.URL
	config.Targets[0].SHA256 = hex.EncodeToString(digest[:])
	if output, err := runInstaller(t, config, env); err == nil || !strings.Contains(output, "checksum mismatch") {
		t.Fatalf("corrupt ZIP accepted: %v: %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "bin space", files[0].Path)); !os.IsNotExist(err) {
		t.Fatal("ZIP validation failure changed a destination")
	}
}

func TestInstallerWindowsDriveAlias(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows DOS drive mappings")
	}
	entries, _ := fixtureFiles()
	files := []File{{Source: "config.toml", Destination: "config", Path: "grove/config.toml"}, {Source: "theme.json", Destination: "data", Path: "grove/config.toml"}}
	config, _, root := installerConfig(t, entries, files)
	drive := ""
	for _, letter := range "ZYXWVUT" {
		candidate := string(letter) + ":"
		if _, err := os.Stat(candidate + `\`); os.IsNotExist(err) {
			drive = candidate

			break
		}
	}
	if drive == "" {
		t.Fatal("no free drive for alias test")
	}
	if output, err := exec.Command("subst", drive, root).CombinedOutput(); err != nil {
		t.Fatalf("subst: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if output, err := exec.Command("subst", drive, "/D").CombinedOutput(); err != nil {
			t.Errorf("subst cleanup: %v: %s", err, output)
		}
	})
	path := filepath.Join(root, "grove", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"TWIG_CONFIG_DIR=" + drive + `\`, "TWIG_DATA_DIR=" + root}
	if output, err := runInstaller(t, config, env); err == nil || !strings.Contains(output, "Conflicting installation destinations") {
		t.Fatalf("drive alias accepted: %v: %s", err, output)
	}
	if data, _ := os.ReadFile(path); string(data) != "edited" {
		t.Fatalf("preserved config replaced: %q", data)
	}
}

func TestInstallerWindowsJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows directory junctions")
	}
	entries, files := fixtureFiles()
	config, env, root := installerConfig(t, entries, files[1:])
	path := filepath.Join(root, "config space", "grove", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(root, "data space")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	junction := filepath.Join(dataRoot, "grove")
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Fatalf("junction: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if err := os.Remove(junction); err != nil {
			t.Error(err)
		}
	})
	if output, err := runInstaller(t, config, env); err == nil || !strings.Contains(output, "Linked installation paths") {
		t.Fatalf("junction accepted: %v: %s", err, output)
	}
	if data, _ := os.ReadFile(path); string(data) != "edited" {
		t.Fatalf("config changed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(outside, "theme.json")); !os.IsNotExist(err) {
		t.Fatal("junction escaped destination root")
	}
}

func TestInstallerRejectsLineBreakRoots(t *testing.T) {
	for _, test := range []struct {
		name      string
		separator string
	}{
		{name: "LF", separator: "\n"},
		{name: "CR", separator: "\r"},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries, files := fixtureFiles()
			config, env, root := installerConfig(t, entries, files)
			path := filepath.Join(root, "config"+test.separator+"space")
			env = append(env, "TWIG_CONFIG_DIR="+path)
			if output, err := runInstaller(t, config, env); err == nil {
				t.Fatalf("accepted line break in root: %s", output)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("created invalid installation root")
			}
		})
	}
}

func TestInstallerIgnoresCurlConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix curl configuration")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".curlrc"), []byte("location-trusted\nheader = \"X-Twig-Config: loaded\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURL_HOME", directory)
	testInstallerRedirect(t, true)
}
