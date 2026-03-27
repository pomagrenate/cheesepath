package agent

import (
	"strings"
	"testing"
)

func TestFocusedUserGoalUsesLastParagraph(t *testing.T) {
	in := "Autonomous execution mode:\n- rules here\n\nList files in root directory"
	got := focusedUserGoal(in)
	if got != "List files in root directory" {
		t.Fatalf("focusedUserGoal=%q", got)
	}
}

func TestInferPathFromGoalPrefersRelative(t *testing.T) {
	in := "Run tests for ./cmd/cheeserag-agent and summarize"
	got := inferPathFromGoal(in)
	if got != "./cmd/cheeserag-agent" {
		t.Fatalf("inferPathFromGoal=%q", got)
	}
}

func TestInferLocalExecFromGoalReadFile(t *testing.T) {
	in := "Read file /home/autocookie/pomaieco/cheeserag/README.md and show first lines"
	got := inferLocalExecFromGoal(in)
	want := "sed -n '1,200p' '/home/autocookie/pomaieco/cheeserag/README.md'"
	if got != want {
		t.Fatalf("inferLocalExecFromGoal=%q want=%q", got, want)
	}
}

func TestClassifyGoalIntent(t *testing.T) {
	if got := classifyGoalIntent("Check endpoint http://127.0.0.1:9090/health"); got != intentHTTPCheck {
		t.Fatalf("intent=%q", got)
	}
	if got := classifyGoalIntent("Run tests for ./cmd/cheeserag-agent and summarize"); got != intentRunTests {
		t.Fatalf("intent=%q", got)
	}
	if got := classifyGoalIntent("List all files in root directory"); got != intentReadOnly {
		t.Fatalf("intent=%q", got)
	}
}

func TestApplyIntentToolPolicyFiltersUnsafeCalls(t *testing.T) {
	t.Setenv("CHEESERAG_DETERMINISTIC_AUTONOMOUS", "1")
	in := []CrabToolCall{
		{ToolName: "local_exec"},
		{ToolName: "proc_start"},
		{ToolName: "proc_logs"},
		{ToolName: "http_check"},
	}
	out := applyIntentToolPolicy(in, intentReadOnly)
	if len(out) != 2 {
		t.Fatalf("len(out)=%d want=2", len(out))
	}
	if out[0].ToolName != "local_exec" || out[1].ToolName != "http_check" {
		t.Fatalf("unexpected filtered result: %+v", out)
	}
}

func TestApplyIntentToolPolicyCanBeDisabled(t *testing.T) {
	t.Setenv("CHEESERAG_DETERMINISTIC_AUTONOMOUS", "0")
	in := []CrabToolCall{
		{ToolName: "local_exec"},
		{ToolName: "proc_start"},
		{ToolName: "http_check"},
	}
	out := applyIntentToolPolicy(in, intentReadOnly)
	if len(out) != len(in) {
		t.Fatalf("deterministic disabled should keep all calls, got=%d want=%d", len(out), len(in))
	}
}

func TestDeterministicAutonomousEnabledDefaultsTrue(t *testing.T) {
	t.Setenv("CHEESERAG_DETERMINISTIC_AUTONOMOUS", "")
	if !deterministicAutonomousEnabled() {
		t.Fatal("expected deterministic autonomous enabled by default")
	}
}

func TestErrorStreakThresholdDefaultsAndBounds(t *testing.T) {
	t.Setenv("CHEESERAG_AUTONOMOUS_ERROR_STREAK", "")
	if got := errorStreakThreshold(); got != 2 {
		t.Fatalf("default threshold=%d", got)
	}
	t.Setenv("CHEESERAG_AUTONOMOUS_ERROR_STREAK", "-1")
	if got := errorStreakThreshold(); got != 0 {
		t.Fatalf("negative threshold should clamp to 0, got=%d", got)
	}
	t.Setenv("CHEESERAG_AUTONOMOUS_ERROR_STREAK", "99")
	if got := errorStreakThreshold(); got != 10 {
		t.Fatalf("high threshold should clamp to 10, got=%d", got)
	}
}

func TestStepErrorSignature(t *testing.T) {
	st := CrabStep{
		ToolCalls: []CrabToolCall{
			{ToolName: "local_exec", Error: "boom\nline2"},
			{ToolName: "http_check", Result: "ok"},
			{ToolName: "proc_start", Error: "bad args"},
		},
	}
	got := stepErrorSignature(st)
	if got == "" {
		t.Fatal("expected non-empty signature")
	}
	if !containsAll(got, []string{"local_exec:boom line2", "proc_start:bad args"}) {
		t.Fatalf("unexpected signature: %q", got)
	}
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
