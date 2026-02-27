package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfirmRiskAction_NonInteractiveRequiresYes(t *testing.T) {
	orig := isInteractiveTerminal
	t.Cleanup(func() { isInteractiveTerminal = orig })
	isInteractiveTerminal = func() bool { return false }

	cmd := &cobra.Command{}
	err := confirmRiskAction(cmd, false, false, true, "risky action detected", "continue?", nil, "action")
	if err == nil {
		t.Fatalf("expected error in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfirmRiskAction_JSONRequiresYes(t *testing.T) {
	orig := isInteractiveTerminal
	t.Cleanup(func() { isInteractiveTerminal = orig })
	isInteractiveTerminal = func() bool { return true }

	cmd := &cobra.Command{}
	err := confirmRiskAction(cmd, true, false, true, "risky action detected", "continue?", nil, "action")
	if err == nil {
		t.Fatalf("expected error in json mode")
	}
	if !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfirmRiskAction_InteractiveDecline(t *testing.T) {
	orig := isInteractiveTerminal
	t.Cleanup(func() { isInteractiveTerminal = orig })
	isInteractiveTerminal = func() bool { return true }

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("n\n"))
	stderr := new(bytes.Buffer)
	cmd.SetErr(stderr)

	err := confirmRiskAction(cmd, false, false, true, "risky action detected", "continue?", []string{"item-a"}, "action")
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
	if !strings.Contains(err.Error(), "action cancelled") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "item-a") {
		t.Fatalf("expected preview output, got %q", stderr.String())
	}
}

func TestConfirmRiskAction_InteractiveAccept(t *testing.T) {
	orig := isInteractiveTerminal
	t.Cleanup(func() { isInteractiveTerminal = orig })
	isInteractiveTerminal = func() bool { return true }

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("yes\n"))

	if err := confirmRiskAction(cmd, false, false, true, "risky action detected", "continue?", nil, "action"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}
