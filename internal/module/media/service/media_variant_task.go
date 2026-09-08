package mediaservice

// media_variant_task.go — 变体生成的 asynq 任务接入：
// 任务类型 media:generate_variants，payload 仅携带 attachmentID；
// handler 经 RegisterVariantTaskHandler 挂进现有队列 worker（pkg/queue facade），
// 不新起 worker 进程；handler 幂等（GenerateVariants 重跑先清旧记录）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/queue"

	"gorm.io/gorm"
)

// TaskMediaGenerateVariants asynq 任务类型常量。
const TaskMediaGenerateVariants = "media:generate_variants"

// VariantTaskPayload 变体生成任务载荷（JSON）。
type VariantTaskPayload struct {
	AttachmentID uint64 `json:"attachment_id"`
}

// variantTask 队列任务门面（固定 taskType，default 队列）。
var variantTask = queue.NewTask(TaskMediaGenerateVariants, queue.WithQueue("default"), queue.WithMaxRetry(3))

// scheduleVariants 投递异步变体生成任务；队列未就绪或入队失败时
// 同步生成兜底（仍不阻断上传——GenerateVariants 内部已降级失败项）。
func (s *Service) scheduleVariants(ctx context.Context, attachmentID uint64) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("media").With("attachment_id", attachmentID).Error(fmt.Errorf("panic: %v", r), "变体任务投递异常（已降级）")
		}
	}()

	if queue.IsInited() {
		if err := variantTask.Enqueue(VariantTaskPayload{AttachmentID: attachmentID}); err == nil {
			return
		}
	}
	// 队列未启用（queue.enabled=false）或入队失败：同步生成兜底。
	if _, err := s.GenerateVariants(ctx, attachmentID); err != nil {
		logger.Scene("media").With("attachment_id", attachmentID).Error(err, "变体同步生成失败（已降级）")
	}
}

// handleVariantTask asynq handler：payload 反序列化 → 同步生成变体。
// 幂等由 GenerateVariants 保证；附件不存在/非图片/非本地存储等不可重试错误返回 nil，
// 避免无意义重试；DB 级错误原样返回交给 asynq 重试。
func handleVariantTask(db *gorm.DB) func(ctx context.Context, payload []byte) error {
	return func(ctx context.Context, payload []byte) error {
		var p VariantTaskPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		if p.AttachmentID == 0 {
			return errors.New("变体任务载荷缺少 attachment_id")
		}
		svc := NewService(
			mediamodel.NewAttachmentModel(db),
			mediamodel.NewFileCategoryModel(db),
			mediamodel.NewMediaVariantModel(db),
		)
		_, err := svc.GenerateVariants(ctx, p.AttachmentID)
		if err != nil {
			switch err.Error() {
			case mediaenums.ErrAttachmentNotFound, mediaenums.ErrAttachmentNotImage, mediaenums.ErrVariantStorageNotLocal:
				// 不可重试的业务降级：记录日志后返回 nil。
				logger.Scene("media").With("attachment_id", p.AttachmentID).With("reason", err.Error()).Warn("变体任务终止（不可重试）")
				return nil
			}
			return err
		}
		return nil
	}
}

// RegisterVariantTaskHandler 把变体生成 handler 注册进队列 worker（路由装配时调用）。
// queue.Register 会重放到 provider（未 Init 时暂存，Init 时重放），无时序要求。
func RegisterVariantTaskHandler(db *gorm.DB) {
	queue.Register(TaskMediaGenerateVariants, handleVariantTask(db))
}
