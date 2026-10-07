package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/doctor"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/bnkscan"
)

// legacyScanFixture adds a 2.3 policy CRD and one F5BnkGateway to the 2.4
// fixture: a cluster mid-migration.
const legacyScanFixture = scanFixture + `
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: bnknetpolicies.gateway.k8s.f5net.com}
spec: {group: gateway.k8s.f5net.com}
---
apiVersion: k8s.f5net.com/v1
kind: F5BnkGateway
metadata: {name: web, namespace: apps}
spec: {}
`

func checkByName(t *testing.T, checks []doctor.Check, name string) doctor.Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			if c.BackendName != "k8s" {
				t.Errorf("%s backend = %q", name, c.BackendName)
			}
			return c
		}
	}
	t.Fatalf("check %q missing in %+v", name, checks)
	return doctor.Check{}
}

func scanIndex(t *testing.T, fixture string, available int32) *bnkscan.Index {
	t.Helper()
	dyn, cs := fakeScanClients(t, fixture, available)
	idx, err := bnkscan.Scan(context.Background(), dyn, cs, bnkscan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestBNKDoctorChecks_Ready24(t *testing.T) {
	checks := bnkDoctorChecks(scanIndex(t, scanFixture, 1), nil)
	if len(checks) != 4 {
		t.Fatalf("checks = %+v", checks)
	}
	for name, detail := range map[string]string{
		"bnk api generation": "2.4 (gateway.k8s.f5.com)",
		"bnk infra":          "1/1 Programmed=True",
		"bnk gateways":       "1/1 Accepted=True,Programmed=True",
		"bnk cne controller": "f5-cne-system/f5-cne-controller 1/1 available",
	} {
		c := checkByName(t, checks, name)
		if c.Status != doctor.StatusOK || c.Detail != detail {
			t.Errorf("%s = %+v", name, c)
		}
	}
}

func TestBNKDoctorChecks_MixedWarnsMigrate(t *testing.T) {
	checks := bnkDoctorChecks(scanIndex(t, legacyScanFixture, 0), nil)
	gen := checkByName(t, checks, "bnk api generation")
	if gen.Status != doctor.StatusWarning || !strings.Contains(gen.Detail, "gateway.k8s.f5net.com") || !strings.Contains(gen.Detail, "azrbnkctl bnk migrate-2.4") {
		t.Errorf("generation = %+v", gen)
	}
	ctrl := checkByName(t, checks, "bnk cne controller")
	if ctrl.Status != doctor.StatusError || !strings.Contains(ctrl.Detail, "0/1 replicas available") {
		t.Errorf("controller = %+v", ctrl)
	}
	if !doctor.HasFailures(checks) {
		t.Error("controller down must fail doctor")
	}
}

func TestBNKDoctorChecks_NoBNKAndScanError(t *testing.T) {
	checks := bnkDoctorChecks(scanIndex(t, "", 0), nil)
	if len(checks) != 1 || checks[0].Status != doctor.StatusWarning || !strings.Contains(checks[0].Detail, "none") {
		t.Errorf("checks = %+v", checks)
	}
	checks = bnkDoctorChecks(nil, errors.New("no route to host"))
	if len(checks) != 1 || checks[0].Status != doctor.StatusWarning || checks[0].Detail != "scan failed: no route to host" {
		t.Errorf("checks = %+v", checks)
	}
	if doctor.HasFailures(checks) {
		t.Error("an unreachable scan is a warning, not a failure")
	}
}

func TestBNKDoctorChecks_InfraMissing(t *testing.T) {
	// 2.4 CRDs served, controller up, but no Infra CR and no Gateway.
	fixture := strings.Split(scanFixture, "---\napiVersion: gateway.k8s.f5.com/v1alpha1")[0]
	checks := bnkDoctorChecks(scanIndex(t, fixture, 1), nil)
	infra := checkByName(t, checks, "bnk infra")
	if infra.Status != doctor.StatusError || !strings.Contains(infra.Detail, "no Infra CR") {
		t.Errorf("infra = %+v", infra)
	}
	if gw := checkByName(t, checks, "bnk gateways"); gw.Status != doctor.StatusOK || gw.Detail != "0/0 Accepted=True,Programmed=True" {
		t.Errorf("gateways = %+v", gw)
	}
}
