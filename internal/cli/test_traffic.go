package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/jumphost"
)

var (
	flagTrafficConfig     string
	flagTrafficVIP        string
	flagTrafficIterations int
	flagTrafficTimeout    time.Duration
	flagTrafficInstanceID string
	flagTrafficRegion     string
	flagTrafficUser       string
	flagTrafficHost       string
	flagTrafficDryRun     bool
)

var testTrafficCmd = &cobra.Command{
	Use:   "traffic",
	Short: "Run HTTP probes from the jumphost or in-cluster probe pod to a BNK Gateway VIP",
	Long: `azrbnkctl test traffic executes curl probes against a BNK Gateway VIP
from the cluster's jumphost or in-cluster test-probe pod.

  azrbnkctl test traffic -f cluster.yaml [--vip 10.0.17.100] [--iterations 5] [--dry-run]`,
	RunE: runTestTraffic,
}

func init() {
	f := testTrafficCmd.Flags()
	f.StringVarP(&flagTrafficConfig, "config", "f", "", "path to cluster.yaml")
	f.StringVar(&flagTrafficVIP, "vip", "", "Gateway VIP to probe (defaults to cluster's DefaultVIP)")
	f.IntVar(&flagTrafficIterations, "iterations", 5, "number of curl probes to run")
	f.DurationVar(&flagTrafficTimeout, "timeout", 10*time.Second, "per-curl timeout")
	f.StringVar(&flagTrafficInstanceID, "instance", "", "override jumphost VM name or resource ID from state")
	f.StringVar(&flagTrafficRegion, "region", "", "override Azure location/region from config/state")
	f.StringVar(&flagTrafficUser, "user", jumphost.DefaultSSHUser, "SSH user on the jumphost")
	f.StringVar(&flagTrafficHost, "host", "", "optional HTTP Host header for hostname-scoped HTTPRoutes")
	f.BoolVar(&flagTrafficDryRun, "dry-run", false, "print the probe plan without executing SSH or curl")

	testCmd.AddCommand(testTrafficCmd)
}

type trafficReport struct {
	Cluster       string                 `json:"cluster,omitempty"`
	Region        string                 `json:"region"`
	ResourceGroup string                 `json:"resource_group,omitempty"`
	InstanceID    string                 `json:"instance_id"`
	SourceIP      string                 `json:"source_ip"`
	VIP           string                 `json:"vip"`
	Host          string                 `json:"host,omitempty"`
	Iterations    int                    `json:"iterations"`
	DryRun        bool                   `json:"dry_run,omitempty"`
	Results       []jumphost.ProbeResult `json:"results,omitempty"`
	Passed        int                    `json:"passed"`
	Failed        int                    `json:"failed"`
	OK            bool                   `json:"ok"`
}

// runCurlProbesFn is replaceable in tests.
var runCurlProbesFn = jumphost.RunCurlProbes

