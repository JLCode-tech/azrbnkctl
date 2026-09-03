package doctor

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestDoctor_Run(t *testing.T) {
	os.Setenv("AZRBNKCTL_SKIP_AUTH", "1")
	defer os.Unsetenv("AZRBNKCTL_SKIP_AUTH")

	cfg := Config{
		SubscriptionID: "00000000-0000-0000-0000-000000000000",
		Location:       "eastus2",
	}

	results := Run(context.Background(), cfg)
	if len(results) == 0 {
		t.Fatal("expected doctor checks, got empty slice")
	}

	var buf bytes.Buffer
	if err := PrintResults(&buf, results); err != nil {
		t.Fatalf("PrintResults failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected formatted output, got empty buffer")
	}
}
