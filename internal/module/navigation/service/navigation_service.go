// Package navigationservice 实现 navigation 模块业务用例（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。
//
// 本文件只放 Service 结构体、构造函数与全局白名单常量；各能力域用例按文件拆开：
// navigation_crud.go（增删改查）、navigation_tree.go（树装配与渲染）、
// navigation_validate.go（字段校验与归一化）、navigation_lock.go（乐观锁与冲突文案）、
// navigation_source.go（来源实体解析）、navigation_facing.go（跨模块错误文案出口），
// 以及既有的 navigation_scope.go（逐工程定位）与 navigation_stale.go（失效派发）。
package navigationservice

import (
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	projectcontract "go_wp/internal/module/project/contract"
)

// 导航类型白名单（与迁移 285 的 CHECK 约束对齐）。
//
// 桌面与移动端是**两个位置、两份数据**（WP 式：两个位置各绑一条菜单）：
// 两端要的菜单项、层级与交互本就不同，合并成"一套数据两种呈现"解决不了。
const (
	kindHeader       = "header"
	kindHeaderMobile = "header_mobile"
	kindFooter       = "footer"
	kindFooterMobile = "footer_mobile"
)

// 菜单项来源白名单（与迁移 054 的 CHECK 约束对齐）。
const (
	sourceCustom   = "custom"
	sourcePage     = "page"
	sourceArticle  = "article"
	sourceProduct  = "product"
	sourceCategory = "category"
	sourceBlock    = "block"
)

// 打开方式白名单（与迁移 054 的 CHECK 约束对齐）。
const (
	targetSelf  = "self"
	targetBlank = "blank"
)

// Service navigation 模块业务实现。
type Service struct {
	m *navigationmodel.Model
	// projects 站点工程契约（装配层注入）：只带 id 的入口要逐工程探测工程归属（DB-009）。
	// 注入的是契约而不是别的模块的 model：本模块只借「列出工程 id」这一个只读能力。
	projects projectcontract.ProjectService
	// sources 来源实体解析器（装配层注入；未注入时来源项退化为记录自身 title/path）。
	sources navigationcontract.SourceResolver
	// staleMenu 导航变更后的依赖失效派发端口（装配层注入；见 navigation_stale.go）。
	// navigation 不 import page / presentation / pipeline，只把「哪个工程哪个位置变了」
	// 交给注入方；未注入时写操作照常成功但不会让任何产物失效（必需端口，装配期自检拦）。
	staleMenu navigationcontract.MenuStaleDispatcher
}

// NewService 构造（model 与工程契约注入，不持有 *gorm.DB）。
//
// projects 必填：漏接装配时逐工程定位直接失败（不再回退到直读 projects 表）
// 的只读清单（见 navigation_scope.go 的 projectIDs），并记 warning。
func NewService(m *navigationmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{m: m, projects: projects}
}

// 编译期契约断言。
var _ navigationcontract.NavigationService = (*Service)(nil)
