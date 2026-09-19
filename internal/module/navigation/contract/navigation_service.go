// Package navigationcontract 定义 navigation 模块对外契约（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离（不可复用 sys_menus 表）。
package navigationcontract

import (
	"context"

	navigationdto "go_wp/internal/module/navigation/dto"
)

// MenuStaleDispatcher 导航变更后的依赖失效派发端口（装配层注入）。
//
// 方向：navigation → 注入方（pipeline.Fanout 的适配器实现）。navigation 模块不认识
// page / presentation，也不 import pipeline —— 它只说「这个工程的这个导航位置变了」，
// 谁会因此失效由依赖扇出按依赖表反查决定（键构造见 pipeline.MenuKey，
// page 与 presentation 两侧用同一个构造函数产出）。
//
// 未注入时导航写操作照常成功，但**不派发任何失效**：已发布页面永远停在旧导航上，
// 而导航在页眉/页脚、全站可见，且没有任何报错。因此它在 wiring 清单里是必需端口，
// 装配期未注入即启动失败。
type MenuStaleDispatcher interface {
	// InvalidateMenu 该工程的该菜单位置（header / footer）发生变更（增 / 删 / 改 / 排序）。
	InvalidateMenu(ctx context.Context, projectID, kind string) error
	// InvalidateNavigation 该**具体菜单项**发生变化（改标题/来源/链接、增删子项、排序）。
	//
	// 与 InvalidateMenu 并列而不是合并：core.nav 有两种引用方式（按位置 / 按菜单项），
	// 两者的依赖键不同（menu:{project}:{kind} / navigation:{itemID}）。按位置引用时
	// 位置键已覆盖该项变化；只按项引用（页眉只放某一支）时位置键根本不会命中 ——
	// 合并成一条会让「只引用了某一支」的页面在菜单改动后永远停在旧链接。
	InvalidateNavigation(ctx context.Context, projectID, navigationID string) error
}

// SourceResolver 来源实体解析能力：菜单项来源非 custom 时，按来源实体取标题与 URL。
//
// 说明：这是 navigation 模块对「外部能力」的依赖声明，由顶层装配注入实现
// （page / content / presentation / block 契约的适配器，见 navigation/outbound/source）。
// 放在 contract 是为了让装配层只依赖契约，不 import 其他模块的 service。
type SourceResolver interface {
	// ResolveSource 按来源类型（page/article/product/category/block）+ 实体 ID
	// 返回菜单项标题与 URL。返回空串表示解析不到，调用方回退记录自身的 title/path。
	//
	// projectID 必须一起传：目标模块的「按 id 查询」把工程归属当作必填的越权防护
	// scope（page / block 的 Detail 都是），漏传只会拿到「参数缺失」。这一层是
	// **回退不报错**的，所以症状不是报错，而是菜单项永远显示记录里的占位标题与占位链接。
	ResolveSource(ctx context.Context, projectID, sourceType, sourceID string) (title, url string, err error)
	// Candidates 列出该工程可加入菜单的来源实体（按来源分组，空组已剔除）。
	// 管理页「按来源添加」消费；依赖模块不可用时对应分组为空，不报错。
	Candidates(ctx context.Context, projectID string) (groups []SourceGroup, err error)
}

// SourceGroup 来源候选分组（管理页「添加菜单项」按来源分组展示）。
type SourceGroup struct {
	// Type 来源类型：page/article/product/category/block。
	Type string
	// Title 分组标题（页面/文章/产品/分类/全局块）。
	Title string
	// Items 该分组下的候选实体。
	Items []SourceCandidate
}

// SourceCandidate 可加入菜单的来源实体。
type SourceCandidate struct {
	// ID 实体 ID（写入菜单项的 source_id）。
	ID string
	// Label 候选项显示名（页面路径/内容 slug）。
	Label string
	// Title 解析出的菜单标题（页面 SEO 标题/内容标题），添加菜单项时写入 title。
	Title string
	// URL 解析出的公开链接；为空表示暂无公开路径（管理页不提供添加）。
	URL string
}

// NavigationService 公开站点导航管理契约。
type NavigationService interface {
	// Create 新建导航项。
	Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error)
	// Update 更新导航项（仅更新传入的非空字段）。
	Update(ctx context.Context, req *navigationdto.UpdateReq) (res *navigationdto.NavigationResp, err error)
	// Get 按 ID 查询导航项。
	Get(ctx context.Context, req *navigationdto.GetReq) (res *navigationdto.NavigationResp, err error)
	// List 按工程（可选 kind）列出导航项，sort_order 升序。
	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)
	// Tree 按工程 + 位置（header/footer）返回导航项树（sort_order 升序）。
	// 构建期编译导航组件与管理页结构面板共用；树为空表示该位置暂无菜单项。
	// 来源非 custom 的项会经 SourceResolver 解析为来源实体的标题与 URL。
	Tree(ctx context.Context, projectID, kind string) (nodes []*navigationdto.NavigationNode, err error)
	// TreeByID 按**具体菜单项 id** 返回该菜单项及其子树（core.nav 按项引用时用）。
	//
	// 与 Tree 并列：Tree 是「整个位置的菜单」，本方法是「这一支」。找不到项时返回
	// ErrNotFound（构建期显式失败优先于静默产出空菜单）。
	TreeByID(ctx context.Context, projectID, navigationID string) (nodes []*navigationdto.NavigationNode, err error)
	// SetSourceResolver 注入来源实体解析器（顶层装配在依赖模块就绪后调用一次）。
	// 未注入时来源项退化为记录自身的 title/path（不报错，保持向后可用）。
	SetSourceResolver(r SourceResolver)
	// SourceGroups 返回该工程可加入菜单的来源候选（管理页「按来源添加」消费）。
	// 未注入解析器时返回空列表（页面退化为仅支持自定义链接）。
	SourceGroups(ctx context.Context, projectID string) (groups []SourceGroup, err error)
	// Delete 删除导航项。
	Delete(ctx context.Context, req *navigationdto.DeleteReq) (err error)
	// Render 返回该工程该 kind 的导航 HTML 片段。
	// 这是未来构建期把导航编译进静态 Artifact 的接口（本轮仅 CRUD + 存储 + 隔离，不做编译集成）。
	Render(ctx context.Context, projectID, kind string) (html string, err error)
}
