package pageservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactenums "go_wp/internal/module/artifact/enums"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 发布链路（docs/03-pipeline.md §6 / 0-A1 §2）：
//
//	Build   草稿确定性编译 → 不可变产物落盘 → 元数据入库 → 暂存指针回写；
//	Publish 校验暂存 → 原子激活符号链接 → active 指针与路由 active 化；
//	Rollback 内核按 hash 直接重激活历史产物（秒级，无重新编译）;
//	UpdateURL 新 URL 构建并激活，旧 URL 按 301 / 取消激活处理。
//
// 内核 pipeline.Publisher 的内存态可由数据库随时重建（LoadRecord），
// 因此进程重启不影响发布正确性；多实例队列化属后续 build 模块。
// page_artifacts.created_by 为 uuid 列：系统操作留空，
// artifact.Record 的 defaultCreator 会兜底为全零 UUID。
// 此前写入 "system" 字面量导致构建入库 500（uuid 解析失败），发布主链无法走通。
const systemCreator = ""

// syncKernel 把页面当前草稿同步进内核记录（幂等；版本号以内核为准续增）。
// path 为实际访问路径（多语言下带 /{lang}/ 前缀），lang 为构建语言：
// 两者一起进入内核记录，决定 Manifest.lang 与激活路径。
func (s *Service) syncKernel(path, lang string, doc json.RawMessage, pageID string) error {
	draft := pipeline.Draft{Path: path, Lang: lang, DocJSON: doc}
	st, err := s.publisher.Status(pageID)
	if errors.Is(err, pipeline.ErrPageNotFound) {
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	if err != nil {
		return err
	}
	if st.Path != path || st.Lang != lang {
		// 内核记录路径/语言落后于数据库（如改 URL 中断恢复、语言切换）：整体重建。
		s.publisher.LoadRecord(&pipeline.PageRecord{ID: pageID})
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	_, err = s.publisher.SaveDraftInput(pageID, st.Version, draft)
	return err
}

// sitePathOf 计算页面实际访问路径（多语言前缀单点映射），失败归一为 ErrInvalidPath。
func sitePathOf(lang string, page *pagemodel.PageEntity) (string, error) {
	path, err := sitePath(lang, page.DraftPath)
	if err != nil {
		return "", ErrInvalidPath
	}
	return path, nil
}

// Build 基于当前草稿构建并暂存产物。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
func (s *Service) Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion > 0 && req.ExpectedVersion != page.DraftVersion {
		return nil, ErrDraftVersionConflict
	}
	// 构建语言（多语言 P2）：请求显式指定优先，否则站点默认语言；
	// 实际访问路径由 sitePath 单点映射（开启前缀时 /{lang}/path）。
	lang := buildLang(req.Lang)
	path, err := sitePathOf(lang, page)
	if err != nil {
		return nil, err
	}
	logger.Scene("build").With("pageId", page.ID).With("lang", lang).With("path", path).Info("开始构建")
	if err = s.syncKernel(path, lang, page.DraftDocument, page.ID); err != nil {
		return nil, err
	}
	hash, err := s.publisher.Build(ctx, page.ID, s.kernelVersion(page.ID))
	if err != nil {
		logger.Scene("build").With("pageId", page.ID).Error(err, "构建失败")
		return nil, mapPublishError(err)
	}

	artifactID, err := s.ensureArtifactRow(ctx, page, hash, page.DraftDocument, lang)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	// 暂存指针按语言记录（多语言 P3）：Build(en-US) 不再覆盖 Build(zh-CN) 的暂存指针，
	// 「先构建两种语言、再逐个发布」由此可用；pages 的单值列仍是最近构建语言的镜像。
	if err = s.model.MarkStagedLang(ctx, page.ID, lang, artifactID, hash, page.DraftVersion, now); err != nil {
		return nil, err
	}
	logger.Scene("build").With("pageId", page.ID).With("hash", hash).Info("构建完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StateReady,
		StagedHash: hash, DraftPath: page.DraftPath,
	}, nil
}

// Publish 激活暂存产物：二次构建校验一致性后原子切换活跃指针。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
func (s *Service) Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	lang := buildLang(req.Lang)
	path, err := sitePathOf(lang, page)
	if err != nil {
		return nil, err
	}
	logger.Scene("publication").With("pageId", page.ID).With("lang", lang).With("path", path).Info("开始发布")
	// 暂存产物按语言取（page_stagings 为真源）：Publish(en-US) 只看 en-US 的暂存，
	// 不会因为中途构建过其他语言而误报「无暂存产物」或发布错语言的产物。
	stagedArt, err := s.stagedArtifactOf(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	if stagedArt.ArtifactKey == "" || stagedArt.PageID != page.ID {
		return nil, ErrNoStagedArtifact
	}

	// FS 激活前预检：目标路径被其他页面/展示实例占用时提前失败（H7），
	// 避免内核先把 FS 覆盖成本页产物、DB 路由写入才报错的状态分裂。
	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, path, page.ID); err != nil {
		logger.Scene("publication").With("pageId", page.ID).With("path", path).Warn("发布被拒绝：路径已被占用")
		return nil, err
	}

	// 确定性构建保证与暂存一致；用「当前草稿」（路径+文档）重建内核——
	// 若草稿在构建后又被 SaveDraft 修改（含改路径），重建 hash 必与暂存不同，
	// 走 ErrRebuildRequired 拒绝发布，避免发布旧内容后界面误报「已发布最新」。
	if err = s.syncKernel(path, lang, page.DraftDocument, page.ID); err != nil {
		return nil, err
	}
	version := s.kernelVersionOrOne(page.ID)
	built, buildErr := s.publisher.Build(ctx, page.ID, version)
	if buildErr != nil {
		logger.Scene("build").With("pageId", page.ID).Error(buildErr, "发布前复构建失败")
		return nil, mapPublishError(buildErr)
	}
	if built != stagedArt.ArtifactHash {
		return nil, ErrRebuildRequired
	}
	hash, err := s.publisher.Publish(page.ID)
	if err != nil {
		logger.Scene("publication").With("pageId", page.ID).Error(err, "发布失败")
		return nil, mapPublishError(err)
	}

	// 记录发布前「本语言」的旧 active 路径快照（page_publications 为该语言真源）：
	// MarkPublishedLang 会把该语言的激活路径更新为本次路径，若 Deactivate 失败后重试
	// （page 重新读取），再读激活记录已是新路径，导致「旧路径永不清理」。
	// 此处以发布前快照为准，重试幂等。
	//
	// 多语言 P3：旧路径只取本语言那一行，因此 Publish(en-US) 不会取消
	// /zh-CN/about 的激活路由——「一页多语言同时在线」由此成立。
	oldPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}

	now := time.Now().UTC()
	if err = s.model.MarkPublishedLang(ctx, pagemodel.PublicationRecord{
		PageID: page.ID, Lang: lang, ActivePath: path,
		ArtifactID: stagedArt.ID, ArtifactHash: hash, PublishedAt: now,
	}); err != nil {
		// FS 已原子激活（线上已生效），此处 DB active 指针更新失败属于部分成功：
		// 错误必须明确暴露，且重试可收敛（复构建 hash 与暂存一致 → 幂等再激活）。
		logger.Scene("publication").With("pageId", page.ID).With("hash", hash).
			Error(err, "发布 FS 已激活，但 DB 活跃指针更新失败（线上已生效，重试可收敛）")
		return nil, fmt.Errorf("发布已生效但数据库状态同步失败: %w", err)
	}
	if s.routes != nil {
		// 旧路径 active 行处置：SaveDraft 改草稿路径后直接发布时，
		// 若不取消旧路径激活，会残留同页双 active 占用（旧路径继续出旧产物）。
		if oldPath != "" && oldPath != path {
			if derr := s.routes.Deactivate(ctx, &pubcontract.DeactivateReq{
				ProjectID: page.ProjectID, Path: oldPath,
			}); derr != nil {
				logger.Scene("publication").With("pageId", page.ID).With("oldPath", oldPath).Error(derr, "发布前取消旧路径激活失败")
				return nil, derr
			}
		}
		if _, err = s.routes.Activate(ctx, &pubcontract.ActivateReq{
			ProjectID: page.ProjectID, Path: path,
			PageID: page.ID, ArtifactID: stagedArt.ID,
		}); err != nil {
			logger.Scene("publication").With("pageId", page.ID).Error(err, "发布路由激活失败")
			return nil, err
		}
	}
	logger.Scene("publication").With("pageId", page.ID).With("hash", hash).Info("发布完成")
	// 站点级 SEO 产物：发布激活后刷新 sitemap.xml / robots.txt。
	// 尽力而为——生成失败只记日志，不回滚已完成的发布（产物可由下次发布或手动接口重建）。
	if err = s.routes.RefreshSiteFiles(ctx, page.ProjectID, siteBaseURL(), pipeline.ActiveRoot(),
		s.enabledLangsOf(ctx, page.ProjectID), s.defaultLocaleOf(ctx, page.ProjectID)); err != nil {
		logger.Scene("publication").With("pageId", page.ID).Error(err, "sitemap/robots 刷新失败")
	}
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StatePublished, ActiveHash: hash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

