package retention

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultBatchSize = 500

	maxBatchesPerRun = 200
)

type Task struct {
	Name       string
	Table      string
	TimeColumn string
	Retain     time.Duration
	BatchSize  int
	Note       string
	Sweep      func(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

func (t Task) Cutoff(now time.Time) time.Time {
	return now.Add(-t.Retain)
}

type Outcome struct {
	Name    string
	Deleted int64
	Batches int
	Err     error
}

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

func RunAll(ctx context.Context, tasks []Task, now time.Time) []Outcome {
	outcomes := make([]Outcome, 0, len(tasks))
	for _, t := range tasks {
		deleted, batches, err := RunTask(ctx, t, now)
		outcomes = append(outcomes, Outcome{Name: t.Name, Deleted: deleted, Batches: batches, Err: err})
	}
	return outcomes
}

func Summary(outcomes []Outcome) (total int64, failed []string) {
	for _, o := range outcomes {
		total += o.Deleted
		if o.Err != nil {
			failed = append(failed, o.Name)
		}
	}
	return total, failed
}
