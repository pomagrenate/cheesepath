// Package panel provides a multi-role agent panel: N agents with different roles
// run concurrently on the same goal, then their answers are synthesized into one
// final response. Roles are fully configurable (strategy, system prefix, tool filter).
package panel

import (
	"context"
	"fmt"
	"io"
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
	// SynthVote asks the LLM to pick the single best answer from the panel and
	// returns it verbatim. Avoids hallucination risk of free-form synthesis;
	// best for factual questions where one role is likely correct.
	SynthVote SynthMode = "vote"
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
	// Model overrides the LLM model for this role. Empty inherits from Panel.
	Model string
	// ServerURL points this role at a specific LLM server endpoint, enabling
	// true multi-model panels (different server per role). Empty = use Panel's client.
	ServerURL string
	// Client is a pre-built LLM client for this role. Set automatically when
	// ServerURL is non-empty via NewClientsForRoles. Nil = use Panel's client.
	Client *llm.Client
}

// RoleResult captures one role's execution output.
type RoleResult struct {
	Role     string
	Model    string
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
	client              *llm.Client
	registry            *tools.Registry
	model               string
	roles               []Role
	maxSteps            int
	synthMode           SynthMode
	cbs                 callback.Handler
	displayW            io.Writer // live ASCII progress display; nil = disabled
	iterativeRefinement bool      // run a critic pass + one refinement round after first pass
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

// WithPanelDisplay enables the live ASCII progress box, written to w.
// Pass os.Stderr to avoid mixing with stdout result output.
func WithPanelDisplay(w io.Writer) PanelOption { return func(p *Panel) { p.displayW = w } }

// WithIterativeRefinement enables a second pass: after all roles complete, a
// critic role reviews all first-pass answers and each role gets one revision
// chance. Doubles latency; best used on complex analytical goals.
func WithIterativeRefinement(enabled bool) PanelOption {
	return func(p *Panel) { p.iterativeRefinement = enabled }
}

// NewClientsForRoles iterates roles and creates a new llm.Client for each role
// that has a non-empty ServerURL but no Client set. Call this after ParseRoles
// and before NewPanel when using per-role server URLs.
func NewClientsForRoles(roles []Role) []Role {
	out := make([]Role, len(roles))
	copy(out, roles)
	for i := range out {
		if out[i].ServerURL != "" && out[i].Client == nil {
			out[i].Client = llm.NewClient(out[i].ServerURL)
		}
	}
	return out
}

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

	// Start the live ASCII display if a writer was provided.
	var disp *panelDisplay
	var dispDone chan struct{}
	if p.displayW != nil {
		disp = newPanelDisplay(p.displayW, p.roles, p.model)
		dispDone = make(chan struct{})
		disp.render(false)
		go func() {
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					disp.render(false)
				case <-dispDone:
					return
				}
			}
		}()
	}

	for i, role := range p.roles {
		wg.Add(1)
		go func(idx int, r Role) {
			defer wg.Done()

			reg := p.filteredRegistry(r.ToolFilter)
			steps := r.MaxSteps
			if steps <= 0 {
				steps = p.maxSteps
			}

			// Role-level client: use role's dedicated client, else panel client.
			roleClient := p.client
			if r.Client != nil {
				roleClient = r.Client
			}

			// Role-level model: use role override, fall back to panel model.
			roleModel := r.Model
			if roleModel == "" {
				roleModel = p.model
			}

			opts := []agent.ExecutorOption{
				agent.WithStrategy(r.Strategy),
				agent.WithMemory(memory.NewBufferMemory()),
				agent.WithMaxSteps(steps),
			}
			if roleModel != "" {
				opts = append(opts, agent.WithModel(roleModel))
			}

			exec := agent.NewExecutor(roleClient, reg, opts...)

			roleGoal := goal
			if r.SystemPrefix != "" {
				roleGoal = r.SystemPrefix + "\n\n" + goal
			}

			if disp != nil {
				disp.markRunning(idx)
			}

			t0 := time.Now()
			events, path := exec.Run(ctx, roleGoal)
			for range events {
			} // drain required to avoid goroutine leak

			dur := time.Since(t0)
			failed := path.Status == agent.PathFailed || path.Status == agent.PathAborted

			if disp != nil {
				disp.markDone(idx, len(path.Steps), dur, failed)
			}

			rr := RoleResult{
				Role:     r.Name,
				Model:    roleModel,
				Answer:   strings.TrimSpace(path.Answer),
				Steps:    len(path.Steps),
				Duration: dur,
			}
			if failed {
				rr.Err = fmt.Errorf("role %q: status=%s", r.Name, path.Status)
			}
			results[idx] = rr
		}(i, role)
	}

	wg.Wait()

	if disp != nil {
		close(dispDone)
		disp.render(true) // final render with total duration
	}

	// Iterative refinement: run a critic pass, then let each role revise once.
	if p.iterativeRefinement && len(results) > 1 {
		results = p.runIterativeRefinement(ctx, goal, results)
	}

	synthesis := p.synthesize(ctx, goal, results)
	p.cbs.OnFinalAnswer(synthesis)

	return &PanelResult{
		Goal:      goal,
		Roles:     results,
		Synthesis: synthesis,
		Duration:  time.Since(start),
	}, nil
}

