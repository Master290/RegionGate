package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFIFOOrderingCancellationAndBounds(t *testing.T) {
	q := New(2)
	callback := func(context.Context) error { return nil }
	if position, err := q.Enqueue(Item{ID: "a", Admit: callback}); err != nil || position != 1 {
		t.Fatalf("first enqueue position=%d err=%v", position, err)
	}
	if _, err := q.Enqueue(Item{ID: "a", Admit: callback}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate error=%v", err)
	}
	if _, err := q.Enqueue(Item{ID: "b", Admit: callback}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(Item{ID: "c", Admit: callback}); !errors.Is(err, ErrFull) {
		t.Fatalf("full error=%v", err)
	}
	if position, ok := q.Position("b"); !ok || position != 2 {
		t.Fatalf("position=%d ok=%v", position, ok)
	}
	if !q.Cancel("a") || q.Cancel("missing") {
		t.Fatal("cancel semantics failed")
	}
	item, ok := q.Pop()
	if !ok || item.ID != "b" || q.Len() != 0 {
		t.Fatalf("item=%+v ok=%v len=%d", item, ok, q.Len())
	}
}

func TestSchedulerRateAndCancellation(t *testing.T) {
	q := New(4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var order []string
	results := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		id := id
		if _, err := q.Enqueue(Item{ID: id, Context: ctx, Admit: func(context.Context) error {
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := Scheduler{Queue: q, Interval: 10 * time.Millisecond, OnResult: func(_ Item, err error) { results <- err }}
	go scheduler.Run(ctx)
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("scheduler did not process item")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order=%v", order)
	}
}

func TestSchedulerSkipsCanceledItem(t *testing.T) {
	q := New(1)
	itemCtx, cancelItem := context.WithCancel(context.Background())
	cancelItem()
	called := false
	if _, err := q.Enqueue(Item{ID: "canceled", Context: itemCtx, Admit: func(context.Context) error {
		called = true
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&Scheduler{Queue: q, Interval: time.Millisecond, OnResult: func(_ Item, err error) { result <- err }}).Run(ctx)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || called {
			t.Fatalf("error=%v called=%v", err, called)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled item was not removed")
	}
}

func TestRemovalClearsBackingArrayReferences(t *testing.T) {
	q := New(2)
	callback := func(context.Context) error { return nil }
	if _, err := q.Enqueue(Item{ID: "a", Context: context.Background(), Admit: callback}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(Item{ID: "b", Context: context.Background(), Admit: callback}); err != nil {
		t.Fatal(err)
	}
	if _, ok := q.Pop(); !ok {
		t.Fatal("expected queued item")
	}
	if len(q.items) != 1 || q.items[0].ID != "b" {
		t.Fatalf("items after pop=%+v", q.items)
	}
	cleared := q.items[:cap(q.items)][1]
	if cleared.ID != "" || cleared.Context != nil || cleared.Admit != nil {
		t.Fatal("popped item was retained in backing array")
	}
	if !q.Cancel("b") {
		t.Fatal("expected second item cancellation")
	}
	cleared = q.items[:cap(q.items)][0]
	if len(q.items) != 0 || cleared.ID != "" || cleared.Context != nil || cleared.Admit != nil {
		t.Fatal("canceled item was retained in backing array")
	}
}
