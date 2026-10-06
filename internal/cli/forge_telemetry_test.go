package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JLCode-tech/azrbnkctl/internal/forge"
)

func resetTelemetryFlags() {
	flagForgeTelemetryLoki = "http://localhost:3100"
	flagForgeTelemetryQuery = ""
	flagForgeTelemetrySince = time.Hour
	flagForgeTelemetryLimit = 1000
	flagForgeTelemetryFile = ""
	flagForgeTelemetryShow = 0
	flagOutput = "text"
}

func writeTelemetryFile(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gov.log")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const govOK = `Rule /Common/mcp-rate-limit-irule <HTTP_RESPONSE_DATA>: BNKGOV {"job":"llm-gateway","model":"mcp:finance-tool","status":"200","latency_ms":37,"prompt_tk":0,"comp_tk":0,"total_tk":0,"cached":0,"cost":0,"userq":"tools/call forecast","client":"10.0.11.15","caller":"agent","action":"allow","req_body":"","resp_body":"","rpc_method":"tools/call","tool":"forecast","session":"abc","transport":"http"}`
const gov429 = `BNKGOV {"job":"llm-gateway","model":"mcp:finance-tool","status":"429","latency_ms":0,"userq":"/v1/mcp/forecast","client":"10.0.12.191","caller":"external","action":"rate_limited","req_body":"","resp_body":"rate limit exceeded","transport":"sse"}`
const govBad = `BNKGOV {"job":"llm-gateway","model":"mcp:finance-tool","status":"200","client":"10.0.0.9","action":"blocked"}`

func TestForgeTelemetry_FileTextAndJSON(t *testing.T) {
	resetTelemetryFlags()
	t.Cleanup(resetTelemetryFlags)
	flagForgeTelemetryFile = writeTelemetryFile(t, govOK, gov429, "noise line")
	flagForgeTelemetryShow = 5

	var out bytes.Buffer
	forgeTelemetryCmd.SetOut(&out)
	if err := runForgeTelemetry(forgeTelemetryCmd, nil); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	for _, want := range []string{"records: 2", "throttled (429): 1", "tools/call", "429 rate_limited    external"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	flagOutput = "json"
	if err := runForgeTelemetry(forgeTelemetryCmd, nil); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Summary forge.TelemetrySummary   `json:"summary"`
		Records []forge.GovernanceRecord `json:"records"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if doc.Summary.Records != 2 || doc.Summary.Throttled != 1 || len(doc.Records) != 2 || doc.Records[0].Tool != "forecast" {
		t.Errorf("doc = %+v", doc)
	}
}

func TestForgeTelemetry_SchemaViolationFails(t *testing.T) {
	resetTelemetryFlags()
	t.Cleanup(resetTelemetryFlags)
	flagForgeTelemetryFile = writeTelemetryFile(t, govOK, govBad)
	var out bytes.Buffer
	forgeTelemetryCmd.SetOut(&out)
	err := runForgeTelemetry(forgeTelemetryCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 record(s) violate the schema") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out.String(), `action="blocked" is not one of`) {
		t.Errorf("output:\n%s", out.String())
	}

	flagForgeTelemetryFile = writeTelemetryFile(t, "nothing here")
	if err := runForgeTelemetry(forgeTelemetryCmd, nil); err == nil || !strings.Contains(err.Error(), "no governance records") {
		t.Errorf("empty: err = %v", err)
	}
}
