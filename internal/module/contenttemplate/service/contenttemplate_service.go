// Package contenttemplateservice contenttemplate 模块业务实现（0-A2）。
package contenttemplateservice

// contenttemplate_service.go — 模块入口与跨能力域共享面：Service 结构体、构造函数、
// 依赖失效扇出、实体类型判据，以及被多个能力域共用的两个小转换（structureSlotLabel / toResp）。
//
// 用例按能力域拆分：
//   - contenttemplate_crud.go     模板增删改与生效切换（写用例）
//   - contenttemplate_query.go    读取入口（按 id / 按类型）与块引用反查
//   - contenttemplate_resolve.go  构建期模板解析（presentation 取用）
//   - contenttemplate_document.go 模板文档校验与版本哈希
//   - contenttemplate_scope.go    工程作用域解析（DB-009）
//   - contenttemplate_impact.go   模板引用反查（影响面提示与删除保护）

import (
	"context"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
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
	// invalidator 依赖失效扇出端口（编排层注入，可空）。
	//
	// 沿用 content 模块的同道范式：失效是写入的**后置副作用**，失败只记日志，
	// 不能反向让已经成功的模板保存报错。
	invalidator DependencyInvalidator
	// impact 引用反查端口（装配层注入，见 contenttemplate_impact.go；可空）。
	//
	// 影响面提示与删除保护共用它 —— 页面文档的 settings.structure 绑定写在 JSONB 里，
	// 本模块的表看不见，靠它把「谁在引用」拿回来。
	impact contenttemplatecontract.TemplateImpactPort
}

// NewService 构造（model + project 契约 + 实体类型注册表注入，不持有 *gorm.DB）。
// project 用于解析模板所属工程（content_templates.project_id 为 NOT NULL 外键）；
// registry 提供「实体类型是否合法」的判据（取代对内容模块的直接依赖）。
func NewService(m *contenttemplatemodel.Model, project projectcontract.ProjectService,
	registry core.EntitySourceRegistry) *Service {
	return &Service{m: m, project: project, registry: registry}
}

// DependencyInvalidator 依赖失效端口（消费者侧最窄接口，与 content 模块同一范式）。
type DependencyInvalidator interface {
	Invalidate(ctx context.Context, kind, key string)
}

// SetInvalidator 注入依赖失效端口（装配期调用；未注入时模板改动只落库、不触发重建）。
func (s *Service) SetInvalidator(inv DependencyInvalidator) { s.invalidator = inv }

// notifyTemplateChanged 模板产生新版本 / 切换生效后的失效扇出。
//
// 缺这条的现象：改了模板（页眉 / 详情结构），引用它的页面与实例**永远停在旧字节**，
// 日志里什么都没有 —— 这正是本批要修的那类静默失效。
func (s *Service) notifyTemplateChanged(ctx context.Context, templateID string) {
	if s == nil || s.invalidator == nil || strings.TrimSpace(templateID) == "" {
		return
	}
	k := pipeline.ContentTemplateKey(templateID)
	s.invalidator.Invalidate(ctx, k.Kind, k.Key)
}

// validEntityType 实体类型是否合法（注册表为 nil 时视为不合法，避免静默放行）。
//
// 结构模板类型（页眉 / 页脚）走独立白名单：它们不是内容实体、没有字段来源，
// 往注册表里塞假来源会给出"可以配字段绑定"的假许可。
func (s *Service) validEntityType(entityType string) bool {
	if contenttemplatemodel.IsStructureTemplateType(entityType) {
		return true
	}
	return s.registry != nil && s.registry.IsValidType(entityType)
}

// 编译期契约断言。
var _ contenttemplatecontract.ContentTemplateService = (*Service)(nil)

// structureSlotLabel 槽位名 → 面向运营的说法（CRUD 的删除拦截与 impact 的引用提示共用）。
func structureSlotLabel(slot string) string {
	switch slot {
	case builder.SlotHeader:
		return "页眉"
	case builder.SlotFooter:
		return "页脚"
	default:
		return "结构槽位 " + slot
	}
}

// toResp 实体 → 响应（各能力域共用；字段与顺序保持不变）。
func toResp(e *contenttemplatemodel.TemplateEntity) *contenttemplatedto.TemplateResp {
	return &contenttemplatedto.TemplateResp{
		ID:            e.ID,
		Name:          e.Name,
		EntityType:    e.EntityType,
		TemplateRole:  e.TemplateRole,
		IsDefault:     e.IsDefault,
		DraftVersion:  e.DraftVersion,
		DraftDocument: e.DraftDocument,
		UpdatedAt:     e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
