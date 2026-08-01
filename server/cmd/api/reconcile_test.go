package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunSnapshotReconcilerRetriesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	done := make(chan struct{})
	reconcile := func(context.Context, int64) (int, int, error) {
		if calls.Add(1) == 2 {
			close(done)
		}
		return 0, 0, nil
	}
	go runSnapshotReconciler(ctx, time.Millisecond, time.Second, 25, reconcile)
	select {
	case <-done:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("periodic snapshot reconciliation did not retry")
	}
	if calls.Load() < 2 {
		t.Fatalf("reconcile calls=%d", calls.Load())
	}
}
