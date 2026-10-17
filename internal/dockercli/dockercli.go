// Package dockercli is a thin wrapper over the docker CLI shared by the drivers
// that run containers directly (dockerx for plain engines, gcp for Google Cloud
// emulators). It keeps the exec/stderr plumbing in one place so every driver
// reports docker failures the same way.
package dockercli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Run executes `docker args...` and returns its trimmed stdout. On failure the
// error carries docker's stderr, which is what actually explains the problem.
func Run(ctx context.Context, args ...string) (string, error) {
	return RunStdin(ctx, nil, args...)
}

// RunStdin is Run with stdin attached (e.g. a Dockerfile for `docker build -`).
func RunStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out, errb bytes.Buffer
	cmd.Stdin = stdin
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Stream runs `docker args...` writing stdout and stderr to w (used for logs).
func Stream(ctx context.Context, w io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// EnsureNetwork creates the named network unless it already exists.
func EnsureNetwork(ctx context.Context, name string) error {
	if _, err := Run(ctx, "network", "inspect", name); err == nil {
		return nil
	}
	_, err := Run(ctx, "network", "create", name)
	return err
}

// ImageExists reports whether image is present locally.
func ImageExists(ctx context.Context, image string) bool {
	_, err := Run(ctx, "image", "inspect", image)
	return err == nil
}

// Build builds tag from an in-memory Dockerfile with an empty build context, so
// the Dockerfile must be self-contained (no COPY of local files).
func Build(ctx context.Context, tag, dockerfile string) error {
	if _, err := RunStdin(ctx, strings.NewReader(dockerfile), "build", "--label", "managed-by=keel", "-t", tag, "-"); err != nil {
		return fmt.Errorf("docker build %s: %w", tag, err)
	}
	return nil
}
