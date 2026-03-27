package agent

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AutoCookies/crabpath/callback"
	"github.com/AutoCookies/crabpath/llm"
	"github.com/AutoCookies/crabpath/memory"
	"github.com/AutoCookies/crabpath/tools"
)

// Executor orchestrates a full agent run: strategy + tools + memory + callbacks.
type Executor struct {
	client   *llm.Client
	strategy Strategy
	registry *tools.Registry
	mem      memory.Memory
	cbs      callback.Handler
	model    string
	maxSteps int
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

func WithStrategy(s Strategy) ExecutorOption          { return func(e *Executor) { e.strategy = s } }
func WithMemory(m memory.Memory) ExecutorOption       { return func(e *Executor) { e.mem = m } }
func WithCallbacks(h callback.Handler) ExecutorOption { return func(e *Executor) { e.cbs = h } }
func WithModel(model string) ExecutorOption           { return func(e *Executor) { e.model = model } }
func WithMaxSteps(n int) ExecutorOption               { return func(e *Executor) { e.maxSteps = n } }

// NewExecutor creates an Executor with defaults: ReAct strategy, BufferMemory, 20 steps.
func NewExecutor(client *llm.Client, registry *tools.Registry, opts ...ExecutorOption) *Executor {
	e := &Executor{
		client:   client,
		strategy: NewReActStrategy(),
		registry: registry,
		mem:      memory.NewBufferMemory(),
		cbs:      callback.NoopHandler{},
		model:    "default",
		maxSteps: 20,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Run executes the agent loop for goal, streaming events on the returned channel.
// The channel is closed when the run ends. Drain it fully to avoid goroutine leaks.
func (e *Executor) Run(ctx context.Context, goal string) (<-chan StreamEvent, *CrabPath) {
	events := make(chan StreamEvent, 64)

	path := &CrabPath{
		ID:        fmt.Sprintf("crab-%d", time.Now().UnixNano()),
		Goal:      goal,
		Status:    PathRunning,
		StartedAt: time.Now(),
	}

	go func() {
		defer close(events)
		defer func() {
			now := time.Now()
			path.EndedAt = &now
		}()

		e.cbs.OnStart(goal)

		// Build tool description block for the system prompt.
		var toolDescs strings.Builder
		for _, t := range e.registry.All() {
			toolDescs.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
		}

		// Seed history from memory + new system prompt.
		history := []llm.Message{
			{Role: "system", Content: e.strategy.BuildSystemPrompt(toolDescs.String())},
		}
		for _, m := range e.mem.Messages() {
			history = append(history, m)
		}
		history = append(history, llm.Message{
			Role:    "user",
			Content: "Goal: " + goal + "\n\nBegin reasoning.",
		})
		ragPolicy := strings.Contains(goal, "RAG rules (tools:")
		autonomousPolicy := strings.Contains(goal, "Autonomous execution mode:")
		goalLower := strings.ToLower(goal)
		needPortCheck := autonomousPolicy && strings.Contains(goalLower, "port")
		needHTTPCheck := autonomousPolicy && (strings.Contains(goalLower, "http") || strings.Contains(goalLower, "endpoint") || strings.Contains(goalLower, "health"))
		goalURL := inferURLFromGoal(goal)
		forcedExecCommand := inferLocalExecFromGoal(goal)
		intent := classifyGoalIntent(goal)
		_, hasRAG := e.registry.Get("rag_retrieve")
		_, hasWebFetch := e.registry.Get("rag_fetch_wikipedia")
		ragRequired := ragPolicy && hasRAG
		webFetchAvailable := ragPolicy && hasWebFetch
		ragUsed := false
		webFetched := false
		sawNoChunks := false
		haveRetrievedContext := false
		lastToolSig := ""
		repeatToolSig := 0
		execSucceeded := false
		usedExecAction := false
		usedVerify := false
		usedInspect := false
		lastVerifyFailed := false
		didPortCheck := false
		didHTTPCheck := false
		idleAfterReady := 0
		noProgressTurns := 0
		lastModelJSON := ""
		lastErrorSig := ""
		errorStreak := 0

		// Deterministic fast-paths for common autonomous tasks.
		if autonomousPolicy && deterministicAutonomousEnabled() && forcedExecCommand != "" {
			if out, ok := runAutoExec(ctx, e, path, events, 0, forcedExecCommand); ok {
				path.Answer = "Auto-executed local command:\n" + out
				path.Status = PathCompleted
				events <- StreamEvent{Type: EventFinalAnswer, Step: 0, Payload: path.Answer}
				e.cbs.OnFinalAnswer(path.Answer)
				return
			}
		}
		if autonomousPolicy && deterministicAutonomousEnabled() && needHTTPCheck && goalURL != "" {
			if runAutoVerify(ctx, e, path, events, 0, "http_check", map[string]any{"url": goalURL}) {
				ans := summarizeReady(path.Steps)
				if strings.TrimSpace(ans) == "" {
					ans = "Auto-verified endpoint: " + goalURL
				}
				path.Answer = ans
				path.Status = PathCompleted
				events <- StreamEvent{Type: EventFinalAnswer, Step: 0, Payload: path.Answer}
				e.cbs.OnFinalAnswer(path.Answer)
				return
			}
		}

		for step := 0; step < e.maxSteps; step++ {
			events <- StreamEvent{Type: EventThinking, Step: step, Payload: ""}

			req := llm.Request{
				Model:         e.model,
				Messages:      history,
				Grammar:       e.strategy.Grammar(),
				Temperature:   0.1,
				RepeatPenalty: 1.1,
			}

			tokenCh, fullCh, errCh := e.client.CompleteStream(ctx, req)

			var raw string
		drain:
			for {
				select {
				case tok, ok := <-tokenCh:
					if !ok {
						tokenCh = nil
					} else if tok != "" {
						events <- StreamEvent{Type: EventStreamToken, Step: step, Payload: tok}
						e.cbs.OnToken(step, tok)
					}
				case full, ok := <-fullCh:
					if ok {
						raw = full
					}
					fullCh = nil
				case err, ok := <-errCh:
					if ok && err != nil {
						events <- StreamEvent{Type: EventError, Step: step, Payload: err.Error()}
						e.cbs.OnError(err)
						path.Status = PathFailed
						return
					}
					errCh = nil
				case <-ctx.Done():
					events <- StreamEvent{Type: EventError, Step: step, Payload: "context cancelled"}
					if shouldAutoSummarize() {
						if ans := summarizeFromSteps(path.Steps); strings.TrimSpace(ans) != "" {
							path.Answer = ans
							events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: ans}
							e.cbs.OnFinalAnswer(ans)
						}
					}
					path.Status = PathAborted
					return
				}
				if tokenCh == nil && fullCh == nil && errCh == nil {
					break drain
				}
			}

			if raw == "" {
				noProgressTurns++
				history = append(history, llm.Message{
					Role:    "user",
					Content: "Your response was empty. Please provide a JSON block as requested.",
				})
				if noProgressTurns >= 3 {
					history = append(history, llm.Message{
						Role:    "user",
						Content: "No progress detected for multiple turns. You must either call exactly one relevant tool now or provide final_answer if done.",
					})
				}
				continue
			}
			lastModelJSON = raw

			thought, toolCalls, err := e.strategy.ParseResponse(raw)
			if err != nil {
				noProgressTurns++
				history = append(history, llm.Message{
					Role:    "user",
					Content: "Your response was invalid JSON. Please provide exactly one JSON block.",
				})
				if noProgressTurns >= 3 {
					history = append(history, llm.Message{
						Role:    "user",
						Content: "Stop format errors: output strict JSON matching grammar. If stuck, call one diagnostic tool.",
					})
				}
				continue
			}

			e.cbs.OnThought(step, callback.ThoughtEvent{
				Reasoning:   thought.Reasoning,
				Plan:        thought.Plan,
				IsFinal:     thought.IsFinal,
				FinalAnswer: thought.FinalAnswer,
			})
			events <- StreamEvent{Type: EventThought, Step: step, Payload: thought}
			if autonomousPolicy && len(toolCalls) > 0 {
				toolCalls = applyIntentToolPolicy(toolCalls, intent)
			}
			if autonomousPolicy && deterministicAutonomousEnabled() && len(path.Steps) == 0 && needHTTPCheck && goalURL != "" {
				if runAutoVerify(ctx, e, path, events, step, "http_check", map[string]any{"url": goalURL}) {
					ans := summarizeReady(path.Steps)
					if strings.TrimSpace(ans) == "" {
						ans = "Auto-verified endpoint: " + goalURL
					}
					path.Answer = ans
					path.Status = PathCompleted
					events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: path.Answer}
					e.cbs.OnFinalAnswer(path.Answer)
					return
				}
			}
			if autonomousPolicy && deterministicAutonomousEnabled() && len(path.Steps) == 0 && forcedExecCommand != "" {
				if out, ok := runAutoExec(ctx, e, path, events, step, forcedExecCommand); ok {
					path.Answer = "Auto-executed local command:\n" + out
					path.Status = PathCompleted
					events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: path.Answer}
					e.cbs.OnFinalAnswer(path.Answer)
					return
				}
			}
			if !thought.IsFinal && execSucceeded && len(toolCalls) == 0 {
				if autonomousPolicy && usedVerify && !lastVerifyFailed {
					idleAfterReady++
					if idleAfterReady >= 2 {
						ans := summarizeReady(path.Steps)
						if strings.TrimSpace(ans) != "" {
							path.Answer = ans
							path.Status = PathCompleted
							events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: ans}
							e.cbs.OnFinalAnswer(ans)
							return
						}
					}
				}
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "You already have concrete command output. Do not continue planning. Output final JSON now with is_final=true and final_answer.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			if !thought.IsFinal && autonomousPolicy && usedExecAction && !usedVerify && len(toolCalls) == 0 {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "Next action must be verification: call port_check first (or http_check), then finalize.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			if len(toolCalls) > 0 {
				idleAfterReady = 0
				noProgressTurns = 0
			} else if !thought.IsFinal {
				noProgressTurns++
			}
			// Autonomous policy: if commands/services were executed, require verification before finalize.
			if thought.IsFinal && autonomousPolicy && len(path.Steps) == 0 {
				if goalURL != "" && runAutoVerify(ctx, e, path, events, step, "http_check", map[string]any{"url": goalURL}) {
					didHTTPCheck = true
					usedVerify = true
					lastVerifyFailed = false
					if ans := summarizeReady(path.Steps); strings.TrimSpace(ans) != "" {
						path.Answer = ans
						path.Status = PathCompleted
						events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: ans}
						e.cbs.OnFinalAnswer(ans)
						return
					}
				}
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "You cannot finalize without running tools. Execute required tools first, then finalize.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			if thought.IsFinal && autonomousPolicy && needPortCheck && !didPortCheck {
				if runAutoVerify(ctx, e, path, events, step, "port_check", map[string]any{"port": inferPortFromGoal(goal)}) {
					didPortCheck = true
					usedVerify = true
					lastVerifyFailed = false
					history = append(history,
						llm.Message{Role: "assistant", Content: raw},
						llm.Message{Role: "user", Content: "Observation:\n[port_check]: auto verification executed.\n\nContinue reasoning toward the goal."},
					)
					_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
					continue
				}
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "Before final_answer, run port_check with a numeric port and include result.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			if thought.IsFinal && autonomousPolicy && needHTTPCheck && !didHTTPCheck {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "Before final_answer, run http_check on the target endpoint and include result.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			if thought.IsFinal && autonomousPolicy && usedExecAction && !usedVerify {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "Before final_answer, you must verify results using port_check or http_check.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			if autonomousPolicy && !thought.IsFinal {
				if needPortCheck && usedExecAction && !didPortCheck && !hasToolCall(toolCalls, "port_check") {
					history = append(history,
						llm.Message{Role: "assistant", Content: raw},
						llm.Message{
							Role:    "user",
							Content: "Next tool must be port_check with numeric args.port (e.g., 8080).",
						},
					)
					_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
					continue
				}
				if needHTTPCheck && (didPortCheck || !needPortCheck) && !didHTTPCheck && !hasToolCall(toolCalls, "http_check") {
					history = append(history,
						llm.Message{Role: "assistant", Content: raw},
						llm.Message{
							Role:    "user",
							Content: "Next tool should be http_check on endpoint/health URL.",
						},
					)
					_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
					continue
				}
			}
			// Autonomous policy: if verification failed recently, require inspection before retry/final.
			if thought.IsFinal && autonomousPolicy && lastVerifyFailed && !usedInspect {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "Verification failed. Inspect proc_status/proc_logs (or run diagnostics) before finalizing.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}

			// Guardrail for RAG mode: do not allow final answer before one retrieval call.
			if thought.IsFinal && ragRequired && !ragUsed {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "Before final_answer, you must call rag_retrieve exactly once and then finalize.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			// If retrieval returned empty and a fetch tool exists, require fetching real data first.
			if thought.IsFinal && sawNoChunks && webFetchAvailable && !webFetched {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "Retrieval found no chunks. Before final_answer, call rag_fetch_wikipedia, then call rag_retrieve again.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}
			// If we already retrieved context, force model to finalize instead of calling more tools.
			if haveRetrievedContext && !thought.IsFinal && len(toolCalls) > 0 {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "You already have retrieved context. Do not call more tools. Output final JSON now with is_final=true and final_answer.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}

			if thought.IsFinal {
				if strings.TrimSpace(thought.FinalAnswer) == "" {
					history = append(history,
						llm.Message{Role: "assistant", Content: raw},
						llm.Message{
							Role:    "user",
							Content: "final_answer is empty. Provide a concise non-empty final_answer with concrete result.",
						},
					)
					_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
					continue
				}
				path.Answer = thought.FinalAnswer
				path.Status = PathCompleted
				events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: thought.FinalAnswer}
				e.cbs.OnFinalAnswer(thought.FinalAnswer)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				return
			}
			if noProgressTurns >= 4 {
				if autonomousPolicy && usedExecAction && !usedVerify {
					history = append(history,
						llm.Message{Role: "assistant", Content: raw},
						llm.Message{Role: "user", Content: "No-progress detected. Immediately run port_check or http_check now."},
					)
					_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
					continue
				}
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{Role: "user", Content: "No-progress detected. Choose one decisive next action: run one tool or finalize with concrete answer."},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}

			// Execute tool calls
			crabStep := CrabStep{Index: step, Thought: thought, ToolCalls: toolCalls}
			var observations []string
			curSig := fmt.Sprintf("%v", toolCalls)
			if curSig != "" && curSig == lastToolSig {
				repeatToolSig++
			} else {
				repeatToolSig = 0
			}
			lastToolSig = curSig
			if !thought.IsFinal && repeatToolSig >= 2 {
				history = append(history,
					llm.Message{Role: "assistant", Content: raw},
					llm.Message{
						Role:    "user",
						Content: "You are repeating the same tool calls. Stop looping. Either run a different needed command or finalize now.",
					},
				)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				continue
			}

			for i, tc := range toolCalls {
				tool, ok := e.registry.Get(tc.ToolName)
				if !ok {
					crabStep.ToolCalls[i].Error = "unknown tool: " + tc.ToolName
					observations = append(observations, fmt.Sprintf("[%s]: ERROR — unknown tool", tc.ToolName))
					continue
				}

				crabStep.ToolCalls[i].Dangerous = tool.Dangerous()
				e.cbs.OnToolCall(step, callback.ToolCallEvent{
					ToolName:  tc.ToolName,
					Args:      tc.Args,
					Dangerous: tool.Dangerous(),
				})

				if tool.Dangerous() {
					events <- StreamEvent{Type: EventApprovalReq, Step: step, Payload: tc}
				}

				if tc.ToolName == "crabtable" {
					events <- StreamEvent{Type: EventCrabTableReq, Step: step, Payload: tc}
				}

				result, execErr := tool.Execute(ctx, tc.Args)
				if tc.ToolName == "local_exec" || tc.ToolName == "proc_start" || tc.ToolName == "proc_stop" {
					usedExecAction = true
				}
				if tc.ToolName == "proc_status" || tc.ToolName == "proc_logs" || tc.ToolName == "proc_list" {
					usedInspect = true
				}
				if tc.ToolName == "rag_retrieve" {
					ragUsed = true
				}
				if tc.ToolName == "rag_fetch_wikipedia" && execErr == nil {
					webFetched = true
				}
				if tc.ToolName == "local_exec" && execErr == nil {
					execSucceeded = true
				}
				if execErr != nil {
					crabStep.ToolCalls[i].Error = execErr.Error()
					observations = append(observations, fmt.Sprintf("[%s]: ERROR — %v", tc.ToolName, execErr))
					if tc.ToolName == "port_check" || tc.ToolName == "http_check" {
						lastVerifyFailed = true
						usedVerify = true
						if !usedInspect {
							_ = runAutoInspect(ctx, e, path, events, step)
							usedInspect = true
						}
					}
					if hint := remediationHint(tc.ToolName, execErr.Error()); hint != "" {
						history = append(history, llm.Message{
							Role:    "user",
							Content: "Tool-call correction: " + hint,
						})
					}
				} else {
					crabStep.ToolCalls[i].Result = result
					if tc.ToolName == "port_check" || tc.ToolName == "http_check" {
						usedVerify = true
						lowRes := strings.ToLower(result)
						if tc.ToolName == "port_check" {
							didPortCheck = true
						}
						if tc.ToolName == "http_check" {
							didHTTPCheck = true
						}
						if strings.Contains(lowRes, "listening=false") || strings.Contains(lowRes, "status=5") || strings.Contains(lowRes, "status=4") {
							lastVerifyFailed = true
						} else {
							lastVerifyFailed = false
						}
					}
					if tc.ToolName == "rag_retrieve" && strings.Contains(strings.ToLower(result), "no matching chunks") {
						sawNoChunks = true
					}
					if tc.ToolName == "rag_retrieve" && !strings.Contains(strings.ToLower(result), "no matching chunks") {
						haveRetrievedContext = true
					}
					observations = append(observations, fmt.Sprintf("[%s]: %s", tc.ToolName, result))
				}
			}

			obs := strings.Join(observations, "\n")
			crabStep.Observation = obs
			path.Steps = append(path.Steps, crabStep)
			errSig := stepErrorSignature(crabStep)
			if strings.TrimSpace(errSig) != "" {
				if errSig == lastErrorSig {
					errorStreak++
				} else {
					lastErrorSig = errSig
					errorStreak = 1
				}
			} else {
				lastErrorSig = ""
				errorStreak = 0
			}

			events <- StreamEvent{Type: EventObservation, Step: step, Payload: obs}
			e.cbs.OnObservation(step, obs)

			history = append(history,
				llm.Message{Role: "assistant", Content: raw},
				llm.Message{Role: "user", Content: "Observation:\n" + obs + "\n\nContinue reasoning toward the goal."},
			)
			_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
			if autonomousPolicy && errorStreakThreshold() > 0 && errorStreak >= errorStreakThreshold() {
				path.Status = PathFailed
				ans := summarizeFromSteps(path.Steps)
				if strings.TrimSpace(ans) == "" {
					ans = "Auto-summary: repeated tool errors detected; stopping early to avoid infinite retries."
				}
				path.Answer = ans
				events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: ans}
				e.cbs.OnFinalAnswer(ans)
				events <- StreamEvent{
					Type:    EventError,
					Step:    step,
					Payload: "stopped early due to repeated identical tool errors",
				}
				e.cbs.OnError(fmt.Errorf("repeated identical tool errors"))
				return
			}
		}

		path.Status = PathFailed
		if shouldAutoSummarize() {
			if ans := summarizeFromSteps(path.Steps); strings.TrimSpace(ans) != "" {
				path.Answer = ans
				events <- StreamEvent{Type: EventFinalAnswer, Step: e.maxSteps, Payload: ans}
				e.cbs.OnFinalAnswer(ans)
			} else if strings.TrimSpace(lastModelJSON) != "" {
				ans := summarizeFromLastJSON(lastModelJSON)
				if strings.TrimSpace(ans) != "" {
					path.Answer = ans
					events <- StreamEvent{Type: EventFinalAnswer, Step: e.maxSteps, Payload: ans}
					e.cbs.OnFinalAnswer(ans)
				}
			}
		}
		events <- StreamEvent{
			Type:    EventError,
			Step:    e.maxSteps,
			Payload: "max steps exceeded — path abandoned",
		}
		e.cbs.OnError(fmt.Errorf("max steps exceeded"))
	}()

	return events, path
}

func shouldAutoSummarize() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CHEESERAG_AUTO_SUMMARY_ON_FAIL")))
	return v == "" || v == "1" || v == "true" || v == "yes"
}

func summarizeFromSteps(steps []CrabStep) string {
	if len(steps) == 0 {
		return ""
	}
	var okObs []string
	var errObs []string
	for _, st := range steps {
		for _, tc := range st.ToolCalls {
			if strings.TrimSpace(tc.Error) != "" {
				errObs = append(errObs, fmt.Sprintf("%s: %s", tc.ToolName, oneLine(tc.Error, 180)))
			}
			if strings.TrimSpace(tc.Result) != "" {
				okObs = append(okObs, fmt.Sprintf("%s: %s", tc.ToolName, oneLine(tc.Result, 180)))
			}
		}
	}
	if len(okObs) == 0 && len(errObs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Auto-summary (model did not finalize in time).\n")
	if len(okObs) > 0 {
		b.WriteString("Successful tool outputs:\n")
		lim := len(okObs)
		if lim > 6 {
			lim = 6
		}
		for i := 0; i < lim; i++ {
			b.WriteString("- ")
			b.WriteString(okObs[i])
			b.WriteString("\n")
		}
	}
	if len(errObs) > 0 {
		b.WriteString("Errors observed:\n")
		lim := len(errObs)
		if lim > 6 {
			lim = 6
		}
		for i := 0; i < lim; i++ {
			b.WriteString("- ")
			b.WriteString(errObs[i])
			b.WriteString("\n")
		}
	}
	b.WriteString("Recommendation: retry with higher timeout or narrower goal.")
	return strings.TrimSpace(b.String())
}

func summarizeReady(steps []CrabStep) string {
	if len(steps) == 0 {
		return ""
	}
	var okObs []string
	for _, st := range steps {
		for _, tc := range st.ToolCalls {
			if strings.TrimSpace(tc.Result) != "" {
				okObs = append(okObs, fmt.Sprintf("%s: %s", tc.ToolName, oneLine(tc.Result, 180)))
			}
		}
	}
	if len(okObs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Auto-finalized from successful execution + verification.\n")
	lim := len(okObs)
	if lim > 6 {
		lim = 6
	}
	for i := 0; i < lim; i++ {
		b.WriteString("- ")
		b.WriteString(okObs[i])
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func oneLine(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if max > 0 && len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func remediationHint(toolName, errText string) string {
	s := strings.ToLower(errText)
	switch toolName {
	case "local_exec":
		if strings.Contains(s, "command required") {
			return "local_exec needs args.command as a shell string, e.g. {\"command\":\"go version\"}."
		}
		if strings.Contains(s, "blocked by allowlist") {
			return "choose an allowed command or ask user to broaden CHEESERAG_EXEC_ALLOW."
		}
		if strings.Contains(s, "outside cheeserag_exec_root") {
			return "set args.cwd inside CHEESERAG_EXEC_ROOT."
		}
	case "port_check":
		if strings.Contains(s, "valid port required") {
			return "port_check requires numeric args.port, e.g. {\"port\":8080}."
		}
	case "http_check":
		if strings.Contains(s, "url required") {
			return "http_check needs args.url, e.g. {\"url\":\"http://127.0.0.1:8080/health\"}."
		}
		if strings.Contains(s, "connection refused") || strings.Contains(s, "no such host") {
			return "service may be down. Use port_check first, then proc_logs/proc_status, then retry http_check."
		}
	case "proc_start":
		if strings.Contains(s, "name and command") {
			return "proc_start needs args.name and args.command."
		}
	}
	return ""
}

func hasToolCall(calls []CrabToolCall, name string) bool {
	for _, c := range calls {
		if c.ToolName == name {
			return true
		}
	}
	return false
}

func inferPortFromGoal(goal string) int {
	re := regexp.MustCompile(`\b([0-9]{2,5})\b`)
	matches := re.FindAllString(goal, -1)
	for _, m := range matches {
		if p, err := strconv.Atoi(m); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	low := strings.ToLower(goal)
	// common defaults
	if strings.Contains(low, "8080") {
		return 8080
	}
	if strings.Contains(low, "3000") {
		return 3000
	}
	if strings.Contains(low, "5173") {
		return 5173
	}
	return 8080
}

func inferURLFromGoal(goal string) string {
	re := regexp.MustCompile(`https?://[^\s"']+`)
	if u := strings.TrimSpace(re.FindString(goal)); u != "" {
		return strings.TrimRight(u, ".,;)")
	}
	return ""
}

func inferLocalExecFromGoal(goal string) string {
	focus := focusedUserGoal(goal)
	low := strings.ToLower(focus)
	path := inferPathFromGoal(focus)
	if strings.Contains(low, "tail log") || strings.Contains(low, "show logs") || strings.Contains(low, "read logs") {
		if path != "" {
			return "tail -n 120 " + shellQuote(path)
		}
		return "ls -la .cache"
	}
	if strings.Contains(low, "read file") || strings.Contains(low, "show file") || strings.Contains(low, "cat file") {
		if path != "" {
			return "sed -n '1,200p' " + shellQuote(path)
		}
	}
	if strings.Contains(low, "git status") {
		return "git status --short"
	}
	if strings.Contains(low, "git diff") {
		return "git diff --stat"
	}
	if strings.Contains(low, "working directory") || strings.Contains(low, "current directory") || strings.Contains(low, "pwd") {
		return "pwd"
	}
	if strings.Contains(low, "run tests") || strings.Contains(low, "go test") {
		if path != "" {
			return "go test " + shellQuote(path)
		}
		return "go test ./cmd/cheeserag-agent ./cmd/cheeserag-ingest"
	}
	if strings.Contains(low, "list dir") || strings.Contains(low, "list directory") || strings.Contains(low, "list folder") {
		if path != "" {
			return "ls -la " + shellQuote(path)
		}
	}
	if strings.Contains(low, "list files") || strings.Contains(low, "list all files") || strings.Contains(low, "ls -la") {
		if p := inferPathFromGoal(goal); p != "" {
			return "ls -la " + shellQuote(p)
		}
	}
	if strings.Contains(low, "list") && strings.Contains(low, "file") {
		switch {
		case strings.Contains(low, "root"), strings.Contains(low, "cheeserag"):
			return "ls -la /home/autocookie/pomaieco/cheeserag"
		case strings.Contains(low, "current directory"), strings.Contains(low, "current dir"):
			return "ls -la"
		default:
			return "ls -la"
		}
	}
	if strings.Contains(low, "show working directory") || strings.Contains(low, "print working directory") {
		return "pwd"
	}
	return ""
}

type goalIntent string

const (
	intentGeneral   goalIntent = "general"
	intentReadOnly  goalIntent = "readonly"
	intentHTTPCheck goalIntent = "http_check"
	intentRunTests  goalIntent = "run_tests"
)

func classifyGoalIntent(goal string) goalIntent {
	focus := strings.ToLower(focusedUserGoal(goal))
	if strings.Contains(focus, "http://") || strings.Contains(focus, "https://") || strings.Contains(focus, "health endpoint") || strings.Contains(focus, "check endpoint") {
		return intentHTTPCheck
	}
	if strings.Contains(focus, "run tests") || strings.Contains(focus, "go test") {
		return intentRunTests
	}
	if strings.Contains(focus, "list file") ||
		strings.Contains(focus, "list files") ||
		strings.Contains(focus, "list all files") ||
		strings.Contains(focus, "list directory") ||
		strings.Contains(focus, "list folder") ||
		strings.Contains(focus, "read file") ||
		strings.Contains(focus, "show file") ||
		strings.Contains(focus, "tail log") ||
		strings.Contains(focus, "show logs") ||
		strings.Contains(focus, "pwd") ||
		strings.Contains(focus, "working directory") ||
		strings.Contains(focus, "git status") ||
		strings.Contains(focus, "git diff") {
		return intentReadOnly
	}
	return intentGeneral
}

func applyIntentToolPolicy(calls []CrabToolCall, intent goalIntent) []CrabToolCall {
	if len(calls) == 0 {
		return calls
	}
	if !deterministicAutonomousEnabled() {
		return calls
	}
	allowed := map[string]bool{}
	switch intent {
	case intentHTTPCheck:
		allowed["http_check"] = true
		allowed["port_check"] = true
	case intentReadOnly, intentRunTests:
		allowed["local_exec"] = true
		allowed["http_check"] = true
		allowed["port_check"] = true
	default:
		return calls
	}
	out := make([]CrabToolCall, 0, len(calls))
	for _, c := range calls {
		if allowed[c.ToolName] {
			out = append(out, c)
		}
	}
	return out
}

func deterministicAutonomousEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CHEESERAG_DETERMINISTIC_AUTONOMOUS")))
	return v == "" || v == "1" || v == "true" || v == "yes" || v == "on"
}

func inferPathFromGoal(goal string) string {
	relRe := regexp.MustCompile(`(\./[^\s"']+)`)
	if m := relRe.FindStringSubmatch(goal); len(m) >= 2 {
		return strings.TrimRight(m[1], ".,;)")
	}
	absRe := regexp.MustCompile(`(/(?:[^\s"']+))`)
	allAbs := absRe.FindAllString(goal, -1)
	for i := len(allAbs) - 1; i >= 0; i-- {
		p := strings.TrimRight(allAbs[i], ".,;)")
		if p == "/dev" || strings.HasPrefix(p, "/proc_") || strings.HasPrefix(p, "/v1/") || strings.HasPrefix(p, "/cmd/") {
			continue
		}
		return p
	}
	return ""
}

func focusedUserGoal(goal string) string {
	goal = strings.TrimSpace(goal)
	parts := strings.Split(goal, "\n\n")
	if len(parts) == 0 {
		return goal
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func shellQuote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "''"
	}
	// Safe single-quote escape: ' -> '\''.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func errorStreakThreshold() int {
	v := strings.TrimSpace(os.Getenv("CHEESERAG_AUTONOMOUS_ERROR_STREAK"))
	if v == "" {
		return 2
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 2
	}
	if n < 0 {
		return 0
	}
	if n > 10 {
		return 10
	}
	return n
}

func stepErrorSignature(st CrabStep) string {
	var parts []string
	for _, tc := range st.ToolCalls {
		if strings.TrimSpace(tc.Error) == "" {
			continue
		}
		parts = append(parts, tc.ToolName+":"+oneLine(tc.Error, 120))
	}
	return strings.Join(parts, "|")
}

func summarizeFromLastJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	return "Auto-summary (no finalized answer): last model response was non-final or malformed; run ended by step limit."
}

func runAutoInspect(ctx context.Context, e *Executor, path *CrabPath, events chan<- StreamEvent, step int) bool {
	if tool, ok := e.registry.Get("proc_list"); ok {
		tc := CrabToolCall{ToolName: "proc_list", Args: map[string]any{}, Dangerous: tool.Dangerous()}
		e.cbs.OnToolCall(step, callback.ToolCallEvent{ToolName: tc.ToolName, Args: tc.Args, Dangerous: tc.Dangerous})
		res, err := tool.Execute(ctx, tc.Args)
		if err != nil {
			tc.Error = err.Error()
		} else {
			tc.Result = res
		}
		crabStep := CrabStep{
			Index: step,
			Thought: CrabThought{
				Reasoning: "auto inspect injected after verify failure",
				Plan:      "list managed processes for diagnostics",
				IsFinal:   false,
			},
			ToolCalls:   []CrabToolCall{tc},
			Observation: fmt.Sprintf("[%s]: %s", tc.ToolName, oneLine(firstNonEmpty(tc.Result, tc.Error), 200)),
		}
		path.Steps = append(path.Steps, crabStep)
		events <- StreamEvent{Type: EventObservation, Step: step, Payload: crabStep.Observation}
		e.cbs.OnObservation(step, crabStep.Observation)
		return err == nil
	}
	return false
}

func runAutoVerify(ctx context.Context, e *Executor, path *CrabPath, events chan<- StreamEvent, step int, toolName string, args map[string]any) bool {
	tool, ok := e.registry.Get(toolName)
	if !ok {
		return false
	}
	tc := CrabToolCall{ToolName: toolName, Args: args, Dangerous: tool.Dangerous()}
	e.cbs.OnToolCall(step, callback.ToolCallEvent{ToolName: toolName, Args: args, Dangerous: tool.Dangerous()})
	result, err := tool.Execute(ctx, args)
	if err != nil {
		tc.Error = err.Error()
	} else {
		tc.Result = result
	}
	crabStep := CrabStep{
		Index: step,
		Thought: CrabThought{
			Reasoning: "auto verification injected by executor",
			Plan:      "verify before finalize",
			IsFinal:   false,
		},
		ToolCalls:   []CrabToolCall{tc},
		Observation: fmt.Sprintf("[%s]: %s", toolName, oneLine(firstNonEmpty(tc.Result, tc.Error), 200)),
	}
	path.Steps = append(path.Steps, crabStep)
	events <- StreamEvent{Type: EventObservation, Step: step, Payload: crabStep.Observation}
	e.cbs.OnObservation(step, crabStep.Observation)
	return err == nil
}

func runAutoExec(ctx context.Context, e *Executor, path *CrabPath, events chan<- StreamEvent, step int, command string) (string, bool) {
	tool, ok := e.registry.Get("local_exec")
	if !ok {
		return "", false
	}
	tc := CrabToolCall{
		ToolName:  "local_exec",
		Args:      map[string]any{"command": command},
		Dangerous: tool.Dangerous(),
	}
	e.cbs.OnToolCall(step, callback.ToolCallEvent{
		ToolName:  tc.ToolName,
		Args:      tc.Args,
		Dangerous: tc.Dangerous,
	})
	result, err := tool.Execute(ctx, tc.Args)
	if err != nil {
		tc.Error = err.Error()
	} else {
		tc.Result = result
	}
	crabStep := CrabStep{
		Index: step,
		Thought: CrabThought{
			Reasoning: "auto execution injected by executor",
			Plan:      "run local command to satisfy explicit goal",
			IsFinal:   false,
		},
		ToolCalls:   []CrabToolCall{tc},
		Observation: fmt.Sprintf("[%s]: %s", tc.ToolName, oneLine(firstNonEmpty(tc.Result, tc.Error), 200)),
	}
	path.Steps = append(path.Steps, crabStep)
	events <- StreamEvent{Type: EventObservation, Step: step, Payload: crabStep.Observation}
	e.cbs.OnObservation(step, crabStep.Observation)
	return tc.Result, err == nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
