package panel

import (
	"context"
	"strings"
	"testing"

	"github.com/AutoCookies/crabpath/callback"
)

// Ensure import used.
var _ callback.Handler = callback.NoopHandler{}

// ─── SynthMode constants ───────────────────────────────────────────────────────

func TestSynthVote_ConstantValue(t *testing.T) {
	if SynthVote != "vote" {
		t.Fatalf("SynthVote constant: want %q, got %q", "vote", SynthVote)
	}
}

func TestSynthMode_AllConstantsDefined(t *testing.T) {
	modes := []SynthMode{SynthConcat, SynthLLM, SynthFirst, SynthVote}
	for _, m := range modes {
		if string(m) == "" {
			t.Fatalf("SynthMode constant is empty: %v", m)
		}
	}
}

// ─── synthVote fallback paths (no LLM required) ───────────────────────────────

// newTestPanel creates a Panel with nil client — only valid for paths that
// don't make LLM calls (e.g., single-candidate shortcut, no-candidate fallback).
func newTestPanel(mode SynthMode) *Panel {
	return &Panel{
		synthMode: mode,
		cbs:       callback.NoopHandler{},
	}
}

func TestSynthVote_NoCandidates_FallsBackToConcat(t *testing.T) {
	p := newTestPanel(SynthVote)
	results := []RoleResult{
		{Role: "Researcher", Err: errSentinel("boom"), Answer: ""},
		{Role: "Critic", Err: errSentinel("fail"), Answer: ""},
	}
	out := p.synthVote(context.Background(), "what is X?", results)
	// With no valid answers synthVote falls back to synthConcat.
	// synthConcat includes role names even when answers are empty.
	if !strings.Contains(out, "Researcher") || !strings.Contains(out, "Critic") {
		t.Fatalf("expected concat fallback with role names, got: %q", out)
	}
}

func TestSynthVote_SingleCandidate_ReturnedDirectly(t *testing.T) {
	p := newTestPanel(SynthVote)
	results := []RoleResult{
		{Role: "Researcher", Answer: "The answer is 42.", Err: nil},
		{Role: "Critic", Err: errSentinel("no output"), Answer: ""},
	}
	out := p.synthVote(context.Background(), "what is the answer?", results)
	// Single valid candidate: returned verbatim, no LLM call.
	if out != "The answer is 42." {
		t.Fatalf("single candidate: want exact answer, got %q", out)
	}
}

func TestSynthVote_AllEmpty_FallsBackToConcat(t *testing.T) {
	p := newTestPanel(SynthVote)
	results := []RoleResult{
		{Role: "A", Answer: "   ", Err: nil}, // blank
		{Role: "B", Answer: "",    Err: nil},
	}
	out := p.synthVote(context.Background(), "something?", results)
	// No usable answers → concat fallback.
	if out == "" {
		t.Fatal("expected non-empty concat fallback")
	}
}

// ─── synthesize dispatch ──────────────────────────────────────────────────────

func TestSynthesize_VoteMode_Dispatches(t *testing.T) {
	// Confirm the switch in synthesize() routes "vote" to synthVote.
	// We test with single candidate (no LLM call) so client can be nil.
	p := newTestPanel(SynthVote)
	results := []RoleResult{
		{Role: "Solo", Answer: "only answer", Err: nil},
	}
	out := p.synthesize(context.Background(), "goal", results)
	if out != "only answer" {
		t.Fatalf("synthesize with vote mode: want 'only answer', got %q", out)
	}
}

func TestSynthesize_ConcatMode(t *testing.T) {
	p := newTestPanel(SynthConcat)
	results := []RoleResult{
		{Role: "R1", Answer: "answer one", Err: nil},
		{Role: "R2", Answer: "answer two", Err: nil},
	}
	out := p.synthesize(context.Background(), "goal", results)
	if !strings.Contains(out, "R1") || !strings.Contains(out, "answer one") {
		t.Fatalf("concat: missing R1 content: %q", out)
	}
	if !strings.Contains(out, "R2") || !strings.Contains(out, "answer two") {
		t.Fatalf("concat: missing R2 content: %q", out)
	}
}

func TestSynthesize_FirstMode(t *testing.T) {
	p := newTestPanel(SynthFirst)
	results := []RoleResult{
		{Role: "R1", Answer: "", Err: errSentinel("failed")},
		{Role: "R2", Answer: "first valid", Err: nil},
		{Role: "R3", Answer: "second valid", Err: nil},
	}
	out := p.synthesize(context.Background(), "goal", results)
	if out != "first valid" {
		t.Fatalf("first mode: want 'first valid', got %q", out)
	}
}

func TestSynthesize_FirstMode_NoValidAnswers(t *testing.T) {
	p := newTestPanel(SynthFirst)
	results := []RoleResult{
		{Role: "R1", Answer: "", Err: errSentinel("err")},
	}
	out := p.synthesize(context.Background(), "goal", results)
	if !strings.Contains(out, "no role produced") {
		t.Fatalf("first mode fallback: want 'no role produced', got %q", out)
	}
}

// ─── synthConcat ─────────────────────────────────────────────────────────────

func TestSynthConcat_FormatsRoles(t *testing.T) {
	p := newTestPanel(SynthConcat)
	results := []RoleResult{
		{Role: "Alpha", Answer: "alpha answer"},
		{Role: "Beta", Answer: "beta answer"},
	}
	out := p.synthConcat(results)
	if !strings.Contains(out, "## Alpha") {
		t.Fatalf("missing ## Alpha header: %q", out)
	}
	if !strings.Contains(out, "## Beta") {
		t.Fatalf("missing ## Beta header: %q", out)
	}
	if !strings.Contains(out, "alpha answer") || !strings.Contains(out, "beta answer") {
		t.Fatalf("missing answers: %q", out)
	}
}

func TestSynthConcat_ErrorRole_ShowsErrorLabel(t *testing.T) {
	p := newTestPanel(SynthConcat)
	results := []RoleResult{
		{Role: "Broken", Err: errSentinel("timed out"), Answer: ""},
	}
	out := p.synthConcat(results)
	if !strings.Contains(out, "error") {
		t.Fatalf("error role should show error label: %q", out)
	}
}

func TestSynthConcat_EmptyAnswer_ShowsPlaceholder(t *testing.T) {
	p := newTestPanel(SynthConcat)
	results := []RoleResult{
		{Role: "Silent", Answer: "", Err: nil},
	}
	out := p.synthConcat(results)
	if !strings.Contains(out, "no answer") {
		t.Fatalf("empty answer should show placeholder: %q", out)
	}
}

// ─── RoleResult struct ────────────────────────────────────────────────────────

func TestRoleResult_ErrField(t *testing.T) {
	rr := RoleResult{Role: "X", Err: errSentinel("boom")}
	if rr.Err == nil || rr.Err.Error() != "boom" {
		t.Fatalf("RoleResult.Err: %v", rr.Err)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