// runIterativeRefinement runs a critic pass over first-pass answers, then gives
// each role one revision opportunity with the critique as added context.
func (p *Panel) runIterativeRefinement(ctx context.Context, goal string, firstPass []RoleResult) []RoleResult {
	// Build critique prompt from all first-pass answers.
	var sb strings.Builder
	sb.WriteString("You are a critical reviewer. The following answers were produced by specialist agents for the goal below. ")
	sb.WriteString("Identify what is missing, unclear, or potentially incorrect across all answers. Be specific and constructive.\n\n")
	fmt.Fprintf(&sb, "## Goal\n\n%s\n\n## First-Pass Answers\n\n", goal)
	for _, r := range firstPass {
		fmt.Fprintf(&sb, "### %s\n\n", r.Role)
		if r.Err != nil || r.Answer == "" {
			sb.WriteString("_(no answer produced)_\n\n")
		} else {
			fmt.Fprintf(&sb, "%s\n\n", r.Answer)
		}
	}
	sb.WriteString("## Your Critique\n\nList specific improvements needed:")

	critiqueRole, ok := BuiltinRole("critic")
	if !ok {
		return firstPass // no critic preset, skip refinement
	}
	critiqueSteps := p.maxSteps / 2
	if critiqueSteps < 2 {
		critiqueSteps = 2
	}
	critiqueRole.MaxSteps = critiqueSteps

	critiqueReg := p.filteredRegistry(critiqueRole.ToolFilter)
	critiqueExec := agent.NewExecutor(p.client, critiqueReg,
		agent.WithStrategy(critiqueRole.Strategy),
		agent.WithMemory(memory.NewBufferMemory()),
		agent.WithMaxSteps(critiqueSteps),
	)
	critiqueEvents, critiquePath := critiqueExec.Run(ctx, sb.String())
	for range critiqueEvents {
	}
	critique := strings.TrimSpace(critiquePath.Answer)
	if critique == "" {
		return firstPass // critic produced nothing useful
	}

	// Second pass: each role revises its answer in light of the critique.
	refined := make([]RoleResult, len(p.roles))
	var wg sync.WaitGroup
	for i, role := range p.roles {
		wg.Add(1)
		go func(idx int, r Role) {
			defer wg.Done()
			refineGoal := fmt.Sprintf(
				"%s\n\n## Your Previous Answer\n\n%s\n\n## Critic Feedback\n\n%s\n\nRevise your answer addressing the critique.",
				goal, firstPass[idx].Answer, critique,
			)
			if r.SystemPrefix != "" {
				refineGoal = r.SystemPrefix + "\n\n" + refineGoal
			}
			steps := r.MaxSteps
			if steps <= 0 {
				steps = p.maxSteps
			}
			roleClient := p.client
			if r.Client != nil {
				roleClient = r.Client
			}
			roleModel := r.Model
			if roleModel == "" {
				roleModel = p.model
			}
			opts := []agent.ExecutorOption{
				agent.WithStrategy(r.Strategy),
				agent.WithMemory(memory.NewBufferMemory()),
				agent.WithMaxSteps(steps),
			}
			if roleModel != "" {
				opts = append(opts, agent.WithModel(roleModel))
			}
			exec := agent.NewExecutor(roleClient, p.filteredRegistry(r.ToolFilter), opts...)
			events, path := exec.Run(ctx, refineGoal)
			for range events {
			}
			rr := firstPass[idx]
			if answer := strings.TrimSpace(path.Answer); answer != "" {
				rr.Answer = answer
				rr.Steps += len(path.Steps)
			}
			refined[idx] = rr
		}(i, role)
	}
	wg.Wait()
	return refined
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

	case SynthVote:
		return p.synthVote(ctx, goal, results)

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

// synthVote asks the LLM to pick the single best role answer and returns it
// verbatim. This avoids free-form synthesis hallucination; the LLM selects,
// not rewrites. Falls back to synthConcat if the LLM call fails or if no
// usable answers exist.
func (p *Panel) synthVote(ctx context.Context, goal string, results []RoleResult) string {
	// Collect usable answers.
	type candidate struct {
		role   string
		answer string
	}
	var candidates []candidate
	for _, r := range results {
		if r.Err == nil && strings.TrimSpace(r.Answer) != "" {
			candidates = append(candidates, candidate{r.Role, r.Answer})
		}
	}
	if len(candidates) == 0 {
		return p.synthConcat(results)
	}
	if len(candidates) == 1 {
		return candidates[0].answer
	}

	var sb strings.Builder
	sb.WriteString("You are a neutral evaluator. Below are answers from specialist agents for the same goal.\n")
	sb.WriteString("Pick the single BEST answer — the one that is most accurate, complete, and directly addresses the goal.\n")
	sb.WriteString("Reply with ONLY: the label of the best answer (e.g. \"[A]\"), a newline, and then the full text of that answer verbatim.\n\n")
	fmt.Fprintf(&sb, "Goal: %s\n\n", goal)
	labels := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for i, c := range candidates {
		label := string(labels[i%len(labels)])
		fmt.Fprintf(&sb, "[%s] %s:\n%s\n\n", label, c.role, c.answer)
	}

	resp, err := p.client.Complete(ctx, llm.Request{
		Messages: []llm.Message{{Role: "user", Content: sb.String()}},
		Model:    p.model,
	})
	if err != nil {
		return p.synthConcat(results)
	}
	resp = strings.TrimSpace(resp)
	// The model should echo the chosen answer after the label line.
	// Find the first newline and return everything after it as the answer.
	if idx := strings.Index(resp, "\n"); idx >= 0 {
		chosen := strings.TrimSpace(resp[idx+1:])
		if chosen != "" {
			return chosen
		}
	}
	// Fallback: return the full response if parsing fails.
	if resp != "" {
		return resp
	}
	return p.synthConcat(results)
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
		model := rr.Model
		if model == "" {
			model = "default"
		}
		fmt.Fprintf(&sb, "  %-16s %-20s steps=%-3d dur=%-12s status=%s\n",
			rr.Role, model, rr.Steps, rr.Duration.Round(time.Millisecond), status)
	}
	sb.WriteString("\n")
	sb.WriteString(r.Synthesis)
	return sb.String()
}
