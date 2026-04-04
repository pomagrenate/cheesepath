package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AutoCookies/crabpath/llm"
)

// Checkpoint captures enough agent state to resume a run after a crash or
// interruption. It is serialised as JSON and written atomically.
type Checkpoint struct {
	ID       string        `json:"id"`
	Goal     string        `json:"goal"`
	Strategy string        `json:"strategy"` // strategy.Name()
	Step     int           `json:"step"`
	History  []llm.Message `json:"history"`
	SavedAt  time.Time     `json:"saved_at"`
}

// SaveCheckpoint writes a checkpoint to path, overwriting any existing file.
// It records the current step index and the full conversation history.
func SaveCheckpoint(path string, id, goal, strategy string, step int, history []llm.Message) error {
	c := Checkpoint{
		ID:       id,
		Goal:     goal,
		Strategy: strategy,
		Step:     step,
		History:  history,
		SavedAt:  time.Now().UTC(),
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("checkpoint: write %s: %w", path, err)
	}
	return nil
}

// LoadCheckpoint reads and deserialises a checkpoint from path.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read %s: %w", path, err)
	}
	var c Checkpoint
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("checkpoint: unmarshal: %w", err)
	}
	return &c, nil
}

// EnableCheckpointing configures the executor to save a checkpoint to path
// every everyN steps. Set everyN ≤ 0 to disable.
func (e *Executor) EnableCheckpointing(path string, everyN int) {
	e.checkpointPath = path
	e.checkpointEvery = everyN
}

// RestoreCheckpoint re-seeds the executor's memory with the checkpoint history
// so the next Run continues from where the previous one left off.
func (e *Executor) RestoreCheckpoint(c *Checkpoint) error {
	if err := e.mem.Clear(); err != nil {
		return fmt.Errorf("checkpoint restore: clear memory: %w", err)
	}
	for _, msg := range c.History {
		if err := e.mem.Add(msg); err != nil {
			return fmt.Errorf("checkpoint restore: add message: %w", err)
		}
	}
	return nil
}
