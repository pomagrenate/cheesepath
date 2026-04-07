package agent

import (
	"testing"
)

// ─── WithMaxHistory option ─────────────────────────────────────────────────────

func TestWithMaxHistory_SetsField(t *testing.T) {
	e := NewExecutor(nil, nil, WithMaxHistory(25))
	if e.maxHistoryMsgs != 25 {
		t.Fatalf("WithMaxHistory(25): got maxHistoryMsgs=%d", e.maxHistoryMsgs)
	}
}

func TestWithMaxHistory_Zero_Unlimited(t *testing.T) {
	e := NewExecutor(nil, nil, WithMaxHistory(0))
	if e.maxHistoryMsgs != 0 {
		t.Fatalf("WithMaxHistory(0) should be unlimited (0), got %d", e.maxHistoryMsgs)
	}
}

func TestNewExecutor_DefaultMaxHistory_Zero(t *testing.T) {
	// Default executor has no history cap (unlimited).
	e := NewExecutor(nil, nil)
	if e.maxHistoryMsgs != 0 {
		t.Fatalf("default maxHistoryMsgs should be 0 (unlimited), got %d", e.maxHistoryMsgs)
	}
}

// ─── WithMaxObservationBytes option ───────────────────────────────────────────

func TestWithMaxObservationBytes_SetsField(t *testing.T) {
	e := NewExecutor(nil, nil, WithMaxObservationBytes(8192))
	if e.maxObsBytes != 8192 {
		t.Fatalf("WithMaxObservationBytes(8192): got maxObsBytes=%d", e.maxObsBytes)
	}
}

func TestWithMaxObservationBytes_Zero_Unlimited(t *testing.T) {
	e := NewExecutor(nil, nil, WithMaxObservationBytes(0))
	if e.maxObsBytes != 0 {
		t.Fatalf("WithMaxObservationBytes(0) should be unlimited (0), got %d", e.maxObsBytes)
	}
}

func TestNewExecutor_DefaultMaxObsBytes_Zero(t *testing.T) {
	e := NewExecutor(nil, nil)
	if e.maxObsBytes != 0 {
		t.Fatalf("default maxObsBytes should be 0 (unlimited), got %d", e.maxObsBytes)
	}
}

// ─── Observation truncation logic (unit-level) ────────────────────────────────

// truncateObservations mirrors the inline truncation logic in executor.go.
// Extracted here as a test helper to validate the algorithm without a full run.
func truncateObservations(observations []string, maxBytes int) []string {
	if maxBytes <= 0 {
		return observations
	}
	result := make([]string, len(observations))
	copy(result, observations)
	for i, o := range result {
		if len(o) > maxBytes {
			result[i] = o[:maxBytes] + "\n…[truncated]"
		}
	}
	return result
}

func TestTruncateObservations_ShortObsUnchanged(t *testing.T) {
	obs := []string{"short", "also short"}
	out := truncateObservations(obs, 100)
	for i, o := range out {
		if o != obs[i] {
			t.Fatalf("obs[%d] should be unchanged, got %q", i, o)
		}
	}
}

func TestTruncateObservations_LongObsTruncated(t *testing.T) {
	long := string(make([]byte, 200))
	for i := range long {
		_ = i
	}
	obs := []string{string(make([]rune, 200))}
	out := truncateObservations(obs, 50)
	if len(out[0]) != 50+len("\n…[truncated]") {
		t.Fatalf("truncated obs length wrong: %d", len(out[0]))
	}
	if out[0][50:] != "\n…[truncated]" {
		t.Fatalf("truncation suffix wrong: %q", out[0][50:])
	}
}

func TestTruncateObservations_ZeroLimit_Unchanged(t *testing.T) {
	long := string(make([]rune, 500))
	obs := []string{long}
	out := truncateObservations(obs, 0)
	if len(out[0]) != len(long) {
		t.Fatalf("zero limit should leave obs unchanged")
	}
}

func TestTruncateObservations_ExactlyAtLimit_Unchanged(t *testing.T) {
	obs := []string{"exactly ten"}
	out := truncateObservations(obs, len("exactly ten"))
	if out[0] != "exactly ten" {
		t.Fatalf("exactly-at-limit should be unchanged, got %q", out[0])
	}
}

func TestTruncateObservations_MultipleObsSomeOver(t *testing.T) {
	obs := []string{
		"short",
		string(make([]rune, 200)),
		"also short",
	}
	out := truncateObservations(obs, 10)
	if out[0] != "short" {
		t.Fatalf("obs[0] should be unchanged")
	}
	if len(out[1]) != 10+len("\n…[truncated]") {
		t.Fatalf("obs[1] should be truncated to 10+suffix")
	}
	if out[2] != "also short" {
		t.Fatalf("obs[2] should be unchanged")
	}
}

// ─── History sliding window logic (unit-level) ────────────────────────────────

// trimHistory mirrors the inline sliding-window logic in executor.go.
func trimHistory(history []string, maxMsgs int) []string {
	if maxMsgs <= 0 || len(history) <= maxMsgs {
		return history
	}
	excess := len(history) - maxMsgs
	result := make([]string, 0, maxMsgs)
	result = append(result, history[0])          // always keep system msg
	result = append(result, history[1+excess:]...) // keep latest
	return result
}

func TestTrimHistory_BelowCap_Unchanged(t *testing.T) {
	h := []string{"sys", "a", "b", "c"}
	out := trimHistory(h, 10)
	if len(out) != len(h) {
		t.Fatalf("below cap: want %d, got %d", len(h), len(out))
	}
}

func TestTrimHistory_ZeroCap_Unchanged(t *testing.T) {
	h := []string{"sys", "a", "b", "c", "d", "e"}
	out := trimHistory(h, 0)
	if len(out) != len(h) {
		t.Fatalf("zero cap: want %d, got %d", len(h), len(out))
	}
}

func TestTrimHistory_OverCap_SystemMsgPreserved(t *testing.T) {
	h := []string{"[system]", "a", "b", "c", "d", "e"}
	out := trimHistory(h, 4)
	if out[0] != "[system]" {
		t.Fatalf("system msg not preserved: %q", out[0])
	}
}

func TestTrimHistory_OverCap_LatestKept(t *testing.T) {
	// h has 6 msgs; cap=4 → keep system + last 3
	h := []string{"sys", "old1", "old2", "keep1", "keep2", "keep3"}
	out := trimHistory(h, 4)
	if len(out) != 4 {
		t.Fatalf("want 4 msgs, got %d: %v", len(out), out)
	}
	if out[1] != "keep1" || out[2] != "keep2" || out[3] != "keep3" {
		t.Fatalf("latest messages not kept: %v", out)
	}
}

func TestTrimHistory_ExactlyAtCap_Unchanged(t *testing.T) {
	h := []string{"sys", "a", "b", "c"}
	out := trimHistory(h, 4)
	if len(out) != 4 {
		t.Fatalf("exactly at cap: want 4, got %d", len(out))
	}
}
