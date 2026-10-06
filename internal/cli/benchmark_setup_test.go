package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/forge"
	"github.com/JLCode-tech/azrbnkctl/internal/jumphost"
)

func TestBenchmarkSetup_Validation(t *testing.T) {
	// Reset flags
	flagBenchRegion = ""
	flagBenchInstanceID = ""
	flagBenchWorkspace = ""
	flagBenchConfig = ""

	cmd := benchmarkSetupCmd

	// Missing region
	err := runBenchmarkSetup(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--region is required") {
		t.Fatalf("expected --region error, got: %v", err)
	}

	// Missing instance-id
	flagBenchRegion = "us-west-2"
	err = runBenchmarkSetup(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--instance-id is required") {
		t.Fatalf("expected --instance-id error, got: %v", err)
	}
}

func TestBenchmarkSetup_Success(t *testing.T) {
	// Mock seams
	oldEnsure := ensureAiperfFn
	oldAccess := registerAccessMethodFn
	oldAgent := registerBenchmarkAgentFn
	oldTarget := registerBenchmarkTargetFn
	oldCheck := checkServedModelFn
	defer func() {
		ensureAiperfFn = oldEnsure
		registerAccessMethodFn = oldAccess
		registerBenchmarkAgentFn = oldAgent
		registerBenchmarkTargetFn = oldTarget
		checkServedModelFn = oldCheck
	}()

	ensureCalled := false
	ensureAiperfFn = func(ctx context.Context, opts jumphost.ProbeOptions) error {
		ensureCalled = true
		if opts.InstanceID != "i-test123" {
			return fmt.Errorf("unexpected instance ID: %s", opts.InstanceID)
		}
		return nil
	}

	accessCalled := false
	registerAccessMethodFn = func(ctx context.Context, opts forge.AccessMethodOptions) (forge.AccessMethodResponse, error) {
		accessCalled = true
		return forge.AccessMethodResponse{ID: 42, Name: opts.Name, Host: opts.Host}, nil
	}

	agentCalled := false
	registerBenchmarkAgentFn = func(ctx context.Context, opts forge.BenchmarkAgentOptions) (forge.BenchmarkAgentResponse, error) {
		agentCalled = true
		return forge.BenchmarkAgentResponse{ID: 101, Name: opts.Name}, nil
	}

	targetCalled := false
	registerBenchmarkTargetFn = func(ctx context.Context, opts forge.BenchmarkTargetOptions) (forge.BenchmarkTargetResponse, error) {
		targetCalled = true
		return forge.BenchmarkTargetResponse{ID: 202, Name: opts.Name}, nil
	}

	preflightCalled := false
	checkServedModelFn = func(ctx context.Context, opts jumphost.ProbeOptions, model string) error {
		preflightCalled = true
		return nil
	}

	flagBenchRegion = "us-east-1"
	flagBenchInstanceID = "i-test123"
	flagBenchSourceIP = "10.0.1.50"
	flagBenchVIP = "10.0.10.100"
	flagBenchModel = "meta-llama/Llama-3.1-8B-Instruct"
	flagBenchWorkspace = "test-ws"
	flagBenchConfig = ""
	flagBenchRegisterAccessMethod = true
	flagBenchSetupPreflight = true

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runBenchmarkSetup(benchmarkSetupCmd, nil)
	_ = w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runBenchmarkSetup failed: %v", err)
	}

	if !ensureCalled {
		t.Error("expected ensureAiperfFn to be called")
	}
	if !accessCalled {
		t.Error("expected registerAccessMethodFn to be called")
	}
	if !agentCalled {
		t.Error("expected registerBenchmarkAgentFn to be called")
	}
	if !targetCalled {
		t.Error("expected registerBenchmarkTargetFn to be called")
	}
	if !preflightCalled {
		t.Error("expected checkServedModelFn to be called")
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "BENCHMARK SETUP SUMMARY") {
		t.Errorf("expected BENCHMARK SETUP SUMMARY in output, got:\n%s", out)
	}
	if !strings.Contains(out, "i-test123") {
		t.Errorf("expected instance ID in summary output")
	}
}
