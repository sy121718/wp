package pageservice

// page_publish_url.go — 改 URL 与路径占用（改地址、301 旧路径、占用预检、站点 base URL）。

import (
	"context"
	"errors"
	"fmt"
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
//
// 已发布页面按 Publish 的同一套协议：**切换之前**登记 pending 回执（action
// update_url，记下旧路径与该路径的处置方式），切换之后把 DB 各步收进一个事务
// （applyUpdateURL，与启动恢复共用实现）；中途失败保持 pending，由
// RecoverPendingPublications 的 update_url 分支补齐 —— 此前完全没有回执，
// 任一 DB 步失败都会留下「线上是新 URL、DB 还是旧路径」，且重启也捞不回来。
//
// 纯草稿页面（从未发布）不需要回执：内核只迁移草稿路径，访问面没有发生任何切换。
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
	// 站点语言集合在**任何内核调用之前**解析（审计 I18N-02 收尾）。
	//
	// 为什么必须在切访问面之前：改 URL 要按启用语言逐语言迁移保留路由，而路径解析
	// 只需一次读语言表。把这次读留到「内核已把 FS 切到新路径之后、事务之内」，
	// 读失败时只剩两种坏选择：带着「只有默认语言」的清单迁移（其余语言的 reserved
	// 行停在旧路径，事务照常提交 —— 三方分裂），或者让已经生效的切换回退（FS 上没有
	// 回退这条路）。放在前面之后，读不到就在**访问面还没动**时失败，什么都不用拆。
	//
	// 口径用发布硬口径（publishLangsOf）：这是会写站点访问路径的动作，
	// 「不知道站点有哪几种语言」不能降级成「只动默认语言」。
	routeLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		logger.Scene("page").With("pageId", page.ID).
			Error(lerr, "改 URL 中止：站点语言清单不可读，不带着不完整的语言集合去切访问面")
		return nil, lerr
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
	stBefore, _ := s.publisher.Status(page.ID)

	// 纯草稿（从未发布）页面：内核 UpdateURL 只迁移草稿路径（未构建未激活），
	// 访问面没有任何切换 —— 因此不需要回执，两处 DB 写收在一个事务里即可。
	// 只迁 draft_path 与 reserved 路由，不归档产物、不激活路由、不建重定向
	//（线上从未存在，无旧路径可处置）——审计 Medium：UpdateURL 纯草稿。
	if !pageHasPublishedState(stBefore) {
		if _, err = s.publisher.UpdateURL(ctx, page.ID, kernelNewPath, req.WithRedirect); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改失败")
			return nil, mapPublishError(err)
		}
		now := time.Now().UTC()
		if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
			if merr := s.model.MoveDraftPathTx(ctx, tx, page.ProjectID, page.ID, newPath, now); merr != nil {
				return merr
			}
			return s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
				ProjectID: page.ProjectID, PageID: page.ID,
				OldLogical: oldPath, NewLogical: newPath, TargetLang: lang, Langs: routeLangs,
			})
		}); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "纯草稿路径迁移失败")
			return nil, err
		}
		st, _ := s.publisher.Status(page.ID)
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

	// 已发布页面：登记 pending 回执，**必须在访问面切换之前**。
	//
	// 改 URL 的产物是切换时按新路径现编译的，登记时还没有对应的产物行，
	// 因此回执的 ToArtifactID 留空（见 publication 侧 DTO 注释），恢复改用
	// 「新路径上的产物 canonicalPath 是否等于新路径」判定；FromArtifactID 记下
	// 切换前的活跃产物，恢复归档新产物行时用它拿冻结源文档。
	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionUpdateURL,
		ProjectID: page.ProjectID, PageID: page.ID,
		Path: kernelNewPath, Lang: lang,
		FromArtifactID: s.publishedArtifactIDOf(ctx, page, lang),
		OldPath:        publishedPath,
		Redirect:       req.WithRedirect,
	})
	if rerr != nil {
		return nil, rerr
	}
	if _, err = s.publisher.UpdateURL(ctx, page.ID, kernelNewPath, req.WithRedirect); err != nil {
		// 内核保证「构建失败不激活、激活失败线上不变」（pipeline.Publisher.UpdateURL
		// 的各错误分支都在 publishLocked 之前）：属于可判定的无副作用失败，显式结案，
		// 不留给启动恢复一个假 pending。
		s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
		logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改失败")
		return nil, mapPublishError(err)
	}
	st, _ := s.publisher.Status(page.ID)

	// 新产物归档换取 page_artifacts 行 ID：路由 artifact_id 是 uuid 列，
	// 必须写产物行主键而非内容 hash（生产 DDL 下写 hash 必然 22P02 失败）。
	// 归档源文档用内核构建输入（st.DocumentJSON，即活动产物冻结源文档），
	// 与 restoreKernelForUpdate 的编译输入一致（H4）。
	artifactRowID, deps, err := s.ensureArtifactRow(ctx, page, activeHashOf(st), st.DocumentJSON, lang)
	if err != nil {
		// FS 已切到新路径：保留 pending，恢复流程会用同一份源文档重做归档。
		s.keepPublishReceiptPending(receiptID, "产物归档失败")
		logger.Scene("page").With("pageId", page.ID).With("hash", activeHashOf(st)).Error(err, "URL 修改后产物归档失败")
		return nil, err
	}

	// 数据库各步收在**一个事务**里（applyUpdateURL 与启动恢复共用同一段实现）：
	// draft_path 迁移、该语言激活路径迁移、各语言 reserved 路由改名、新路径路由激活、
	// 旧路径处置（301 或取消激活）、依赖记录同步。任一步失败整体回滚 + 回执保持
	// pending，由启动恢复按链接的实际指向补齐。
	if err = s.applyUpdateURL(ctx, updateURLApplyInput{
		Page: page, ArtifactRowID: artifactRowID, Lang: lang,
		KernelNewPath: kernelNewPath, NewLogicalPath: newPath,
		OldLogicalPath: oldPath, OldKernelPath: publishedPath,
		WithRedirect: req.WithRedirect, Deps: deps, RouteLangs: routeLangs,
	}); err != nil {
		s.keepPublishReceiptPending(receiptID, "DB 状态写入失败")
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).
			Error(err, "URL 修改 FS 已切换，但 DB 事务失败（线上已生效，恢复会补齐）")
		return nil, fmt.Errorf("URL 修改已生效但数据库状态同步失败: %w", err)
	}
	// 旧路径的访问面处置（跨系统动作，事务之外）：非重定向时解除链接，重定向时重放
	// 同一份 301 产物 —— 内核那一步失败只记日志，不在这里重放就会留下「DB 说
	// redirect、FS 还指着旧页面」的错位。两步都幂等；失败保持 pending，恢复会重放。
	if derr := s.settleOldPath(kernelNewPath, publishedPath, req.WithRedirect); derr != nil {
		s.keepPublishReceiptPending(receiptID, "旧路径访问面处置失败")
		logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).Error(derr, "旧 URL 访问面处置失败")
		return nil, derr
	}
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		logger.Scene("page").With("pageId", page.ID).With("receiptId", receiptID).
			Error(cerr, "改 URL 回执结案失败（状态已一致，收敛例程会幂等收尾）")
		// 同上：事务已提交、回执未收口 —— 快通道让收敛立刻收掉。
		s.NotifyPendingReceipt()
	}
	status := pipeline.StatePublished
	if st != nil && st.Status != "" {
		status = st.Status
	}
	logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).With("newPath", newPath).Info("URL 修改完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: status, ActiveHash: activeHashOf(st),
		OldPath: publishedPath, DraftPath: newPath,
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
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
