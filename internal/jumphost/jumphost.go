// Package jumphost provides SSH probe utilities for the multi-NIC jumphost
// model used by scenarios. It is a pure leaf package — it must NOT import
// internal/cli or internal/scenarios. The CLI layer translates cobra flags into
// ProbeOptions at the boundary.
package jumphost

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// DefaultSSHUser is the default Linux username on Azure jumphost instances.
const DefaultSSHUser = "azureuser"

// ProbeOptions configures a jumphost curl probe run.
type ProbeOptions struct {
	// Location is the Azure location / region (e.g. "eastus2").
	Location string
	// VMName is the Azure VM name.
	VMName string
	// ResourceID is the full Azure resource ID of the VM (used for Bastion tunnel).
	ResourceID string
	// JumphostIP is the IP address of the jumphost (public IP or private IP reachable from operator).
	JumphostIP string
	// SourceIP is the jumphost's BNK_EXT interface IP.
	// curl --interface uses this so traffic hits the real data path.
	SourceIP string
	// VIP is the Gateway VIP to curl.
	VIP string
	// Iterations is the number of curl probes. Default 5.
	Iterations int
	// Timeout is the per-curl timeout. Default 10s.
	Timeout time.Duration
	// User is the SSH user on the jumphost. Default DefaultSSHUser ("azureuser").
	User string
	// Hostname sets the HTTP Host header so requests match hostname-scoped
	// HTTPRoutes. Empty means no Host header (matches no-hostname routes).
	Hostname string
	// SSHKeyPath is an optional path to an existing private SSH key.
	// When empty, an ephemeral ED25519 key is generated.
	SSHKeyPath string
	// UseBastionTunnel indicates whether to use Azure Bastion tunneling for SSH.
	UseBastionTunnel bool

	// Backwards-compatible aliases:
	// Region is an alias for Location.
	Region string
	// InstanceID is an alias for VMName.
	InstanceID string
}

// EffectiveLocation returns Location if set, falling back to Region.
func (p ProbeOptions) EffectiveLocation() string {
	if p.Location != "" {
		return p.Location
	}
	return p.Region
}

// EffectiveVMName returns VMName if set, falling back to InstanceID.
func (p ProbeOptions) EffectiveVMName() string {
	if p.VMName != "" {
		return p.VMName
	}
	return p.InstanceID
}

// EffectiveUser returns User if set, falling back to DefaultSSHUser ("azureuser").
func (p ProbeOptions) EffectiveUser() string {
	if p.User != "" {
		return p.User
	}
	return DefaultSSHUser
}

// TargetHost returns the destination host for SSH.
// JumphostIP takes precedence, then EffectiveVMName, then SourceIP.
func (p ProbeOptions) TargetHost() string {
	if p.JumphostIP != "" {
		return p.JumphostIP
	}
	if vm := p.EffectiveVMName(); vm != "" {
		return vm
	}
	return p.SourceIP
}

// ProbeResult records the outcome of one curl iteration.
type ProbeResult struct {
	Iteration int     `json:"iteration"`
	HTTPCode  int     `json:"http_code"`
	Seconds   float64 `json:"seconds"`
	Err       string  `json:"error,omitempty"`
}

// prepareProbeKey checks if an existing key path was supplied; otherwise
// delegates to prepareEICEKeyFn to mint an ephemeral key.
func prepareProbeKey(ctx context.Context, opts ProbeOptions) (keyPath, pubKeyPath string, cleanup func(), err error) {
	if opts.SSHKeyPath != "" {
		pubKey := opts.SSHKeyPath + ".pub"
		if _, statErr := os.Stat(pubKey); statErr != nil {
			pubKey = ""
		}
		return opts.SSHKeyPath, pubKey, func() {}, nil
	}
	return prepareEICEKeyFn(ctx, opts.EffectiveLocation(), opts.EffectiveVMName())
}

