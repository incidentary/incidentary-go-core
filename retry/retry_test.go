package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fastPolicy() Policy {
	return Policy{
		MaxAttempts: 5,
		Base:        time.Microsecond,
		Factor:      2,
		MaxWait:     time.Millisecond,
		Jitter:      false,
	}
}

func TestDoStopsOnFirstSuccess(t *testing.T) {
	calls := 0
	err := Do(context.Background(), fastPolicy(), func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoRetriesUntilSuccess(t *testing.T) {
	calls := 0
	err := Do(context.Background(), fastPolicy(), func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoStopsOnPermanentError(t *testing.T) {
	calls := 0
	target := errors.New("4xx")
	err := Do(context.Background(), fastPolicy(), func(ctx context.Context) error {
		calls++
		return Permanent(target)
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, target) {
		t.Fatalf("error chain should include target; got %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on Permanent)", calls)
	}
}

func TestDoExhaustsAttempts(t *testing.T) {
	calls := 0
	err := Do(context.Background(), fastPolicy(), func(ctx context.Context) error {
		calls++
		return errors.New("transient")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 5 {
		t.Fatalf("calls = %d, want 5 (MaxAttempts)", calls)
	}
}

func TestDoRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Do(ctx, fastPolicy(), func(ctx context.Context) error {
		calls++
		cancel()
		return errors.New("boom")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled in chain; got %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoValidatesPolicy(t *testing.T) {
	cases := []Policy{
		{MaxAttempts: 0, Base: time.Second, Factor: 2, MaxWait: time.Minute},
		{MaxAttempts: 3, Base: 0, Factor: 2, MaxWait: time.Minute},
		{MaxAttempts: 3, Base: time.Second, Factor: 0.5, MaxWait: time.Minute},
		{MaxAttempts: 3, Base: time.Minute, Factor: 2, MaxWait: time.Second},
	}
	for i, p := range cases {
		err := Do(context.Background(), p, func(ctx context.Context) error { return nil })
		if err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestDoNilBody(t *testing.T) {
	if err := Do(context.Background(), fastPolicy(), nil); err == nil {
		t.Fatalf("expected error for nil body")
	}
}

func TestDelayCappedAtMaxWait(t *testing.T) {
	p := Policy{
		MaxAttempts: 10,
		Base:        time.Second,
		Factor:      10,
		MaxWait:     5 * time.Second,
		Jitter:      false,
	}
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		d := p.delay(attempt)
		if d > p.MaxWait {
			t.Fatalf("attempt %d: delay %s > MaxWait %s", attempt, d, p.MaxWait)
		}
	}
}

func TestDefaultPolicyMatchesPlan(t *testing.T) {
	p := Default()
	if p.MaxAttempts != 5 {
		t.Fatalf("MaxAttempts = %d, want 5", p.MaxAttempts)
	}
	if p.Base != 100*time.Millisecond {
		t.Fatalf("Base = %s, want 100ms", p.Base)
	}
	if p.Factor != 2 {
		t.Fatalf("Factor = %v, want 2", p.Factor)
	}
	if p.MaxWait != 30*time.Second {
		t.Fatalf("MaxWait = %s, want 30s", p.MaxWait)
	}
}
