package netcalc

import (
	"testing"
)

func TestNth(t *testing.T) {
	tests := []struct {
		cidr    string
		n       int
		want    string
		wantErr bool
	}{
		{"10.0.16.0/24", 1, "10.0.16.1", false},
		{"10.0.16.0/24", 4, "10.0.16.4", false},
		{"10.0.16.0/24", 254, "10.0.16.254", false},
		{"10.0.16.0/24", 256, "", true},
		{"invalid", 1, "", true},
	}

	for _, tt := range tests {
		got, err := Nth(tt.cidr, tt.n)
		if (err != nil) != tt.wantErr {
			t.Errorf("Nth(%q, %d) error = %v, wantErr %v", tt.cidr, tt.n, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("Nth(%q, %d) = %q, want %q", tt.cidr, tt.n, got, tt.want)
		}
	}
}

func TestInCIDR(t *testing.T) {
	in, err := InCIDR("10.0.16.10", "10.0.0.0/16")
	if err != nil || !in {
		t.Fatalf("expected 10.0.16.10 in 10.0.0.0/16, got %v, err=%v", in, err)
	}

	out, err := InCIDR("192.168.1.1", "10.0.0.0/16")
	if err != nil || out {
		t.Fatalf("expected 192.168.1.1 not in 10.0.0.0/16, got %v, err=%v", out, err)
	}
}

func TestCIDRAddrAndPrefixLen(t *testing.T) {
	addr, err := CIDRAddr("10.0.16.0/24")
	if err != nil || addr != "10.0.16.0" {
		t.Fatalf("CIDRAddr failed: addr=%q, err=%v", addr, err)
	}

	prefix, err := CIDRPrefixLen("10.0.16.0/24")
	if err != nil || prefix != 24 {
		t.Fatalf("CIDRPrefixLen failed: prefix=%d, err=%v", prefix, err)
	}
}
