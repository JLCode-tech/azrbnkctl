package forge

import (
	"context"
	"fmt"
	"strings"
)

// BenchmarkAgentEndpoint is the forge REST path for benchmark agents.
const BenchmarkAgentEndpoint = "/api/benchmarks/agents"

// BenchmarkAgentOptions carries all data for registering a benchmark agent.
type BenchmarkAgentOptions struct {
	RestURL      string
	Creds        RestCreds
	Name         string
	Hostname     string
	IPAddress    string
	Tags         map[string]string
	Capabilities []string
}

// BenchmarkAgentResponse is the subset of forge's BenchmarkAgent fields needed.
type BenchmarkAgentResponse struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
}

// RegisterBenchmarkAgent creates or reuses a forge BenchmarkAgent record.
func RegisterBenchmarkAgent(ctx context.Context, opts BenchmarkAgentOptions) (BenchmarkAgentResponse, error) {
	if opts.RestURL == "" {
		return BenchmarkAgentResponse{}, fmt.Errorf("forge.RegisterBenchmarkAgent: RestURL is required")
	}
	if opts.Name == "" {
		return BenchmarkAgentResponse{}, fmt.Errorf("forge.RegisterBenchmarkAgent: Name is required")
	}

	base := strings.TrimRight(opts.RestURL, "/")
	token, err := restLogin(ctx, base, opts.Creds.restUsername(), opts.Creds.restPassword())
	if err != nil {
		return BenchmarkAgentResponse{}, fmt.Errorf("forge benchmark agent: login: %w", err)
	}

	body := map[string]any{
		"name":     opts.Name,
		"hostname": opts.Hostname,
	}
	if opts.IPAddress != "" {
		body["ip_address"] = opts.IPAddress
	}
	if len(opts.Tags) > 0 {
		body["tags"] = opts.Tags
	}
	if len(opts.Capabilities) > 0 {
		body["capabilities"] = map[string]any{"engines": opts.Capabilities}
	}

	var created BenchmarkAgentResponse
	postErr := restPost(ctx, base+BenchmarkAgentEndpoint, token, body, &created)
	if postErr == nil {
		return created, nil
	}

	// 409 or 400-with-"already exists": fall back to list-and-match.
	if !isConflictHTTP(postErr) {
		return BenchmarkAgentResponse{}, fmt.Errorf("forge benchmark agent: create: %w", postErr)
	}

	existing, lookupErr := benchmarkAgentFindByName(ctx, base, token, opts.Name)
	if lookupErr != nil {
		return BenchmarkAgentResponse{}, fmt.Errorf("forge benchmark agent: conflict + list failed: %w (original: %v)", lookupErr, postErr)
	}
	return existing, nil
}

func benchmarkAgentFindByName(ctx context.Context, base, token, name string) (BenchmarkAgentResponse, error) {
	var list []BenchmarkAgentResponse
	if err := restGet(ctx, base+BenchmarkAgentEndpoint, token, &list); err != nil {
		return BenchmarkAgentResponse{}, fmt.Errorf("list benchmark agents: %w", err)
	}
	for _, a := range list {
		if a.Name == name {
			return a, nil
		}
	}
	return BenchmarkAgentResponse{}, fmt.Errorf("benchmark agent %q not found in forge", name)
}
