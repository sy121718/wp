package adminenums

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
