package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/forge"
)

// Flags of forge telemetry.
var (
	flagForgeTelemetryLoki  string
	flagForgeTelemetryQuery string
	flagForgeTelemetrySince time.Duration
	flagForgeTelemetryLimit int
	flagForgeTelemetryFile  string
	flagForgeTelemetryShow  int
)

var forgeTelemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Check the MCP governance records BNK ships to Loki against Forge's LLM Observability schema",
	Long: `forge telemetry reads the governance stream the agentfoundry-demo pipeline
(examples/agentfoundry-demo/cluster.yaml: iRule -> f5-fluentbit sidecar ->
Fluent Bit DaemonSet -> Loki) produces and
validates every record against the schema Forge's LLM Observability panel
reads: stream labels job="llm-gateway" and model, an HTTP status, the client
address the collector requires, and an action of allow, rate_limited,
tool_forbidden or backend_refused. The MCP extension fields (rpc_method, tool,
session, transport) are checked when present.

The summary counts records by status, action, JSON-RPC method, tool and
caller, reports the 429 throttling and 403 decisions, distinct sessions and
latency percentiles, and lists every schema violation with its count. A
non-zero exit means at least one record violated the schema.

Loki is in-cluster (service loki.llm-egress:3100); reach it with
  azrbnkctl k port-forward -n llm-egress svc/loki 3100:3100
and the default --loki-url. --file reads exported log lines instead.`,
	Args: cobra.NoArgs,
	RunE: runForgeTelemetry,
}

func init() {
	f := forgeTelemetryCmd.Flags()
	f.StringVar(&flagForgeTelemetryLoki, "loki-url", "http://localhost:3100", "Loki base URL")
	f.StringVar(&flagForgeTelemetryQuery, "query", "", `LogQL selector (default {job="llm-gateway"})`)
	f.DurationVar(&flagForgeTelemetrySince, "since", time.Hour, "how far back to read")
	f.IntVar(&flagForgeTelemetryLimit, "limit", 1000, "maximum number of lines to read")
	f.StringVar(&flagForgeTelemetryFile, "file", "", "read log lines from this file instead of Loki")
	f.IntVar(&flagForgeTelemetryShow, "show", 0, "print the newest N records after the summary")
	forgeCmd.AddCommand(forgeTelemetryCmd)
}

func runForgeTelemetry(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	var lines []string
	var err error
	if flagForgeTelemetryFile != "" {
		f, ferr := os.Open(flagForgeTelemetryFile) // #nosec G304 -- operator-supplied log export path
		if ferr != nil {
			return fmt.Errorf("forge telemetry: %w", ferr)
		}
		defer f.Close()
		lines, err = forge.ReadLines(f)
	} else {
		lines, err = forge.QueryLoki(ctx, forge.LokiQueryOptions{
			URL: flagForgeTelemetryLoki, Query: flagForgeTelemetryQuery,
			Since: flagForgeTelemetrySince, Limit: flagForgeTelemetryLimit,
		})
	}
	if err != nil {
		return fmt.Errorf("forge telemetry: %w", err)
	}

	summary, recs := forge.Summarize(lines)
	show := flagForgeTelemetryShow
	if show > len(recs) {
		show = len(recs)
	}
	if flagOutput == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		doc := struct {
			Summary forge.TelemetrySummary   `json:"summary"`
			Records []forge.GovernanceRecord `json:"records,omitempty"`
		}{Summary: summary, Records: recs[:show]}
		if err := enc.Encode(doc); err != nil {
			return err
		}
	} else {
		w := cmd.OutOrStdout()
		summary.Write(w)
		for _, r := range recs[:show] {
			fmt.Fprintf(w, "%s %-15s %-9s %-32s %s %s %dms\n", r.Status, r.Action, r.Caller, r.UserQuery, r.RPCMethod, r.Tool, r.LatencyMS)
		}
	}
	if summary.Records == 0 {
		return fmt.Errorf("forge telemetry: no governance records in %d line(s)", len(lines))
	}
	if summary.Invalid > 0 {
		return fmt.Errorf("forge telemetry: %d of %d record(s) violate the schema", summary.Invalid, summary.Records)
	}
	return nil
}
