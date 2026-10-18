// Package navigationenums 统一管理 navigation 模块响应消息（0-C）。
package navigationenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

// 成功消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess" // 导航项创建成功
	MsgUpdateSuccess = "MsgUpdateSuccess" // 导航项更新成功
	MsgListSuccess   = "MsgListSuccess"   // 导航列表获取成功
	MsgDetailSuccess = "MsgDetailSuccess" // 导航详情获取成功
	MsgDeleteSuccess = "MsgDeleteSuccess" // 导航项删除成功
)

// 错误消息。
const (
	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 导航项不存在
	ErrInvalidKind  = "ErrInvalidKind"  // 非法的导航类型，仅支持 header 或 footer
	ErrPathTaken    = "ErrPathTaken"    // 同工程同类型下已存在相同路径的导航项
	// ErrInvalidSource 非法的菜单项来源（或非 custom 来源未指定来源实体）。
	ErrInvalidSource = "ErrInvalidSource" // 非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体
	// ErrInvalidTarget 非法的打开方式。
	ErrInvalidTarget = "ErrInvalidTarget" // 非法的打开方式，仅支持 self 或 blank
	// ErrInvalidParent 非法的父引用（自引用 / 成环 / 跨工程 / 跨类型 / 父项不存在）。
	ErrInvalidParent = "ErrInvalidParent" // 非法的父引用：父项必须存在、同工程、同类型，且不得形成环
	// ErrProjectRequired 缺可作用域的工程（DB-009）：只带 id 的入口要逐工程定位归属，
	// 而工程清单为空或读不到。显式失败而不是静默返回「找不到」——后者会把
	// 「读不到工程表」伪装成「导航项不存在」。
	ErrProjectRequired = "ErrProjectRequired" // 缺少可作用域的工程，无法定位导航项的工程归属
	// ErrStaleVersion 乐观锁冲突：调用方手上的 update_time 与库内当前值不一致。
	//
	// 后台菜单页与工作台检查器是同一份数据的两个入口，两处同时改此前是静默覆盖
	//（last-write-wins）。冲突一律**打回给人**：不自动合并、不加后缀、不丢弃其中一方。
	// 文案后接「: <菜单项标题>」的定位信息（定位走 pkg/facing 的 Tail：只翻 key、定位原样
	// 拼回，所以本词条内不带 %s 占位符）。写侧一律 enums.ErrDetail。
	ErrStaleVersion = "ErrStaleVersion"
	// ErrSortTied 同级排序值并列（重复），交换两行取值不产生任何位移。
	//
	// sort_order 上没有唯一约束（「同级唯一」只是应用层不变量：Create 用 MaxSortOrder+1 追加），
	// 外部脚本按 /api/navigation/update 裸置位或手工写库都能造出并列；此时交换两行的取值
	// 等于什么都没换（并列由 id ASC 决胜），所以显式**不位移**并回这句，而不是回一个
	// 「已完成」把没发生的事说成发生了。零写零失效：不进事务、不派发 stale。
	ErrSortTied = "ErrSortTied"
)

// errFallbacks 错误兜底（对外文案的中文那一半）：与常量同处声明，出口只调一次 pkg/facing.FactOf。
//
// 为什么不再有白名单（`var NavigationFacingMessages []string` + 逐条 switch + 两个拆分函数）：
// 那是第二份真相 —— service 换措辞、或某条错误改由别的模块提供时白名单静默失配，
// 症状是把业务文案吞成归口、或把内部错误当文案透出，两边都不报错。
// 现在「能不能对外说」由 facing.Fact 的形状判定，「说什么」由这里的一句话给。
//
// 取值与 sys_i18n 的 zh-CN 词条**逐字一致**（迁移 058 / 290 / 403 / 429 / 602，含同 key 被别的
// 模块先登记的那几条）：兜底只在词条缺席时可见，与词条不一致会让人以为「词条坏了」。
var errFallbacks = map[string]string{
	ErrInvalidParam:    "请求参数无效",
	ErrNotFound:        "请求资源不存在",
	ErrInvalidKind:     "不支持的页面类型",
	ErrPathTaken:       "同工程同类型下已存在相同路径的导航项",
	ErrInvalidSource:   "非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体",
	ErrInvalidTarget:   "非法的打开方式，仅支持 self 或 blank",
	ErrInvalidParent:   "父级菜单不合法：父项必须存在、与本项属于同一工程与同一类型，且不能形成循环",
	ErrProjectRequired: "请先选择要操作的站点工程",
	ErrStaleVersion:    "该菜单项已被其他窗口修改，请刷新后重试",
	ErrSortTied:        "同一级里存在重复的排序值，本次没有产生位移；请先把这一级的顺序理顺",
}

