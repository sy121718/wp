package pageservice

// page_artifact_rebuild.go — 产物文件丢失后的重建与激活面巡检（灾难恢复）。
//
// 背景：访问面（/site）直接服务 active 目录的文件系统状态，产物文件被误删或磁盘
// 损坏后 DB 侧毫无察觉 —— page_artifacts 行还在、payload_state 仍是 available、
// pages.active_artifact_id 仍指着它，表现是「线上 404 但后台一切正常」。
// 本文件提供两个只读/只重建的能力：按元数据重建单个产物、巡检全部悬空链接。

import (
	"context"
	"fmt"
	"strings"
	"time"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/pipeline"

	"go_wp/pkg/logger"
)

// RebuildArtifact 用 DB 冻结的 source_document 重新编译并落盘，恢复丢失的产物文件。
//
// 语义（灾难恢复，不是重新发布）：
//   - 只重建文件；不激活 URL、不改 DB 指针、不建重定向；
//   - 重建后**必须**校验 hash。产物 hash = SHA256(manifestJSON + "\n" + indexHTML)，
//     HTML 取决于「源文档 + 组件注册表 + 编译期依赖内容（CMS/主题/导航）」。
//     只有这些输入全部未变才得到同一个 hash；任一变化（典型是组件更新）会产出
//     另一个版本，此时返回不一致详情而**不冒充**旧产物，交由调用方决定走正常发布。
func (s *Service) RebuildArtifact(ctx context.Context, req *pagedto.RebuildArtifactReq) (res *pagedto.RebuildArtifactResp, err error) {
	if req == nil || strings.TrimSpace(req.ArtifactID) == "" {
		return nil, ErrInvalidParam
	}
	art, err := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: req.ArtifactID})
	if err != nil {
		return nil, err
	}
	if len(art.SourceDocument) == 0 {
		return nil, fmt.Errorf("产物 %s 缺少冻结源文档，无法重建", art.ID)
	}
	res = &pagedto.RebuildArtifactResp{
		ArtifactID:   art.ID,
		Lang:         art.Lang,
		Path:         art.CanonicalPath,
		ExpectedHash: art.ArtifactHash,
	}

	// 幂等短路：文件已在且校验通过，不重复编译。
	if verr := s.publisher.ArtifactExists(art.ArtifactHash); verr == nil {
		res.Restored, res.HashMatched, res.AlreadyThere = true, true, true
		res.ActualHash = art.ArtifactHash
		return res, nil
	}

	// 站点语言输入取**产物自己记录的那一份**（Manifest.siteLangs / siteDefaultLang，
	// 审计 I18N-01）：这里要原样复现这一份产物，凡是按当前配置重算的输入都会让它
	// 复现不出原字节 —— 而 hreflang 正是其中最隐蔽的一项（路径没变、链接变了）。
	// 取不到（该字段引入之前的存量产物）时为 nil，退回现场解析的既有行为。
	rebuilt, rerr := s.publisher.RestoreArtifact(ctx, pipeline.BuildInput{
		PageID:  art.PageID,
		Lang:    art.Lang,
		Path:    art.CanonicalPath,
		DocJSON: art.SourceDocument,
		Plan:    publicationPlanFromManifest(art.Manifest),
	})
	if rerr != nil {
		res.Reason = "重新编译失败: " + rerr.Error()
		return res, fmt.Errorf("重建产物失败: %w", rerr)
	}
	res.ActualHash = rebuilt.Hash
	if rebuilt.Hash != art.ArtifactHash {
		res.Reason = fmt.Sprintf(
			"重建产物 hash 与元数据不一致（期望 %s，实际 %s）：构建输入已变化 —— "+
				"典型原因是组件注册表更新，或编译期依赖内容（CMS/主题/导航）变动。"+
				"该产物无法原样恢复；如需上线新版本请走正常发布流程（build + publish）。",
			art.ArtifactHash, rebuilt.Hash)
		logger.Scene("page").With("artifactId", art.ID).
			With("expected", art.ArtifactHash).With("actual", rebuilt.Hash).
			Warn("产物重建 hash 不一致（构建输入已变化）")
		return res, nil
	}
	res.Restored, res.HashMatched = true, true
	logger.Scene("page").With("artifactId", art.ID).With("hash", rebuilt.Hash).
		Info("产物文件重建成功（hash 一致）")
	return res, nil
}

