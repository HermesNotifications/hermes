// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package eventwriter_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/eventwriter"
)

func TestBatch_FlushOnSize(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]int

	batch := eventwriter.NewBatch[int](3, time.Minute, func(items []eventwriter.BatchItem[int]) {
		mu.Lock()
		msgs := make([]int, len(items))
		for i, item := range items {
			msgs[i] = item.Msg
		}
		flushed = append(flushed, msgs)
		mu.Unlock()
	})

	ctx := context.Background()
	batch.Add(ctx, 1)
	batch.Add(ctx, 2)
	batch.Add(ctx, 3) // triggers flush

	mu.Lock()
	if len(flushed) != 1 || len(flushed[0]) != 3 {
		t.Fatalf("expected 1 flush of 3 items, got %v", flushed)
	}
	mu.Unlock()
}

func TestBatch_FlushOnInterval(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]int

	batch := eventwriter.NewBatch[int](100, 50*time.Millisecond, func(items []eventwriter.BatchItem[int]) {
		mu.Lock()
		msgs := make([]int, len(items))
		for i, item := range items {
			msgs[i] = item.Msg
		}
		flushed = append(flushed, msgs)
		mu.Unlock()
	})

	ctx := context.Background()
	batch.Add(ctx, 1)
	batch.Add(ctx, 2)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(flushed) != 1 || len(flushed[0]) != 2 {
		t.Fatalf("expected 1 flush of 2 items, got %v", flushed)
	}
	mu.Unlock()
}

func TestBatch_ManualFlush(t *testing.T) {
	var flushed [][]int

	batch := eventwriter.NewBatch[int](100, time.Minute, func(items []eventwriter.BatchItem[int]) {
		msgs := make([]int, len(items))
		for i, item := range items {
			msgs[i] = item.Msg
		}
		flushed = append(flushed, msgs)
	})

	batch.Add(context.Background(), 1)
	batch.Flush()

	if len(flushed) != 1 || len(flushed[0]) != 1 {
		t.Fatalf("expected 1 flush with 1 item, got %v", flushed)
	}
}
