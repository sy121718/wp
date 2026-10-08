// Package navigationenums 统一管理 navigation 模块响应消息（0-C）。
package navigationenums

import "strings"

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
	// 文案后接「：<菜单项标题>」的定位信息（页面出口经 enums.SplitFacingDetail 拆出
	// key 与定位、只翻 key，所以本词条内不带 %s 占位符）。
	ErrStaleVersion = "ErrStaleVersion"
	// ErrInternal 未归类的内部错误（SQL / 表名 / 约束名 / 文件路径等）对外归口文案。
	//
	// 值刻意带模块前缀：sys_i18n 的主键是 (item_key, lang)，裸 key "ErrInternal" 已被
	// admin 批占用（迁移 268）；形态与 cart / user 的 "cart.err.internal" 一致。
	ErrInternal = "navigation.err.internal" // 操作失败，请稍后重试（细节只进日志）
)

// NavigationFacingMessages 可以原样展示给前端的导航业务文案（**白名单**）。
//
// 命中 → 原样透出（前端据此提示「哪一项不合法」「路径已被占用」）；
// 未命中 → inbound/http 的 navigationErrText 记结构化日志并返回 ErrInternal。
// 方向是安全的：漏写一个常量只会让前端看到一句通用提示（一眼可见），
// 而不会把 service 上抛的 PostgreSQL 原文（表名 / 约束名 / SQLSTATE）透出去。
//
// ErrInternal 本身不进白名单：它是未命中时的返回值，不是业务文案。
var NavigationFacingMessages = []string{
	ErrInvalidParam, ErrNotFound, ErrInvalidKind, ErrPathTaken,
	ErrInvalidSource, ErrInvalidTarget, ErrInvalidParent, ErrProjectRequired,
	ErrStaleVersion,
}

// —— 白名单的两种读法（本模块内共享，别在各处再抄一份判据）——
//
// 白名单命中判定与「key + 定位信息」的拆分此前只存在于 inbound/http 的一处循环里；
// 消费者侧（工作台检查器要就地建菜单项）需要的正是同一份判据 —— 它拿不到本模块的
// enums，只能经 contract 的出口取文案。把判据放在白名单旁边，两侧就不会各判一套。

// FacingDetailSep 业务文案与定位信息的分隔符（写侧固定用全角「：」）。
// 半角「: 」只出现在读侧（shell.FacingNotice 的形态 3 两种都认）。
const FacingDetailSep = "："

// HitFacingMessage 判定错误文本是否命中白名单，命中返回原文。
// 三种形态：整串相等 / key|param 带参 / key：<定位信息>。
func HitFacingMessage(msg string) (string, bool) {
	for _, m := range NavigationFacingMessages {
		if msg == m || strings.HasPrefix(msg, m+"|") ||
			strings.HasPrefix(msg, m+FacingDetailSep) || strings.HasPrefix(msg, m+": ") {
			return msg, true
		}
	}
	return "", false
}

// SplitFacingDetail 从白名单文案里拆出「key + 定位信息」；不是该形态返回 ok=false。
func SplitFacingDetail(msg string) (key, detail string, ok bool) {
	for _, m := range NavigationFacingMessages {
		if rest, found := strings.CutPrefix(msg, m+FacingDetailSep); found {
			return m, rest, true
		}
		if rest, found := strings.CutPrefix(msg, m+": "); found {
			return m, rest, true
		}
	}
	return "", "", false
}

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
	// 这批 key 有两个取用者，**分层是有意的**：inbound/http 的 navSourceGroupTitles 带中文兜底
	//（展示层才拿得到请求语言），outbound/source 的 sourceGroupTitleKey 只回 key（该适配器在
	// service 层之下，写中文等于把一种语言焊进契约数据）。常量共用这一份，两处形态各自保留。
	SourcePage     = "admin.navigations.source.page"     // 页面
	SourceArticle  = "admin.navigations.source.article"  // 文章
	SourceProduct  = "admin.navigations.source.product"  // 产品
	SourceCategory = "admin.navigations.source.category" // 分类
)