// prepareEICEKey mints an ephemeral ED25519 keypair, writes it to temp files,
// pushes the public half via PushSSHPublicKey, and returns the private-key path,
// the public-key path, and a cleanup func that removes both files.
func prepareEICEKey(ctx context.Context, region, instanceID string) (keyPath, pubKeyPath string, cleanup func(), err error) {
	priv, pubAuth, err := GenerateEphemeralED25519()
	if err != nil {
		return "", "", func() {}, fmt.Errorf("ephemeral key: %w", err)
	}

	keyFile, err := os.CreateTemp("", "azrbnkctl-jumphost-*.key")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("temp key file: %w", err)
	}
	if _, err := keyFile.Write(priv); err != nil {
		_ = keyFile.Close()
		_ = os.Remove(keyFile.Name())
		return "", "", func() {}, fmt.Errorf("write key: %w", err)
	}
	if err := keyFile.Chmod(0o600); err != nil {
		_ = keyFile.Close()
		_ = os.Remove(keyFile.Name())
		return "", "", func() {}, fmt.Errorf("chmod key: %w", err)
	}
	if err := keyFile.Close(); err != nil {
		_ = os.Remove(keyFile.Name())
		return "", "", func() {}, fmt.Errorf("close key: %w", err)
	}

	pubFile, err := os.CreateTemp("", "azrbnkctl-jumphost-*.pub")
	if err != nil {
		_ = os.Remove(keyFile.Name())
		return "", "", func() {}, fmt.Errorf("temp pub file: %w", err)
	}
	if _, err := pubFile.WriteString(pubAuth); err != nil {
		_ = pubFile.Close()
		_ = os.Remove(keyFile.Name())
		_ = os.Remove(pubFile.Name())
		return "", "", func() {}, fmt.Errorf("write pub: %w", err)
	}
	if err := pubFile.Close(); err != nil {
		_ = os.Remove(keyFile.Name())
		_ = os.Remove(pubFile.Name())
		return "", "", func() {}, fmt.Errorf("close pub: %w", err)
	}

	cleanupFn := func() {
		_ = os.Remove(keyFile.Name())
		_ = os.Remove(pubFile.Name())
	}

	if err := pushSSHPublicKeyFn(ctx, region, instanceID, pubFile.Name()); err != nil {
		cleanupFn()
		return "", "", func() {}, fmt.Errorf("send-ssh-public-key: %w", err)
	}

	// Allow the key a moment to settle.
	time.Sleep(2 * time.Second)

	return keyFile.Name(), pubFile.Name(), cleanupFn, nil
}

// buildSSHArgs constructs SSH arguments supporting direct SSH (user@targetHost),
// Azure Bastion proxy commands (az network bastion tunnel --target-resource-id ...),
// and legacy AWS EICE fallback.
func buildSSHArgs(user, targetHost, keyPath string, useBastion bool, resourceID, location, remoteCmd string) []string {
	var args []string
	if resourceID == "" && strings.HasPrefix(targetHost, "/subscriptions/") {
		resourceID = targetHost
	}

	if useBastion || (resourceID != "" && strings.HasPrefix(resourceID, "/subscriptions/")) {
		if resourceID != "" {
			proxy := fmt.Sprintf("ProxyCommand=az network bastion tunnel --target-resource-id %s --resource-port 22", resourceID)
			args = append(args, "-o", proxy)
		}
	} else if strings.HasPrefix(targetHost, "i-") {
		proxy := fmt.Sprintf("ProxyCommand=aws ec2-instance-connect open-tunnel --instance-id %s --region %s", targetHost, location)
		args = append(args, "-o", proxy)
	}

	args = append(args,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=15",
	)
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	if user == "" {
		user = DefaultSSHUser
	}
	connectHost := targetHost
	if strings.HasPrefix(connectHost, "/subscriptions/") {
		if idx := strings.LastIndex(connectHost, "/"); idx >= 0 {
			connectHost = connectHost[idx+1:]
		}
	}
	args = append(args, fmt.Sprintf("%s@%s", user, connectHost), remoteCmd)
	return args
}

