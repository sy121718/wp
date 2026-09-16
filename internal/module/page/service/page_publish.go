package pageservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
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
	path, err := s.sitePathOf(ctx, lang, page)
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

	artifactID, deps, err := s.ensureArtifactRow(ctx, page, hash, page.DraftDocument, lang)
	if err != nil {
		return nil, err
	}
	// 依赖记录落库（docs/03-pipeline.md §8.2）：本次产物声明的依赖集合，
	// 供依赖源变更时按 (kind,key) 反查受影响页面（PIPE-3 精确 fan-out）。
	s.persistDependencies(ctx, page.ProjectID, page.ID, artifactID, deps)
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
	path, err := s.sitePathOf(ctx, lang, page)
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
	// 活跃产物的依赖记录必须齐备（fan-out 反查的前提）：发布时按 Manifest 补写一次。
	s.persistDependenciesFromManifest(ctx, page.ProjectID, page.ID, stagedArt.ID, stagedArt.Manifest)

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
		// 暂存与复构建不一致。两种成因必须分开处理：
		//
		//  1. **草稿变了**（构建后又被 SaveDraft，含改 URL）：绝不能发布 —— 那正是
		//     「发布旧内容后界面误报已发布最新」。走 ErrRebuildRequired。
		//  2. **草稿没变、站点级外部状态变了**：语言切换器按访问面过滤（审计 I18N-021），
		//     其它语言恰好在这两次构建之间发布了，于是同一份草稿产出不同字节。
		//     这时报错会把「先构建多种语言、再逐个发布」这个自然操作挡死。
		//
		// 区分依据是**暂存行的草稿版本**，不是内核版本：syncKernel 刚刚把内核刷成了
		// 当前草稿，拿内核版本比永远相等，等于取消这道闸（草稿被改过也会照发）。
		// 暂存行记录的是「这份产物是从哪个草稿版本构建的」—— 它与当前草稿版本不一致
		// 就说明产物旧了，必须拒绝。
		staging, gerr := s.model.GetStaging(ctx, page.ID, lang)
		if gerr != nil || staging == nil || staging.DraftVersion != page.DraftVersion {
			return nil, ErrRebuildRequired
		}
		refreshed, ferr := s.publisher.Build(ctx, page.ID, version)
		if ferr != nil {
			return nil, mapPublishError(ferr)
		}
		if refreshed != built {
			// 重新构建仍与第一次不同：输入不稳定，属于真问题，不再往下掩盖。
			return nil, ErrRebuildRequired
		}
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Warn("暂存产物落后于站点级状态（如其它语言刚发布），已按当前草稿重新构建")
		// 这里只调了内核的 Build，而 s.Build 的另一半责任（写产物行 + 依赖记录）要补上：
		// 少了它，激活用的 hash 在产物表里没有对应行 —— 符号链接指向一个「查不到出处」
		// 的产物，回滚与引用保护都会从这里出问题。
		artifactID, deps, aerr := s.ensureArtifactRow(ctx, page, refreshed, page.DraftDocument, lang)
		if aerr != nil {
			return nil, aerr
		}
		s.persistDependencies(ctx, page.ProjectID, page.ID, artifactID, deps)
		// 三个地方都要换成本次的 hash，否则「暂存指针 / 激活指针 / 符号链接」各自指向
		// 不同产物：stagedArt 用于落库与激活，built 供后续步骤读取。
		stagedArt.ID = artifactID
		stagedArt.ArtifactHash = refreshed
		built = refreshed
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
			// DB 路由行删除只表示「不再占用」，访问面的符号链接必须另行解除：
			// /site 直接服务 active 目录的文件系统状态，只删 DB 行会让旧 URL
			// 继续输出旧产物（同页双 active 占用），且此后没有任何入口能查到
			// 该清哪个链接 —— 与页面删除同一根因。
			if derr := s.deactivatePaths([]string{oldPath}); derr != nil {
				logger.Scene("publication").With("pageId", page.ID).With("oldPath", oldPath).Error(derr, "发布前解除旧路径访问面激活失败")
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
	// 站点级 SEO 产物与自定义 404 页：发布激活后刷新
	// sitemap.xml / robots.txt / feed.xml / 404.html。
	// 尽力而为——生成失败只记日志，不回滚已完成的发布（产物可由下次发布或手动接口重建）。
	if err = s.routes.RefreshSiteFiles(ctx, page.ProjectID, siteBaseURL(), pipeline.ActiveRoot(),
		s.enabledLangsOf(ctx, page.ProjectID), s.defaultLocaleOf(ctx, page.ProjectID),
		s.notFoundHTMLOf(ctx, page.ProjectID)); err != nil {
		logger.Scene("publication").With("pageId", page.ID).Error(err, "sitemap/robots 刷新失败")
	}
	s.notifyIndexNow(ctx, page.ProjectID, path)
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StatePublished, ActiveHash: hash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

// notFoundHTMLOf 站点自定义 404 页内容（projects.settings.notFoundHtml，空 = 未配置）。
//
// 读不到工程时返回空串：404 页是可选能力，「取不到」与「没配」在访问面等价
// （都退回默认 404 行为），不该让它阻断发布 —— 与 enabledLangsOf / defaultLocaleOf
// 的降级口径一致。内容只做长度校验（保存时在后台入口），这里原样透传：
// 它是管理员配置的一份 HTML 文档，等同页面正文，发布链不做二次加工。
func (s *Service) notFoundHTMLOf(ctx context.Context, projectID string) string {
	if s.project == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return ""
	}
	return projectcontract.ParseSiteSettings(project.Settings).NotFoundHTML
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
			// DB 路由行删除只表示「不再占用」，访问面的符号链接必须另行解除：
			// /site 直接服务 active 目录的文件系统状态，只删 DB 行会让旧 URL
			// 继续输出旧产物（同页双 active 占用），且此后没有任何入口能查到
			// 该清哪个链接 —— 与页面删除同一根因。
			if derr := s.deactivatePaths([]string{old}); derr != nil {
				logger.Scene("page").With("pageId", page.ID).With("oldPath", old).Error(derr, "回滚前解除旧路径访问面激活失败")
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
