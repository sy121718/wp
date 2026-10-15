// entity_templates.go — 商品后台页「详情模板清单 + 默认模板」的只读端口。
//
// 消费者是商品模块的页面读模型（`service.ProductNewPage` 的右侧模板卡片区），
// 实现方是内容模板模块。与 WarehouseOptionsPort 同一取舍：**只声明读方法** ——
// contenttemplate 的 ContentTemplateService 上还有建模板 / 改模板 / 激活版本这类写能力，
// 渲染一张卡片不需要其中任何一条。
//
// 返回的是本模块的渲染视图（dto 的重导出），不是对方的 dto。
//
// 注入链同样是两跳：装配 `SetDetailTemplateDeps(templatePort)` →
// `inbound/http/product_detail_template.go` 转交 `service.SetEntityTemplatePort`
// → 读模型字段。装配登记见 `internal/routers/wiring.go` 的
// `portProductPageTemplates`。
package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// EntityTemplateOptionView 模板候选的渲染视图（dto 的重导出）。
type EntityTemplateOptionView = productdto.EntityTemplateOptionView

// EntityTemplatePort 某实体类型的模板清单与当前模板的只读端口（由内容模板模块实现）。
//
// 两个方法的失败语义在调用方：清单读不到 = 「这一页的模板卡片区暂时没有候选」，
// 不构成整页失败（右侧卡片是**可选辅助**，见 docs/rules/template-boundary.md
// §整页 GET 的失败出口）；因此实现方按普通 error 返回即可，调用方决定降级还是失败。
type EntityTemplatePort interface {
	// 两个方法的 projectID 都紧随 ctx（与 contenttemplate 的
	// ResolveTemplateScoped(ctx, projectID, entityType) 同族）：内容模板表带 FORCE 工程策略，
	// 不传工程时实现方只能回落「唯一工程」，多工程部署下读口回 ErrProjectRequired，
	// 调用方（商品新建页读模型）手里本来就有工程 id —— 不传这一个参数就够不着本工程的模板。
	// 空串语义与其余读口一致：= 唯一工程。

	// EntityTemplates 该实体类型的模板清单（entityType 见 EntityTypeProduct）。
	EntityTemplates(ctx context.Context, projectID, entityType string) (out []EntityTemplateOptionView, err error)
	// DefaultEntityTemplate 该实体类型的当前模板 id（未绑定 / 无模板时返回空串）。
	DefaultEntityTemplate(ctx context.Context, projectID, entityType string) (templateID string, err error)
}
