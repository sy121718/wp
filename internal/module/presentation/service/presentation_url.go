// presentation_url.go — 详情页改 URL（发布实例的线上路径变更）。
//
// 与手工页面 page.Service.UpdateURL 同一语义，动作清单逐条对齐（顺序不可换）：
//  1. 路径归一化 + 同路径短路；
//  2. 占用预检（page_routes 与 presentation_instances 两个真源）；
//  3. 按新路径重新构建产物（canonical / OG / JSON-LD 已烘进 HTML 字节，必须重编）；
//  4. 落库：快照 + 产物行 + 指针 + url_path（同一事务）；
//  5. 激活新路径（访问面符号链接 + page_routes 登记）；
//  6. 旧路径处置：WithRedirect 落盘 301 产物并激活，否则取消旧路径激活。
//
// 与手工页面唯一的差别：presentation 没有草稿路径概念（实例创建即发布），
// 因此没有「纯草稿只迁移路径」那条分支。
//
// 为什么改 URL 必须重建产物：静态站里路径不是元数据而是产物的一部分 ——
// canonical / og:url / JSON-LD 的 url 在编译期就写死在 HTML 字节里，改路径而
// 不重编会留下「页面仍宣称自己住在旧地址」的自相矛盾。动态站（WP）改 slug
// 立刻生效，是因为它的 URL 每次请求现算；静态发布没有这个奢侈。
package presentationservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// UpdateURL 修改已发布实例的线上路径（改 URL）。
func (s *Service) UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 工程作用域（DB-009 第二批）：实例表的定位、占用预检、锁内重读全部在本工程内进行。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.locateInstance(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	newLogical, err := s.normalizeLogicalPath(ctx, inst.ProjectID, req.NewPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrInvalidPath, err)
	}
	// 实例级互斥：与创建/重建共用同一把锁，「构建 → 落库 → 激活」整段串行。
	// 否则改 URL 与同时到达的内容变更重建会各自推进产物版本号、交错覆盖指针。
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	// 锁内重读：等锁期间实例可能已被重建、改过 URL 或删除。
	inst, err = s.m.GetInstance(ctx, inst.ProjectID, inst.ID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	oldLogical := s.instanceLogicalPath(ctx, inst)
	if newLogical == oldLogical {
		return nil, errors.New(presentationenums.ErrSamePath)
	}
	oldPubs, _ := s.m.ListPublications(ctx, inst.ID)
	// 预检：新逻辑路径下全部语言访问路径均空闲。
	if err = s.ensureLogicalPathFree(ctx, inst.ProjectID, newLogical, inst.ID); err != nil {
		return nil, err
	}
	// 模板沿用实例当前绑定（改 URL 不是换模板）：resolveBoundTemplate 传空
	// 显式 id，绑定优先且不回落「同类型最新」。
	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return nil, err
	}
	// mode=nil：改 URL 不是改渲染模式。
	primaryArtifactID, err := s.publishAllLangs(ctx, inst, tpl, newLogical, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	// 旧路径处置：逐语言取消激活或 301。
	for _, pub := range oldPubs {
		if pub.ActivePath == "" {
			continue
		}
		if derr := s.disposeOldPath(ctx, inst, pub.ActivePath, newLogical, primaryArtifactID, req.WithRedirect); derr != nil {
			logger.Scene("build").With("instanceId", inst.ID).With("oldPath", pub.ActivePath).
				Warn("改 URL 后旧路径处置失败（新路径已生效）: " + derr.Error())
		}
	}
	logger.Scene("build").With("instanceId", inst.ID).With("oldPath", oldLogical).With("newPath", newLogical).
		Info("详情页 URL 修改完成")
	return s.toResp(ctx, inst)
}

// locateInstance 按 ID 或 (实体类型, 实体 id) 定位实例。
//
// 两种入口都保留：后台「详情页模板」页只持有实体 id（列表行给的是商品/文章），
// 而 API 调用方通常持有实例 id。
func (s *Service) locateInstance(ctx context.Context, projectID string, req *presentationdto.UpdateURLReq) (*presentationmodel.InstanceEntity, error) {
	if id := strings.TrimSpace(req.ID); id != "" {
		inst, err := s.m.GetInstance(ctx, projectID, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New(presentationenums.ErrNotFound)
			}
			return nil, err
		}
		return inst, nil
	}
	entityType, entityID := strings.TrimSpace(req.EntityType), strings.TrimSpace(req.EntityID)
	if entityType == "" || entityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstanceByEntity(ctx, projectID, entityType, entityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return inst, nil
}

