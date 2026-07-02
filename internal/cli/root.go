package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/deligoez/hc/internal/output"
)

var version = "dev"

var (
	flagJSON    bool
	flagQuiet   bool
	flagNoColor bool

	printer *output.Printer
)

// exitError signals a specific process exit code from a command's RunE.
// The error itself has already been printed by the time it is returned;
// Execute only translates it into os.Exit.
type exitError struct {
	code int
}

func (e *exitError) Error() string {
	return fmt.Sprintf("exit code %d", e.code)
}

func NewRootCmd() *cobra.Command {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}

	root := &cobra.Command{
		Use:   "hc",
		Short: "hc -- hunk commits: hunk-based atomic commits for AI agents",
		Long:  "hc splits large diffs into precise, atomic commits by selecting specific diff hunks per commit.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			printer = output.NewPrinter()
			printer.ForceJSON = flagJSON
			printer.Quiet = flagQuiet
			printer.NoColor = flagNoColor

			if flagNoColor || os.Getenv("NO_COLOR") != "" {
				color.NoColor = true
				printer.NoColor = true
			}
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.Version = version
	root.SetVersionTemplate("hc version {{.Version}}\n")

	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "Force JSON output")
	root.PersistentFlags().BoolVar(&flagQuiet, "quiet", false, "Suppress info messages")
	root.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable colors")

	root.AddCommand(newDiffCmd())
	root.AddCommand(newRunCmd())

	return root
}

// Execute runs the root command.
func Execute() {
	root := NewRootCmd()
	err := root.Execute()
	if err == nil {
		return
	}

	var ee *exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}

	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
