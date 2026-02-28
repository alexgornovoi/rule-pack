package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var isInteractiveTerminal = func() bool {
	return term.IsTerminal(0)
}

var confirmReaders sync.Map

func confirmRiskAction(cmd *cobra.Command, jsonMode bool, yes bool, risk bool, nonInteractiveMessage string, prompt string, preview []string, cancelledMessage string) error {
	if !risk || yes {
		return nil
	}
	if jsonMode || !isInteractiveTerminal() {
		return fmt.Errorf("%s; rerun with --yes", nonInteractiveMessage)
	}
	writePreview(cmd.ErrOrStderr(), preview)
	answer, err := readConfirmAnswer(cmd, prompt)
	if err != nil {
		return err
	}
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("%s cancelled", cancelledMessage)
	}
	return nil
}

func promptOptionalAction(cmd *cobra.Command, prompt string, preview []string) (bool, error) {
	writePreview(cmd.ErrOrStderr(), preview)
	answer, err := readConfirmAnswer(cmd, prompt)
	if err != nil {
		return false, err
	}
	return answer == "y" || answer == "yes", nil
}

func readConfirmAnswer(cmd *cobra.Command, prompt string) (string, error) {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N]: ", prompt)
	reader := confirmReader(cmd)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(line)), nil
}

func confirmReader(cmd *cobra.Command) *bufio.Reader {
	if r, ok := confirmReaders.Load(cmd); ok {
		return r.(*bufio.Reader)
	}
	r := bufio.NewReader(cmd.InOrStdin())
	actual, _ := confirmReaders.LoadOrStore(cmd, r)
	return actual.(*bufio.Reader)
}

func writePreview(w io.Writer, lines []string) {
	if len(lines) == 0 {
		return
	}
	for _, line := range lines {
		_, _ = fmt.Fprintf(w, "  - %s\n", line)
	}
}
