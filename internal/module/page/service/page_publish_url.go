package pageservice

// page_publish_url.go — 改 URL 与路径占用（改地址、301 旧路径、占用预检、站点 base URL）。

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// sitePathOf 计算页面实际访问路径（语言 URL 方案单点映射），失败归一为 ErrInvalidPath。
func (s *Service) sitePathOf(ctx context.Context, lang string, page *pagemodel.PageEntity) (string, error) {
	path, err := sitePath(s.langURLRuleOf(ctx, page.ProjectID), lang, page.DraftPath)
	if err != nil {
		return "", ErrInvalidPath
	}
	return path, nil
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
	rule := s.langURLRuleOf(ctx, page.ProjectID)
	kernelNewPath, err := sitePath(rule, lang, newPath)
	if err != nil {
		return nil, ErrInvalidPath
	}
	oldPath := page.DraftPathValue()
	oldRoutePath, err := sitePath(rule, lang, oldPath)
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
		if err = s.model.MoveDraftPath(ctx, page.ProjectID, page.ID, newPath, now); err != nil {
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
	artifactRowID, deps, err := s.ensureArtifactRow(ctx, page, activeHashOf(st), st.DocumentJSON, lang)
	if err != nil {
		logger.Scene("page").With("pageId", page.ID).With("hash", activeHashOf(st)).Error(err, "URL 修改后产物归档失败")
		return nil, err
	}
	// 归档即补依赖记录：URL 变更不改变依赖集合，但产物行可能新建，
	// 依赖表必须同步（否则该产物的精确失效查询会漏掉它）。
	s.persistDependencies(ctx, page.ProjectID, page.ID, artifactRowID, deps)

	now := time.Now().UTC()
	if err = s.model.MoveDraftPath(ctx, page.ProjectID, page.ID, newPath, now); err != nil {
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
			} else if derr := s.deactivatePaths([]string{publishedPath}); derr != nil {
				// 内核的旧路径处置失败只记日志（新 URL 已上线，不阻断流程），
				// 这里再幂等地清一次；两层都失败才残留，且此时会明确报错。
				logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).Error(derr, "旧 URL 解除访问面激活失败")
				return nil, derr
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
	if !i18n.SiteLangURLsSeparated() {
		return page.ActivePathValue(), nil
	}
	return "", nil
}

// ---- 内核记录重建辅助 ----

// ensureRedirectRoute 把「本语言的旧发布路径」登记为 redirect 行。
//
// publishedPath 由调用方按语言解析（page_publications 真源）——旧实现用
// pages.active_path 单值，多语言下会拿到别的语言的路径。
//
// 这里**只登记 DB 占用**，不落盘重定向产物：旧路径 → 新路径的 301 产物由内核
// publisher.UpdateURL 落盘并激活到旧路径（internal/pipeline/publisher.go 的
// withRedirect 分支），本条 UpdateURL 流程正是经由那个调用进入这里。
//
// 此前这里还会自己 NewRedirectArtifact(publishedPath, 301) 再 PutRedirect 一次 ——
// 该函数第一个参数是 targetPath，传旧路径自身等于生成一条 A→A 的自环 301。它落在
// 内容寻址 store 里、从不被激活（PutRedirect 只写 artifacts/redirects/<hash>，
// 激活由调用方另行发起），所以线上行为一直是对的（生效的是内核那份），代价是
// 每工程每改一次 URL 就多一份永不使用的垃圾产物；而一旦有人把它的 Locator 拿去
// 激活，得到的就是无限重定向。已删除。
func (s *Service) ensureRedirectRoute(ctx context.Context, page *pagemodel.PageEntity, publishedPath string) error {
	// 未发布页面没有旧线上路径：无法（也无需）登记重定向。
	// 旧实现无条件用 ActivePathValue()（未发布为空串）构造产物，
	// 在 FS/DB 已迁移后报「路径不能为空」，造成状态分裂。
	if publishedPath == "" {
		return nil
	}
	_, rerr := s.routes.Redirect(ctx, &pubcontract.RedirectReq{
		ProjectID: page.ProjectID, OldPath: publishedPath, PageID: page.ID,
	})
	return rerr
}

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

// siteBaseURL 站点公开根地址（sitemap/robots 用）。
// 通过环境变量 WP_SITE_BASE_URL 配置；未配置时返回空串，生成器会省略绝对 URL 前缀。
func siteBaseURL() string {
	return strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
}