// Err 声明一条自带对外文案的导航业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// ErrDetail 把业务错误与**定位数据**拼成一条（key + 明细）。
//
// 明细是给操作者用的数据（冲突的那一条菜单项标题），由本模块自己构造，不含表名 / SQLSTATE。
// 分隔符必须用 ASCII「: 」：pkg/facing 的复合形态判据只认这一种，写成全角「：」会让整条
// 过不了判据、被当成内部错误打成 500（页面与接口同时失去「哪一条撞了」这句定位）。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key（文案真源在迁移 451），命名按「去掉模块子域前缀
// （`admin.navigation_translations.` / `admin.navigations.`）后的语义路径」：
// 包名 navigationenums 已经给出模块上下文，再带一遍子域只是噪音。
//
// 为什么与上面那批 `MsgXxx = "MsgXxx"` 分开成组：老式形态的值就是常量名本身
// （`sys_i18n` 里存同名 key），与点分 key 混在同一段里会让人以为值也是 `MsgXxx`。
// 两组不可互换：老式常量走「常量名即资源 key」，新式常量走 sys_i18n 的 item_key。
//
// 中文兜底**不在这里** —— 兜底留在调用点（词条缺失时的回落），见各 inbound/http 文件。
const (
	// 译文工作台的页面标题（页面本体与回跳链接共用同一条词条）。
	TranslationHeading = "admin.navigation_translations.heading" // 导航译文

	// 译文工作台的保存结果文案。
	Saved     = "admin.navigation_translations.saved"     // 已保存 %d 条译文；已标记受影响页面待重建（下次构建生效）
	SavedNone = "admin.navigation_translations.savedNone" // 没有需要写入的变化

	// 译文工作台的行级校验与整体失败文案（进页面错误槽）。
	ErrRowCountMismatch   = "admin.navigation_translations.err.rowCountMismatch"   // 提交的行数不一致，请刷新后重试
	ErrStorageUnavailable = "admin.navigation_translations.err.storageUnavailable" // 译文存储不可用
	ErrContextInvalid     = "admin.navigation_translations.err.contextInvalid"     // 语境非法：导航标签只接受 %s
	ErrSourceChanged      = "admin.navigation_translations.err.sourceChanged"      // 菜单文字已变化，请刷新后重试
	ErrNotTranslatable    = "admin.navigation_translations.err.notTranslatable"    // %s：该菜单文字不参与翻译（纯数字或纯符号）
	ErrSaveFailed         = "admin.navigation_translations.err.saveFailed"         // 保存失败：%s
	ErrProjectListFailed  = "admin.navigation_translations.err.projectListFailed"  // 读取工程列表失败，请稍后重试

	// 导航菜单列表页与写路径共用的事实文案（页面标题被回跳链接复用）。
	Heading              = "admin.navigations.heading"               // 导航菜单
	ActionDone           = "admin.navigations.actionDone"            // 操作已完成
	NoPublicPath         = "admin.navigations.no_public_path"        // （暂无公开路径，先发布后再添加）
	NoticeNoSourcePicked = "admin.navigations.notice.noSourcePicked" // 请至少勾选一项要加入菜单的内容。

	// 归口文案 key：**未归类失败**（SQL / 表名 / 约束名 / 文件路径）在出口处取这一条，
	// 业务失败一律走 errFallbacks。它不是错误哨兵 —— 不要在 service 里拿它构造 error
	// （那会把内部故障伪装成业务拒绝）。
	InternalKey = "navigation.err.internal" // 操作失败，请稍后重试（细节只进日志）

	// 导航类型标题（译文工作台按位置分组）。
	KindFooter = "admin.navigations.kind.footer" // 页脚导航
	KindHeader = "admin.navigations.kind.header" // 页眉导航

	// 面板块默认名的位置词（新建块时按当前请求语言拼出，见 navigation_panel_page.go）。
	PanelBlockNameMenu         = "admin.navigations.panel.blockName.menu"         // 菜单
	PanelBlockNameHeader       = "admin.navigations.panel.blockName.header"       // 页眉菜单
	PanelBlockNameHeaderMobile = "admin.navigations.panel.blockName.headerMobile" // 页眉移动菜单
	PanelBlockNameFooter       = "admin.navigations.panel.blockName.footer"       // 页脚菜单
	PanelBlockNameFooterMobile = "admin.navigations.panel.blockName.footerMobile" // 页脚移动菜单
	PanelBlockNamePanel        = "admin.navigations.panel.blockName.panel"        // 面板

	// 来源分组标题（「从已有内容添加」抽屉按来源实体分组）。
	//
	// 这批 key 有两个取用者，**分层是有意的**：模板 admin/navigation/navigations.html
	// 按来源类型取词并自带中文兜底（展示层才拿得到请求语言），outbound/source 的
	// sourceGroupTitleKey 只回 key（该适配器在 service 层之下，写中文等于把一种语言
	// 焊进契约数据）。常量共用这一份，两处形态各自保留。
	SourcePage     = "admin.navigations.source.page"     // 页面
	SourceArticle  = "admin.navigations.source.article"  // 文章
	SourceProduct  = "admin.navigations.source.product"  // 产品
	SourceCategory = "admin.navigations.source.category" // 分类
)