// Rollback 秒级回滚到历史产物：指针切换，不重新编译。
// nil 请求 / 空 ID / 空 TargetHash 属于请求不合法（ErrInvalidParam）；
// 合法 ID 无页面才返回 ErrPageNotFound，目标 hash 无产物返回 ErrRollbackTargetMiss。
func (s *Service) Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.TargetHash) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("targetHash", req.TargetHash).Info("开始回滚")
	targetArt, err := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: req.TargetHash})
	if err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "回滚目标产物缺失")
		return nil, ErrRollbackTargetMiss
	}
	// 回滚语言取目标产物冻结语言（产物 hash 覆盖 Manifest.lang，同 hash 必同语言）；
	// 目标产物未记录语言时回退请求语言 / 站点默认语言。
	// 按语言作用域回滚：只处置「该语言」的旧激活路由，其他语言保持在线。
	lang := buildLang(req.Lang)
	if strings.TrimSpace(targetArt.Lang) != "" {
		lang = targetArt.Lang
	}
	oldPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}
	if err = s.restoreKernelForHistory(page, targetArt); err != nil {
		return nil, err
	}
	if err = s.publisher.Rollback(page.ID, req.TargetHash); err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "回滚失败")
		return nil, mapPublishError(err)
	}

	now := time.Now().UTC()
	if err = s.model.MarkPublishedLang(ctx, pagemodel.PublicationRecord{
		PageID: page.ID, Lang: lang, ActivePath: targetArt.CanonicalPath,
		ArtifactID: targetArt.ID, ArtifactHash: targetArt.ArtifactHash, PublishedAt: now,
	}); err != nil {
		return nil, err
	}
	if s.routes != nil {
		// 回滚到不同路径的历史产物时，先取消本语言旧 active 路径激活，
		// 避免残留同页双 active 占用（旧路径继续出旧产物，与 Publish 一致）。
		if old := oldPath; old != "" && old != targetArt.CanonicalPath {
			if derr := s.routes.Deactivate(ctx, &pubcontract.DeactivateReq{
				ProjectID: page.ProjectID, Path: old,
			}); derr != nil {
				logger.Scene("page").With("pageId", page.ID).With("oldPath", old).Error(derr, "回滚前取消旧路径激活失败")
				return nil, derr
			}
		}
		if _, err = s.routes.Activate(ctx, &pubcontract.ActivateReq{
			ProjectID: page.ProjectID, Path: targetArt.CanonicalPath,
			PageID: page.ID, ArtifactID: targetArt.ID,
		}); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "回滚路由激活失败")
			return nil, err
		}
	}
	st, _ := s.publisher.Status(page.ID)
	respStatus := pipeline.StatePublished
	if st != nil && st.Status != "" {
		respStatus = st.Status
	}
	logger.Scene("page").With("pageId", page.ID).With("targetHash", req.TargetHash).Info("回滚完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: respStatus, ActiveHash: req.TargetHash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

// UpdateURL 修改访问路径：新 URL 构建激活后，旧 URL 注册 301 或取消激活。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；合法 ID 无页面才返回 ErrPageNotFound。
// FS 激活发生在 publisher.UpdateURL 内部（先于 DB 路由写入），因此新路径
// 占用检查必须在此之前完成，避免「FS 先覆盖、DB 后报错」的状态分裂（H2/H7）。
func (s *Service) UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	// 逻辑新路径（DB 语义，写入 pages.draft_path）与内核实际路径（带语言前缀）分离：
	// 多语言下二者不同，混淆会导致下次构建重复加前缀（/zh-CN/zh-CN/about）。
	newPath, err := normalizePagePath(req.NewPath)
	if err != nil {
		return nil, err
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	lang := buildLang(req.Lang)
	kernelNewPath, err := sitePath(lang, newPath)
	if err != nil {
		return nil, ErrInvalidPath
	}
	oldPath := page.DraftPathValue()
	oldRoutePath, err := sitePath(lang, oldPath)
	if err != nil {
		return nil, ErrInvalidPath
	}
	// FS 激活前预检：新路径已被其他页面/展示实例占用（active/redirect/
	// reserved 任一 kind）时提前失败，绝不触发内核的 FS 覆盖。
	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, kernelNewPath, page.ID); err != nil {
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).Warn("改 URL 被拒绝：新路径已被占用")
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("lang", lang).With("newPath", kernelNewPath).Info("开始修改 URL")
	// 本语言当前线上路径（page_publications 为该语言真源）；该语言尚未发布时
	// 回退本语言的草稿路由路径（纯草稿分支不会用到它做重定向/取消激活）。
	publishedPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}
	if publishedPath == "" {
		publishedPath = oldRoutePath
	}

	// 内核以旧发布路径为基线执行 UpdateURL（内部完成构建+激活+旧路径处置）。
	if err = s.restoreKernelForUpdate(ctx, page, publishedPath); err != nil {
		return nil, err
	}
	if _, err = s.publisher.UpdateURL(ctx, page.ID, kernelNewPath, req.WithRedirect); err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改失败")
		return nil, mapPublishError(err)
	}
	st, _ := s.publisher.Status(page.ID)

	// 纯草稿（从未发布）页面：内核 UpdateURL 已只迁移草稿路径（未构建未激活）。
	// 此处同步 DB 侧：只迁 draft_path 与 reserved 路由，不归档产物、不激活路由、
	// 不建重定向（线上从未存在，无旧路径可处置）——审计 Medium：UpdateURL 纯草稿。
	if !pageHasPublishedState(st) {
		now := time.Now().UTC()
		if err = s.model.MoveDraftPath(ctx, page.ID, newPath, now); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "纯草稿路径迁移失败")
			return nil, err
		}
		if s.routes != nil {
			if rerr := s.renameReservedAllLangs(ctx, page.ProjectID, page.ID, oldPath, newPath, lang); rerr != nil {
				logger.Scene("page").With("pageId", page.ID).Error(rerr, "URL 修改后重命名保留路由失败，中止流程")
				return nil, rerr
			}
		}
		status := pipeline.StateDraft
		if st != nil && st.Status != "" {
			status = st.Status
		}
		logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).With("newPath", newPath).
			Info("纯草稿 URL 修改完成（仅迁移路径与保留路由）")
		return &pagedto.PublishResp{
			PageID: page.ID, Status: status, DraftPath: newPath,
			PublishedAt: now.Format(time.RFC3339),
		}, nil
	}

	// 新产物归档换取 page_artifacts 行 ID：路由 artifact_id 是 uuid 列，
	// 必须写产物行主键而非内容 hash（生产 DDL 下写 hash 必然 22P02 失败）。
	// 归档源文档用内核构建输入（st.DocumentJSON，即活动产物冻结源文档），
	// 与 restoreKernelForUpdate 的编译输入一致（H4）。
	artifactRowID, err := s.ensureArtifactRow(ctx, page, activeHashOf(st), st.DocumentJSON, lang)
	if err != nil {
		logger.Scene("page").With("pageId", page.ID).With("hash", activeHashOf(st)).Error(err, "URL 修改后产物归档失败")
		return nil, err
	}

	now := time.Now().UTC()
	if err = s.model.MoveDraftPath(ctx, page.ID, newPath, now); err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "草稿路径迁移失败")
		return nil, err
	}
	// 该语言的激活路径同步（page_publications 行 + pages 单值镜像）：
	// 只影响本语言，其他语言的激活路径与路由行不动（多语言 P3）。
	if err = s.model.MovePublicationPath(ctx, page.ID, lang, kernelNewPath, now); err != nil {
		logger.Scene("page").With("pageId", page.ID).With("lang", lang).Error(err, "激活路径迁移失败")
		return nil, err
	}
	if s.routes != nil {
		// 改名失败必须中止：reserved 行滞留旧路径会让路由表与 pages 表脱节，
		// 后续 SaveDraft 基于错误基线增删路由（不得仅记日志继续）。
		// 多语言 P3：逐启用语言迁移 reserved 行（逻辑路径 → 各语言站点路径）。
		if rerr := s.renameReservedAllLangs(ctx, page.ProjectID, page.ID, oldPath, newPath, lang); rerr != nil {
			logger.Scene("page").With("pageId", page.ID).Error(rerr, "URL 修改后重命名保留路由失败，中止流程")
			return nil, rerr
		}
		if _, err = s.routes.Activate(ctx, &pubcontract.ActivateReq{
			ProjectID: page.ProjectID, Path: kernelNewPath,
			PageID: page.ID, ArtifactID: artifactRowID,
		}); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改后路由激活失败")
			return nil, err
		}
		if oldRoutePath != kernelNewPath {
			if req.WithRedirect {
				if err = s.ensureRedirectRoute(ctx, page, publishedPath); err != nil {
					logger.Scene("page").With("pageId", page.ID).Error(err, "重定向路由注册失败")
					return nil, err
				}
			} else if err = s.routes.Deactivate(ctx, &pubcontract.DeactivateReq{
				ProjectID: page.ProjectID, Path: publishedPath,
			}); err != nil {
				logger.Scene("page").With("pageId", page.ID).Error(err, "旧 URL 取消激活失败")
				return nil, err
			}
		}
	}
	status := pipeline.StatePublished
	if st != nil && st.Status != "" {
		status = st.Status
	}
	logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).With("newPath", newPath).Info("URL 修改完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: status, ActiveHash: activeHashOf(st),
		OldPath: publishedPath, DraftPath: newPath,
		PublishedAt: now.Format(time.RFC3339),
	}, nil
}


