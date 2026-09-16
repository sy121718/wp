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
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
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
	if !json.Valid(req.DraftDocument) {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	// 合入激活主题快照（EDT-003：与手工 Page 保存同口径）。
	merged, err := pipeline.MergeActiveThemeIntoDocument(ctx, s.project, projectID, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	// 校验 DraftDocument 是合法 Page Document 并规范化为存储字节（含字段绑定的
	// 数据源白名单校验：越界绑定在保存时即拒绝，不等发布才炸）。
	doc, err := s.validateDocument(req.EntityType, merged)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	role := strings.TrimSpace(req.TemplateRole)
	if role == "" {
		role = contenttemplatemodel.TemplateRoleDetail
	}
	if !contenttemplatemodel.IsValidTemplateRole(role) {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e := &contenttemplatemodel.TemplateEntity{
		ID: uuid.NewString(), ProjectID: projectID, Name: req.Name, EntityType: req.EntityType,
		TemplateRole:  role,
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
	// 工程作用域（DB-009 第二批）：content_templates 带 FORCE 策略，
	// 按 id 取模板也必须带上工程，否则换非超级角色后静默「模板不存在」。
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	e, err := s.m.Get(ctx, projectID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	if !json.Valid(req.DraftDocument) {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	merged, err := pipeline.MergeActiveThemeIntoDocument(ctx, s.project, e.ProjectID, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	doc, err := s.validateDocument(e.EntityType, merged)
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
	if err = s.m.SaveWithVersion(ctx, e.ProjectID, &contenttemplatemodel.VersionEntity{
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
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.GetScoped(ctx, projectID, req.ID)
}

// GetScoped 在**显式工程作用域**内按 id 取模板（DB-009 第二批）。
//
// 给「手里已经有工程 id」的调用方用（如 presentation 构建链路）：不传工程时
// service 只能靠 resolveProjectID 取唯一工程，多工程下必须报参数错误 ——
// 与其让调用方撞上「需要显式指定工程」，不如在这里把 id 直接透下去。
func (s *Service) GetScoped(ctx context.Context, projectID, id string) (res *contenttemplatedto.TemplateResp, err error) {
	e, err := s.m.Get(ctx, projectID, id)
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
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	rows, err := s.m.List(ctx, projectID, req.EntityType)
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
//  1. 优先取 is_default=true 的模板；无默认时回落 update_time 最新一条并记 warn（EDT-014）；
//  2. 取该模板最新版本（LatestVersion）的 document；
//  3. 组装 ResolvedTemplate{TemplateID, VersionID, Version, EntityType, Document}。
func (s *Service) ResolveTemplate(ctx context.Context, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.ResolveTemplateScoped(ctx, projectID, entityType)
}

// ResolveTemplateScoped 在显式工程作用域内解析该类型的当前模板版本。
//
// 与 ResolveTemplate 的分工：后者没有工程参数，只能取「唯一工程」；
// 构建链路（presentation）手里本来就有工程 id，走这条不会在多工程部署下
// 撞上「需要显式指定工程」——而模板解析失败会让整次构建失败。
func (s *Service) ResolveTemplateScoped(ctx context.Context, projectID, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !s.validEntityType(entityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	// 只取详情角色（审计 EDT-004）：归档模板与详情模板可以同类型共存，
	// 不过滤就会把归档模板当成详情模板取用。
	return s.ResolveTemplateByRoleScoped(ctx, projectID, entityType, contenttemplatemodel.TemplateRoleDetail)
}

// ResolveTemplateByRole 按实体类型与角色解析模板（审计 EDT-004）。
//
// 归档型实例（分类页 / 标签页 / 品牌页）走这个入口取归档模板；没有配置时返回
// ErrNotFound，由调用方决定是「跳过」还是「报错」——不在这里替调用方做决定。
func (s *Service) ResolveTemplateByRole(ctx context.Context, entityType, role string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.ResolveTemplateByRoleScoped(ctx, projectID, entityType, role)
}

// ResolveTemplateByRoleScoped 在显式工程作用域内按实体类型与角色解析模板。
func (s *Service) ResolveTemplateByRoleScoped(ctx context.Context, projectID, entityType, role string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !s.validEntityType(entityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	if role != "" && !contenttemplatemodel.IsValidTemplateRole(role) {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	rows, err := s.m.ListByRole(ctx, projectID, entityType, role)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New(contenttemplateenums.ErrNotFound)
	}
	tpl := rows[0]
	if !tpl.IsDefault {
		logger.Scene("contenttemplate").With("entityType", entityType).With("templateId", tpl.ID).
			Warn("未标记默认模板，回落到最新更新的模板")
	}
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return s.resolvedTemplateFromVersion(tpl, ver)
}

// ResolveTemplateByID 按模板 ID 解析其当前版本（issue #14：同一实体类型下可建多套
// 命名模板，发布/预览按 ID 显式指定用哪一套）。
//
// 与 ResolveTemplate 共享同一条版本解析口径（都取该模板的 LatestVersion）：
// 「模板」与「模板版本」是两层——换一套模板是换 TemplateID，
// 同一套模板改版式则产生新版本，两条路径都不需要调用方区分。
func (s *Service) ResolveTemplateByID(ctx context.Context, templateID string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	projectID, err := s.resolveProjectID(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.ResolveTemplateByIDScoped(ctx, projectID, templateID)
}

// ResolveTemplateByIDScoped 在显式工程作用域内按模板 ID 解析其当前版本。
func (s *Service) ResolveTemplateByIDScoped(ctx context.Context, projectID, templateID string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if strings.TrimSpace(templateID) == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	tpl, err := s.m.Get(ctx, projectID, templateID)
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
	return s.resolvedTemplateFromVersion(tpl, ver)
}

// resolvedTemplateFromVersion 组装 ResolvedTemplate；发布取用前走严格校验（EDT-013）。
func (s *Service) resolvedTemplateFromVersion(tpl *contenttemplatemodel.TemplateEntity, ver *contenttemplatemodel.VersionEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
	doc, err := s.validateDocumentStrict(tpl.EntityType, ver.Document)
	if err != nil {
		return nil, err
	}
	return &contenttemplatecontract.ResolvedTemplate{
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		VersionID:    ver.ID,
		Version:      ver.Version,
		EntityType:   tpl.EntityType,
		Document:     doc,
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

// validateDocument 草稿保存：ValidatePageTolerant + 字段绑定白名单（EDT-013）。
func (s *Service) validateDocument(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, true)
}

// validateDocumentStrict 发布/解析口径：完整 ValidatePage，非法模板在取用前拒绝。
func (s *Service) validateDocumentStrict(entityType string, raw json.RawMessage) (json.RawMessage, error) {
	return s.validateDocumentMode(entityType, raw, false)
}

func (s *Service) validateDocumentMode(entityType string, raw json.RawMessage, tolerant bool) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if tolerant {
		if _, err = builder.ValidatePageTolerant(page); err != nil {
			return nil, errors.New(contenttemplateenums.ErrDataInvalid)
		}
	} else if err = builder.ValidatePage(page); err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, fmt.Errorf("%s: %w", contenttemplateenums.ErrFieldBindingInvalid, err)
	}
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