// RunCurlProbes mints an ephemeral SSH key (or uses opts.SSHKeyPath), pushes it,
// then runs opts.Iterations curls from the jumphost. Returns per-iteration results.
func RunCurlProbes(ctx context.Context, opts ProbeOptions) ([]ProbeResult, error) {
	if opts.Iterations <= 0 {
		opts.Iterations = 5
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	user := opts.EffectiveUser()
	loc := opts.EffectiveLocation()
	vm := opts.EffectiveVMName()
	targetHost := opts.TargetHost()

	keyPath, pubKeyPath, cleanup, err := prepareProbeKey(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	results := make([]ProbeResult, 0, opts.Iterations)
	for i := 1; i <= opts.Iterations; i++ {
		if pubKeyPath != "" {
			_ = pushSSHPublicKeyFn(ctx, loc, vm, pubKeyPath)
		}
		code, secs, perr := sshCurlProbe(ctx, opts, keyPath, targetHost, user)
		res := ProbeResult{Iteration: i, HTTPCode: code, Seconds: secs}
		if perr != nil {
			res.Err = perr.Error()
		}
		results = append(results, res)
	}
	return results, nil
}

// PushSSHPublicKey sends/updates the public key on the target VM.
// On Azure, it updates the VM user key via `az vm user update` if available,
// or gracefully no-ops when the CLI isn't available.
// On AWS instances (instanceID starting with "i-"), it falls back to EC2 Instance Connect.
func PushSSHPublicKey(ctx context.Context, region, instanceID, pubKeyPath string) error {
	if strings.HasPrefix(instanceID, "i-") {
		if _, err := exec.LookPath("aws"); err != nil {
			return nil
		}
		args := []string{
			"ec2-instance-connect", "send-ssh-public-key",
			"--instance-id", instanceID,
			"--instance-os-user", DefaultSSHUser,
			"--ssh-public-key", "file://" + pubKeyPath,
			"--region", region,
		}
		cmd := exec.CommandContext(ctx, "aws", args...) // #nosec G204 -- fixed aws subcommand with controlled args
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		_ = cmd.Run()
		return nil
	}

	if _, err := exec.LookPath("az"); err != nil {
		// CLI not available; gracefully no-op.
		return nil
	}

	pubBytes, err := os.ReadFile(pubKeyPath) // #nosec G304 -- local public key path
	if err != nil {
		return nil
	}
	pubKey := strings.TrimSpace(string(pubBytes))
	if pubKey == "" {
		return nil
	}

	var args []string
	if strings.HasPrefix(instanceID, "/subscriptions/") {
		args = []string{
			"vm", "user", "update",
			"--ids", instanceID,
			"--username", DefaultSSHUser,
			"--ssh-key-value", pubKey,
		}
	} else if instanceID != "" {
		args = []string{
			"vm", "user", "update",
			"--name", instanceID,
			"--username", DefaultSSHUser,
			"--ssh-key-value", pubKey,
		}
	} else {
		return nil
	}

	cmd := exec.CommandContext(ctx, "az", args...) // #nosec G204 -- fixed az subcommand with controlled args
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	return nil
}

// buildCurlCmd returns the remote curl command. When host is non-empty it
// adds -H 'Host: <host>' so requests match hostname-scoped HTTPRoutes.
func buildCurlCmd(sourceIP, vip, host string, timeoutSecs int) string {
	hostHdr := ""
	if host != "" {
		hostHdr = fmt.Sprintf(`-H 'Host: %s' `, host)
	}
	return fmt.Sprintf(`curl -s -o /dev/null -w '%%{http_code} %%{time_total}' %s--interface %s --max-time %d http://%s/`,
		hostHdr, sourceIP, timeoutSecs, vip)
}

// SSHCurlViaEICE opens an SSH connection to the jumphost and runs a single
// curl --interface <sourceIP> http://<vip>/ with the given timeout.
func SSHCurlViaEICE(ctx context.Context, region, instanceID, keyPath, sourceIP, vip, host string, timeout time.Duration) (int, float64, error) {
	opts := ProbeOptions{
		Region:     region,
		InstanceID: instanceID,
		SourceIP:   sourceIP,
		VIP:        vip,
		Hostname:   host,
		Timeout:    timeout,
	}
	return sshCurlProbe(ctx, opts, keyPath, opts.TargetHost(), opts.EffectiveUser())
}

func sshCurlProbe(ctx context.Context, opts ProbeOptions, keyPath, targetHost, user string) (int, float64, error) {
	remoteCmd := buildCurlCmd(opts.SourceIP, opts.VIP, opts.Hostname, int(opts.Timeout.Seconds()))
	args := buildSSHArgs(user, targetHost, keyPath, opts.UseBastionTunnel, opts.ResourceID, opts.EffectiveLocation(), remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...) // #nosec G204 -- ssh arguments built from validated parameters
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("ssh-curl: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	out := strings.TrimSpace(stdout.String())
	parts := strings.Fields(out)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected ssh-curl output %q", out)
	}
	var code int
	var secs float64
	if _, err := fmt.Sscanf(parts[0], "%d", &code); err != nil {
		return 0, 0, fmt.Errorf("parsing http_code %q: %w", parts[0], err)
	}
	if _, err := fmt.Sscanf(parts[1], "%f", &secs); err != nil {
		return code, 0, fmt.Errorf("parsing time_total %q: %w", parts[1], err)
	}
	return code, secs, nil
}

// BodyProbeResult records the outcome of one body-capturing curl iteration.
type BodyProbeResult struct {
	Iteration int    `json:"iteration"`
	HTTPCode  int    `json:"http_code"`
	Body      string `json:"body,omitempty"`
	Err       string `json:"error,omitempty"`
}

// buildCurlBodyCmd returns the remote curl command that captures the response body.
func buildCurlBodyCmd(sourceIP, vip, host string, timeoutSecs int) string {
	hostHdr := ""
	if host != "" {
		hostHdr = fmt.Sprintf(`-H 'Host: %s' `, host)
	}
	return fmt.Sprintf(`curl -s -w '\n%%{http_code}' %s--interface %s --max-time %d http://%s/`,
		hostHdr, sourceIP, timeoutSecs, vip)
}

// SSHCurlBodyViaEICE opens an SSH connection to the jumphost and runs a single body-capturing curl.
func SSHCurlBodyViaEICE(ctx context.Context, region, instanceID, keyPath, sourceIP, vip, host string, timeout time.Duration) (body string, code int, err error) {
	opts := ProbeOptions{
		Region:     region,
		InstanceID: instanceID,
		SourceIP:   sourceIP,
		VIP:        vip,
		Hostname:   host,
		Timeout:    timeout,
	}
	return sshCurlBodyProbe(ctx, opts, keyPath, opts.TargetHost(), opts.EffectiveUser())
}

func sshCurlBodyProbe(ctx context.Context, opts ProbeOptions, keyPath, targetHost, user string) (string, int, error) {
	remoteCmd := buildCurlBodyCmd(opts.SourceIP, opts.VIP, opts.Hostname, int(opts.Timeout.Seconds()))
	args := buildSSHArgs(user, targetHost, keyPath, opts.UseBastionTunnel, opts.ResourceID, opts.EffectiveLocation(), remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...) // #nosec G204 -- ssh arguments built from validated parameters
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("ssh-curl-body: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	raw := stdout.String()
	lastNL := strings.LastIndex(raw, "\n")
	if lastNL < 0 {
		return "", 0, fmt.Errorf("unexpected ssh-curl-body output %q", raw)
	}
	bodyStr := strings.TrimSpace(raw[:lastNL])
	codeStr := strings.TrimSpace(raw[lastNL+1:])
	var httpCode int
	if _, parseErr := fmt.Sscanf(codeStr, "%d", &httpCode); parseErr != nil {
		return "", 0, fmt.Errorf("parsing http_code %q: %w", codeStr, parseErr)
	}
	return bodyStr, httpCode, nil
}

// RunCurlBodyProbes mints an ephemeral SSH key (or uses opts.SSHKeyPath), pushes it,
// then runs opts.Iterations body-capturing curls from the jumphost.
func RunCurlBodyProbes(ctx context.Context, opts ProbeOptions) ([]BodyProbeResult, error) {
	if opts.Iterations <= 0 {
		opts.Iterations = 5
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	user := opts.EffectiveUser()
	loc := opts.EffectiveLocation()
	vm := opts.EffectiveVMName()
	targetHost := opts.TargetHost()

	keyPath, pubKeyPath, cleanup, err := prepareProbeKey(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	results := make([]BodyProbeResult, 0, opts.Iterations)
	for i := 1; i <= opts.Iterations; i++ {
		if pubKeyPath != "" {
			_ = pushSSHPublicKeyFn(ctx, loc, vm, pubKeyPath)
		}
		body, code, perr := sshCurlBodyProbe(ctx, opts, keyPath, targetHost, user)
		res := BodyProbeResult{Iteration: i, HTTPCode: code, Body: body}
		if perr != nil {
			res.Err = perr.Error()
		}
		results = append(results, res)
	}
	return results, nil
}

// SSHRunViaEICE opens an SSH connection to the jumphost and runs an arbitrary
// remote command, returning its stdout (trimmed).
func SSHRunViaEICE(ctx context.Context, region, instanceID, keyPath, remoteCmd string) (string, error) {
	return SSHRunViaEICEWithStdin(ctx, region, instanceID, keyPath, remoteCmd, nil)
}

// SSHRunViaEICEWithStdin is SSHRunViaEICE with the local ssh process's stdin
// attached to the given reader.
func SSHRunViaEICEWithStdin(ctx context.Context, region, instanceID, keyPath, remoteCmd string, stdin io.Reader) (string, error) {
	opts := ProbeOptions{
		Region:     region,
		InstanceID: instanceID,
	}
	user := opts.EffectiveUser()
	targetHost := opts.TargetHost()
	args := buildSSHArgs(user, targetHost, keyPath, opts.UseBastionTunnel, opts.ResourceID, opts.EffectiveLocation(), remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...) // #nosec G204 -- ssh arguments built from validated parameters
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stdout.String()), fmt.Errorf("ssh-run: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// shellSingleQuote wraps s in single quotes safe for /bin/sh.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildHTTPResponderCmd returns the remote shell command that writes marker to
// /tmp/azrbnkctl-extpool/index.html and (re)starts a systemd transient unit.
func buildHTTPResponderCmd(port int, marker string) string {
	q := shellSingleQuote(marker)
	unit := fmt.Sprintf("azrbnkctl-extpool-%d", port)
	return fmt.Sprintf(
		`mkdir -p /tmp/azrbnkctl-extpool && printf '%%s' %s > /tmp/azrbnkctl-extpool/index.html; sudo systemctl stop %s 2>/dev/null; sudo systemctl reset-failed %s 2>/dev/null; sudo systemd-run --unit=%s --working-directory=/tmp/azrbnkctl-extpool python3 -m http.server %d >/dev/null 2>&1; sleep 2; curl -s -o /dev/null -w '%%{http_code}' http://127.0.0.1:%d/`,
		q, unit, unit, unit, port, port,
	)
}

// buildHTTPResponderStopCmd returns the remote command that stops the systemd transient unit.
func buildHTTPResponderStopCmd(port int) string {
	unit := fmt.Sprintf("azrbnkctl-extpool-%d", port)
	return fmt.Sprintf(`sudo systemctl stop %s 2>/dev/null; sudo systemctl reset-failed %s 2>/dev/null; true`, unit, unit)
}

// StartHTTPResponder starts a backgrounded python3 HTTP server on the jumphost.
func StartHTTPResponder(ctx context.Context, opts ProbeOptions, port int, marker string) error {
	keyPath, _, cleanup, err := prepareProbeKey(ctx, opts)
	if err != nil {
		return err
	}
	defer cleanup()

	out, runErr := sshRunViaEICEFn(ctx, opts.EffectiveLocation(), opts.EffectiveVMName(), keyPath, buildHTTPResponderCmd(port, marker))
	if runErr != nil {
		return fmt.Errorf("starting HTTP responder on :%d: %w (remote stdout: %q)", port, runErr, out)
	}
	if strings.TrimSpace(out) != "200" {
		return fmt.Errorf("HTTP responder on :%d did not return 200 (remote stdout: %q)", port, out)
	}
	return nil
}

// StopHTTPResponder kills any python3 http.server on port on the jumphost.
func StopHTTPResponder(ctx context.Context, opts ProbeOptions, port int) error {
	keyPath, _, cleanup, err := prepareProbeKey(ctx, opts)
	if err != nil {
		return err
	}
	defer cleanup()

	_, _ = sshRunViaEICEFn(ctx, opts.EffectiveLocation(), opts.EffectiveVMName(), keyPath, buildHTTPResponderStopCmd(port))
	return nil
}

// GenerateEphemeralED25519 mints a fresh ED25519 keypair and returns
// (private-key in OpenSSH PEM, public-key in authorized_keys format, error).
func GenerateEphemeralED25519() (privPEM []byte, pubAuthLine string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "azrbnkctl-jumphost-ephemeral")
	if err != nil {
		return nil, "", err
	}
	pemBytes := pem.EncodeToMemory(pemBlock)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", err
	}
	auth := fmt.Sprintf("ssh-ed25519 %s azrbnkctl-jumphost-ephemeral\n",
		base64.StdEncoding.EncodeToString(sshPub.Marshal()))
	return pemBytes, auth, nil
}