// stagedArtifactOf 取该语言的暂存产物（page_stagings 为真源）。
//
// 兼容口径：迁移 063 之前只写 pages.staged_artifact_id，该镜像仅在「产物语言与
// 目标语言一致」时采用（多语言站点里镜像可能属于别的语言，绝不将错就错）。
func (s *Service) stagedArtifactOf(ctx context.Context, page *pagemodel.PageEntity, lang string) (*artifactcontract.ArtifactResp, error) {
	artifactID := ""
	st, err := s.model.GetStaging(ctx, page.ID, lang)
	switch {
	case err == nil && st != nil:
		artifactID = st.ArtifactID
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}
	// 回退镜像：仅当镜像产物确实属于目标语言时可用。
	if artifactID == "" {
		if page.StagedArtifactID == nil || *page.StagedArtifactID == "" {
			return nil, ErrNoStagedArtifact
		}
		artifactID = *page.StagedArtifactID
	}
	art, derr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: artifactID})
	if derr != nil {
		// 仅真实「无暂存产物」（artifact 侧 ErrArtifactNotFound）归一为 409 业务冲突；
		// DB 故障等其他系统错误原样透传并记日志，避免被误判为「无暂存产物」误导前端。
		if strings.Contains(derr.Error(), artifactenums.ErrArtifactNotFound) {
			return nil, ErrNoStagedArtifact
		}
		logger.Scene("publication").With("pageId", page.ID).With("artifactID", artifactID).
			Error(derr, "查询暂存产物失败")
		return nil, derr
	}
	if art.Lang != "" && art.Lang != lang {
		// 该语言没有暂存产物（镜像属于其他语言）：按「无暂存产物」处理，不跨语言发布。
		return nil, ErrNoStagedArtifact
	}
	return art, nil
}

