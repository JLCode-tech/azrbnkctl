package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/doctor"
)

var (
	flagDoctorSubscription string
	flagDoctorLocation     string
	flagDoctorStrict       bool
	flagDoctorBackend      string
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Verify host tooling (az, kubectl), Azure auth, resource providers, and subscription",
	Long: `azrbnkctl doctor checks your environment before provisioning:
  - Required CLI tools on PATH (az, kubectl)
  - Valid Azure credentials (DefaultAzureCredential or az login)
  - Target Azure Subscription resolution
  - Required Azure Resource Providers registered (Compute, Network, ContainerService, ManagedIdentity)

Example:
  azrbnkctl doctor
  azrbnkctl doctor --subscription-id 00000000-0000-0000-0000-000000000000 --location eastus2 --strict
  azrbnkctl doctor --backend k8s`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().StringVar(&flagDoctorSubscription, "subscription-id", "", "Azure Subscription ID to verify")
	doctorCmd.Flags().StringVar(&flagDoctorLocation, "location", "", "Azure location/region to check")
	doctorCmd.Flags().BoolVar(&flagDoctorStrict, "strict", false, "Fail with non-zero exit if any warnings are raised")
	doctorCmd.Flags().StringVar(&flagDoctorBackend, "backend", "", "Run doctor checks against a specific backend (e.g. k8s)")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := os.Stdout

	fmt.Fprintln(out, "azrbnkctl doctor: verifying environment & prerequisites...")
	fmt.Fprintln(out)

	cfg := doctor.Config{
		SubscriptionID: flagDoctorSubscription,
		Location:       flagDoctorLocation,
		Strict:         flagDoctorStrict,
	}

	results := doctor.Run(ctx, cfg)
	if flagDoctorBackend != "" {
		results = append(results, runBackendChecks(ctx, flagDoctorBackend)...)
	}
	if c, ok := runDNSProbeCheck(ctx, ""); ok {
		results = append(results, c)
	}

	if err := doctor.PrintResults(out, results); err != nil {
		return err
	}

	fmt.Fprintln(out)
	if doctor.HasFailures(results) {
		return fmt.Errorf("doctor detected failed prerequisites — see commands above to resolve")
	}

	if flagDoctorStrict && doctor.HasWarnings(results) {
		return fmt.Errorf("doctor detected warnings with --strict enabled")
	}

	fmt.Fprintln(out, "✓ Environment ready for azrbnkctl provisioning.")
	return nil
}