// ensurePathFree 占用预检：目标路径不得被其他页面或展示实例占用。
//
// 两个真源都要查，缺一不可：
//   - page_routes：手工页面与（本次起）已登记的展示实例的占用；
//   - presentation_instances.url_path：本次之前创建的历史实例 —— 它们的占用
//     从未进过路由表，只查路由表会放行「抢一个历史详情页的路径」，随后 FS
//     激活直接覆盖对方线上内容。
//
// excludeInstanceID 为空的场景是创建实例（自己还不存在，无需排除）。
func (s *Service) ensurePathFree(ctx context.Context, projectID, path, excludeInstanceID string) error {
	if s.routes != nil {
		occupied, err := s.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
			ProjectID: projectID, Path: path, ExcludePresentationID: excludeInstanceID,
		})
		if err != nil {
			return err
		}
		if occupied {
			logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他实体占用")
			return errors.New(presentationenums.ErrPathOccupied)
		}
	}
	if _, err := s.m.FindInstanceByPath(ctx, projectID, path, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他展示实例占用")
		return errors.New(presentationenums.ErrPathOccupied)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if _, err := s.m.FindInstanceByActivePath(ctx, projectID, path, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他展示实例的多语言路径占用")
		return errors.New(presentationenums.ErrPathOccupied)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

// registerRoute 把实例的线上路径登记进 page_routes（active，同归属者幂等）。
//
// 登记的目的是让「谁占着这个路径」成为可查事实：不登记时页面侧发布/改 URL 的
// 占用预检（IsPathOccupied 查 page_routes）看不见详情页，两边可以先后激活同一
// 路径，后者覆盖前者的线上内容且全程无报错。routes 为 nil（降级装配）时跳过。
func (s *Service) registerRoute(ctx context.Context, inst *presentationmodel.InstanceEntity, urlPath, artifactID string) error {
	if s.routes == nil {
		return nil
	}
	if _, err := s.routes.Activate(ctx, &pubcontract.ActivateReq{
		ProjectID:      inst.ProjectID,
		Path:           urlPath,
		PresentationID: inst.ID,
		ArtifactID:     artifactID,
	}); err != nil {
		return err
	}
	return nil
}

// disposeOldPath 处置改 URL 后遗留的旧路径。
//
//   - withRedirect：落盘 301 产物（不经过 Compiler，只有 redirect.json）并激活
//     到旧路径，同时把路由行标记为 redirect —— 旧链接继续可用，SEO 权重转移；
//   - 否则：取消旧路径的访问面激活并释放路由占用 —— 旧链接 404。
//
// 调用时机固定在「新路径已激活」之后，失败由调用方记日志而不回滚新路径。
func (s *Service) disposeOldPath(ctx context.Context, inst *presentationmodel.InstanceEntity,
	oldPath, newPath, artifactID string, withRedirect bool) error {
	if withRedirect {
		ra, err := pipeline.NewRedirectArtifact(newPath, 301)
		if err != nil {
			return err
		}
		loc, err := s.store.PutRedirect(ra)
		if err != nil {
			return err
		}
		if err = s.publication.Activate(oldPath, loc); err != nil {
			return err
		}
		if s.routes != nil {
			if _, err = s.routes.Redirect(ctx, &pubcontract.RedirectReq{
				ProjectID:      inst.ProjectID,
				OldPath:        oldPath,
				PresentationID: inst.ID,
				ArtifactID:     artifactID,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := s.publication.Deactivate(oldPath); err != nil {
		return err
	}
	if s.routes != nil {
		if err := s.routes.Deactivate(ctx, &pubcontract.DeactivateReq{
			ProjectID:      inst.ProjectID,
			Path:           oldPath,
			PresentationID: inst.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}