// publishedPathOf 取该语言当前线上激活路径（page_publications 为真源）。
//
// 兼容口径：关闭站点语言前缀（i18n.site_lang_prefix=false）的单语言站点，
// pages.active_path 单值即该语言的路径，历史行（迁移 062 回填前）也按此读；
// 开启前缀时不猜测——没有该语言的激活记录就返回空（本语言从未发布），
// 绝不拿别的语言的路径去 Deactivate（这正是 Publish(en-US) 取消 /zh-CN/about 的根因）。
func (s *Service) publishedPathOf(ctx context.Context, page *pagemodel.PageEntity, lang string) (string, error) {
	pub, err := s.model.GetPublication(ctx, page.ID, lang)
	if err == nil && pub != nil {
		return pub.ActivePath, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if !i18n.SiteLangPrefixEnabled() {
		return page.ActivePathValue(), nil
	}
	return "", nil
}

// ---- 内核记录重建辅助 ----

// restoreKernelForHistory 以目标产物为基线重建内核记录（回滚前置）。
func (s *Service) restoreKernelForHistory(page *pagemodel.PageEntity, target *artifactcontract.ArtifactResp) error {
	doc := page.DraftDocumentFor(target.SourceDocument)
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: target.CanonicalPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc,
		Histories: []*pipeline.HistoryEntry{{
			Hash: target.ArtifactHash, Path: target.CanonicalPath,
			Status: pipeline.StateSuperseded, Order: 1,
		}},
	}
	s.publisher.LoadRecord(rec)
	return nil
}

