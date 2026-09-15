// Package retention —— 数据生命周期：统一声明 + 统一执行（审计 IDX-019）。
//
// 背景：13 张只增不减的表里，此前只有 page_views 有清理（IDX-001），其余全靠人工。
// 「留多久、按哪一列、怎么删」如果散在各个模块里各写一遍，结果一定是有人写、有人忘。
// 所以这里放两样东西：
//
//   - 声明：Task 是**元数据**（表名 / 时间列 / 保留期 / 批次），运维手册与后台展示
//     都从这里取，避免文档与代码各说一套；
//   - 执行：按批次循环删除，直到删空或触到批次上限（批次存在的意义是不制造长事务与
//     锁表，删除量与锁持有时间都是可控的）。
//
// 表访问权仍在各模块的 model 里：Task.Sweep 由模块自己构造（回调里调自己的 model 方法），
// 本包不认识任何业务表 —— 这样既统一了口径，又没有破坏模块间的表隔离。
package retention

import (
	"context"
	"fmt"
	"time"
)

const (
	// defaultBatchSize 每批删除行数：批量太大照样锁表，太小则清理追不上写入。
	defaultBatchSize = 500
	// maxBatchesPerRun 单次运行的批次上限：防止「一边删一边写」导致清理循环永不结束。
	maxBatchesPerRun = 200
)

// Task 一条生命周期声明。Sweep 必须**分批**删除（最多 limit 行）并返回实际删除行数。
type Task struct {
	Name       string // 任务名（日志与指标维度，如 page_revisions）
	Table      string // 表名（运维手册口径）
	TimeColumn string // 判定时间的列名
	Retain     time.Duration
	BatchSize  int
	Note       string // 为什么是这个保留期（写清楚，免得后人随手改）
	Sweep      func(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// Cutoff 返回本次清理的时间分界：早于它的行属于过期。
func (t Task) Cutoff(now time.Time) time.Time {
	return now.Add(-t.Retain)
}

// Outcome 单个任务的执行结果（失败不阻断其它任务，因此逐条记录）。
type Outcome struct {
	Name    string
	Deleted int64
	Batches int
	Err     error
}

// RunTask 分批清理直到删空或触到批次上限。
//
// 「删空」的判定是本批返回的行数少于 limit —— 说明这一批没有再删满，后面已无过期行。
func RunTask(ctx context.Context, t Task, now time.Time) (deleted int64, batches int, err error) {
	if t.Sweep == nil {
		return 0, 0, fmt.Errorf("retention: 任务 %s 未提供 Sweep", t.Name)
	}
	batch := t.BatchSize
	if batch <= 0 {
		batch = defaultBatchSize
	}
	cutoff := t.Cutoff(now)
	for batches < maxBatchesPerRun {
		// 每批开始前检查取消：清理是可以被打断的后台工作，不是必须跑完的事务。
		if cerr := ctx.Err(); cerr != nil {
			return deleted, batches, cerr
		}
		n, serr := t.Sweep(ctx, cutoff, batch)
		if serr != nil {
			return deleted, batches, serr
		}
		deleted += n
		batches++
		if n < int64(batch) {
			return deleted, batches, nil
		}
	}
	return deleted, batches, nil
}

// RunAll 顺序执行一组任务。单条失败只记录在 Outcome 里，不影响其余任务 ——
// 一张表清理失败不该让另外几张表的保留期一起失效。
func RunAll(ctx context.Context, tasks []Task, now time.Time) []Outcome {
	outcomes := make([]Outcome, 0, len(tasks))
	for _, t := range tasks {
		deleted, batches, err := RunTask(ctx, t, now)
		outcomes = append(outcomes, Outcome{Name: t.Name, Deleted: deleted, Batches: batches, Err: err})
	}
	return outcomes
}

// Summary 汇总结果（日志与后台展示用）。
func Summary(outcomes []Outcome) (total int64, failed []string) {
	for _, o := range outcomes {
		total += o.Deleted
		if o.Err != nil {
			failed = append(failed, o.Name)
		}
	}
	return total, failed
}
