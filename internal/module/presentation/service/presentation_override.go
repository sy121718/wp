package presentationservice

// presentation_override.go — 商品独立文档的保存通道与「重新套用预设」（迁移 281/282，双轨）。
//
// 双轨语义（docs/04-C-instance-override.md）：
//   template（默认）：文档 = 绑定模板文档；模板更新可全局下发；
//   document：文档 = override_document；模板更新不影响它（UI 上叫「独立文档」）。
//
// 分叉判据是**文档结构真的变了**，不是「保存了商品」：改名称/价格/描述属于实体数据，
// 不改文档 → 不分叉；改了又改回去（归一化后与生效底稿相同）→ 同样不分叉。
// 判定必须由服务端做（前端拿不到"生效底稿"的权威字节），因此未确认时返回
// ErrDetachConfirmRequired，前端据此弹确认并带 confirmDetach 重试。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
)

// SaveOverrideDocument 保存商品独立文档并重建发布。
//
// 模式与文档的落库发生在 publishAllLangs → persistMultiLangArtifacts 的同一个事务里
// （与快照/产物/指针一体），不是先写一列再发布 —— 理由见 instanceModePending 注释。
func (s *Service) SaveOverrideDocument(ctx context.Context, req *presentationdto.SaveOverrideReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || len(req.Document) == 0 {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	if !json.Valid(req.Document) {
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

	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return nil, err
	}
	// 生效底稿：document 模式 = 该商品文档；template 模式 = 模板文档。
	effective := instanceDocumentFor(inst, tpl)

	// 结构未变 = 不分叉、不重建（幂等保存：反复保存同一份不改动也算成功）。
	if documentsEqual(json.RawMessage(req.Document), effective.Document) {
		return s.toResp(ctx, inst)
	}

	// 从「跟随模板」转入「独立文档」必须显式确认：这是用户唯一会真的丢掉
	// 「模板全局同步」能力的时刻，判据只有服务端算得准（见文件头注释）。
	if !presentationmodel.IsDocumentMode(inst.RenderMode) && !req.ConfirmDetach {
		return nil, errors.New(presentationenums.ErrDetachConfirmRequired)
	}

	// 编译底稿 = 本次提交的文档；binding 照常解析，实体数据取最新（数据不丢）。
	pending := *inst
	pending.OverrideDocument = json.RawMessage(req.Document)
	pending.RenderMode = presentationmodel.RenderModeDocument
	doc := instanceDocumentFor(&pending, tpl)

	mode := &instanceModePending{
		renderMode: presentationmodel.RenderModeDocument,
		document:   json.RawMessage(req.Document),
	}
	if _, err = s.publishAllLangs(ctx, inst, doc, inst.URLPath, mode); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return s.toResp(ctx, inst)
}

// ReapplyPreset 重新套用预设：放弃该商品独立文档，回到跟随模板（可反悔的另一半）。
//
// 与「转入独立」对称：模式与文档的清除同样落在发布事务里，避免
// 「模式已回 template、文档列还留着旧自定义」的矛盾行。
func (s *Service) ReapplyPreset(ctx context.Context, req *presentationdto.ReapplyPresetReq) (res *presentationdto.InstanceResp, err error) {
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
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	// switchTo 非空 = 同时换一套模板（换底稿）；模板身份切换由发布事务内的
	// UpdateInstanceTemplateTx 落库，这里只负责取到正确的文档。
	switchTo := strings.TrimSpace(req.TemplateID)
	tpl, err := s.resolveBoundTemplate(ctx, inst, switchTo)
	if err != nil {
		return nil, err
	}
	mode := &instanceModePending{renderMode: presentationmodel.RenderModeTemplate}
	if _, err = s.publishAllLangs(ctx, inst, tpl, inst.URLPath, mode); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return s.toResp(ctx, inst)
}

// ClearOverride 兼容旧契约：语义等同「重新套用预设」（放弃独立文档、回到跟随模板）。
//
// 保留它是因为商品详情页/模板面板既有调用点都用它；新代码请直接用 ReapplyPreset
// （名字与双轨语义一致，避免「清覆盖」被读成「清空内容」）。
func (s *Service) ClearOverride(ctx context.Context, req *presentationdto.ClearOverrideReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	return s.ReapplyPreset(ctx, &presentationdto.ReapplyPresetReq{
		InstanceID: req.InstanceID, ProjectID: req.ProjectID, TemplateID: req.TemplateID,
	})
}