// AuditPublication 巡检激活面，返回所有悬空/异常链接。
func (s *Service) AuditPublication(ctx context.Context) (res *pagedto.PublicationAuditResp, err error) {
	issues, checked, aerr := s.publication.AuditActiveLinks()
	if aerr != nil {
		return nil, aerr
	}
	res = &pagedto.PublicationAuditResp{Checked: checked, Healthy: len(issues) == 0}
	res.Issues = make([]pagedto.PublicationIssue, 0, len(issues))
	for _, it := range issues {
		res.Issues = append(res.Issues, pagedto.PublicationIssue{URLPath: it.URLPath, Link: it.Link, Reason: it.Reason})
	}
	// 反向对账（IDX-015）：磁盘上的产物目录是否都有人认领。
	// 失败不影响正向结论 —— 正向异常是「线上已经 404」，必须先给出来。
	orphans, orphanChecked, oerr := s.auditOrphanArtifacts(ctx)
	if oerr != nil {
		logger.Scene("page").Error(oerr, "产物反向对账失败")
	} else {
		res.Orphans = orphans
		res.OrphanChecked = orphanChecked
	}
	if len(issues) > 0 {
		logger.Scene("page").With("count", len(issues)).Warn("激活面巡检发现异常链接")
	}
	return res, nil
}

// defaultArtifactRetentionDays 默认保留窗口：30 天内的产物一律不回收（回滚窗口）。
const defaultArtifactRetentionDays = 30