func runTestTraffic(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var cl *intent.Cluster
	var st *state.State

	if flagTrafficConfig != "" {
		loaded, err := intent.Load(flagTrafficConfig)
		if err != nil {
			return fmt.Errorf("test traffic: %w", err)
		}
		cl = loaded
		st, err = state.Load(cl.StateDir())
		if err != nil {
			return fmt.Errorf("test traffic: loading state: %w", err)
		}
	} else if _, err := os.Stat("cluster.yaml"); err == nil {
		if loaded, lerr := intent.Load("cluster.yaml"); lerr == nil {
			cl = loaded
			st, _ = state.Load(cl.StateDir())
		}
	}

	instanceID := flagTrafficInstanceID
	if instanceID == "" && st != nil {
		instanceID = st.Get("JUMPHOST_INSTANCE_NAME")
		if instanceID == "" {
			instanceID = st.Get("JUMPHOST_INSTANCE_ID")
		}
	}
	if instanceID == "" && os.Getenv("AZRBNKCTL_FORCE_SSH") != "1" && (os.Getenv("AZRBNKCTL_PROBE_IN_CLUSTER") != "" || jumphost.IsProbePodRunning(ctx)) {
		instanceID = jumphost.DefaultProbePod
	}
	if instanceID == "" && flagTrafficDryRun && cl != nil {
		instanceID = cl.Metadata.Name + "-jumphost"
	}
	if instanceID == "" {
		return errors.New("test traffic: jumphost instance not found (pass --instance, ensure JUMPHOST_INSTANCE_ID is in state.env, or run a test-probe pod)")
	}

	region := flagTrafficRegion
	if region == "" && cl != nil {
		region = cl.Metadata.Location
	}
	if region == "" && st != nil {
		region = st.Get("LOCATION")
		if region == "" {
			region = st.Get("REGION")
		}
	}

	rg := ""
	if cl != nil {
		rg = cl.Derived.ResourceGroupName
		if rg == "" {
			rg = cl.Metadata.ResourceGroup
		}
	}
	if rg == "" && st != nil {
		rg = st.Get("AZURE_RESOURCE_GROUP")
	}

	sourceIP := ""
	if st != nil {
		sourceIP = st.Get("JUMPHOST_BNK_EXT_ENI_IP")
		if sourceIP == "" {
			sourceIP = st.Get("JUMPHOST_BNK_EXT_NIC_IP")
		}
		if sourceIP == "" {
			sourceIP = st.Get("JUMPHOST_IP")
		}
	}

	vip := flagTrafficVIP
	if vip == "" && cl != nil {
		if dv, err := cl.DefaultVIP(); err == nil {
			vip = dv
		}
	}
	if vip == "" {
		return errors.New("test traffic: --vip is required when not resolvable from cluster.yaml")
	}

	iterations := flagTrafficIterations
	if iterations <= 0 {
		iterations = 5
	}

	clusterName := ""
	if cl != nil {
		clusterName = cl.Metadata.Name
	}

	rep := trafficReport{
		Cluster:       clusterName,
		Region:        region,
		ResourceGroup: rg,
		InstanceID:    instanceID,
		SourceIP:      sourceIP,
		VIP:           vip,
		Host:          flagTrafficHost,
		Iterations:    iterations,
		DryRun:        flagTrafficDryRun,
	}

	if flagTrafficDryRun {
		rep.OK = true
		if flagOutput == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		curlCmd := jumphost.BuildCurlCmd(sourceIP, vip, flagTrafficHost, int(flagTrafficTimeout.Seconds()))
		fmt.Fprintf(os.Stdout, "→ dry-run: test traffic plan\n")
		fmt.Fprintf(os.Stdout, "  instance:   %s (region=%s, rg=%s)\n", instanceID, region, rg)
		fmt.Fprintf(os.Stdout, "  source IP:  %s\n", sourceIP)
		fmt.Fprintf(os.Stdout, "  target VIP: %s (iterations=%d)\n", vip, iterations)
		fmt.Fprintf(os.Stdout, "  remote cmd: %s\n", curlCmd)
		return nil
	}

	opts := jumphost.ProbeOptions{
		Location:   region,
		Region:     region,
		VMName:     instanceID,
		InstanceID: instanceID,
		SourceIP:   sourceIP,
		VIP:        vip,
		Iterations: iterations,
		Timeout:    flagTrafficTimeout,
		User:       flagTrafficUser,
		Hostname:   flagTrafficHost,
	}

	results, err := runCurlProbesFn(ctx, opts)
	if err != nil {
		return fmt.Errorf("test traffic: %w", err)
	}
	rep.Results = results
	for _, r := range results {
		if r.Err == "" && r.HTTPCode >= 200 && r.HTTPCode < 400 {
			rep.Passed++
		} else {
			rep.Failed++
		}
	}
	rep.OK = rep.Failed == 0 && rep.Passed > 0

	if flagOutput == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(rep); encErr != nil {
			return encErr
		}
		if !rep.OK {
			return fmt.Errorf("traffic probe failed: %d/%d probes succeeded", rep.Passed, rep.Iterations)
		}
		return nil
	}

	for _, r := range results {
		if r.Err != "" {
			fmt.Fprintf(os.Stdout, "  [%d/%d] FAIL (code=%d, %.3fs): %s\n", r.Iteration, iterations, r.HTTPCode, r.Seconds, r.Err)
		} else {
			fmt.Fprintf(os.Stdout, "  [%d/%d] HTTP %d (%.3fs)\n", r.Iteration, iterations, r.HTTPCode, r.Seconds)
		}
	}
	fmt.Fprintf(os.Stdout, "Summary: %d/%d succeeded (vip=%s)\n", rep.Passed, iterations, vip)
	if !rep.OK {
		return fmt.Errorf("traffic probe failed: %d/%d probes succeeded", rep.Passed, rep.Iterations)
	}
	return nil
}
