package presentationservice

// presentation_stale.go — 失效与预览（依赖变更标记、批量重建、只读预览）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"

	"go_wp/pkg/logger"
)

// MarkStaleByDependency 实现 pipeline.DependencyTarget：按依赖源精确标记。
func (s *Service) MarkStaleByDependency(ctx context.Context, kind, key string) ([]string, error) {
	return s.m.MarkStaleByDependency(ctx, kind, key, time.Now().UTC())
}

// SetBuildQueue 注入自动重建入队端口（装配期调用；PERF-020）。
//
// 注入后 RebuildStale 改为全量入队：重建由构建队列的消费 worker 执行
// （FOR UPDATE SKIP LOCKED claim 保证多实例部署下同一实例不会被两个 worker 同时重建），
// 触发进程不再在请求路径上持实例锁串行重建。未注入时回退原同步重建行为
// （单实例部署 / 既有测试不受影响）。
func (s *Service) SetBuildQueue(q presentationcontract.BuildQueueEnqueuer) {
	if s == nil {
		return
	}
	s.buildQueue = q
}

// RebuildStale 实现 pipeline.StaleRebuilder：重建受影响实例并重新发布。
//
// 策略（§8.3）：presentation 实例的语义就是「内容驱动的自动发布页面」，
// 创建即上线，因此重建成功后直接回写线上（与 page 侧「仅已发布语言自动回写」
// 的口径在结果上一致——presentation 不存在「从未发布」的实例）。
//
// 队列已接入时（PERF-020）：全部实例入队后立即返回，重建由队列消费侧执行；
// 单条入队失败只记日志（该实例保持 stale，由下次触发兜底）。
// 未接入时回退同步重建：单个实例失败不阻断其余（记日志后继续），返回 nil 由 stale 标记兜底。
func (s *Service) RebuildStale(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if s.buildQueue != nil {
		return s.enqueueStaleRebuilds(ctx, ids)
	}
	if len(ids) > maxAutoRebuildInstances {
		logger.Scene("dependency").With("affected", len(ids)).With("limit", maxAutoRebuildInstances).
			Warn("自动重建超出单次上限，剩余实例保持 stale 等待后续触发")
		ids = ids[:maxAutoRebuildInstances]
	}
	rebuilt := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return nil
		}
		if err := s.RebuildInstance(ctx, id); err != nil {
			logger.Scene("dependency").With("presentation_id", id).
				Error(err, "依赖失效后的自动重建失败（实例保持 stale）")
			continue
		}
		rebuilt++
	}
	if rebuilt > 0 {
		logger.Scene("dependency").With("rebuilt", rebuilt).Info("依赖失效后的自动重建完成")
	}
	return nil
}

// enqueueStaleRebuilds 把受影响实例全部交给构建队列（PERF-020）。
//
// 入队是幂等的：队列侧部分唯一索引保证同一实例同时只有一条待办，扇出反复标记
// 同一实例不会堆出多份任务。整批入队是轻量操作，因此不再有单次上限截断——
// 截断过的实例若「下次触发」不来就永远停在 stale。
func (s *Service) enqueueStaleRebuilds(ctx context.Context, ids []string) error {
	queued := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		if err := s.buildQueue.EnqueuePresentationBuild(ctx, id); err != nil {
			logger.Scene("dependency").With("presentation_id", id).
				Error(err, "自动重建任务入队失败（实例保持 stale）")
			continue
		}
		queued++
	}
	logger.Scene("dependency").With("queued", queued).With("affected", len(ids)).
		Info("依赖失效的自动重建已交给构建队列")
	return nil
}

// RebuildInstance 按实例 id 重建（构建队列 executor 的执行体；PERF-020）。
//
// 用实例**绑定**的模板重建（issue #14）：依赖失效是内容变更触发的自动重建，
// 不应改变「这个商品用哪套详情模板」——按类型重解析会把切换过的模板悄悄换回去。
func (s *Service) RebuildInstance(ctx context.Context, instanceID string) error {
	inst, err := s.m.GetInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("%s: %w", presentationenums.ErrNotFound, err)
	}
	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return err
	}
	_, err = s.rebuildInstance(ctx, inst, tpl)
	return err
}

func (s *Service) PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tpl, err := s.resolveTemplate(ctx, req.EntityType, req.TemplateID)
	if err != nil {
		return nil, err
	}
	if len(req.DraftDocument) > 0 {
		if !json.Valid(req.DraftDocument) {
			return nil, errors.New(presentationenums.ErrInvalidParam)
		}
		override := *tpl
		override.Document = req.DraftDocument
		tpl = &override
	}
	// urlPath 传空：预览不激活 URL，canonical 由模板 settings.seo 决定（通常为空）。
	// 这是预览与发布在字节上的唯一有意差异（见 presentation_seo.go 取舍 2）。
	html, err := s.renderHTML(ctx, req.EntityType, req.EntityID, "", projectID, "", tpl)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return &presentationdto.PreviewInstanceResp{
		HTML: string(html), EntityType: req.EntityType, EntityID: req.EntityID,
		TemplateID: tpl.TemplateID, TemplateName: tpl.TemplateName,
		TemplateVersionID: tpl.VersionID, TemplateVersion: tpl.Version,
	}, nil
}
