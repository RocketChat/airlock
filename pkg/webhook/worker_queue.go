package webhook

import (
	"context"
	"sync"
)

type workerFunc func(ctx context.Context)

type workerQueue struct {
	mu      *sync.Mutex
	workers map[string][]workerFunc
	busy    map[string]bool
}

func newWorkerQueue() *workerQueue {
	return &workerQueue{
		mu:      &sync.Mutex{},
		workers: make(map[string][]workerFunc),
		busy:    make(map[string]bool),
	}
}

func (w *workerQueue) enqueue(ctx context.Context, resource string, worker workerFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()
	workers, exists := w.workers[resource]
	if !exists {
		workers = []workerFunc{}
	}
	workers = append(workers, worker)
	w.workers[resource] = workers

	if w.busy[resource] {
		return
	}
	w.busy[resource] = true
	go w.consume(ctx, resource)
}

func (w *workerQueue) consume(ctx context.Context, resource string) {
	for {
		w.mu.Lock()
		workers := w.workers[resource]
		if len(workers) == 0 {
			delete(w.workers, resource)
			delete(w.busy, resource)
			w.mu.Unlock()
			return
		}

		worker := workers[0]
		w.workers[resource] = workers[1:]
		w.mu.Unlock()

		worker(ctx)
	}
}
