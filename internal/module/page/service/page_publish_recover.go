package pageservice

// page_publish_recover.go —— 访问面切换回执的 DB 步骤共享实现与启动恢复补齐。
//
// 三种回执形态（发布 / 改 URL / 回滚）在「访问面已切换、数据库没跟上」窗口里要补的
// 数据库步骤各不相同，但**主链与恢复必须共用同一段实现**：恢复例程若另写一遍
// 「补齐逻辑」，两边迟早分叉，而分叉的表现是「恢复后状态看着收敛了、但与正常
// 走一遍的结果不同」——例如漏迁移 reserved 路由、漏处置旧路径。
//
// 事务边界：DB 各步收在一个事务里（pages / page_publications / page_stagings 走
// 本模块的 *Tx 方法，page_routes 走 publication 的 *Tx 方法）。文件系统那一步
// （符号链接切换）在事务之外，用回执兜底 —— 这是规则允许的补偿形态：
// 跨系统、幂等、留痕、可重放。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// publishActivationInput 发布（switch_active）的 DB 步骤入参。
type publishActivationInput struct {
	Page         *pagemodel.PageEntity
	Lang         string
	Path         string
	ArtifactID   string
	ArtifactHash string
	// OldPath 该语言发布前的线上路径（发布改路径后要取消它的激活）。
	OldPath string
}

// applyPublishActivation 在一个事务里落定发布的数据库状态（主链与恢复共用）。
//
// 幂等：upsert 活跃指针、按归属重复激活路由、按路径重复取消激活都不会产生新状态；
// 因此恢复重放它不会把已收敛的状态改坏。
func (s *Service) applyPublishActivation(ctx context.Context, in publishActivationInput) error {
	now := time.Now().UTC()
	return s.model.TransactionScoped(ctx, in.Page.ProjectID, func(tx *gorm.DB) error {
		if merr := s.model.MarkPublishedLangTx(ctx, tx, in.Page.ProjectID, pagemodel.PublicationRecord{
			PageID: in.Page.ID, Lang: in.Lang, ActivePath: in.Path,
			ArtifactID: in.ArtifactID, ArtifactHash: in.ArtifactHash, PublishedAt: now,
		}); merr != nil {
			return merr
		}
		// 故障注入点（测试用；生产恒为 nil）：命中「访问面已切换、数据库尚未落定」窗口。
		// 放在第一条写之后，要证明的是「半截写随事务一起回滚」，而不是「还没开始写」。
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if s.routes == nil {
			return nil
		}
		if in.OldPath != "" && in.OldPath != in.Path {
			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: in.Page.ProjectID, Path: in.OldPath,
			}); derr != nil {
				return derr
			}
		}
		_, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.Path, PageID: in.Page.ID, ArtifactID: in.ArtifactID,
		})
		return aerr
	})
}

// updateURLApplyInput 改 URL 的 DB 步骤入参（主链与恢复共用）。
type updateURLApplyInput struct {
	Page           *pagemodel.PageEntity
	ArtifactRowID  string
	Lang           string
	KernelNewPath  string
	NewLogicalPath string
	// OldLogicalPath 改 URL 前的**草稿**逻辑路径（reserved 路由按它迁移）。
	OldLogicalPath string
	// OldKernelPath 改 URL 前的**线上**路径（301 或取消激活按它处置）。
	OldKernelPath string
	WithRedirect  bool
	// RouteLangs 站点语言集合（发布口径，默认语言在前），由调用方在**事务之前**解析：
	// 保留路由要逐语言迁移，而把它留到事务内再读一次语言表，读失败就会只迁移默认语言
	// （审计 I18N-02 收尾）。主链在切访问面之前解析，恢复路径在补写之前解析。
	RouteLangs []string
	// Deps 本次产物声明的构建期依赖（Manifest.dependencies）。URL 变更不改变依赖集合，
	// 但产物行可能是本次新建的，依赖投影必须同步 —— 否则该产物的精确失效查询会漏掉它。
	Deps []pipeline.Dependency
}

