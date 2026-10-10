package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	twig "github.com/waysider-space/twig/installer"
)

func generateCommand() *cobra.Command {
	var directory string
	var force bool
	command := &cobra.Command{
		Use: "generate MANIFEST", Short: "Generate shell and PowerShell installers", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := readManifest(args[0])
			if err != nil {
				return err
			}
			scripts, err := twig.Generate(manifest.Config())
			if err != nil {
				return err
			}
			for name := range scripts {
				info, err := os.Lstat(filepath.Join(directory, name))
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				if info != nil && (!force || !info.Mode().IsRegular()) {
					return fmt.Errorf("%s already exists; use --force to replace regular files", name)
				}
			}
			if err := os.MkdirAll(directory, 0o755); err != nil {
				return err
			}
			for _, name := range []string{"install.sh", "install.ps1"} {
				content, ok := scripts[name]
				if !ok {
					continue
				}
				path := filepath.Join(directory, name)
				if err := writeScript(path, content); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), path)
			}

			return nil
		},
	}
	command.Flags().StringVar(&directory, "output", "dist", "Directory for generated installers")
	command.Flags().BoolVar(&force, "force", false, "Replace existing generated scripts")

	return command
}

func writeScript(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".twig-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(content); err != nil {
		file.Close()

		return err
	}
	if err := file.Chmod(0o644); err != nil {
		file.Close()

		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return os.Rename(file.Name(), path)
}
