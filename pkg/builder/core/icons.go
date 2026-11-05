package core

import "go_wp/pkg/view/ui/icons"

type IconMeta = icons.IconMeta

func IconSVG(name string) (string, bool) { return icons.IconSVG(name) }

func IconSVGClass(name, class string) (string, bool) { return icons.IconSVGClass(name, class) }

func IconInner(name string) (string, bool) { return icons.IconInner(name) }

func IconNames() []string { return icons.IconNames() }

func IconCategories() []string { return icons.IconCategories() }

func IconCategory(name string) (string, bool) { return icons.IconCategory(name) }

func IconLabel(name string) string { return icons.IconLabel(name) }

func IconStyle(name string) (string, bool) { return icons.IconStyle(name) }

func FilterIcons(category, style, keyword string) []string {
	return icons.FilterIcons(category, style, keyword)
}
