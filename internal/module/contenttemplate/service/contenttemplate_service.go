// Package contenttemplateservice contenttemplate 模块业务实现（0-A2）。
package contenttemplateservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	projectcontract "go_wp/internal/module/project/contract"
)

// systemCreator 版本行 created_by 的占位（NOT NULL uuid 列）。
//
// 与 artifact 模块 defaultCreator 同一口径：uuid 列不接受空串，
// 无登录上下文的自动写入统一落零值 UUID（表示「系统写入」）。
const systemCreator = "00000000-0000-0000-0000-000000000000"

// Service contenttemplate 模块业务实现。
type Service struct {
	m        *contenttemplatemodel.Model
	project  projectcontract.ProjectService
	registry core.EntitySourceRegistry
}

// NewService 构造（model + project 契约 + 实体类型注册表注入，不持有 *gorm.DB）。
// project 用于解析模板所属工程（content_templates.project_id 为 NOT NULL 外键）；
// registry 提供「实体类型是否合法」的判据（取代对内容模块的直接依赖）。
func NewService(m *contenttemplatemodel.Model, project projectcontract.ProjectService,
	registry core.EntitySourceRegistry) *Service {
	return &Service{m: m, project: project, registry: registry}
}

// validEntityType 实体类型是否合法（注册表为 nil 时视为不合法，避免静默放行）。
func (s *Service) validEntityType(entityType string) bool {
	return s.registry != nil && s.registry.IsValidType(entityType)
}

// 编译期契约断言。
var _ contenttemplatecontract.ContentTemplateService = (*Service)(nil)

// Create 创建模板（初始 draft_version=1 并写入 version=1 快照）。
func (s *Service) Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || !s.validEntityType(req.EntityType) || req.Name == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 校验 DraftDocument 是合法 Page Document 并规范化为存储字节（含字段绑定的
	// 数据源白名单校验：越界绑定在保存时即拒绝，不等发布才炸）。
	doc, err := s.validateDocument(req.EntityType, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &contenttemplatemodel.TemplateEntity{
		ID: uuid.NewString(), ProjectID: projectID, Name: req.Name, EntityType: req.EntityType,
		DraftDocument: doc, DraftVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	// 模板行 + 首个不可变版本 + 当前版本指针回填，三者原子写入。
	// 分步写会在中途失败时留下「模板存在但无 LatestVersion」的死模板。
	verID := uuid.NewString()
	e.CurrentVersionID = &verID
	if err = s.m.CreateWithVersion(ctx, e, &contenttemplatemodel.VersionEntity{
		ID: verID, TemplateID: e.ID, Version: 1, Document: doc,
		SourceHash: hashDocument(doc), CreatedBy: systemCreator, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 更新模板（draft_version 递增并写新不可变版本）。
func (s *Service) Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	doc, err := s.validateDocument(e.EntityType, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	e.DraftDocument = doc
	e.DraftVersion++ // 单调递增（版本号即不可变快照序号）
	e.UpdatedAt = time.Now().UTC()
	verID := uuid.NewString()
	e.CurrentVersionID = &verID
	// 新版本行与草稿/指针更新必须原子：分步写时若 Save 失败，指针停在旧版本，
	// 新版本成为不可达孤儿，且 draft_version 已自增导致重试版本号错位。
	if err = s.m.SaveWithVersion(ctx, &contenttemplatemodel.VersionEntity{
		ID: verID, TemplateID: e.ID, Version: e.DraftVersion, Document: doc,
		SourceHash: hashDocument(doc), CreatedBy: systemCreator, CreatedAt: e.UpdatedAt,
	}, e); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *contenttemplatedto.GetReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

// List 按类型列表。
func (s *Service) List(ctx context.Context, req *contenttemplatedto.ListReq) (list []*contenttemplatedto.TemplateResp, err error) {
	if req == nil {
		req = &contenttemplatedto.ListReq{}
	}
	if req.EntityType != "" && !s.validEntityType(req.EntityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	rows, err := s.m.List(ctx, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*contenttemplatedto.TemplateResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

// ResolveTemplate 取 entityType 的当前激活模板版本（presentation 派生
// DocumentSnapshot 的唯一入口）。逻辑：
//  1. 取该类型最新的模板（updated_at 倒序首条），无则 ErrNotFound；
//  2. 取该模板最新版本（LatestVersion）的 document；
//  3. 组装 ResolvedTemplate{TemplateID, VersionID, Version, EntityType, Document}。
func (s *Service) ResolveTemplate(ctx context.Context, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !s.validEntityType(entityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	rows, err := s.m.List(ctx, entityType)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New(contenttemplateenums.ErrNotFound)
	}
	tpl := rows[0]
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return &contenttemplatecontract.ResolvedTemplate{
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		VersionID:    ver.ID,
		Version:      ver.Version,
		EntityType:   tpl.EntityType,
		Document:     ver.Document,
	}, nil
}

// ResolveTemplateByID 按模板 ID 解析其当前版本（issue #14：同一实体类型下可建多套
// 命名模板，发布/预览按 ID 显式指定用哪一套）。
//
// 与 ResolveTemplate 共享同一条版本解析口径（都取该模板的 LatestVersion）：
// 「模板」与「模板版本」是两层——换一套模板是换 TemplateID，
// 同一套模板改版式则产生新版本，两条路径都不需要调用方区分。
func (s *Service) ResolveTemplateByID(ctx context.Context, templateID string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if strings.TrimSpace(templateID) == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	tpl, err := s.m.Get(ctx, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return &contenttemplatecontract.ResolvedTemplate{
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		VersionID:    ver.ID,
		Version:      ver.Version,
		EntityType:   tpl.EntityType,
		Document:     ver.Document,
	}, nil
}

// resolveProjectID 解析模板所属工程：显式传入优先（校验存在），
// 否则经 project 契约取唯一工程；无工程或多工程时要求显式指定。
func (s *Service) resolveProjectID(ctx context.Context, explicit string) (string, error) {
	if id := strings.TrimSpace(explicit); id != "" {
		if s.project != nil {
			ok, err := s.project.Exists(ctx, id)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", errors.New(contenttemplateenums.ErrProjectNotFound)
			}
		}
		return id, nil
	}
	if s.project == nil {
		return "", errors.New(contenttemplateenums.ErrProjectRequired)
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(contenttemplateenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}

// hashDocument 版本文档内容哈希（content_template_versions.source_hash）。
func hashDocument(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

// validateDocument 解析并校验 Page Document，返回规范化存储字节。
//
// entityType 为模板的目标实体类型：文档内组件声明的字段绑定必须落在该数据源的
// 字段白名单内（issue #6，不变量 4），白名单来自实体类型注册表（装配期由各领域
// 模块注册），本模块不认识具体领域。
func (s *Service) validateDocument(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if err = builder.ValidatePage(page); err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, fmt.Errorf("%s: %w", contenttemplateenums.ErrFieldBindingInvalid, err)
	}
	// 重新编码保证存储 JSON 的规范格式，不接受散乱字节。
	doc, err := json.Marshal(page)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	return doc, nil
}

// toResp 实体 → 响应。
func toResp(e *contenttemplatemodel.TemplateEntity) *contenttemplatedto.TemplateResp {
	return &contenttemplatedto.TemplateResp{
		ID:            e.ID,
		Name:          e.Name,
		EntityType:    e.EntityType,
		DraftVersion:  e.DraftVersion,
		DraftDocument: e.DraftDocument,
		UpdatedAt:     e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
