package cli

import (
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/deligoez/ac/internal/output"
)

var (
	flagJSON    bool
	flagQuiet   bool
	flagNoColor bool

	printer *output.Printer
)

func NewRootCmd() *cobra.Command {
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

	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "Force JSON output")
	root.PersistentFlags().BoolVar(&flagQuiet, "quiet", false, "Suppress info messages")
	root.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable colors")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newRunCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("ac version 0.1.0")
		},
	}
}

// Execute runs the root command.
func Execute() {
	root := NewRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
