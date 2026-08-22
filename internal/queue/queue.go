package queue

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrFull      = errors.New("admission queue is full")
	ErrDuplicate = errors.New("admission queue item already exists")
)

type Item struct {
	ID      string
	Context context.Context
	Admit   func(context.Context) error
}

type FIFO struct {
	mu      sync.Mutex
	maxSize int
	items   []Item
	queued  map[string]struct{}
}

func New(maxSize int) *FIFO {
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &FIFO{maxSize: maxSize, items: make([]Item, 0, maxSize), queued: make(map[string]struct{})}
}

func (q *FIFO) Enqueue(item Item) (int, error) {
	if item.ID == "" || item.Admit == nil {
		return 0, errors.New("queue item requires ID and Admit callback")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.queued[item.ID]; exists {
		return 0, ErrDuplicate
	}
	if len(q.items) >= q.maxSize {
		return 0, ErrFull
	}
	if item.Context == nil {
		item.Context = context.Background()
	}
	q.items = append(q.items, item)
	q.queued[item.ID] = struct{}{}
	return len(q.items), nil
}

func (q *FIFO) Cancel(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for index, item := range q.items {
		if item.ID != id {
			continue
		}
		copy(q.items[index:], q.items[index+1:])
		last := len(q.items) - 1
		q.items[last] = Item{}
		q.items = q.items[:last]
		delete(q.queued, id)
		return true
	}
	return false
}

func (q *FIFO) Pop() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return Item{}, false
	}
	item := q.items[0]
	copy(q.items, q.items[1:])
	last := len(q.items) - 1
	q.items[last] = Item{}
	q.items = q.items[:last]
	delete(q.queued, item.ID)
	return item, true
}

func (q *FIFO) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *FIFO) Position(id string) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for index, item := range q.items {
		if item.ID == id {
			return index + 1, true
		}
	}
	return 0, false
}

type Scheduler struct {
	Queue    *FIFO
	Interval time.Duration
	OnResult func(Item, error)
}

func (s *Scheduler) Run(ctx context.Context) {
	if s.Queue == nil {
		return
	}
	interval := s.Interval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			item, ok := s.Queue.Pop()
			if !ok {
				continue
			}
			if item.Context.Err() != nil {
				if s.OnResult != nil {
					s.OnResult(item, item.Context.Err())
				}
				continue
			}
			err := item.Admit(item.Context)
			if s.OnResult != nil {
				s.OnResult(item, err)
			}
		}
	}
}
