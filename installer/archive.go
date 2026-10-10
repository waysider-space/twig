package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
)

// ValidateTarGz checks selected regular files and gzip integrity within archive limits.
func ValidateTarGz(ctx context.Context, reader io.Reader, files []File, limits Limits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLimits(limits); err != nil {
		return err
	}
	gz, err := gzip.NewReader(&contextReader{ctx: ctx, reader: reader})
	if err != nil {
		return err
	}
	defer gz.Close()
	var expandedReader io.Reader = &contextReader{ctx: ctx, reader: gz}
	var limited *io.LimitedReader
	if limits.MaxExpandedBytes > 0 {
		limited = &io.LimitedReader{R: expandedReader, N: limits.MaxExpandedBytes + 1}
		expandedReader = limited
	}
	archive := tar.NewReader(expandedReader)
	required, err := requiredSources(files, false)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	counts := sourceCounts(files, false)
	var staged int64
	for count := 0; ; count++ {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if limits.MaxEntries > 0 && count >= limits.MaxEntries {
			return fmt.Errorf("too many archive entries")
		}
		if limits.MaxExpandedBytes > 0 && (header.Size > limits.MaxExpandedBytes || limited.N == 0) {
			return fmt.Errorf("archive exceeds expanded size limit")
		}

		key := canonicalEntry(header.Name, false)
		if source, needed := required[key]; needed {
			if seen[key] || header.Name != source {
				return fmt.Errorf("duplicate or ambiguous archive file %q", source)
			}
			if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size == 0 {
				return fmt.Errorf("archive file %q must be a nonempty regular file", source)
			}
			if limits.MaxExpandedBytes > 0 {
				if header.Size > (limits.MaxExpandedBytes-staged)/int64(counts[key]) {
					return fmt.Errorf("archive exceeds expanded size limit")
				}
				staged += header.Size * int64(counts[key])
			}
			seen[key] = true
		}
	}
	if _, err := io.Copy(io.Discard, expandedReader); err != nil {
		return err
	}
	if limited != nil && limited.N == 0 {
		return fmt.Errorf("archive exceeds expanded size limit")
	}

	return checkRequired(required, seen)
}

// ValidateZIP checks selected regular files, checksums, and Windows executable architecture.
func ValidateZIP(ctx context.Context, reader io.ReaderAt, size int64, arch string, files []File, limits Limits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLimits(limits); err != nil {
		return err
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported ZIP architecture")
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return err
	}
	if limits.MaxEntries > 0 && len(archive.File) > limits.MaxEntries {
		return fmt.Errorf("too many archive entries")
	}

	required, err := requiredSources(files, true)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	binaries := make(map[string]bool)
	for _, file := range files {
		if file.Destination == "bin" {
			binaries[canonicalEntry(file.Source, true)] = true
		}
	}
	counts := sourceCounts(files, true)
	var staged uint64
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := canonicalEntry(entry.Name, true)
		source, needed := required[key]
		if !needed {
			continue
		}
		if seen[key] || entry.Name != source {
			return fmt.Errorf("duplicate or ambiguous archive file %q", source)
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 {
			return fmt.Errorf("archive file %q must be a nonempty regular file", source)
		}
		if entry.UncompressedSize64 >= math.MaxInt64 {
			return fmt.Errorf("invalid archive file size")
		}
		if limits.MaxExpandedBytes > 0 {
			if entry.UncompressedSize64 > (uint64(limits.MaxExpandedBytes)-staged)/uint64(counts[key]) {
				return fmt.Errorf("archive exceeds expanded size limit")
			}
			staged += entry.UncompressedSize64 * uint64(counts[key])
		}
		stream, err := entry.Open()
		if err != nil {
			return err
		}
		limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: stream}, N: int64(entry.UncompressedSize64) + 1}
		if binaries[key] {
			err = validatePE(limited, arch)
		}
		if err == nil {
			_, err = io.Copy(io.Discard, limited)
		}
		closeErr := stream.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if limited.N == 0 {
			return fmt.Errorf("archive file exceeds declared size")
		}
		seen[key] = true
	}

	return checkRequired(required, seen)
}

func requiredSources(files []File, fold bool) (map[string]string, error) {
	required := make(map[string]string, len(files))
	for _, file := range files {
		key := canonicalEntry(file.Source, fold)
		if previous, exists := required[key]; exists && previous != file.Source {
			return nil, fmt.Errorf("conflicting archive source spellings %q and %q", previous, file.Source)
		}
		required[key] = file.Source
	}

	return required, nil
}

func canonicalEntry(name string, fold bool) string {
	name = path.Clean(strings.TrimLeft(strings.ReplaceAll(name, "\\", "/"), "/"))
	if fold {
		name = strings.ToLower(name)
	}

	return name
}

func checkRequired(required map[string]string, seen map[string]bool) error {
	for key, source := range required {
		if !seen[key] {
			return fmt.Errorf("archive file %q is missing", source)
		}
	}

	return nil
}

func validatePE(reader io.Reader, arch string) error {
	header := make([]byte, 64)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	if string(header[:2]) != "MZ" {
		return fmt.Errorf("missing MZ signature")
	}
	offset := int64(binary.LittleEndian.Uint32(header[60:]))
	if offset < 64 || offset > 1<<20 {
		return fmt.Errorf("invalid PE header offset")
	}
	if _, err := io.CopyN(io.Discard, reader, offset-64); err != nil {
		return err
	}
	signature := make([]byte, 6)
	if _, err := io.ReadFull(reader, signature); err != nil {
		return err
	}
	expected := uint16(0x8664)
	if arch == "arm64" {
		expected = 0xaa64
	}
	if string(signature[:4]) != "PE\x00\x00" || binary.LittleEndian.Uint16(signature[4:]) != expected {
		return fmt.Errorf("PE architecture does not match %s", arch)
	}

	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

// Read reads from the wrapped stream unless its context is canceled.
func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.reader.Read(p)
}

func sourceCounts(files []File, fold bool) map[string]int {
	counts := make(map[string]int, len(files))
	for _, file := range files {
		counts[canonicalEntry(file.Source, fold)]++
	}

	return counts
}
