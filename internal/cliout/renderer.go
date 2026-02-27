package cliout

import "encoding/json"

type Table struct {
	Title   string     `json:"title"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

type Event struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type HumanPayload struct {
	Command string
	Title   string
	Tables  []Table
	Events  []Event
	Summary map[string]string
	Done    string
}

type Renderer interface {
	RenderHuman(payload HumanPayload)
	RenderJSON(command string, payload any) error
	RenderError(command string, err error)
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}
