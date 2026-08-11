// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package eventwriter

import (
	"context"
	"sync"
	"time"
)

// BatchItem pairs a message with its originating trace context.
type BatchItem[T any] struct {
	Ctx context.Context
	Msg T
}

type Batch[T any] struct {
	items    []BatchItem[T]
	maxSize  int
	flushFn  func([]BatchItem[T])
	mu       sync.Mutex
	timer    *time.Timer
	interval time.Duration
}

func NewBatch[T any](maxSize int, interval time.Duration, flushFn func([]BatchItem[T])) *Batch[T] {
	return &Batch[T]{
		maxSize:  maxSize,
		interval: interval,
		flushFn:  flushFn,
	}
}

func (b *Batch[T]) Add(ctx context.Context, item T) {
	b.mu.Lock()
	b.items = append(b.items, BatchItem[T]{Ctx: ctx, Msg: item})

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
