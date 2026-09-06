// Package presentationservice presentation 模块业务实现（0-A2）。
//
// 自动发布：内容实体 + ContentTemplate → 派生快照 → 同一 Publish Compiler
// → ArtifactStore → PublicationStore（与手工 Page 共享管线，docs/02 §3）。
//
// MVP 取舍（开发阶段）：DocumentSnapshot 保存模板 AST（含 binding 节点），
// 编译时经 ContentResolver 解析为字面量——而非领域模型 §3.3 的「快照已
// 解析为字面量」。收益：复用 builder.WithContentResolver 注入，避免遍历
// AST 预解析的组件耦合；实体更新后 Rebuild 重编译即得新数据。
package presentationservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Service presentation 模块业务实现。
type Service struct {
	m           *presentationmodel.Model
	templates   contenttemplatecontract.ContentTemplateService
	content     contentcontract.ContentService
	store       *pipeline.LocalStore
	publication *pipeline.LocalPublicationStore
}

// NewService 构造（依赖 contenttemplate/content 契约 + pipeline 内核）。
func NewService(m *presentationmodel.Model,
	templates contenttemplatecontract.ContentTemplateService,
	content contentcontract.ContentService) *Service {
	return &Service{
		m:           m,
		templates:   templates,
		content:     content,
		store:       &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()},
		publication: &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()},
	}
}

// 编译期契约断言。
var _ presentationcontract.PresentationService = (*Service)(nil)

// CreateInstance 创建自动发布实例：解析模板 → 编译 → 发布 → 记快照。
func (s *Service) CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" || req.URLPath == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 同实体已存在实例 → 视为幂等（返回已有）。
	if existing, gerr := s.m.GetInstanceByEntity(ctx, req.EntityType, req.EntityID); gerr == nil {
		return s.toResp(ctx, existing), nil
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	// 解析模板版本 + 实体解析器。
	tpl, err := s.templates.ResolveTemplate(ctx, req.EntityType)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNoTemplate)
	}
	// 编译发布。
	hash, html, err := s.buildAndPublish(ctx, req.EntityType, req.EntityID, req.URLPath, tpl.Document)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	// 记实例 + 快照。
	now := time.Now().UTC()
	inst := &presentationmodel.InstanceEntity{
		ID: uuid.NewString(), EntityType: req.EntityType, EntityID: req.EntityID,
		URLPath: req.URLPath, Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateInstance(ctx, inst); err != nil {
		return nil, err
	}
	snapID := uuid.NewString()
	snap := &presentationmodel.SnapshotEntity{
		ID: snapID, PresentationInstanceID: inst.ID,
		SourceTemplateVersionID: tpl.VersionID, SourceEntityRevision: 0,
		Document: tpl.Document, CreatedAt: now,
	}
	if err = s.m.CreateSnapshot(ctx, snap); err != nil {
		return nil, err
	}
	inst.CurrentSnapshotID = &snapID
	inst.ArtifactHash = &hash
	if err = s.m.UpdateInstance(ctx, inst); err != nil {
		return nil, err
	}
	_ = html // 产物已入 ArtifactStore，快照持模板 AST
	return s.toResp(ctx, inst), nil
}

// Rebuild 实体数据更新后重建：重解析模板+实体 → 重新编译发布。
func (s *Service) Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 反查实例（entity_id 匹配）。
	inst, err := s.findByEntityID(ctx, req.EntityID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	tpl, err := s.templates.ResolveTemplate(ctx, inst.EntityType)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNoTemplate)
	}
	hash, _, err := s.buildAndPublish(ctx, inst.EntityType, inst.EntityID, inst.URLPath, tpl.Document)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
	}
	// 新快照（revision 由 entity 决定，此处保守置 0 占位，Rebuild 语义为重编译）。
	now := time.Now().UTC()
	snap := &presentationmodel.SnapshotEntity{
		ID: uuid.NewString(), PresentationInstanceID: inst.ID,
		SourceTemplateVersionID: tpl.VersionID, SourceEntityRevision: 0,
		Document: tpl.Document, CreatedAt: now,
	}
	if err = s.m.CreateSnapshot(ctx, snap); err != nil {
		return nil, err
	}
	inst.CurrentSnapshotID = &snap.ID
	inst.ArtifactHash = &hash
	inst.UpdatedAt = now
	if err = s.m.UpdateInstance(ctx, inst); err != nil {
		return nil, err
	}
	return s.toResp(ctx, inst), nil
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstance(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst), nil
}

// List 按类型列表。
func (s *Service) List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error) {
	if req == nil {
		req = &presentationdto.ListReq{}
	}
	rows, err := s.m.ListInstances(ctx, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*presentationdto.InstanceResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.toResp(ctx, r))
	}
	return out, nil
}

// Delete 删除实例（级联删快照 + 反激活 URL）。
func (s *Service) Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstance(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(presentationenums.ErrNotFound)
		}
		return err
	}
	_ = s.publication.Deactivate(inst.URLPath)
	return s.m.DeleteInstance(ctx, req.ID)
}

// buildAndPublish 编译模板 AST（经实体 resolver）→ 产物 → 激活 URL。
func (s *Service) buildAndPublish(ctx context.Context, entityType, entityID, urlPath string, templateDoc []byte) (hash string, html []byte, err error) {
	page, err := builder.ParsePage(templateDoc)
	if err != nil {
		return "", nil, err
	}
	resolver, err := s.content.ResolverFor(ctx, entityType, entityID)
	if err != nil {
		return "", nil, err
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return "", nil, err
	}
	compiled, err := builder.Compile(page,
		builder.WithComponentSet(set),
		builder.WithContentResolver(resolver))
	if err != nil {
		return "", nil, err
	}
	html = []byte(builder.RenderDocument(compiled))
	artifact, err := pipeline.NewArtifact(html, &pipeline.Manifest{
		ManifestSchemaVersion:     1,
		PageDocumentSchemaVersion: 1,
		SourceID:                  entityID,
		SourceType:                "presentation",
		CanonicalPath:             urlPath,
	})
	if err != nil {
		return "", nil, err
	}
	loc, err := s.store.PutArtifact(artifact)
	if err != nil {
		return "", nil, err
	}
	if err = s.publication.Activate(urlPath, loc); err != nil {
		return "", nil, err
	}
	return artifact.Hash, html, nil
}

// findByEntityID 按内容实体 ID 反查实例。
func (s *Service) findByEntityID(ctx context.Context, entityID string) (*presentationmodel.InstanceEntity, error) {
	rows, err := s.m.ListInstances(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.EntityID == entityID {
			return r, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// toResp 实体 → 响应。
func (s *Service) toResp(ctx context.Context, e *presentationmodel.InstanceEntity) *presentationdto.InstanceResp {
	resp := &presentationdto.InstanceResp{
		ID: e.ID, EntityType: e.EntityType, EntityID: e.EntityID,
		URLPath: e.URLPath, Status: e.Status,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
	if e.ArtifactHash != nil {
		resp.ArtifactHash = *e.ArtifactHash
	}
	if e.CurrentSnapshotID != nil {
		resp.SnapshotID = *e.CurrentSnapshotID
	}
	return resp
}
