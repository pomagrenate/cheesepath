// Package panel provides a multi-role agent panel: N agents with different roles
// run concurrently on the same goal, then their answers are synthesized into one
// final response. Roles are fully configurable (strategy, system prefix, tool filter).
package panel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AutoCookies/crabpath/agent"
	"github.com/AutoCookies/crabpath/callback"
	"github.com/AutoCookies/crabpath/llm"
	"github.com/AutoCookies/crabpath/memory"
	"github.com/AutoCookies/crabpath/tools"
)

// SynthMode controls how role answers are merged into one final answer.
type SynthMode string

const (
	// SynthConcat joins all role answers under Markdown headers. No LLM call.
	SynthConcat SynthMode = "concat"
	// SynthLLM sends all answers to the LLM for a synthesized response.
	SynthLLM SynthMode = "llm"
	// SynthFirst returns the first role's non-empty, non-error answer.
	SynthFirst SynthMode = "first"
)

// Role defines one participant in the panel.
type Role struct {
	// Name is a human-readable label, e.g. "Researcher", "Critic".
	Name string
	// Strategy is the reasoning strategy assigned to this role.
	Strategy agent.Strategy
	// SystemPrefix is prepended to the goal so the model knows its role.
	// Example: "You are a thorough researcher. Focus only on gathering facts."
	SystemPrefix string
	// MaxSteps caps the role's agent loop. 0 inherits from Panel.maxSteps.
	MaxSteps int
	// ToolFilter limits which tools this role can call. Empty = all tools.
	ToolFilter []string
}

// RoleResult captures one role's execution output.
type RoleResult struct {
	Role     string
	Answer   string
	Steps    int
	Duration time.Duration
	Err      error
}

// PanelResult is the final output of Panel.Run.
type PanelResult struct {
	Goal      string
	Roles     []RoleResult
	Synthesis string
	Duration  time.Duration
}

// Panel orchestrates concurrent multi-role agents then synthesizes their answers.
type Panel struct {
	client    *llm.Client
	registry  *tools.Registry
	model     string
	roles     []Role
	maxSteps  int
	synthMode SynthMode
	cbs       callback.Handler
}

// PanelOption configures a Panel.
type PanelOption func(*Panel)

// WithSynthMode sets the synthesis strategy (default: SynthConcat).
func WithSynthMode(m SynthMode) PanelOption { return func(p *Panel) { p.synthMode = m } }

// WithPanelCallbacks attaches a callback handler for observability.
func WithPanelCallbacks(h callback.Handler) PanelOption { return func(p *Panel) { p.cbs = h } }

// WithPanelMaxSteps sets the default per-role step budget (default: 8).
func WithPanelMaxSteps(n int) PanelOption { return func(p *Panel) { p.maxSteps = n } }

// WithPanelModel overrides the LLM model for all roles (default: inherits from client).
func WithPanelModel(m string) PanelOption { return func(p *Panel) { p.model = m } }

