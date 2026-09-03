// Package netcalc provides pure-Go CIDR and IP calculations for azrbnkctl.
package netcalc

import (
	"fmt"
	"net"
)

// Nth returns the address at network-offset n inside cidr — i.e. the network
// address plus n.
//
// In Azure VNets, offset 1 is the default subnet gateway (e.g. 10.0.16.1 for 10.0.16.0/24).
//
// Returns an error if cidr does not parse, is not IPv4, or if the resulting
// address falls outside the network.
func Nth(cidr string, n int) (string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("netcalc: parse cidr %q: %w", cidr, err)
	}
	base4 := ipnet.IP.To4()
	if base4 == nil {
		return "", fmt.Errorf("netcalc: %q is not an IPv4 network", cidr)
	}
	base := uint32(base4[0])<<24 | uint32(base4[1])<<16 | uint32(base4[2])<<8 | uint32(base4[3])
	addr := base + uint32(n)                                                      // #nosec G115 -- n is caller-supplied small offset, not attacker-controlled
	result := net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr)) // #nosec G115 -- addr shift fits inside byte bounds
	if !ipnet.Contains(result) {
		return "", fmt.Errorf("netcalc: %s is outside %s", result, ipnet)
	}
	return result.String(), nil
}

// InCIDR reports whether ip falls inside cidr.
func InCIDR(ip, cidr string) (bool, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false, fmt.Errorf("netcalc: not an IP address: %q", ip)
	}
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, fmt.Errorf("netcalc: parse cidr %q: %w", cidr, err)
	}
	return ipnet.Contains(parsed), nil
}

// CIDRAddr returns the network-address portion of cidr.
//
// Example: CIDRAddr("10.0.0.0/16") -> "10.0.0.0".
func CIDRAddr(cidr string) (string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("netcalc: parse cidr %q: %w", cidr, err)
	}
	return ipnet.IP.String(), nil
}

// CIDRPrefixLen returns the prefix-length portion of cidr.
//
// Example: CIDRPrefixLen("10.0.0.0/16") -> 16.
func CIDRPrefixLen(cidr string) (int, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, fmt.Errorf("netcalc: parse cidr %q: %w", cidr, err)
	}
	ones, _ := ipnet.Mask.Size()
	return ones, nil
}
