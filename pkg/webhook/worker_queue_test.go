package webhook

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerQueue_EnqueueSingleWorkerRuns(t *testing.T) {
	q := newWorkerQueue()
	ctx := context.Background()
	var ran atomic.Bool
	done := make(chan struct{})

	q.enqueue(ctx, "res1", func(ctx context.Context) {
		ran.Store(true)
		close(done)
	})

	select {
	case <-done:
		if !ran.Load() {
			t.Error("worker did not run")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not run within timeout")
	}
}

func TestWorkerQueue_EnqueueMultipleWorkersSameResourceFIFO(t *testing.T) {
	q := newWorkerQueue()
	ctx := context.Background()
	var order []int
	var mu sync.Mutex
	done := make(chan struct{})

	for i := 0; i < 3; i++ {
		idx := i
		q.enqueue(ctx, "res1", func(ctx context.Context) {
			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
			if idx == 2 {
				close(done)
			}
		})
	}

	select {
	case <-done:
		mu.Lock()
		got := order
		mu.Unlock()
		if len(got) != 3 {
			t.Errorf("expected 3 workers to run, got %d", len(got))
		}
		for i, v := range got {
			if v != i {
				t.Errorf("FIFO violation: position %d got value %d, expected %d", i, v, i)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workers did not complete within timeout")
	}
}

func TestWorkerQueue_EnqueueSameResourceSerialized(t *testing.T) {
	q := newWorkerQueue()
	ctx := context.Background()
	var current int32
	var completed int32
	done := make(chan struct{})

	for i := 0; i < 5; i++ {
		q.enqueue(ctx, "res1", func(ctx context.Context) {
			c := atomic.AddInt32(&current, 1)
			if c != 1 {
				t.Errorf("expected only one worker at a time for same resource, got current=%d", c)
			}
			time.Sleep(15 * time.Millisecond)
			atomic.AddInt32(&current, -1)
			if atomic.AddInt32(&completed, 1) == 5 {
				close(done)
			}
		})
	}

	select {
	case <-done:
		// All 5 ran; we already asserted c==1 in each worker
	case <-time.After(5 * time.Second):
		t.Fatal("workers did not complete within timeout")
	}
}

func TestWorkerQueue_DifferentResourcesRunConcurrently(t *testing.T) {
	q := newWorkerQueue()
	ctx := context.Background()
	var concurrent int32
	start := make(chan struct{})
	done := make(chan struct{})

	for _, res := range []string{"res1", "res2", "res3"} {
		resource := res
		q.enqueue(ctx, resource, func(ctx context.Context) {
			<-start
			c := atomic.AddInt32(&concurrent, 1)
			defer atomic.AddInt32(&concurrent, -1)
			time.Sleep(30 * time.Millisecond)
			if c == 3 {
				close(done)
			}
		})
	}
	close(start)

	select {
	case <-done:
		// All three resources ran; at some point concurrent was 3.
		return
	case <-time.After(5 * time.Second):
		t.Fatal("expected workers for different resources to run concurrently")
	}
}

func TestWorkerQueue_ContextPassedToWorker(t *testing.T) {
	type ctxKey struct{}
	q := newWorkerQueue()
	ctx := context.WithValue(context.Background(), ctxKey{}, "value")
	done := make(chan struct{})

	q.enqueue(ctx, "res1", func(workerCtx context.Context) {
		defer close(done)
		v := workerCtx.Value(ctxKey{})
		if v != "value" {
			t.Errorf("worker got context value %v, want \"value\"", v)
		}
	})

	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not run within timeout")
	}
}

func TestWorkerQueue_QueueDrainsThenAcceptsNewWork(t *testing.T) {
	q := newWorkerQueue()
	ctx := context.Background()
	var firstBatch, secondBatch bool
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})

	q.enqueue(ctx, "res1", func(ctx context.Context) {
		time.Sleep(10 * time.Millisecond)
		firstBatch = true
		close(firstDone)
	})
	q.enqueue(ctx, "res1", func(ctx context.Context) {
		// second in first drain
	})

	<-firstDone
	// After first batch drains, queue should be idle; enqueue again.
	q.enqueue(ctx, "res1", func(ctx context.Context) {
		secondBatch = true
		close(secondDone)
	})

	select {
	case <-secondDone:
		if !firstBatch || !secondBatch {
			t.Errorf("firstBatch=%v secondBatch=%v", firstBatch, secondBatch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second enqueue did not run within timeout")
	}
}

// assertMapsCleared checks that internal workers and busy maps are empty (queue fully drained).
func (w *workerQueue) assertMapsCleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.workers) != 0 {
		t.Errorf("workers map not cleared after drain: len=%d, keys=%v", len(w.workers), mapKeys(w.workers))
	}
	if len(w.busy) != 0 {
		t.Errorf("busy map not cleared after drain: len=%d, keys=%v", len(w.busy), mapKeys(w.busy))
	}
}

func mapKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestWorkerQueue_SingleResourceEnqueuedFromGoroutines_AllConsumedAndMapsCleared(t *testing.T) {
	q := newWorkerQueue()
	ctx := context.Background()
	const numGoroutines = 8
	const workersPerGoroutine = 4
	const totalWorkers = numGoroutines * workersPerGoroutine

	var completed int32
	allDone := make(chan struct{})

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < workersPerGoroutine; i++ {
				q.enqueue(ctx, "res1", func(ctx context.Context) {
					time.Sleep(5 * time.Millisecond)
					if atomic.AddInt32(&completed, 1) == totalWorkers {
						close(allDone)
					}
				})
			}
		}()
	}
	wg.Wait()

	select {
	case <-allDone:
		if atomic.LoadInt32(&completed) != totalWorkers {
			t.Errorf("completed=%d, want %d", completed, totalWorkers)
		}
		// Give consume() time to delete keys and return
		time.Sleep(50 * time.Millisecond)
		q.assertMapsCleared(t)
	case <-time.After(10 * time.Second):
		t.Fatalf("only %d/%d workers completed", atomic.LoadInt32(&completed), totalWorkers)
	}
}

func TestWorkerQueue_MultipleResourcesEnqueuedFromGoroutines_AllConsumedAndMapsCleared(t *testing.T) {
	q := newWorkerQueue()
	ctx := context.Background()
	resources := []string{"res1", "res2", "res3"}
	const workersPerResource = 5

	var completed int32
	expected := len(resources) * workersPerResource
	allDone := make(chan struct{})

	for _, res := range resources {
		resource := res
		for i := 0; i < workersPerResource; i++ {
			go func() {
				q.enqueue(ctx, resource, func(ctx context.Context) {
					time.Sleep(10 * time.Millisecond)
					if atomic.AddInt32(&completed, 1) == int32(expected) {
						close(allDone)
					}
				})
			}()
		}
	}

	select {
	case <-allDone:
		if atomic.LoadInt32(&completed) != int32(expected) {
			t.Errorf("completed=%d, want %d", completed, expected)
		}
		time.Sleep(50 * time.Millisecond)
		q.assertMapsCleared(t)
	case <-time.After(15 * time.Second):
		t.Fatalf("only %d/%d workers completed", atomic.LoadInt32(&completed), expected)
	}
}
