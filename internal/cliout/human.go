package cliout

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

type HumanRenderer struct {
	color bool
}

func NewHumanRenderer(noColor bool) *HumanRenderer {
	useColor := !noColor && term.IsTerminal(int(os.Stdout.Fd()))
	return &HumanRenderer{color: useColor}
}

func (r *HumanRenderer) RenderHuman(payload HumanPayload) {
	header := payload.Command
	if payload.Title != "" {
		header = payload.Title
	}
	fmt.Println(r.styleHeader(header))
	for _, evt := range payload.Events {
		switch evt.Level {
		case "warn":
			fmt.Println(r.styleWarn("! " + evt.Message))
		case "error":
			fmt.Println(r.styleErr("x " + evt.Message))
		default:
			fmt.Println(r.styleInfo("- " + evt.Message))
		}
	}
	for _, table := range payload.Tables {
		fmt.Println()
		if table.Title != "" {
			fmt.Println(r.styleSubhead(table.Title))
		}
		fmt.Println(renderTable(table.Columns, table.Rows))
	}
	if len(payload.Summary) > 0 {
		fmt.Println()
		fmt.Println(r.styleSubhead("Summary"))
		keys := make([]string, 0, len(payload.Summary))
		for k := range payload.Summary {
			keys = append(keys, k)
		}
		// keep deterministic output ordering
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for _, k := range keys {
			fmt.Printf("  %s: %s\n", k, payload.Summary[k])
		}
	}
	if payload.Done != "" {
		fmt.Println()
		fmt.Println(r.styleDone(payload.Done))
	}
}

func (r *HumanRenderer) RenderJSON(_ string, payload any) error {
	_, err := os.Stdout.Write(mustJSON(payload))
	return err
}

func (r *HumanRenderer) RenderError(_ string, err error) {
	fmt.Fprintln(os.Stderr, r.styleErr("Error: "+err.Error()))
}

func (r *HumanRenderer) styleHeader(s string) string {
	st := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	if r.color {
		st = st.Foreground(lipgloss.Color("39"))
	}
	return st.Render(s)
}

func (r *HumanRenderer) styleSubhead(s string) string {
	st := lipgloss.NewStyle().Bold(true)
	if r.color {
		st = st.Foreground(lipgloss.Color("45"))
	}
	return st.Render(s)
}

func (r *HumanRenderer) styleInfo(s string) string {
	st := lipgloss.NewStyle()
	if r.color {
		st = st.Foreground(lipgloss.Color("252"))
	}
	return st.Render(s)
}

func (r *HumanRenderer) styleWarn(s string) string {
	st := lipgloss.NewStyle()
	if r.color {
		st = st.Foreground(lipgloss.Color("214"))
	}
	return st.Render(s)
}

func (r *HumanRenderer) styleErr(s string) string {
	st := lipgloss.NewStyle().Bold(true)
	if r.color {
		st = st.Foreground(lipgloss.Color("196"))
	}
	return st.Render(s)
}

func (r *HumanRenderer) styleDone(s string) string {
	st := lipgloss.NewStyle().Bold(true)
	if r.color {
		st = st.Foreground(lipgloss.Color("42"))
	}
	return st.Render("OK " + s)
}

func renderTable(cols []string, rows [][]string) string {
	if len(cols) == 0 {
		return ""
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	for _, row := range rows {
		for i := range cols {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			if len(val) > widths[i] {
				widths[i] = len(val)
			}
		}
	}
	var b strings.Builder
	writeRow := func(values []string) {
		b.WriteString("|")
		for i := range cols {
			val := ""
			if i < len(values) {
				val = values[i]
			}
			b.WriteString(" ")
			b.WriteString(val)
			b.WriteString(strings.Repeat(" ", widths[i]-len(val)))
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	writeSep := func() {
		b.WriteString("|")
		for _, w := range widths {
			b.WriteString(strings.Repeat("-", w+2))
			b.WriteString("|")
		}
		b.WriteString("\n")
	}
	writeRow(cols)
	writeSep()
	for _, row := range rows {
		writeRow(row)
	}
	return strings.TrimRight(b.String(), "\n")
}
