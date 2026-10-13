package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecOptions captures the flag-parsed inputs to `azrbnkctl k exec`.
type ExecOptions struct {
	PodName        string
	Namespace      string
	Container      string
	Stdin          bool
	TTY            bool
	Command        []string
	KubeconfigPath string

	IOStreams genericiooptions.IOStreams

	// SizeQueue is consulted when TTY is true so the remote PTY tracks
	// terminal resizes. Optional; nil disables resize forwarding.
	SizeQueue remotecommand.TerminalSizeQueue
}

// Run opens an SPDY exec stream against the pod and proxies stdio.
func (o *ExecOptions) Run(ctx context.Context) error {
	if o.PodName == "" {
		return errors.New("pod name required")
	}
	if len(o.Command) == 0 {
		return errors.New("command required after `--`")
	}
	cfg, err := BuildRESTConfig(o.KubeconfigPath)
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	ns := o.Namespace
	if ns == "" {
		ns = "default"
	}

	req := cs.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(o.PodName).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: o.Container,
			Command:   o.Command,
			Stdin:     o.Stdin,
			Stdout:    true,
			Stderr:    !o.TTY,
			TTY:       o.TTY,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("setting up SPDY executor: %w", err)
	}

	streamOpts := remotecommand.StreamOptions{
		Stdout: o.IOStreams.Out,
		Stderr: o.IOStreams.ErrOut,
		Tty:    o.TTY,
	}
	if o.Stdin {
		streamOpts.Stdin = o.IOStreams.In
	}
	if o.TTY && o.SizeQueue != nil {
		streamOpts.TerminalSizeQueue = o.SizeQueue
	}
	if streamOpts.Stdout == nil {
		streamOpts.Stdout = io.Discard
	}
	if streamOpts.Stderr == nil {
		streamOpts.Stderr = io.Discard
	}

	return exec.StreamWithContext(ctx, streamOpts)
}