// GarbageCollectArtifacts 回收超出保留窗口且不再被任何指针引用的产物文件。
//
// 保护集合（任一命中即绝不回收）：
//   - pages.active_artifact_id / pages.staged_artifact_id
//   - page_publications.artifact_id（每语言激活真源）、page_stagings.artifact_id
//   - page_routes.artifact_id（访问面实际指向）
//
// 内容寻址去重：同一 hash 的物理文件可能被多条元数据行引用 —— 只有当没有任何其他
// available 行引用该 hash 时才删文件，否则只把本行标为 gc_pending（表示「想回收但
// 被共享占用」）。
//
// 可回滚性：删除的是物理文件，page_artifacts.source_document 始终保留 ——
// 需要时可经 POST /api/page/artifact/rebuild 重建（hash 一致则完美恢复）。
func (s *Service) GarbageCollectArtifacts(ctx context.Context, req *pagedto.GCArtifactsReq) (res *pagedto.GCArtifactsResp, err error) {
	retention, dryRun := defaultArtifactRetentionDays, true
	if req != nil {
		if req.RetentionDays > 0 {
			retention = req.RetentionDays
		}
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
	}
	before := time.Now().UTC().AddDate(0, 0, -retention)
	res = &pagedto.GCArtifactsResp{RetentionDays: retention, DryRun: dryRun}

	// 共享内容对象（content_objects）的孤儿回收（IDX-016）挂在 defer 上，而不是写在函数末尾：
	// 产物 GC 有多处提前返回（保护集合为空、候选查询失败），写在末尾会被那些路径整段跳过 ——
	// 而内容对象回收自有一套完整的引用判定（闭包投影 + 产物负载状态），不依赖产物的保护集合，
	// 没有理由跟着一起不跑。defer 覆盖全部退出路径，且仍发生在产物删除之后。
	defer func() {
		if res == nil {
			return
		}
		objRes, oerr := s.collectOrphanContentObjects(ctx, retention, dryRun)
		if oerr != nil {
			// 失败不影响产物侧结论：文件已经删了，回收内容对象失败只是表里多留一轮垃圾。
			logger.Scene("artifact").Error(oerr, "回收孤儿内容对象失败")
			return
		}
		if objRes == nil {
			return
		}
		res.OrphanObjects = objRes.Orphans
		res.ObjectsDeleted = objRes.Deleted
		res.ObjectsSkippedExternal = objRes.SkippedExtern
		res.ObjectsFailed = objRes.Failed
	}()

	protected, err := s.model.ListProtectedArtifactIDs(ctx)
	if err != nil {
		return nil, err
	}
	if s.routes != nil {
		refs, rerr := s.routes.ListReferencedArtifactIDs(ctx)
		if rerr != nil {
			return nil, rerr
		}
		protected = append(protected, refs...)
	}
	if len(protected) == 0 {
		// 保护集合为空：查询异常或系统尚未发布任何内容 —— 宁可不回收也不误删。
		return res, nil
	}

	cands, err := s.artifacts.ListGCCandidates(ctx, before, protected)
	if err != nil {
		return nil, err
	}
	res.Scanned = len(cands)
	for _, c := range cands {
		item := pagedto.GCRecoveredArtifact{ID: c.ID, ArtifactHash: c.ArtifactHash, Lang: c.Lang}
		others, cerr := s.artifacts.CountOtherAvailableByHash(ctx, c.ArtifactHash, c.ID)
		if cerr != nil {
			item.Action, item.Reason = "skipped", "同 hash 引用检查失败: "+cerr.Error()
			res.Failed++
			res.Items = append(res.Items, item)
			continue
		}
		if others > 0 {
			item.Action = "kept_shared"
			item.Reason = fmt.Sprintf("同 hash 仍被 %d 条产物行引用，文件保留", others)
			res.SkippedShared++
			if !dryRun {
				_, _ = s.artifacts.MarkPayloadState(ctx, []string{c.ID}, artifactcontract.PayloadStateGCPending)
			}
			res.Items = append(res.Items, item)
			continue
		}
		if dryRun {
			item.Action = "would_delete"
			res.Items = append(res.Items, item)
			continue
		}
		if derr := s.store.DeleteArtifact(pipeline.ArtifactLocator(c.ArtifactHash), c.ArtifactHash); derr != nil {
			item.Action, item.Reason = "delete_failed", derr.Error()
			res.Failed++
			logger.Scene("artifact").With("id", c.ID).With("hash", c.ArtifactHash).Error(derr, "产物文件删除失败")
			res.Items = append(res.Items, item)
			continue
		}
		if _, merr := s.artifacts.MarkPayloadState(ctx, []string{c.ID}, artifactcontract.PayloadStateDeleted); merr != nil {
			item.Action, item.Reason = "state_failed", "文件已删但状态未更新: "+merr.Error()
			res.Failed++
		} else {
			item.Action = "deleted"
			res.Deleted++
		}
		res.Items = append(res.Items, item)
	}
	logger.Scene("artifact").With("scanned", res.Scanned).With("deleted", res.Deleted).
		With("skippedShared", res.SkippedShared).With("dryRun", dryRun).Info("产物回收完成")
	return res, nil
}

// collectOrphanContentObjects 调用 artifact 模块做内容对象孤儿回收。
//
// 保留窗口与 dryRun 都与产物 GC 同值：两个 GC 的语义必须一致，否则会出现
// 「产物 dryRun 预演、内容对象真删」这种把风险藏起来的组合。
func (s *Service) collectOrphanContentObjects(ctx context.Context, retention int, dryRun bool) (res *artifactcontract.ContentObjectGCResp, err error) {
	if s.artifacts == nil {
		return nil, nil
	}
	return s.artifacts.GarbageCollectContentObjects(ctx, &artifactcontract.ContentObjectGCReq{
		RetentionDays: retention, DryRun: &dryRun,
	})
}
