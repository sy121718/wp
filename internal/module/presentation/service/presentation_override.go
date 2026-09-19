package presentationservice

// presentation_override.go — 实例级文档覆盖的保存/清除通道（迁移 281，
// docs/04-C-instance-override.md，方案 B：商品级可视化自定义）。
//
// 保存 = 落 override_document + 按覆盖文档重建发布。两步不包进同一个事务：
// 重建链（publishAllLangs → 产物落盘 → 激活）含外部系统边界（ArtifactStore 落盘、
// PublicationStore 激活），本就不在 DB 事务内（与 page 侧发布同一取舍）；中间态
// 「覆盖已保存、产物仍是旧文档」是一致状态 —— 下次任何重建都会按覆盖文档编译，
// 不需要补偿。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"

	"gorm.io/gorm"
)

// SaveOverrideDocument 保存实例覆盖文档并重建发布（workbench 实例模式保存）。
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
	// 实例级互斥：与重建同锁，避免保存与并发重建交错出交错指针。
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.UpdateInstanceOverrideTx(tx, projectID, inst.ID, req.Document, now)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	inst.OverrideDocument = json.RawMessage(req.Document)

	// 按覆盖文档重建发布（persistBuild 内部自带四表事务）。
	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return nil, err
	}
	tpl = withInstanceDocument(inst, tpl)
	if _, err = s.publishAllLangs(ctx, inst, tpl, inst.URLPath); err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	return s.toResp(ctx, inst)
}

// ClearOverride 清除实例覆盖文档并按（新）模板重建：放弃自定义 / 换底稿。
func (s *Service) ClearOverride(ctx context.Context, req *presentationdto.ClearOverrideReq) (res *presentationdto.InstanceResp, err error) {
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
	// 锁语义：这里**不**预先取实例锁 —— 清除本身是单条事务写，随后的
	// rebuildInstance 自带实例级互斥；先取锁再进 rebuildInstance 会自死锁
	//（sync.Mutex 不可重入，本会话实测触发过测试挂死）。
	// TemplateID 非空 = 换底稿：两件事一体成型（清覆盖 + 切模板）同事务落库，
	// 随后 rebuildInstance 以新模板编译（inst 内存态同步改，避免重查）。
	switchTo := strings.TrimSpace(req.TemplateID)
	now := time.Now()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.ClearInstanceOverrideTx(tx, projectID, inst.ID, switchTo, now)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	inst.OverrideDocument = nil
	if switchTo != "" {
		inst.TemplateID = switchTo
	}

	tpl, err := s.resolveBoundTemplate(ctx, inst, switchTo)
	if err != nil {
		return nil, err
	}
	return s.rebuildInstance(ctx, inst, tpl)
}

// 编译期防护：model 实体字段必须存在（迁移 281 落列前该文件无法编译过），
// 占位引用避免 presentationmodel 仅在注释中出现。
var _ = presentationmodel.InstanceEntity{}
