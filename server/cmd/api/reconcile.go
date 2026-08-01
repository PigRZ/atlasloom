// 快照补偿循环在进程存活期间持续重试，不把一次启动扫描当成最终保障。
package main

import (
	"context"
	"log/slog"
	"time"
)

type snapshotReconcileFunc func(context.Context, int64) (int, int, error)

func reconcilePendingOnce(
	parent context.Context,
	timeout time.Duration,
	batch int64,
	reconcile snapshotReconcileFunc,
) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	mirrored, failed, err := reconcile(ctx, batch)
	if err != nil {
		slog.Warn(
			"pending snapshot reconciliation failed",
			"error", err,
			"batch", batch,
		)
		return
	}
	if mirrored > 0 || failed > 0 {
		slog.Info(
			"pending snapshot reconciliation finished",
			"mirrored", mirrored,
			"failed", failed,
		)
	}
}

func runSnapshotReconciler(
	ctx context.Context,
	interval, timeout time.Duration,
	batch int64,
	reconcile snapshotReconcileFunc,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcilePendingOnce(ctx, timeout, batch, reconcile)
		}
	}
}
