package buildcontract

import (
	"context"

	"gorm.io/gorm"

	builddto "go_wp/internal/module/build/dto"
)

type (
	Job            = builddto.Job
	EnqueueReq     = builddto.EnqueueReq
	ListReq        = builddto.ListReq
	QueueStatsResp = builddto.QueueStatsResp
)

func MenuStaleSourceID(projectID, kind string) (sourceID string, ok bool) {
	return builddto.MenuStaleSourceID(projectID, kind)
}

func SplitMenuStaleSourceID(sourceID string) (projectID, kind string, ok bool) {
	return builddto.SplitMenuStaleSourceID(sourceID)
}

const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusSuperseded = "superseded"
	StatusFailed     = "failed"
	StatusSucceeded  = "succeeded"
)

type Executor func(ctx context.Context, job *builddto.Job) error

type BuildService interface {
	Enqueue(ctx context.Context, req *builddto.EnqueueReq) (job *builddto.Job, created bool, err error)

	EnqueuePageBuild(ctx context.Context, pageID, projectID, lang, intent string, draftVersion int64, buildInputHash string) error

	EnqueuePresentationBuild(ctx context.Context, presentationID, projectID string) error

	EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (inputVersion int64, err error)

	RunOnce(ctx context.Context) (processed bool, err error)

	ReclaimStale(ctx context.Context) (reclaimed int64, err error)

	Stats(ctx context.Context, projectID string) (res *builddto.QueueStatsResp, err error)

	List(ctx context.Context, req *builddto.ListReq) (list []*builddto.Job, err error)

	Retry(ctx context.Context, projectID, id string) error

	RegisterExecutor(sourceType string, fn Executor)

	// StartWorkers 返回的 stop 取消循环并等待在途任务退出；
	// 调用方必须在关闭数据库之前调用它。
	StartWorkers(ctx context.Context, n int) func()
}