// applyUpdateURL 在一个事务里落定改 URL 的全部数据库状态（主链与恢复共用）。
//
// 五步同事务：draft_path 迁移 → 该语言激活路径迁移 → 各语言 reserved/本语言 active
// 路由改名 → 新路径路由激活 → 旧路径处置（301 或取消激活）。
// 顺序与语义和改动前逐条一致，区别只在于「中途失败整体回滚」——
// 不再出现「FS 已是新 URL、pages.draft_path 与 page_routes 还在旧路径」。
func (s *Service) applyUpdateURL(ctx context.Context, in updateURLApplyInput) error {
	now := time.Now().UTC()
	return s.model.TransactionScoped(ctx, in.Page.ProjectID, func(tx *gorm.DB) error {
		if merr := s.model.MoveDraftPathTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.NewLogicalPath, now); merr != nil {
			return merr
		}
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if merr := s.model.MovePublicationPathTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.Lang, in.KernelNewPath, now); merr != nil {
			return merr
		}
		// 依赖投影与路由同事务：它是「依赖源变更时精确标 stale」的依据，
		// 失败只记日志会让该页长期显示旧内容（Build 侧已按同一口径收口）。
		// deps 为空视为「本次没有要写的条目」而不是「清空」：URL 变更不改变依赖集合，
		// 空集合只可能来自 Manifest 缺失，不该顺手把既有投影删掉。
		if len(in.Deps) > 0 {
			if derr := s.persistDependenciesTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.ArtifactRowID, in.Deps); derr != nil {
				return derr
			}
		}
		if s.routes == nil {
			return nil
		}
		if rerr := s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
			ProjectID: in.Page.ProjectID, PageID: in.Page.ID,
			OldLogical: in.OldLogicalPath, NewLogical: in.NewLogicalPath,
			TargetLang: in.Lang, Langs: in.RouteLangs,
		}); rerr != nil {
			return rerr
		}
		if _, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.KernelNewPath,
			PageID: in.Page.ID, ArtifactID: in.ArtifactRowID,
		}); aerr != nil {
			return aerr
		}
		if in.OldKernelPath == "" || in.OldKernelPath == in.KernelNewPath {
			return nil
		}
		if in.WithRedirect {
			// 旧路径 → 新路径登记为 redirect 行（**只登记 DB 占用**，不落盘重定向产物：
			// 301 产物由内核 publisher.UpdateURL 落盘并激活到旧路径；恢复路径下
			// settleOldPath 会用同一份产物幂等地重放）。
			//
			// 历史上这里曾自己 NewRedirectArtifact(publishedPath, 301) 再 PutRedirect 一次 ——
			// 该函数第一个参数是 targetPath，传旧路径自身等于生成一条 A→A 的自环 301。
			// 它落在内容寻址 store 里、从不被激活，所以线上行为一直是对的（生效的是内核那份），
			// 代价是每工程每改一次 URL 就多一份永不使用的垃圾产物。已删除，勿再引入。
			_, rerr := s.routes.RedirectTx(ctx, tx, &pubcontract.RedirectReq{
				ProjectID: in.Page.ProjectID, OldPath: in.OldKernelPath, PageID: in.Page.ID,
			})
			return rerr
		}
		return s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
			ProjectID: in.Page.ProjectID, Path: in.OldKernelPath,
		})
	})
}

// rollbackApplyInput 回滚的 DB 步骤入参（主链与恢复共用）。
type rollbackApplyInput struct {
	Page       *pagemodel.PageEntity
	Lang       string
	TargetPath string
	TargetID   string
	TargetHash string
	// OldPath 回滚前的线上路径（与目标路径不同才需要取消它的激活）。
	OldPath string
}

// applyRollback 在一个事务里落定回滚的数据库状态（主链与恢复共用）。
func (s *Service) applyRollback(ctx context.Context, in rollbackApplyInput) error {
	now := time.Now().UTC()
	return s.model.TransactionScoped(ctx, in.Page.ProjectID, func(tx *gorm.DB) error {
		if merr := s.model.MarkPublishedLangTx(ctx, tx, in.Page.ProjectID, pagemodel.PublicationRecord{
			PageID: in.Page.ID, Lang: in.Lang, ActivePath: in.TargetPath,
			ArtifactID: in.TargetID, ArtifactHash: in.TargetHash, PublishedAt: now,
		}); merr != nil {
			return merr
		}
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if s.routes == nil {
			return nil
		}
		if in.OldPath != "" && in.OldPath != in.TargetPath {
			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: in.Page.ProjectID, Path: in.OldPath,
			}); derr != nil {
				return derr
			}
		}
		_, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.TargetPath, PageID: in.Page.ID, ArtifactID: in.TargetID,
		})
		return aerr
	})
}

