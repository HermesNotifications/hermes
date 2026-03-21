package eventwriter

import (
	"sync"
	"time"
)

type Batch[T any] struct {
	items    []T
	maxSize  int
	flushFn  func([]T)
	mu       sync.Mutex
	timer    *time.Timer
	interval time.Duration
}

func NewBatch[T any](maxSize int, interval time.Duration, flushFn func([]T)) *Batch[T] {
	return &Batch[T]{
		maxSize:  maxSize,
		interval: interval,
		flushFn:  flushFn,
	}
}

func (b *Batch[T]) Add(item T) {
	b.mu.Lock()
	b.items = append(b.items, item)

	if len(b.items) >= b.maxSize {
		items := b.items
		b.items = nil
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
		b.mu.Unlock()
		b.flushFn(items) // call outside lock
		return
	}

	// Start timer on first item
	if len(b.items) == 1 && b.interval > 0 {
		b.timer = time.AfterFunc(b.interval, func() {
			b.mu.Lock()
			if len(b.items) > 0 {
				items := b.items
				b.items = nil
				if b.timer != nil {
					b.timer.Stop()
					b.timer = nil
				}
				b.mu.Unlock()
				b.flushFn(items)
			} else {
				b.mu.Unlock()
			}
		})
	}
	b.mu.Unlock()
}

func (b *Batch[T]) Flush() {
	b.mu.Lock()
	if len(b.items) == 0 {
		b.mu.Unlock()
		return
	}
	items := b.items
	b.items = nil
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.mu.Unlock()
	b.flushFn(items)
}
