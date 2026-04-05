package cli

import (
	"os"
	"runtime/debug"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/deligoez/ac/internal/output"
)

var version = "dev"

var (
	flagJSON    bool
	flagQuiet   bool
	flagNoColor bool

	printer *output.Printer
)

func NewRootCmd() *cobra.Command {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}

	root := &cobra.Command{
		Use:   "ac",
		Short: "ac -- agentic commits: hunk-based atomic commits for AI agents",
		Long:  "ac splits large diffs into precise, atomic commits by selecting specific diff hunks per commit.",
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
	}

	root.Version = version
	root.SetVersionTemplate("ac version {{.Version}}\n")

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
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
