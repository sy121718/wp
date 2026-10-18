// detail_template_port.go — 商品后台「详情页模板」整页的跨模块只读端口。
//
// 消费者是商品模块的页面读模型（`service.ProductDetailTemplatePage`），实现方有**两个**模块：
// 内容模板模块（候选清单 / 当前默认模板）与自动发布模块（商品当前绑定的发布实例）。
// 两个模块合成一条端口，是因为这一屏的「读」是一个整体动作 —— 读完才能说出
// 「这套是不是当前绑定的 / 是不是默认的那套 / 那个实例发布在哪个路径」，
// 拆成两个端口让读模型自己拼，结论就散在读模型里了（而它是**页面**结论，不是各自的业务结论）。
//
// 与 contract/entity_templates.go 的分工：那条端口服务的是**卡片区**（只要 id + 名字，
// 读不到就降级为「没有候选」）；本条服务的是这一屏的主数据（要版本号与更新时间，
// 读不到就是整页装载失败）。同一个上游、两种宽度 —— 不合并是因为失败语义不同：
// 合起来会让卡片区的降级把整页拖成 500。
//
// 注入链与既有两条一致：装配 `SetDetailTemplateDeps(templateSvc, instancePort)`
// → inbound/http 构造适配器 → `productPagePortWiring.SetDetailTemplatePort`
// → 读模型字段。装配登记见 `internal/routers/wiring.go` 的 `portProductPageDetailTemplate`。
package productcontract

import (
	"context"
	"time"
)

// DetailTemplateCandidateView 一套命名模板在这一屏上的渲染原料。
//
// 只取这一屏展示的四个字段：模板的角色 / 文档 / 生效标记是内容模板模块自己的业务事实，
// 这一屏看不到也不该猜（`IsBound` / `IsDefault` 由本模块按「绑定实例 + 默认解析」算出来，
// 不复用对方的 IsDefault —— 那是 (工程, 类型) 作用域，这里要的是实例实际绑的那一套）。
type DetailTemplateCandidateView struct {
	ID           string
	Name         string
	DraftVersion int64
	UpdatedAt    time.Time
}

// BoundTemplateView 商品当前绑定的发布实例（读模型据此算 IsBound 与发布状态）。
type BoundTemplateView struct {
	// Exists 该实体是否已经有发布实例；为假时下面三个字段一律零值。
	Exists bool
	// TemplateID 实例绑定的模板 id。
	TemplateID string
	// Status 实例状态（未发布 / 已发布 / …，按 presentation 模块的取值）。
	Status string
	// URLPath 实例已激活的路径（未发布时为空）。
	URLPath string
}

// DetailTemplatePort 「详情页模板」整页的只读端口（两个上游模块的并集）。
//
// 三个方法都带**工程作用域**，参数位置紧随 ctx（与 contenttemplate 契约的
// ResolveTemplateScoped(ctx, projectID, entityType) 一致 —— 工程是作用域，不是实体参数）。
//
// 为什么必须有：本端口的两段下游（contenttemplate 的 List / ResolveTemplate、presentation 的
// GetByEntity）在多工程部署下都要显式工程，不带时它们回 ErrProjectRequired，症状是**整页 500**；
// 而调用方（读模型）手里本来就有解析好的工程 id（service.ProductDetailTemplatePage 的 selected），
// 漏传即缺陷。
type DetailTemplatePort interface {
	// DetailTemplateCandidates 本工程内该实体类型的模板清单。失败 = 这一屏的主数据没读到
	// （整页装载失败，不是「暂时没有候选」—— 与 EntityTemplatePort 的降级语义相反）。
	DetailTemplateCandidates(ctx context.Context, projectID, entityType string) (out []DetailTemplateCandidateView, err error)
	// DefaultDetailTemplate 本工程内该实体类型当前解析到的默认模板 id（未解析到时返回空串，不报错）。
	DefaultDetailTemplate(ctx context.Context, projectID, entityType string) (templateID string, err error)
	// BoundDetailTemplate 该实体当前绑定的发布实例。
	//
	// 读不到按「没有实例」处理（Exists=false），不报错：这一屏对实例的依赖是**展示**
	// （显示还没发布 / 发布在哪个路径），不是前置条件 —— 实例读不出来时页面仍要能
	// 让人去点「首次发布」。
	BoundDetailTemplate(ctx context.Context, projectID, entityType, entityID string) (view BoundTemplateView)
}