// settleOldPath 处置旧路径的**访问面**状态（跨系统补偿：文件系统，事务边界之外）。
//
// 幂等 + 可重放：非重定向直接删链接（未激活时 Deactivate 返回 nil）；重定向重放
// 同一份 301 产物并重新激活（内容寻址，重复 Put 是 no-op）。失败时把错误交回调用方，
// 让回执保持 pending —— 下次启动重放同一段补齐，而 DB 那几步是幂等的。
func (s *Service) settleOldPath(newPath, oldPath string, withRedirect bool) error {
	if strings.TrimSpace(oldPath) == "" || oldPath == newPath {
		return nil
	}
	if !withRedirect {
		return s.deactivatePaths([]string{oldPath})
	}
	if s.store == nil || s.publication == nil {
		return nil
	}
	ra, rerr := pipeline.NewRedirectArtifact(newPath, 301)
	if rerr != nil {
		return rerr
	}
	loc, perr := s.store.PutRedirect(ra)
	if perr != nil {
		return perr
	}
	if aerr := s.publication.Activate(oldPath, loc); aerr != nil {
		return aerr
	}
	logger.Scene("page").With("oldPath", oldPath).With("newPath", newPath).
		Info("旧路径的 301 已重放（改 URL 恢复）")
	return nil
}

// activeArtifactHashAt 读路径当前激活产物的 hash（未激活 / 重定向 / 读不到时为空串）。
func (s *Service) activeArtifactHashAt(path string) string {
	if s == nil || s.publication == nil {
		return ""
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, "artifacts/")
}

// updateURLArtifactAt 读新路径上的激活产物并校验它确实是「按该路径编译」的产物。
//
// 改 URL 的产物 canonicalPath 就是新路径（canonicalPath 进 Manifest 并参与 hash），
// 所以「新路径上的链接指向一个 canonicalPath == 新路径的产物」足以证明切换发生过；
// 链接不存在、指向别处、是 301、或产物读不出来一律判定为未生效（保守：拿不准就
// 不做数据库写入，只把回执结案）。
func (s *Service) updateURLArtifactAt(path string) (*pipeline.Artifact, string, bool) {
	if s == nil || s.publication == nil || s.store == nil {
		return nil, "", false
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Kind != pipeline.PublicationPage || state.Locator == nil {
		return nil, "", false
	}
	art, aerr := s.store.GetArtifact(*state.Locator)
	if aerr != nil || art == nil || art.CanonicalPath != path {
		return nil, "", false
	}
	hash := strings.TrimPrefix(state.Locator.Key, "artifacts/")
	if hash == "" {
		hash = art.Hash
	}
	return art, hash, true
}

// ensureUpdateURLArtifactRow 取（必要时归档）改 URL 新产物的元数据行 id。
//
// 崩溃点可能落在「FS 已切到新路径」之后、归档之前，此时产物行还不存在。
// 源文档必须取**活动产物的冻结源文档**：改 URL 以它为构建基线（restoreKernelForUpdate），
// 用本页当前草稿归档会让日后按该产物回滚编译出不同 hash（ErrRollbackPathMismatch）。
func (s *Service) ensureUpdateURLArtifactRow(ctx context.Context, page *pagemodel.PageEntity, hash, fromArtifactID, lang string) (string, []pipeline.Dependency, error) {
	if strings.TrimSpace(hash) == "" {
		return "", nil, errors.New("改 URL 恢复：产物 hash 为空")
	}
	if existing, derr := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: hash}); derr == nil && existing != nil && existing.ID != "" {
		return existing.ID, nil, nil
	}
	source := json.RawMessage(nil)
	if id := strings.TrimSpace(fromArtifactID); id != "" {
		if old, oerr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: id}); oerr == nil && old != nil {
			source = old.SourceDocument
		}
	}
	if len(source) == 0 {
		source = page.DraftDocument
		logger.Scene("page").With("pageId", page.ID).With("hash", hash).
			Warn("改 URL 恢复：取不到活动产物的冻结源文档，回退当前草稿归档（该产物行的 source_document 可能与产物字节不符）")
	}
	rowID, deps, aerr := s.ensureArtifactRow(ctx, page, hash, source, lang)
	if aerr != nil {
		return "", nil, aerr
	}
	// 依赖集合交给调用方，与路由改名等步骤落进同一个事务（崩溃点若落在归档之前，
	// 依赖行同样缺失；写失败的后果是「内容改了该页不被精确标 stale」）。
	return rowID, deps, nil
}