// restoreKernelForUpdate 以当前发布路径重建内核记录并预激活现有产物（URL 变更前置）。
func (s *Service) restoreKernelForUpdate(ctx context.Context, page *pagemodel.PageEntity, publishedPath string) error {
	doc := page.DraftDocument
	activeHash := ""
	histories := []*pipeline.HistoryEntry{}
	if page.ActiveArtifactID != nil && *page.ActiveArtifactID != "" {
		art, err := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: *page.ActiveArtifactID})
		if err != nil {
			// 活动产物行缺失是数据不一致（产物行被删而指针未清）：显式失败而非
			// 降级为纯草稿——否则 histories/activeHash 留空，UpdateURL 误判纯草稿，
			// 只迁 draft_path 不构建不激活，线上旧 URL 继续出旧内容。
			return fmt.Errorf("页面活动产物缺失（artifact_id=%s），无法修改 URL: %w", *page.ActiveArtifactID, err)
		}
		activeHash = art.ArtifactHash
		histories = append(histories, &pipeline.HistoryEntry{
			Hash: art.ArtifactHash, Path: art.CanonicalPath,
			Status: pipeline.StatePublished, Order: 1,
		})
		doc = page.DraftDocumentFor(art.SourceDocument)
	}
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: publishedPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc, ActiveHash: activeHash, Histories: histories,
	}
	s.publisher.LoadRecord(rec)
	return nil
}

