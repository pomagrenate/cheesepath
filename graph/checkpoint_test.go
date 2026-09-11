package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMemorySaverAndTimeTravel(t *testing.T) {
	ctx := context.Background()
	saver := NewMemorySaver[CounterState]()

	g := NewStateGraph[CounterState](counterReducer)
	g.AddNode("inc", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1, History: []string{"inc"}}, nil
	})
	g.SetEntryPoint("inc")
	g.AddConditionalEdges("inc", func(ctx context.Context, s CounterState) (string, error) {
		if s.Count >= 3 {
			return END, nil
		}
		return "inc", nil
	}, nil)

	compiled, err := g.Compile(WithCheckpointer[CounterState](saver))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	threadID := "test-thread-1"
	final, err := compiled.Invoke(ctx, CounterState{}, WithThreadID(threadID))
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if final.Count != 3 {
		t.Fatalf("expected count 3, got %d", final.Count)
	}

	// Verify checkpoints were recorded
	list, err := saver.List(ctx, threadID)
	if err != nil {
		t.Fatalf("list checkpoints failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(list))
	}

	// Verify state progression across steps
	for i, cp := range list {
		if cp.State.Count != i+1 {
			t.Errorf("step %d: expected count %d, got %d", i, i+1, cp.State.Count)
		}
		if cp.ThreadID != threadID {
			t.Errorf("wrong thread ID: %s", cp.ThreadID)
		}
	}

	// Time-travel: Fetch specific tuple
	secondCP, err := saver.GetTuple(ctx, threadID, list[1].CheckpointID)
	if err != nil {
		t.Fatalf("get tuple failed: %v", err)
	}
	if secondCP.State.Count != 2 {
		t.Fatalf("expected count 2 in second checkpoint, got %d", secondCP.State.Count)
	}
}

func TestFileSaver(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "cheesepath-checkpoint-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	saver, err := NewFileSaver[CounterState](tempDir)
	if err != nil {
		t.Fatalf("new filesaver: %v", err)
	}

	threadID := "thread-persist"
	cp := &Checkpoint[CounterState]{
		ThreadID:     threadID,
		CheckpointID: "cp-001",
		Step:         1,
		State:        CounterState{Count: 42, History: []string{"persistent"}},
	}

	if err := saver.Put(ctx, cp); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	retrieved, err := saver.Get(ctx, threadID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if retrieved.State.Count != 42 {
		t.Fatalf("expected count 42, got %d", retrieved.State.Count)
	}

	// Verify file exists on disk
	files, _ := filepath.Glob(filepath.Join(tempDir, threadID, "*.json"))
	if len(files) != 1 {
		t.Fatalf("expected 1 json file, found %d", len(files))
	}
}

func TestHumanInTheLoopInterrupt(t *testing.T) {
	ctx := context.Background()
	saver := NewMemorySaver[CounterState]()

	g := NewStateGraph[CounterState](counterReducer)
	g.AddNode("draft", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1, History: []string{"draft"}}, nil
	})
	g.AddNode("human_approval", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 5, History: []string{"approved"}}, nil
	})
	g.AddNode("publish", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 10, History: []string{"published"}}, nil
	})

	g.SetEntryPoint("draft")
	g.AddEdge("draft", "human_approval")
	g.AddEdge("human_approval", "publish")
	g.SetFinishPoint("publish")

	// Interrupt BEFORE human_approval
	compiled, err := g.Compile(
		WithCheckpointer[CounterState](saver),
		WithInterruptBefore[CounterState]("human_approval"),
	)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	threadID := "approval-thread"
	_, err = compiled.Invoke(ctx, CounterState{}, WithThreadID(threadID))
	if err == nil {
		t.Fatal("expected interrupt error, got nil")
	}

	if !IsInterrupt(err) {
		t.Fatalf("expected ErrInterrupt, got: %v", err)
	}

	// Verify checkpoint was saved before the interrupted node
	latest, err := saver.Get(ctx, threadID)
	if err != nil {
		t.Fatalf("get latest checkpoint failed: %v", err)
	}
	if latest.State.Count != 1 {
		t.Fatalf("expected count 1 before interrupt, got %d", latest.State.Count)
	}

	// Now RESUME with human feedback / approval state!
	resumedState, err := compiled.Resume(ctx, threadID, CounterState{History: []string{"user_feedback"}})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	// Draft(1) + user_feedback + approved(5) + published(10) = 16
	if resumedState.Count != 16 {
		t.Fatalf("expected count 16 after resume, got %d", resumedState.Count)
	}
}
