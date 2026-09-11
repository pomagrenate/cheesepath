package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

func TestPipe(t *testing.T) {
	ctx := context.Background()
	step1 := Func(func(ctx context.Context, in int) (string, error) {
		return strconv.Itoa(in * 2), nil
	})
	step2 := Func(func(ctx context.Context, in string) (int, error) {
		val, _ := strconv.Atoi(in)
		return val + 10, nil
	})

	pipeline := Pipe(step1, step2)
	res, err := pipeline.Invoke(ctx, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != 20 { // (5 * 2) + 10 = 20
		t.Fatalf("expected 20, got %d", res)
	}
}

func TestParallel(t *testing.T) {
	ctx := context.Background()
	b1 := Func(func(ctx context.Context, in int) (string, error) {
		return fmt.Sprintf("double:%d", in*2), nil
	})
	b2 := Func(func(ctx context.Context, in int) (string, error) {
		return fmt.Sprintf("triple:%d", in*3), nil
	})

	par := Parallel(map[string]Runnable[int, string]{
		"d": b1,
		"t": b2,
	})

	res, err := par.Invoke(ctx, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["d"] != "double:8" || res["t"] != "triple:12" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestBranch(t *testing.T) {
	ctx := context.Background()
	even := Func(func(ctx context.Context, in int) (string, error) {
		return "even", nil
	})
	odd := Func(func(ctx context.Context, in int) (string, error) {
		return "odd", nil
	})

	router := Branch(
		func(ctx context.Context, in int) string {
			if in%2 == 0 {
				return "even"
			}
			return "odd"
		},
		map[string]Runnable[int, string]{
			"even": even,
			"odd":  odd,
		},
		nil,
	)

	r1, _ := router.Invoke(ctx, 4)
	if r1 != "even" {
		t.Fatalf("expected even, got %s", r1)
	}
	r2, _ := router.Invoke(ctx, 7)
	if r2 != "odd" {
		t.Fatalf("expected odd, got %s", r2)
	}
}

func TestFallback(t *testing.T) {
	ctx := context.Background()
	fail := Func(func(ctx context.Context, in string) (string, error) {
		return "", errors.New("primary broken")
	})
	success := Func(func(ctx context.Context, in string) (string, error) {
		return "recovered:" + in, nil
	})

	fb := Fallback(fail, success)
	res, err := fb.Invoke(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "recovered:test" {
		t.Fatalf("expected recovered:test, got %s", res)
	}
}
