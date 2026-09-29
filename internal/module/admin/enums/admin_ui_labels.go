// admin_ui_labels.go — admin 模块后台页面的展示标签（枚举 → 展示名）的真源。
package adminenums

// MenuTypeLabel 管理菜单类型（sys_menus.type）→ (词条 key, 中文兜底)。
//
// key 与后台模板 tr("admin.menus.type.*") 是**同一批**：菜单管理页的徽章在模板里取词，
// 权限分配树的类型标签在 Go 侧拼 —— 此前 Go 侧那份写的是中文硬编码，同一页两处
// 会因为语言不同而自相矛盾（英文后台的树里冒出一堆「目录 / 菜单 / 按钮」）。
//
// iframe（4）key 留空：它是技术名词，模板侧同样直接写字面量，没有第二条语言可切。
// 认不出的取值给 unknown 那条 —— 页面上要能看出「这是个不认识的类型」，
// 而不是一片空白（空白读不出任何信息）。
func MenuTypeLabel(menuType int) (key, fallback string) {
	switch menuType {
	case 1:
		return "admin.menus.type.dir", "目录"
	case 2:
		return "admin.menus.type.menu", "菜单"
	case 3:
		return "admin.menus.type.button", "按钮"
	case 4:
		return "", "iframe"
	case 5:
		return "admin.menus.type.link", "外链"
	default:
		return "admin.menus.type.unknown", "未知"
	}
}
