package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var isInteractiveTerminal = func() bool {
	return term.IsTerminal(0)
}

func confirmRiskAction(cmd *cobra.Command, jsonMode bool, yes bool, risk bool, nonInteractiveMessage string, prompt string, preview []string, cancelledMessage string) error {
	if !risk || yes {
		return nil
	}
	if jsonMode || !isInteractiveTerminal() {
		return fmt.Errorf("%s; rerun with --yes", nonInteractiveMessage)
	}
	writePreview(cmd.ErrOrStderr(), preview)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N]: ", prompt)
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("%s cancelled", cancelledMessage)
	}
	return nil
}

func writePreview(w io.Writer, lines []string) {
	if len(lines) == 0 {
		return
	}
	for _, line := range lines {
		_, _ = fmt.Fprintf(w, "  - %s\n", line)
	}
}
