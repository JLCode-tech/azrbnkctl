// Package cli wires the cobra command tree for azrbnkctl.
//
// Build-time variables (Version, Commit, BuildDate) are populated via
// -ldflags by the Makefile/goreleaser.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// Build metadata, populated via -ldflags at link time.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// Persistent flag values, bound on the root command.
var (
	flagVerbose bool
	flagQuiet   bool
	flagNoColor bool
	flagOutput  string
)

var rootCmd = &cobra.Command{
	Use:   "azrbnkctl",
	Short: "Deploy and validate F5 BIG-IP Next for Kubernetes (BNK) on Azure AKS",
	Long: `azrbnkctl deploys F5 BIG-IP Next for Kubernetes (BNK) onto Azure AKS
with Accelerated Networking, multi-subnet networking, Azure NAT Gateway,
and the full BNK install (cert-manager, FLO, CNEInstance, License,
F5SPKVlan/StaticRoutes, and egress).

  azrbnkctl validate <cluster.yaml>   Parse + validate (no Azure API calls)
  azrbnkctl up   -f <cluster.yaml>    Provision + install
  azrbnkctl down -f <cluster.yaml>    Tear down cluster and managed resources`,
	SilenceUsage: true,
}

// Execute runs the root command. Wires SIGINT (Ctrl+C) to a cancellable
// context so long-running operations like `azrbnkctl up` terminate promptly.
//
// Loads $PWD/.env at startup if present.
func Execute() {
	loadDotenv()
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(fmt.Sprintf(
		"azrbnkctl {{.Version}} (commit %s, built %s)\n", Commit, BuildDate))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "azrbnkctl: %v\n", err)
		os.Exit(1)
	}
}

// loadDotenv reads ./.env if present.
func loadDotenv() {
	if _, err := os.Stat(".env"); err != nil {
		return
	}
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "azrbnkctl: warning: parsing .env: %v\n", err)
	}
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "suppress all but errors")
	pf.BoolVar(&flagNoColor, "no-color", false, "disable colored output")
	pf.StringVarP(&flagOutput, "output", "o", "", "output format (e.g. json)")
}

// RootCommand returns the wired-up root cobra command for tooling that needs
// to walk the command tree (e.g. doc generation, tests).
func RootCommand() *cobra.Command {
	return rootCmd
}
