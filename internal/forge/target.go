package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// BenchmarkTargetEndpoint is the forge REST path for benchmark targets.
const BenchmarkTargetEndpoint = "/api/benchmarks/targets"

// BenchmarkTargetOptions carries all data for registering a benchmark target.
type BenchmarkTargetOptions struct {
	RestURL        string
	Creds          RestCreds
	Name           string
	ClusterID      int
	LLMBaseURL     string
	LLMModel       string
	LLMNamespace   string
	LLMEndpoint    string
	ProxyNamespace string
	Tags           map[string]string
}

// BenchmarkTargetResponse is the subset of forge's BenchmarkTarget fields needed.
type BenchmarkTargetResponse struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ClusterID int    `json:"cluster_id"`
}

// ErrTargetNoClusterID is returned when ClusterID is 0.
var ErrTargetNoClusterID = errors.New("forge.RegisterBenchmarkTarget: ClusterID is required")

// RegisterBenchmarkTarget creates or reuses a forge BenchmarkTarget record.
func RegisterBenchmarkTarget(ctx context.Context, opts BenchmarkTargetOptions) (BenchmarkTargetResponse, error) {
	if opts.RestURL == "" {
		return BenchmarkTargetResponse{}, fmt.Errorf("forge.RegisterBenchmarkTarget: RestURL is required")
	}
	if opts.Name == "" {
		return BenchmarkTargetResponse{}, fmt.Errorf("forge.RegisterBenchmarkTarget: Name is required")
	}
	if opts.ClusterID == 0 {
		return BenchmarkTargetResponse{}, ErrTargetNoClusterID
	}

	base := strings.TrimRight(opts.RestURL, "/")
	token, err := restLogin(ctx, base, opts.Creds.restUsername(), opts.Creds.restPassword())
	if err != nil {
		return BenchmarkTargetResponse{}, fmt.Errorf("forge benchmark target: login: %w", err)
	}

	body := map[string]any{
		"name":       opts.Name,
		"cluster_id": opts.ClusterID,
	}
	if opts.LLMBaseURL != "" {
		body["llm_base_url"] = opts.LLMBaseURL
	}
	if opts.LLMModel != "" {
		body["llm_model"] = opts.LLMModel
	}
	if opts.LLMNamespace != "" {
		body["llm_namespace"] = opts.LLMNamespace
	}
	if opts.LLMEndpoint != "" {
		body["llm_endpoint"] = opts.LLMEndpoint
	}
	if opts.ProxyNamespace != "" {
		body["proxy_namespace"] = opts.ProxyNamespace
	}
	if len(opts.Tags) > 0 {
		body["tags"] = opts.Tags
	}

	var created BenchmarkTargetResponse
	postErr := restPost(ctx, base+BenchmarkTargetEndpoint, token, body, &created)
	if postErr == nil {
		return created, nil
	}

	var herr *restHTTPErr
	if !errors.As(postErr, &herr) {
		return BenchmarkTargetResponse{}, fmt.Errorf("forge benchmark target: create: %w", postErr)
	}
	isConflict := herr.StatusCode == http.StatusConflict ||
		(herr.StatusCode == http.StatusBadRequest && strings.Contains(herr.Body, "already exists"))
	if !isConflict {
		return BenchmarkTargetResponse{}, fmt.Errorf("forge benchmark target: create: %w", postErr)
	}

	existing, lookupErr := benchmarkTargetFindByName(ctx, base, token, opts.Name)
	if lookupErr != nil {
		return BenchmarkTargetResponse{}, fmt.Errorf("forge benchmark target: conflict but lookup failed: %w (orig: %v)", lookupErr, postErr)
	}
	return existing, nil
}

func benchmarkTargetFindByName(ctx context.Context, base, token, name string) (BenchmarkTargetResponse, error) {
	var list []BenchmarkTargetResponse
	if err := restGet(ctx, base+BenchmarkTargetEndpoint, token, &list); err != nil {
		return BenchmarkTargetResponse{}, fmt.Errorf("list targets: %w", err)
	}
	for _, t := range list {
		if t.Name == name {
			return t, nil
		}
	}
	return BenchmarkTargetResponse{}, fmt.Errorf("target %q not found in forge", name)
}