// recoverSwitchActiveReceipt 补齐「发布：FS 已切、DB 没跟上」的回执。
//
// 判定证据：路径上的符号链接确实指向本次要激活的产物（hash 相等）。
// 证据不足一律走回滚分支（只结案、不动数据库）——判定错会写出错误的活跃指针。
func (s *Service) recoverSwitchActiveReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	actualHash := s.activeArtifactHashAt(item.Path)
	expectedHash, herr := s.model.ArtifactHashByID(ctx, item.ToArtifactID)
	if herr != nil {
		s.abortPublishReceipt(ctx, item.ID, "读取回执产物失败")
		return false, nil
	}
	if actualHash == "" || expectedHash == "" || actualHash != expectedHash {
		// 切换没发生（或指向的还是旧产物）：数据库保持原样即可，只结案。
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	if err := s.applyPublishActivation(ctx, publishActivationInput{
		Page: page, Lang: buildLang(item.Lang), Path: item.Path,
		ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash, OldPath: item.OldPath,
	}); err != nil {
		return false, err
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if serr := s.settleOldPath(item.Path, item.OldPath, false); serr != nil {
		return false, serr
	}
	logger.Scene("publication").With("pageId", page.ID).With("path", item.Path).
		Info("发布在崩溃前已生效，已补齐数据库状态")
	return true, nil
}

// recoverUpdateURLReceipt 补齐「改 URL：FS 已切到新路径、DB 没跟上」的回执。
func (s *Service) recoverUpdateURLReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	_, hash, ok := s.updateURLArtifactAt(item.Path)
	if !ok {
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			Warn("未结案的改 URL 回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次改 URL 的新产物")
		return false, nil
	}
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	lang := buildLang(item.Lang)
	rowID, deps, aerr := s.ensureUpdateURLArtifactRow(ctx, page, hash, item.FromArtifactID, lang)
	if aerr != nil {
		return false, aerr
	}
	rule := s.langURLRuleOf(ctx, page.ProjectID)
	// 语言集合在补写事务**之前**解析（审计 I18N-02 收尾）：与主链同一判据 —— 读不到就
	// 不做「只迁默认语言」的打折迁移，直接返回错误让回执保持 pending，等下一次收敛重放。
	// 此时访问面早已切换（崩溃点就在切换之后），所以宁可原地不动也不能迁一半：
	// 迁一半留下的是「新路径已激活、其余语言的保留路由还在旧路径」，没有任何入口能发现。
	routeLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		return false, lerr
	}
	if err := s.applyUpdateURL(ctx, updateURLApplyInput{
		Page: page, ArtifactRowID: rowID, Lang: lang,
		KernelNewPath:  item.Path,
		NewLogicalPath: rule.Strip(lang, item.Path),
		// 事务没提交时 draft_path 仍是旧逻辑路径；若事务其实已提交（只是回执未结案），
		// 这里取到的是新路径 —— 那时的改名是一个 no-op，重放安全。
		OldLogicalPath: page.DraftPath,
		OldKernelPath:  item.OldPath,
		WithRedirect:   item.Redirect,
		Deps:           deps,
		RouteLangs:     routeLangs,
	}); err != nil {
		return false, err
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if serr := s.settleOldPath(item.Path, item.OldPath, item.Redirect); serr != nil {
		return false, serr
	}
	logger.Scene("page").With("pageId", page.ID).With("path", item.Path).
		Info("改 URL 在崩溃前已生效，已补齐数据库状态")
	return true, nil
}

// recoverRollbackReceipt 补齐「回滚：FS 已切回历史产物、DB 没跟上」的回执。
func (s *Service) recoverRollbackReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	actualHash := s.activeArtifactHashAt(item.Path)
	expectedHash, herr := s.model.ArtifactHashByID(ctx, item.ToArtifactID)
	if herr != nil {
		s.abortPublishReceipt(ctx, item.ID, "读取回执产物失败")
		return false, nil
	}
	if actualHash == "" || expectedHash == "" || actualHash != expectedHash {
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的回滚回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向回滚目标产物")
		return false, nil
	}
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	if err := s.applyRollback(ctx, rollbackApplyInput{
		Page: page, Lang: buildLang(item.Lang), TargetPath: item.Path,
		TargetID: item.ToArtifactID, TargetHash: expectedHash, OldPath: item.OldPath,
	}); err != nil {
		return false, err
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if serr := s.settleOldPath(item.Path, item.OldPath, false); serr != nil {
		return false, serr
	}
	logger.Scene("page").With("pageId", page.ID).With("path", item.Path).
		Info("回滚在崩溃前已生效，已补齐数据库状态")
	return true, nil
}
