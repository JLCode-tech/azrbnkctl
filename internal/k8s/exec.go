package k8s

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecCapture executes a command in a pod container and buffers stdout/stderr.
func ExecCapture(ctx context.Context, cfg *rest.Config, clientset kubernetes.Interface, ns, pod, container string, command []string) (stdout, stderr string, err error) {
	if cfg == nil {
		return "", "", fmt.Errorf("k8s: ExecCapture: rest.Config is nil")
	}
	if clientset == nil {
		return "", "", fmt.Errorf("k8s: ExecCapture: clientset is nil")
	}
	if pod == "" {
		return "", "", fmt.Errorf("k8s: ExecCapture: pod name required")
	}
	if len(command) == 0 {
		return "", "", fmt.Errorf("k8s: ExecCapture: command required")
	}

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(pod).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("k8s: ExecCapture: SPDY executor for %s/%s: %w", ns, pod, err)
	}

	var outBuf, errBuf bytes.Buffer
	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &outBuf,
		Stderr: &errBuf,
	})
	if streamErr != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf("k8s: ExecCapture: exec %v in %s/%s: %w", command, ns, pod, streamErr)
	}
	return outBuf.String(), errBuf.String(), nil
}
