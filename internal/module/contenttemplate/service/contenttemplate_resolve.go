package contenttemplateservice

// contenttemplate_resolve.go — 构建期模板解析（presentation 派生 DocumentSnapshot 的入口）。
//
// 两条轴：按「实体类型 + 角色」解析生效模板（详情 / 归档分角色取用），或按模板 id 显式指定
// 用哪一套；每条轴都有「无工程参数（取唯一工程）」与「显式工程作用域」两个入口。
// 解析出的文档一律走严格校验（EDT-013）：非法模板在取用前拒绝，不等渲染期才炸。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	"go_wp/pkg/logger"
)

// ResolveTemplate 取 entityType 的当前激活模板版本（presentation 派生
// DocumentSnapshot 的唯一入口）。逻辑：
//  1. 优先取 is_default=true 的模板；无默认时回落 update_time 最新一条并记 warn（EDT-014）；
//  2. 取该模板最新版本（LatestVersion）的 document；
//  3. 组装 ResolvedTemplate{TemplateID, VersionID, Version, EntityType, Document}。
//
// 多工程部署下必须由调用方给出工程（DB-009 第三批）：**不**逐工程扇出 ——
// 「该类型的当前模板」只可能属于一个工程，扇出会得到多份互不一致的结果，
// 而调用方（构建链）没法从中挑一份。已有工程 id 的调用方走 ResolveTemplateScoped。
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
//
// 多工程部署下必须由调用方给出工程（DB-009 第三批）：同 ResolveTemplate，
// 扇出会得到多份结果而无法挑一份，所以这里保持显式失败，不做「不限工程」的兜底；
// 已持有工程 id 的调用方走 ResolveTemplateByRoleScoped。
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
	if strings.TrimSpace(templateID) == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	// 逐工程定位（DB-009 第三批）：模板 id 是主键，逐工程探测的结果唯一；
	// 已持有工程 id 的调用方（presentation 构建链）应走 ResolveTemplateByIDScoped。
	tpl, err := s.locateTemplate(ctx, templateID)
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
		TemplateRole: tpl.TemplateRole,
		Document:     doc,
	}, nil
}
