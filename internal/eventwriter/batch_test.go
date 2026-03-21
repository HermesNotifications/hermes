package eventwriter_test

import (
	"sync"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/eventwriter"
)

func TestBatch_FlushOnSize(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]int

	batch := eventwriter.NewBatch[int](3, time.Minute, func(items []int) {
		mu.Lock()
		flushed = append(flushed, items)
		mu.Unlock()
	})

	batch.Add(1)
	batch.Add(2)
	batch.Add(3) // triggers flush

	mu.Lock()
	if len(flushed) != 1 || len(flushed[0]) != 3 {
		t.Fatalf("expected 1 flush of 3 items, got %v", flushed)
	}
	mu.Unlock()
}

func TestBatch_FlushOnInterval(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]int

	batch := eventwriter.NewBatch[int](100, 50*time.Millisecond, func(items []int) {
		mu.Lock()
		flushed = append(flushed, items)
		mu.Unlock()
	})

	batch.Add(1)
	batch.Add(2)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(flushed) != 1 || len(flushed[0]) != 2 {
		t.Fatalf("expected 1 flush of 2 items, got %v", flushed)
	}
	mu.Unlock()
}

func TestBatch_ManualFlush(t *testing.T) {
	var flushed [][]int

	batch := eventwriter.NewBatch[int](100, time.Minute, func(items []int) {
		flushed = append(flushed, items)
	})

	batch.Add(1)
	batch.Flush()

	if len(flushed) != 1 || len(flushed[0]) != 1 {
		t.Fatalf("expected 1 flush with 1 item, got %v", flushed)
	}
}
