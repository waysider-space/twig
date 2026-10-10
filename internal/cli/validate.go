package cli

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	twig "github.com/waysider-space/twig/installer"
)

func readManifest(path string) (twig.Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return twig.Manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()

	return twig.ParseManifest(file)
}

func validateCommand() *cobra.Command {
	var artifacts string
	command := &cobra.Command{
		Use: "validate MANIFEST", Short: "Validate a manifest and optionally its local archives", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := readManifest(args[0])
			if err != nil {
				return err
			}
			if artifacts != "" {
				for _, target := range manifest.Targets {
					if err := validateArtifact(cmd, artifacts, target, manifest.Limits); err != nil {
						return err
					}
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Valid manifest.")

			return nil
		},
	}
	command.Flags().StringVar(&artifacts, "artifacts", "", "Directory containing declared local artifacts")

	return command
}

func validateArtifact(cmd *cobra.Command, directory string, target twig.Target, limits twig.Limits) error {
	if target.Artifact == "" {
		return fmt.Errorf("target %s/%s needs an artifact filename for local validation", target.OS, target.Arch)
	}
	path := filepath.Join(directory, target.Artifact)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("artifact must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	for {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		n, err := file.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	wanted, _ := hex.DecodeString(target.SHA256)
	if subtle.ConstantTimeCompare(hash.Sum(nil), wanted) != 1 {
		return fmt.Errorf("artifact checksum mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if target.OS == "windows" {
		return twig.ValidateZIP(cmd.Context(), file, info.Size(), target.Arch, target.Files, limits)
	}

	return twig.ValidateTarGz(cmd.Context(), file, target.Files, limits)
}
