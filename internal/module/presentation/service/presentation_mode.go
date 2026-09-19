package presentationservice

// presentation_mode.go — 双轨的编译底稿判定、文档比较与两类回滚（迁移 282）。
//
// 回滚复用既有内核与既有表（不引入 page_revisions）：
//   · 产物指针回滚：publication.Activate + 实例指针切换（秒级，不重编译）；
//   · 快照级文档回滚：取 document_snapshots 的历史文档重发（重新编译，数据取最新）。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// instanceModePending 一次发布要落的模式变更（nil = 本次不改模式）。
//
// 为什么走参数而不是先单独写一列：模式与文档必须和快照/产物/指针同一事务
// （persistMultiLangArtifacts 的那一个 tx），否则中间态「文档已换、模式没换」
// 会被下一次模板更新按 template 模式重建回模板文档 —— 自定义凭空消失。
type instanceModePending struct {
	renderMode string
	document   json.RawMessage
}

// instanceDocumentFor 按渲染模式取编译底稿。
//
// template：原样返回模板（每次构建参与，模板更新可全局下发）；
// document：以覆盖文档为底稿（binding 照常解析 → 实体数据仍取最新）。
// 这里复用 render.go 的 withInstanceDocument（只换文档、保留模板身份字段），
// 不重复实现覆盖应用逻辑。
func instanceDocumentFor(inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) *contenttemplatecontract.ResolvedTemplate {
	if inst != nil && presentationmodel.IsDocumentMode(inst.RenderMode) {
		return withInstanceDocument(inst, tpl)
	}
	return tpl
}

// documentsEqual 文档归一化后比较：判定「结构是否真的变了」。
//
// encoding/json 序列化 map 时按键排序，因此同一结构的不同书写顺序结论一致；
// 解析失败按「不同」处理 —— 让后续编译给出明确的文档错误，而不是在这里静默放过
// （静默放过会表现为「保存后什么都没发生」，是本项目最难查的一类问题）。
func documentsEqual(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	an, aerr := json.Marshal(av)
	bn, berr := json.Marshal(bv)
	if aerr != nil || berr != nil {
		return false
	}
	return bytes.Equal(an, bn)
}

// RollbackArtifact 产物指针回滚：把实例的线上指针切回历史产物（秒级，不重新编译）。
//
// 与 page 侧 Rollback 同一语义（只切指针、不动暂存/文档指针）：presentation 的产物行
// 在自家表里（presentation_artifacts），所以按 hash 找行 + 校验文件在位 + 重新激活 URL，
// 最后在**一个事务**里落指针。
func (s *Service) RollbackArtifact(ctx context.Context, req *presentationdto.RollbackArtifactReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.TargetHash) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	art, aerr := s.m.GetArtifactByHash(ctx, inst.ID, req.TargetHash)
	if aerr != nil {
		return nil, errors.New(presentationenums.ErrRollbackTargetMiss)
	}
	// 文件不在位（误删 / 磁盘损坏）不能切指针：切了就是「路由指向不存在的文件」，
	// 比拒绝回滚严重得多（线上直接 404，且审计里看不出原因）。
	loc := pipeline.ArtifactLocator(art.ArtifactHash)
	if verr := s.store.VerifyArtifact(loc, art.ArtifactHash); verr != nil {
		logger.Scene("build").With("presentation_id", inst.ID).With("hash", art.ArtifactHash).
			Warn("产物回滚目标文件不在位，拒绝回滚")
		return nil, errors.New(presentationenums.ErrRollbackTargetMiss)
	}
	if aerr = s.publication.Activate(inst.URLPath, loc); aerr != nil {
		logger.Scene("build").With("presentation_id", inst.ID).With("path", inst.URLPath).
			Error(aerr, "产物回滚激活失败")
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrRollbackFailed, aerr)
	}
	now := time.Now().UTC()
	artID := art.ID
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		inst.ActiveArtifactID = &artID
		inst.StagedArtifactID = &artID
		inst.Stale = false
		inst.PublishedAt = &now
		inst.UpdatedAt = now
		return s.m.UpdateInstancePointersTx(tx, inst.ProjectID, inst)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrRollbackFailed, err)
	}
	return s.toResp(ctx, inst)
}

// RollbackDocument 快照级文档回滚：取历史快照的文档重发（重新编译，实体数据取最新）。
//
// 语义上等于「把这份历史文档作为该商品当前文档」→ 实例进入 document 模式
// （模板更新不再覆盖它），这与「内容回到那一版」的直觉一致；想回到跟随模板走
// ReapplyPreset（可反悔）。
func (s *Service) RollbackDocument(ctx context.Context, req *presentationdto.RollbackDocumentReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.SnapshotID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	snap, serr := s.m.GetSnapshot(ctx, req.SnapshotID)
	if serr != nil {
		return nil, errors.New(presentationenums.ErrRollbackTargetMiss)
	}
	// 归属校验：快照表没有 project_id，拿错实例的快照必须显式拒绝
	//（否则会把别的商品的内容发布到这个商品的 URL 上）。
	if snap.PresentationInstanceID != inst.ID {
		return nil, errors.New(presentationenums.ErrSnapshotMismatch)
	}
	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return nil, err
	}
	pending := *inst
	pending.OverrideDocument = snap.Document
	pending.RenderMode = presentationmodel.RenderModeDocument
	doc := instanceDocumentFor(&pending, tpl)

	mode := &instanceModePending{
		renderMode: presentationmodel.RenderModeDocument,
		document:   snap.Document,
	}
	if _, err = s.publishAllLangs(ctx, inst, doc, inst.URLPath, mode); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return s.toResp(ctx, inst)
}

// ListSnapshots 实例的历史快照清单（新→旧），供后台选择文档回滚目标。
//
// 先定位实例再查快照：快照表没有工程列，用实例的工程作用域兜住越权读。
// CountByTemplate 按绑定模板统计实例数（template 模式 / document 模式各多少）。
//
// 给后台的「编辑模板（影响 N 个商品）」提供影响面数字：模板编辑是全局动作，
// 按钮上不写清影响范围，用户只能凭猜 —— 猜错的代价是整站商品页一起变样。
// 只数未删除的实例，且按模式分开：转独立后的商品**不**受模板更新影响，
// 把它们的数量算进「影响面」会吓退用户（数字必须是真的）。
func (s *Service) CountByTemplate(ctx context.Context, req *presentationdto.CountByTemplateReq) (
	res *presentationdto.CountByTemplateResp, err error) {
	if req == nil || strings.TrimSpace(req.TemplateID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tplMode, docMode, err := s.m.CountInstancesByTemplate(ctx, projectID, req.TemplateID)
	if err != nil {
		return nil, err
	}
	return &presentationdto.CountByTemplateResp{
		TemplateMode: tplMode, DocumentMode: docMode, Total: tplMode + docMode,
	}, nil
}

func (s *Service) ListSnapshots(ctx context.Context, req *presentationdto.ListSnapshotsReq) (
	list []*presentationdto.SnapshotSummary, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	rows, err := s.m.ListSnapshots(ctx, inst.ID, req.Limit)
	if err != nil {
		return nil, err
	}
	list = make([]*presentationdto.SnapshotSummary, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		list = append(list, &presentationdto.SnapshotSummary{
			ID:                      r.ID,
			SourceTemplateVersionID: r.SourceTemplateVersionID,
			CreatedAt:               r.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return list, nil
}
