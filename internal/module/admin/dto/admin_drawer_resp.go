package admindto

import "go_wp/pkg/pagination"

// admin_drawer_resp.go — 后台抽屉**片段**的渲染视图（htmx 局部替换）。
//
// 与整页视图分开：片段只有一个槽位，整页是一屏数据；两者失败形态不同。
// **导出字段名就是模板契约**：internal/templates/admin/system/*_drawer.html 的 `.Field`。

// DrawerSavedResp 写动作成功后的抽屉片段（模板只渲染一个「已保存」标记）。
type DrawerSavedResp struct {
	Saved bool
}

// AdminListPageResp 管理员列表整页（/admin/administrators）。
//
// 字段名 = 模板键。降级渲染（取数失败）时行与总数给零值、Err 给归口文案 ——
// 页面照常渲染，而不是把用户丢在空白上。
type AdminListPageResp struct {
	Title       string `view:"title"`
	Menu        string `view:"menu"`
	Rows        any
	Total       int64
	FilterName  string
	FilterEmail string
	Err         string
	// ListQuery 批量/表单动作 action 上的筛选上下文。
	ListQuery string

	Pagination pagination.Page
}

// RuleListPageResp 数据规则列表页（/admin/datarules）。
type RuleListPageResp struct {
	Title        string `view:"title"`
	Menu         string `view:"menu"`
	Rows         any
	Total        int64
	Domains      any
	FilterDomain string
	Err          string
	ListQuery    string

	Pagination pagination.Page
}

// DepartmentListPageResp 部门列表页（/admin/departments）。
type DepartmentListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string

	Pagination pagination.Page
}

// MenuListPageResp 菜单列表页（/admin/menus）。
//
// 列表是**树状分页**：Rows 已按 DFS 前序摊平（顶级行 + 其子树），层级用 Depth 表达、
// 初始可见性用 Hidden/Expanded 表达 —— 摊平与可见性都归 service，模板只渲染。
type MenuListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string
	// SearchMode 让模板切到搜索态（给命中行标「匹配」、给祖先标「上级路径」）。
	SearchMode bool
	// PermChoices 新建抽屉的权限点候选（新建时没有任何已绑码）。
	PermChoices any

	Pagination pagination.Page
}

// RoleListPageResp 角色列表页（/admin/roles）。
type RoleListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string

	Pagination pagination.Page
}

// DataruleEditPageResp 数据规则编辑页（/admin/datarules/edit）。
type DataruleEditPageResp struct {
	Title   string `view:"title"`
	Menu    string `view:"menu"`
	Detail  any
	Domains any
	// Editor 条件编辑器上下文（字段/操作符下拉来自数据域声明）。
	Editor any
}

// PermissionEditFormResp 权限点编辑抽屉片段。
type PermissionEditFormResp struct {
	PermissionEdit any
}

// I18nEditFormResp 词条编辑抽屉片段。
type I18nEditFormResp struct {
	I18nEdit any
}

// 后台抽屉 / 片段视图（片段出口，键集小且固定）。
//
// 一律走 PrepareBareView：片段不套页壳，只需注入取词函数。
// 漏注入 t 时 Jet 静默输出空串（不报错、状态码 200、日志干净）。

// RoleDrawerResp 角色分配抽屉（管理员勾选角色）。
type RoleDrawerResp struct {
	Account any
	Rows    any
	// Err 抽屉片段里的错误槽（模板直接渲染文本，不取词 —— 槽位契约暂时还是字符串）。
	Err string
}

// MenuDrawerResp 菜单分配抽屉（管理员勾选菜单）。
type MenuDrawerResp struct {
	Account any
	Rows    any
	// Err 抽屉片段里的错误槽（模板直接渲染文本）。
	Err string
}

// MenuEditFormResp 菜单编辑抽屉片段。
type MenuEditFormResp struct {
	MenuEdit    any
	PermChoices any
	// Parents 上级菜单候选。候选取不到时**不设**（模板随之退化成 hidden parent_id）：
	// 缺了上级下拉只是少一项能力，而缺了 parent_id 提交就会把菜单搬到顶级 ——
	// 一个读失败不该换来一次静默的数据改动。
	Parents any
	// MenuEditErr 保存失败的提示（已取好词；空串 = 无错误）。
	MenuEditErr string
	// MenuEditEcho 保存失败时的回显值（模板用 .MenuEditEcho.title 这类索引读）。
	//
	// 交 map 而不是结构体：模板按**字段名字面量**索引，而字段集与表单控件一一对应 ——
	// 改成结构体会逼模板逐个改成点号取值，而这份模板同时被新建与失败回显两条路径用。
	MenuEditEcho map[string]string
	// MenuEditEchoCodes 权限码回显（多值，塞不进上面那个 map）。
	MenuEditEchoCodes []string
	// MenuEditParent 上级下拉的选中值（uint64 单给一份：MenuEditEcho 里是表单原始字符串，
	// 拿它跟候选 ID 比恒不相等，回显会让下拉跳回「（根菜单）」）。
	MenuEditParent uint64
}

// PermissionEditFailResp 权限点保存失败的抽屉片段（带回显值）。
type PermissionEditFailResp struct {
	PermissionEdit     any
	PermissionEditErr  string
	PermissionEditEcho any
}

// RolePermissionsResp 角色分权抽屉。
type RolePermissionsResp struct {
	Role any
	Rows any
	// Err 权限树片段里的错误槽（模板直接渲染文本）。
	Err string
}

// PermSavedResp 权限保存成功后的片段（只渲染一个「已保存」标记）。
type PermSavedResp struct {
	PermSaved bool
}

// DataruleEditorResp 数据规则条件编辑器片段。
type DataruleEditorResp struct {
	Editor any
}

// LoginPageResp 后台登录页（无会话，不走 Prepare 的权限集部分）。
type LoginPageResp struct {
	// Title 模板数据槽（layout 壳读 .title），槽位契约是字符串 → 已取好词。
	Title        string `view:"title"`
	CaptchaID    string `view:"captcha_id"`
	CaptchaImage string `view:"captcha_image"`
	// DevLogin debug 模式才显示一键登录入口（release 下路由压根不存在，显示了也是死链）。
	DevLogin bool `view:"DevLogin"`
}

// I18nListPageResp 文案词条列表页（/admin/i18n）。
//
// 失败档与成功档是**同一张模板**：必需键全部给值（Jet 缺键即整页中断），
// 标题仍是 i18n key（英文后台的标题不能变成中文），筛选取值照原样回带。
type I18nListPageResp struct {
	Title string `view:"title"`

	Entries      any
	I18nEditURLs []string

	Keyword    string
	LangFilter string
	CatFilter  string
	Categories []string
	// LangOptions 语言下拉的供数（字典只读口）。读失败时留空 ——
	// 页面仍可筛「全部语言」，只是少了按语言筛的能力，不该因此整页打不开。
	LangOptions any

	// LoadFailed / LoadError 装载失败档（文案已归口，原文只进日志）。
	LoadFailed bool
	LoadError  string

	Pagination pagination.Page
}
