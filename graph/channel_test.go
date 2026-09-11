package graph

import (
	"errors"
	"testing"
)

func TestLastValueChannel(t *testing.T) {
	ch := NewLastValueChannel[string]("test_key")

	// 1. Initial get should fail with ErrEmptyChannel
	if _, err := ch.Get(); !errors.Is(err, ErrEmptyChannel) {
		t.Fatalf("expected ErrEmptyChannel, got %v", err)
	}
	if ch.IsAvailable() {
		t.Fatal("expected IsAvailable=false")
	}

	// 2. Single update succeeds
	updated, err := ch.Update([]string{"val1"})
	if err != nil || !updated {
		t.Fatalf("expected update=true, got err=%v", err)
	}
	val, err := ch.Get()
	if err != nil || val != "val1" {
		t.Fatalf("expected val1, got %s (err: %v)", val, err)
	}

	// 3. Multiple updates in single step without aggregator should fail (LangGraph specification)
	_, err = ch.Update([]string{"val2", "val3"})
	if err == nil || !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("expected ErrInvalidUpdate for multiple values, got: %v", err)
	}

	// 4. Overwrite in next step
	updated, err = ch.Update([]string{"val4"})
	if err != nil || !updated {
		t.Fatalf("expected update=true, got: %v", err)
	}
	val, _ = ch.Get()
	if val != "val4" {
		t.Fatalf("expected val4, got: %s", val)
	}
}

func TestBinOpChannel(t *testing.T) {
	// Addition operator
	addOp := func(cur, up int) int {
		return cur + up
	}
	ch := NewBinOpChannel[int]("counter", addOp)

	// Update with multiple values in the same step
	updated, err := ch.Update([]int{10, 20, 30})
	if err != nil || !updated {
		t.Fatalf("expected update success, got: %v", err)
	}

	val, err := ch.Get()
	if err != nil || val != 60 {
		t.Fatalf("expected 60 (10+20+30), got %d (err: %v)", val, err)
	}

	// Next step: add 40
	_, _ = ch.Update([]int{40})
	val, _ = ch.Get()
	if val != 100 {
		t.Fatalf("expected 100, got %d", val)
	}
}

func TestTopicChannel(t *testing.T) {
	// Non-accumulating topic: clears on new step
	t1 := NewTopicChannel[string]("events", false)

	_, _ = t1.Update([]string{"e1", "e2"})
	vals, _ := t1.Get()
	if len(vals) != 2 || vals[0] != "e1" || vals[1] != "e2" {
		t.Fatalf("unexpected values: %v", vals)
	}

	// Next step: should clear previous and only retain new
	_, _ = t1.Update([]string{"e3"})
	vals, _ = t1.Get()
	if len(vals) != 1 || vals[0] != "e3" {
		t.Fatalf("expected only [e3], got: %v", vals)
	}

	// Accumulating topic
	t2 := NewTopicChannel[string]("history", true)
	_, _ = t2.Update([]string{"h1"})
	_, _ = t2.Update([]string{"h2", "h3"})
	vals, _ = t2.Get()
	if len(vals) != 3 {
		t.Fatalf("expected 3 accumulated items, got: %d", len(vals))
	}
}

func TestEphemeralChannel(t *testing.T) {
	ch := NewEphemeralChannel[string]("temp", true)

	// Step 1: Write value
	_, _ = ch.Update([]string{"hello"})
	val, err := ch.Get()
	if err != nil || val != "hello" {
		t.Fatalf("expected hello, got %s", val)
	}

	// Step 2: Empty update (consumed) -> clears
	updated, _ := ch.Update([]string{})
	if !updated {
		t.Fatal("expected updated=true when clearing")
	}
	if ch.IsAvailable() {
		t.Fatal("expected ephemeral channel to be unavailable")
	}
	if _, err := ch.Get(); !errors.Is(err, ErrEmptyChannel) {
		t.Fatalf("expected ErrEmptyChannel, got %v", err)
	}
}
