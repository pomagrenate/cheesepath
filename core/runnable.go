package core

import (
	"context"
	"fmt"
	"sync"
)

// Runnable defines the standard composable execution unit in Cheesepath (LCEL).
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, input I) (O, error)
	Stream(ctx context.Context, input I) (<-chan O, error)
}

// Func wraps a plain function as a Runnable.
func Func[I, O any](fn func(ctx context.Context, in I) (O, error)) Runnable[I, O] {
	return &funcRunnable[I, O]{fn: fn}
}

type funcRunnable[I, O any] struct {
	fn func(ctx context.Context, in I) (O, error)
}

func (f *funcRunnable[I, O]) Invoke(ctx context.Context, in I) (O, error) {
	return f.fn(ctx, in)
}

func (f *funcRunnable[I, O]) Stream(ctx context.Context, in I) (<-chan O, error) {
	ch := make(chan O, 1)
	go func() {
		defer close(ch)
		out, err := f.fn(ctx, in)
		if err == nil {
			ch <- out
		}
	}()
	return ch, nil
}

// Pipe chains two Runnables together: the output of first becomes the input to second.
func Pipe[A, B, C any](first Runnable[A, B], second Runnable[B, C]) Runnable[A, C] {
	return &pipeRunnable[A, B, C]{first: first, second: second}
}

type pipeRunnable[A, B, C any] struct {
	first  Runnable[A, B]
	second Runnable[B, C]
}

func (p *pipeRunnable[A, B, C]) Invoke(ctx context.Context, input A) (C, error) {
	mid, err := p.first.Invoke(ctx, input)
	if err != nil {
		var zero C
		return zero, err
	}
	return p.second.Invoke(ctx, mid)
}

func (p *pipeRunnable[A, B, C]) Stream(ctx context.Context, input A) (<-chan C, error) {
	mid, err := p.first.Invoke(ctx, input)
	if err != nil {
		return nil, err
	}
	return p.second.Stream(ctx, mid)
}

// Parallel runs multiple Runnables concurrently on the same input, returning a map of outputs.
func Parallel[I, O any](branches map[string]Runnable[I, O]) Runnable[I, map[string]O] {
	return &parallelRunnable[I, O]{branches: branches}
}

type parallelRunnable[I, O any] struct {
	branches map[string]Runnable[I, O]
}

func (p *parallelRunnable[I, O]) Invoke(ctx context.Context, input I) (map[string]O, error) {
	results := make(map[string]O)
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup

	for key, r := range p.branches {
		wg.Add(1)
		go func(k string, runner Runnable[I, O]) {
			defer wg.Done()
			out, err := runner.Invoke(ctx, input)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("branch %q: %w", k, err)
				return
			}
			results[k] = out
		}(key, r)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func (p *parallelRunnable[I, O]) Stream(ctx context.Context, input I) (<-chan map[string]O, error) {
	res, err := p.Invoke(ctx, input)
	if err != nil {
		return nil, err
	}
	ch := make(chan map[string]O, 1)
	ch <- res
	close(ch)
	return ch, nil
}

// Branch conditionally routes input to a matching branch based on selector.
func Branch[I, O any](
	selector func(ctx context.Context, input I) string,
	branches map[string]Runnable[I, O],
	defaultBranch Runnable[I, O],
) Runnable[I, O] {
	return &branchRunnable[I, O]{
		selector:      selector,
		branches:      branches,
		defaultBranch: defaultBranch,
	}
}

type branchRunnable[I, O any] struct {
	selector      func(ctx context.Context, input I) string
	branches      map[string]Runnable[I, O]
	defaultBranch Runnable[I, O]
}

func (b *branchRunnable[I, O]) Invoke(ctx context.Context, input I) (O, error) {
	target := b.selector(ctx, input)
	if runner, ok := b.branches[target]; ok {
		return runner.Invoke(ctx, input)
	}
	if b.defaultBranch != nil {
		return b.defaultBranch.Invoke(ctx, input)
	}
	var zero O
	return zero, fmt.Errorf("branch %q not found and no default branch configured", target)
}

func (b *branchRunnable[I, O]) Stream(ctx context.Context, input I) (<-chan O, error) {
	target := b.selector(ctx, input)
	if runner, ok := b.branches[target]; ok {
		return runner.Stream(ctx, input)
	}
	if b.defaultBranch != nil {
		return b.defaultBranch.Stream(ctx, input)
	}
	return nil, fmt.Errorf("branch %q not found and no default branch configured", target)
}

// Fallback tries primary first; if it returns an error, it tries fallbacks in sequence.
func Fallback[I, O any](primary Runnable[I, O], fallbacks ...Runnable[I, O]) Runnable[I, O] {
	return &fallbackRunnable[I, O]{
		primary:   primary,
		fallbacks: fallbacks,
	}
}

type fallbackRunnable[I, O any] struct {
	primary   Runnable[I, O]
	fallbacks []Runnable[I, O]
}

func (f *fallbackRunnable[I, O]) Invoke(ctx context.Context, input I) (O, error) {
	out, err := f.primary.Invoke(ctx, input)
	if err == nil {
		return out, nil
	}
	var lastErr error = err
	for i, fb := range f.fallbacks {
		out, err = fb.Invoke(ctx, input)
		if err == nil {
			return out, nil
		}
		lastErr = fmt.Errorf("fallback %d failed: %w (original: %v)", i, err, lastErr)
	}
	var zero O
	return zero, lastErr
}

func (f *fallbackRunnable[I, O]) Stream(ctx context.Context, input I) (<-chan O, error) {
	ch, err := f.primary.Stream(ctx, input)
	if err == nil {
		return ch, nil
	}
	for _, fb := range f.fallbacks {
		ch, err = fb.Stream(ctx, input)
		if err == nil {
			return ch, nil
		}
	}
	return nil, err
}
