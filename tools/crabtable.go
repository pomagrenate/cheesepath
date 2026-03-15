package tools

import (
	"context"
)

// CrabTableTool allows the agent to interact with the Crab Table view.
type CrabTableTool struct{}

func NewCrabTableTool() *CrabTableTool {
	return &CrabTableTool{}
}

func (t *CrabTableTool) Name() string { return "crabtable" }

func (t *CrabTableTool) Description() string {
	return "Interact with the Crab Table spreadsheet. Actions: 'set_data' (args: range, values), 'clear' (args: none), 'get_data' (args: none), 'create_table' (args: description, range, values). Example: {'action': 'set_data', 'range': 'A1:B2', 'values': [['Name', 'Age'], ['Alice', 30]]}."
}

func (t *CrabTableTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"get_data", "set_data", "clear", "create_table"},
			},
			"range": map[string]any{
				"type": "string",
				"description": "Range in A1 notation (e.g. 'A1:B5' or just 'A1')",
			},
			"values": map[string]any{
				"type": "array",
				"items": map[string]any{"type": "array", "items": map[string]any{"type": "any"}},
				"description": "2D array of values for 'set_data'",
			},
			"description": map[string]any{
				"type": "string",
				"description": "Description for 'create_table' (e.g. 'multiplication table')",
			},
		},
		"required": []string{"action"},
	}
}

func (t *CrabTableTool) Dangerous() bool { return false }

func (t *CrabTableTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	// This base implementation will be wrapped by the service to handle frontend communication.
	// The service will intercept this call and relay it to the browser.
	return "CRABTABLE_PENDING", nil
}
