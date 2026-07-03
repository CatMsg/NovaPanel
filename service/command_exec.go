package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	externalCommandTimeout = 12 * time.Second
)

type externalCommandError struct {
	Command string
	Reason  string
	Output  string
	Cause   error
}

func (e *externalCommandError) Error() string {
	parts := []string{e.Command}
	if e.Reason != "" {
		parts = append(parts, e.Reason)
	}
	if e.Output != "" {
		parts = append(parts, e.Output)
	}
	return strings.Join(parts, ": ")
}

func (e *externalCommandError) Unwrap() error {
	return e.Cause
}

func runCommandOutput(timeout time.Duration, name string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = externalCommandTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}

	trimmed := strings.TrimSpace(string(output))
	command := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))

	reason := "failed"
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		reason = "timed out"
	case errors.Is(err, exec.ErrNotFound):
		reason = "command not found"
	}

	return output, &externalCommandError{
		Command: command,
		Reason:  reason,
		Output:  trimmed,
		Cause:   err,
	}
}

func formatExternalCommandError(prefix string, err error) error {
	if err == nil {
		return nil
	}
	var cmdErr *externalCommandError
	if errors.As(err, &cmdErr) {
		return fmt.Errorf("%s: %w", prefix, cmdErr)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