// NewPanel creates a Panel with sane defaults.
func NewPanel(client *llm.Client, registry *tools.Registry, roles []Role, opts ...PanelOption) *Panel {
	p := &Panel{
		client:    client,
		registry:  registry,
		roles:     roles,
		maxSteps:  8,
		synthMode: SynthConcat,
		cbs:       callback.NoopHandler{},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Run executes all roles concurrently, waits for all to finish, then synthesizes.
// Each role gets its own Executor with isolated memory and a filtered tool registry.
func (p *Panel) Run(ctx context.Context, goal string) (*PanelResult, error) {
	start := time.Now()
	results := make([]RoleResult, len(p.roles))
	var wg sync.WaitGroup

	p.cbs.OnStart(fmt.Sprintf("[panel] goal=%q roles=%d", goal, len(p.roles)))

	for i, role := range p.roles {
		wg.Add(1)
		go func(idx int, r Role) {
			defer wg.Done()

			reg := p.filteredRegistry(r.ToolFilter)
			steps := r.MaxSteps
			if steps <= 0 {
				steps = p.maxSteps
			}

			opts := []agent.ExecutorOption{
				agent.WithStrategy(r.Strategy),
				agent.WithMemory(memory.NewBufferMemory()),
				agent.WithMaxSteps(steps),
			}
			if p.model != "" {
				opts = append(opts, agent.WithModel(p.model))
			}

			exec := agent.NewExecutor(p.client, reg, opts...)

			roleGoal := goal
			if r.SystemPrefix != "" {
				roleGoal = r.SystemPrefix + "\n\n" + goal
			}

			t0 := time.Now()
			events, path := exec.Run(ctx, roleGoal)
			for range events {
			} // drain required to avoid goroutine leak

			rr := RoleResult{
				Role:     r.Name,
				Answer:   strings.TrimSpace(path.Answer),
				Steps:    len(path.Steps),
				Duration: time.Since(t0),
			}
			if path.Status == agent.PathFailed || path.Status == agent.PathAborted {
				rr.Err = fmt.Errorf("role %q: status=%s", r.Name, path.Status)
			}
			results[idx] = rr
		}(i, role)
	}

	wg.Wait()

	synthesis := p.synthesize(ctx, goal, results)
	p.cbs.OnFinalAnswer(synthesis)

	return &PanelResult{
		Goal:      goal,
		Roles:     results,
		Synthesis: synthesis,
		Duration:  time.Since(start),
	}, nil
}

// filteredRegistry returns a sub-registry restricted to the named tools.
// If filter is empty, the full shared registry is returned unchanged.
func (p *Panel) filteredRegistry(filter []string) *tools.Registry {
	if len(filter) == 0 {
		return p.registry
	}
	sub := tools.NewRegistry()
	for _, name := range filter {
		if t, ok := p.registry.Get(name); ok {
			sub.Register(t)
		}
	}
	return sub
}

// synthesize merges role results according to the configured SynthMode.
func (p *Panel) synthesize(ctx context.Context, goal string, results []RoleResult) string {
	switch p.synthMode {
	case SynthFirst:
		for _, r := range results {
			if r.Err == nil && r.Answer != "" {
				return r.Answer
			}
		}
		return "(panel: no role produced an answer)"

	case SynthLLM:
		return p.synthLLM(ctx, goal, results)

	default: // SynthConcat
		return p.synthConcat(results)
	}
}

func (p *Panel) synthConcat(results []RoleResult) string {
	var sb strings.Builder
	for _, r := range results {
		fmt.Fprintf(&sb, "## %s", r.Role)
		if r.Err != nil {
			fmt.Fprintf(&sb, " (error: %v)", r.Err)
		}
		fmt.Fprintf(&sb, "\n\n")
		if r.Answer != "" {
			sb.WriteString(r.Answer)
		} else {
			sb.WriteString("_(no answer)_")
		}
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}

func (p *Panel) synthLLM(ctx context.Context, goal string, results []RoleResult) string {
	var sb strings.Builder
	sb.WriteString("You are a synthesis expert. Given the outputs from a panel of specialist agents below, produce a single concise, accurate, and well-structured final answer to the original goal.\n\n")
	fmt.Fprintf(&sb, "## Original Goal\n\n%s\n\n## Panel Outputs\n\n", goal)
	for _, r := range results {
		fmt.Fprintf(&sb, "### %s\n\n", r.Role)
		if r.Err != nil || r.Answer == "" {
			fmt.Fprintf(&sb, "_(agent did not produce a useful answer)_\n\n")
		} else {
			fmt.Fprintf(&sb, "%s\n\n", r.Answer)
		}
	}
	sb.WriteString("## Synthesized Final Answer\n\nProvide a clear, unified answer below:")

	resp, err := p.client.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: sb.String()},
		},
		Model: p.model,
	})
	if err != nil {
		// Fallback to concat on synthesis failure.
		return p.synthConcat(results)
	}
	return strings.TrimSpace(resp)
}

// Format renders a PanelResult as a human-readable string for CLI output.
func (r *PanelResult) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Panel completed in %s\n\n", r.Duration.Round(time.Millisecond))
	for _, rr := range r.Roles {
		status := "ok"
		if rr.Err != nil {
			status = "failed"
		}
		fmt.Fprintf(&sb, "  %-16s steps=%-3d dur=%-12s status=%s\n",
			rr.Role, rr.Steps, rr.Duration.Round(time.Millisecond), status)
	}
	sb.WriteString("\n")
	sb.WriteString(r.Synthesis)
	return sb.String()
}
