package installer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"testing"
)

type archiveEntry struct {
	name string
	body []byte
	kind byte
}

func tarArchive(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	w := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		if err := w.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o755, Typeflag: kind, Size: int64(len(entry.body)), Linkname: "elsewhere"}); err != nil {
			t.Fatal(err)
		}
		if len(entry.body) > 0 {
			if _, err := w.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return output.Bytes()
}

func zipArchive(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		h.SetMode(0o644)
		if entry.kind == tar.TypeSymlink {
			h.SetMode(0o777 | 1<<27)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return output.Bytes()
}

func peBytes(arch string) []byte {
	data := make([]byte, 70)
	copy(data, "MZ")
	binary.LittleEndian.PutUint32(data[60:], 64)
	copy(data[64:], "PE\x00\x00")
	machine := uint16(0x8664)
	if arch == "arm64" {
		machine = 0xaa64
	}
	binary.LittleEndian.PutUint16(data[68:], machine)

	return data
}

func TestValidateTarGzSelectedFiles(t *testing.T) {
	files := []File{{Source: "bin/grove", Destination: "bin", Path: "grove"}, {Source: "config.toml", Destination: "config", Path: "grove/config.toml"}}
	good := []archiveEntry{{name: "bin/grove", body: []byte("binary")}, {name: "config.toml", body: []byte("config")}}
	if err := ValidateTarGz(context.Background(), bytes.NewReader(tarArchive(t, good...)), files, Limits{}); err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]archiveEntry{
		good[:1],
		append(append([]archiveEntry{}, good...), good[0]),
		append(append([]archiveEntry{}, good...), archiveEntry{name: "./bin/grove", body: []byte("alias")}),
		{{name: "bin/grove", kind: tar.TypeSymlink}, good[1]},
		{{name: "bin/grove"}, good[1]},
	} {
		if err := ValidateTarGz(context.Background(), bytes.NewReader(tarArchive(t, entries...)), files, Limits{}); err == nil {
			t.Fatal("accepted unsafe archive")
		}
	}

	files = append(files, File{Source: "bin/grove", Destination: "data", Path: "grove/copy"})
	if err := ValidateTarGz(context.Background(), bytes.NewReader(tarArchive(t, good...)), files, Limits{}); err != nil {
		t.Fatalf("shared source: %v", err)
	}
	corrupt := tarArchive(t, good...)
	corrupt[len(corrupt)-8] ^= 1
	if err := ValidateTarGz(context.Background(), bytes.NewReader(corrupt), files, Limits{}); err == nil {
		t.Fatal("accepted corrupt gzip")
	}
}

func TestValidateZIPSelectedFiles(t *testing.T) {
	files := []File{{Source: "grove.exe", Destination: "bin", Path: "grove.exe"}, {Source: "config.toml", Destination: "config", Path: "grove/config.toml"}}
	good := []archiveEntry{{name: "grove.exe", body: peBytes("amd64")}, {name: "config.toml", body: []byte("config")}}
	data := zipArchive(t, good...)
	if err := ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "amd64", files, Limits{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "arm64", files, Limits{}); err == nil {
		t.Fatal("accepted wrong PE architecture")
	}
	for _, entries := range [][]archiveEntry{good[:1], append(append([]archiveEntry{}, good...), good[0]), append(append([]archiveEntry{}, good...), archiveEntry{name: "GROVE.EXE", body: peBytes("amd64")}), {{name: "grove.exe", body: peBytes("amd64"), kind: tar.TypeSymlink}, good[1]}} {
		data := zipArchive(t, entries...)
		if err := ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "amd64", files, Limits{}); err == nil {
			t.Fatal("accepted unsafe ZIP")
		}
	}
}

func TestArchiveValidationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateTarGz(ctx, bytes.NewReader(nil), nil, Limits{}); err != context.Canceled {
		t.Fatalf("tar cancellation: %v", err)
	}
	if err := ValidateZIP(ctx, bytes.NewReader(nil), 0, "amd64", nil, Limits{}); err != context.Canceled {
		t.Fatalf("ZIP cancellation: %v", err)
	}
}

func TestArchiveValidationRejectsConflictingSourceSpellings(t *testing.T) {
	for _, sources := range [][]string{{"./config.toml", "config.toml"}, {"CONFIG.TOML", "config.toml"}} {
		files := []File{{Source: sources[0], Destination: "config", Path: "grove/first"}, {Source: sources[1], Destination: "data", Path: "grove/second"}}
		entries := []archiveEntry{{name: "config.toml", body: []byte("config")}}
		data := zipArchive(t, entries...)
		if err := ValidateZIP(context.Background(), bytes.NewReader(data), int64(len(data)), "amd64", files, Limits{}); err == nil {
			t.Fatal("ZIP accepted conflicting source spellings")
		}
		if sources[0][0] == '.' {
			if err := ValidateTarGz(context.Background(), bytes.NewReader(tarArchive(t, entries...)), files, Limits{}); err == nil {
				t.Fatal("tar accepted conflicting source spellings")
			}
		}
	}
}
