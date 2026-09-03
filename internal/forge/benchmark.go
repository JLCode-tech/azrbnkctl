package forge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// BenchmarkResultEndpoint is the forge REST path for raw benchmark results.
const BenchmarkResultEndpoint = "/api/benchmarks/results"

// BenchmarkResultPayload carries a benchmark result upload payload.
type BenchmarkResultPayload struct {
	TargetID      int            `json:"target_id,omitempty"`
	AgentID       int            `json:"agent_id,omitempty"`
	ProxyID       int            `json:"proxy_id,omitempty"`
	BenchmarkTool string         `json:"benchmark_tool"`
	Status        string         `json:"status"`
	Summary       map[string]any `json:"summary,omitempty"`
	Metrics       map[string]any `json:"metrics,omitempty"`
	RawOutput     string         `json:"raw_output,omitempty"`
	StartedAt     time.Time      `json:"started_at"`
	CompletedAt   time.Time      `json:"completed_at"`
	DurationSec   float64        `json:"duration_seconds"`
}

// BenchmarkRunResponse is the subset of forge's BenchmarkRun fields needed.
type BenchmarkRunResponse struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

// PublishBenchmarkResultOptions carries parameters for PublishBenchmarkResult.
type PublishBenchmarkResultOptions struct {
	RestURL string
	Creds   RestCreds
	Payload BenchmarkResultPayload
}

// PublishBenchmarkResult pushes a completed benchmark run to forge.
func PublishBenchmarkResult(ctx context.Context, opts PublishBenchmarkResultOptions) (BenchmarkRunResponse, error) {
	if opts.RestURL == "" {
		return BenchmarkRunResponse{}, fmt.Errorf("forge.PublishBenchmarkResult: RestURL is required")
	}

	base := strings.TrimRight(opts.RestURL, "/")
	token, err := restLogin(ctx, base, opts.Creds.restUsername(), opts.Creds.restPassword())
	if err != nil {
		return BenchmarkRunResponse{}, fmt.Errorf("forge benchmark result: login: %w", err)
	}

	var created BenchmarkRunResponse
	url := base + BenchmarkResultEndpoint
	if err := restPost(ctx, url, token, opts.Payload, &created); err != nil {
		return BenchmarkRunResponse{}, fmt.Errorf("forge benchmark result: post: %w", err)
	}
	return created, nil
}
