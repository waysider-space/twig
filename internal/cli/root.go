package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

// Run executes Twig's CLI with the supplied arguments and output streams.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	root := &cobra.Command{Use: "twig", Short: "Generate installers for existing release archives", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	root.AddCommand(validateCommand(), generateCommand())

	return root.ExecuteContext(ctx)
}
