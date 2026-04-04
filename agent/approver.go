package agent

import "context"

// Approver is called before any tool with Dangerous() == true is executed.
// Returning false blocks the tool call; the executor emits a blocked observation.
type Approver interface {
	Approve(ctx context.Context, toolName string, args map[string]any) bool
}

// AllowAllApprover permits every dangerous tool call unconditionally.
// This is the default, preserving the existing executor behaviour.
type AllowAllApprover struct{}

func (AllowAllApprover) Approve(_ context.Context, _ string, _ map[string]any) bool { return true }

// BlockAllApprover rejects every dangerous tool call unconditionally.
// Useful for read-only or sandboxed agent modes.
type BlockAllApprover struct{}

func (BlockAllApprover) Approve(_ context.Context, _ string, _ map[string]any) bool { return false }

// FuncApprover delegates approval to an arbitrary function, enabling
// interactive prompts, policy-based checks, or test overrides.
type FuncApprover func(ctx context.Context, toolName string, args map[string]any) bool

func (f FuncApprover) Approve(ctx context.Context, name string, args map[string]any) bool {
	return f(ctx, name, args)
}