// ensureArtifactRow 返回该 hash 对应的产物元数据行 ID；不存在则归档新建。
// sourceDocument 必须与构建该产物的输入一致：Build 路径为当前草稿，
// UpdateURL 路径为活动产物冻结源文档（内核 restoreKernelForUpdate 的输入）。
// 若统一归档 page.DraftDocument，草稿较新时产物字节与归档 SourceDocument/
// SourceHash 不对应，日后按该产物回滚会编译出不同 hash（ErrRollbackPathMismatch）。
//
// lang 为本次构建语言：产物行唯一键是 (page_id, version, lang)，同页多语言各占一行；
// 预检查询按 hash（hash 覆盖 Manifest.lang，必同语言）即可，写入必须带 lang。
func (s *Service) ensureArtifactRow(ctx context.Context, page *pagemodel.PageEntity, hash string, sourceDocument json.RawMessage, lang string) (string, error) {
	existing, err := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: hash})
	if err == nil {
		return existing.ID, nil
	}
	loc := pipeline.ArtifactLocator(hash)
	art, err := s.store.GetArtifact(loc)
	if err != nil {
		return "", err
	}
	manifestJSON, err := json.Marshal(art.Manifest)
	if err != nil {
		return "", err
	}
	recorded, err := s.artifacts.EnsureRecord(ctx, &artifactcontract.RecordReq{
		ArtifactID:       uuid.NewString(),
		PageID:           page.ID,
		Version:          page.DraftVersion,
		Lang:             lang,
		SourceDocument:   sourceDocument,
		SchemaVersion:    art.Manifest.PageDocumentSchemaVersion,
		SourceHash:       art.Manifest.SourceHash,
		BuildInputHash:   art.Manifest.BuildInputHash,
		ArtifactProvider: "local",
		ArtifactKey:      loc.Key,
		ArtifactHash:     hash,
		CompilerVersion:  art.Manifest.CompilerVersion,
		RegistryVersion:  art.Manifest.CompilerVersion,
		Manifest:         manifestJSON,
		CreatedBy:        systemCreator,
	})
	if err != nil {
		return "", err
	}
	return recorded.ID, nil
}

