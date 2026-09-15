package retention

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRunTaskDrainsInBatches 批次跑满则继续，未跑满即认为删空并停止。
func TestRunTaskDrainsInBatches(t *testing.T) {
	remaining := int64(12)
	var calls int
	task := Task{
		Name: "t", BatchSize: 5,
		Sweep: func(_ context.Context, _ time.Time, limit int) (int64, error) {
			calls++
			n := int64(limit)
			if n > remaining {
				n = remaining
			}
			remaining -= n
			return n, nil
		},
	}
	deleted, batches, err := RunTask(context.Background(), task, time.Now())
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if deleted != 12 || batches != 3 {
		t.Fatalf("应分 3 批删 12 行，实际 %d 行 %d 批（调用 %d 次）", deleted, batches, calls)
	}
	if remaining != 0 {
		t.Fatalf("应删空，剩余 %d", remaining)
	}
}

// TestRunTaskStopsImmediatelyWhenNothingToDelete 无过期行时只调用一次就结束（不做无谓轮询）。
func TestRunTaskStopsImmediatelyWhenNothingToDelete(t *testing.T) {
	calls := 0
	task := Task{
		Name: "t", BatchSize: 5,
		Sweep: func(context.Context, time.Time, int) (int64, error) {
			calls++
			return 0, nil
		},
	}
	deleted, batches, err := RunTask(context.Background(), task, time.Now())
	if err != nil || deleted != 0 || batches != 1 || calls != 1 {
		t.Fatalf("空清理应一次结束：deleted=%d batches=%d calls=%d err=%v", deleted, batches, calls, err)
	}
}

// TestRunTaskUsesCutoffFromRetain 传给 Sweep 的分界必须等于 now - Retain。
func TestRunTaskUsesCutoffFromRetain(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	var got time.Time
	task := Task{
		Name: "t", Retain: 90 * 24 * time.Hour, BatchSize: 10,
		Sweep: func(_ context.Context, cutoff time.Time, _ int) (int64, error) {
			got = cutoff
			return 0, nil
		},
	}
	if _, _, err := RunTask(context.Background(), task, now); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	want := now.Add(-90 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("分界应为 %s，实际 %s", want, got)
	}
}

// TestRunTaskPropagatesError 单批失败立即返回，不继续删。
func TestRunTaskPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	task := Task{
		Name: "t", BatchSize: 5,
		Sweep: func(context.Context, time.Time, int) (int64, error) {
			calls++
			return 0, boom
		},
	}
	if _, _, err := RunTask(context.Background(), task, time.Now()); !errors.Is(err, boom) {
		t.Fatalf("应返回底层错误，实际 %v", err)
	}
	if calls != 1 {
		t.Fatalf("失败后不应继续，调用 %d 次", calls)
	}
}

// TestRunAllIsolatesFailures 一条任务失败不影响其它任务：保留期是逐表承诺，不是批量承诺。
func TestRunAllIsolatesFailures(t *testing.T) {
	boom := errors.New("boom")
	tasks := []Task{
		{Name: "a", BatchSize: 1, Sweep: func(context.Context, time.Time, int) (int64, error) { return 0, boom }},
		// Sweep 必须遵守 limit：这里返回 3 行而批次是 5，因此一批即结束。
		{Name: "b", BatchSize: 5, Sweep: func(context.Context, time.Time, int) (int64, error) { return 3, nil }},
	}
	outcomes := RunAll(context.Background(), tasks, time.Now())
	if len(outcomes) != 2 {
		t.Fatalf("应有两个结果，实际 %d", len(outcomes))
	}
	if outcomes[0].Err == nil || outcomes[1].Err != nil {
		t.Fatalf("失败应被隔离：a=%v b=%v", outcomes[0].Err, outcomes[1].Err)
	}
	total, failed := Summary(outcomes)
	if total != 3 || len(failed) != 1 || failed[0] != "a" {
		t.Fatalf("汇总不正确：total=%d failed=%v", total, failed)
	}
}

// TestRunTaskRequiresSweep 没有 Sweep 的任务是声明错误，必须显式报错而不是静默跳过。
func TestRunTaskRequiresSweep(t *testing.T) {
	if _, _, err := RunTask(context.Background(), Task{Name: "empty"}, time.Now()); err == nil {
		t.Fatal("缺少 Sweep 应报错")
	}
}

// TestRunTaskHonoursContextCancellation 取消后立即停止（后台任务不该无视退出信号）。
func TestRunTaskHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	task := Task{
		Name: "t", BatchSize: 5,
		Sweep: func(context.Context, time.Time, int) (int64, error) { calls++; return 5, nil },
	}
	if _, _, err := RunTask(ctx, task, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 context.Canceled，实际 %v", err)
	}
	if calls != 0 {
		t.Fatalf("已取消时不该执行删除，调用 %d 次", calls)
	}
}
