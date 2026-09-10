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

	rebuilt, rerr := s.publisher.RestoreArtifact(ctx, pipeline.BuildInput{
		PageID:  art.PageID,
		Lang:    art.Lang,
		Path:    art.CanonicalPath,
		DocJSON: art.SourceDocument,
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
	if len(issues) > 0 {
		logger.Scene("page").With("count", len(issues)).Warn("激活面巡检发现异常链接")
	}
	return res, nil
}
