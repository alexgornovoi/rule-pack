package cliout

import (
	"encoding/json"
	"os"
)

type JSONRenderer struct{}

func NewJSONRenderer() *JSONRenderer {
	return &JSONRenderer{}
}

func (r *JSONRenderer) RenderHuman(payload HumanPayload) {
	_ = r.RenderJSON(payload.Command, payload)
}

func (r *JSONRenderer) RenderJSON(command string, payload any) error {
	out := map[string]any{
		"command": command,
		"result":  payload,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func (r *JSONRenderer) RenderError(command string, err error) {
	_ = r.RenderJSON("error", map[string]any{
		"failedCommand": command,
		"error": map[string]string{
			"message": err.Error(),
		},
	})
}
