// Package runnable defines the core composable unit of crabchain, inspired by
// LangChain's LCEL. Any component that transforms an input into an output
// implements Runnable[I, O] and can be composed with Pipe.
package runnable

import "context"

// Runnable is the core interface: transforms I → O.
type Runnable[I, O any] interface {
	Invoke(ctx context.Context, input I) (O, error)
	Stream(ctx context.Context, input I) (<-chan O, error)
}

// Pipe chains two Runnables: output of first becomes input of second.
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