// ensureRedirectRoute 把「本语言的旧发布路径」占用标记为 redirect 并落盘重定向产物。
// publishedPath 由调用方按语言解析（page_publications 真源）——旧实现用
// pages.active_path 单值，多语言下会拿到别的语言的路径。
func (s *Service) ensureRedirectRoute(ctx context.Context, page *pagemodel.PageEntity, publishedPath string) error {
	// 未发布页面没有旧线上路径：无法（也无需）创建 301 重定向产物。
	// 旧实现无条件用 ActivePathValue()（未发布为空串）构造产物，
	// 在 FS/DB 已迁移后报「路径不能为空」，造成状态分裂。
	if publishedPath == "" {
		return nil
	}
	ra, raErr := pipeline.NewRedirectArtifact(publishedPath, 301)
	if raErr != nil {
		return raErr
	}
	if _, saErr := s.store.PutRedirect(ra); saErr != nil {
		return saErr
	}
	_, rerr := s.routes.Redirect(ctx, &pubcontract.RedirectReq{
		ProjectID: page.ProjectID, OldPath: publishedPath, PageID: page.ID,
	})
	return rerr
}

// kernelVersion 读取内核记录当前版本（不存在视为 1）。
func (s *Service) kernelVersion(pageID string) int {
	if st, err := s.publisher.Status(pageID); err == nil && st.Version > 0 {
		return st.Version
	}
	return 1
}

// pageHasPublishedState 判断页面是否曾上线（与 pipeline.PageRecord.hasPublishedHistory 口径一致）。
// UpdateURL 对纯草稿（从未发布）页面只迁移路径与保留路由，不构建不激活。
func pageHasPublishedState(rec *pipeline.PageRecord) bool {
	if rec == nil {
		return false
	}
	if rec.ActiveHash != "" {
		return true
	}
	for _, h := range rec.Histories {
		if h.Status == pipeline.StatePublished {
			return true
		}
	}
	return false
}

func (s *Service) kernelVersionOrOne(pageID string) int { return s.kernelVersion(pageID) }

// ensureRouteNotOccupied 校验目标路径未被其他页面/展示实例占用
// （page_routes 中 active/redirect/reserved 任一 kind；page_id 为空即展示
// 实例占用）。本页面自己的占用行不算冲突。
// 该检查必须在触发 FS 激活的 publisher 调用之前执行（H7 前置防线），
// 并发抢占窗口由 publication Activate 事务内的归属校验兜底。
// 经 publication contract 查询（page_routes 单一所有归 publication）。
func (s *Service) ensureRouteNotOccupied(ctx context.Context, projectID, path, selfPageID string) error {
	if s.routes == nil {
		return nil
	}
	occupied, err := s.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: projectID, Path: path, ExcludePageID: selfPageID,
	})
	if err != nil {
		return err
	}
	if occupied {
		return ErrPathOccupied
	}
	return nil
}

func activeHashOf(rec *pipeline.PageRecord) string {
	if rec == nil {
		return ""
	}
	return rec.ActiveHash
}

func mapPublishError(err error) error {
	switch {
	case errors.Is(err, pipeline.ErrVersionConflict):
		return ErrDraftVersionConflict
	case errors.Is(err, pipeline.ErrNoStagedArtifact):
		return ErrNoStagedArtifact
	case errors.Is(err, pipeline.ErrRollbackPathMismatch):
		return ErrRebuildRequired
	case errors.Is(err, pipeline.ErrPageNotFound):
		return ErrPageNotFound
	default:
		return err
	}
}

// siteBaseURL 站点公开根地址（sitemap/robots 用）。
// 通过环境变量 WP_SITE_BASE_URL 配置；未配置时返回空串，生成器会省略绝对 URL 前缀。
func siteBaseURL() string {
	return strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
}
