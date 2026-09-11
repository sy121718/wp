package builder

// theme_default.go — 站点默认主题。
//
// 为什么要有一套内置默认主题：继承链是「主题 → 页面 → 组件」，链条的起点必须先存在。
// 工程建好却没有主题时，页面拿不到任何令牌，组件只能落到各自的 fallback（默认蓝），
// 用户在后台改主题色也无处生效 —— 这正是「主题和页面的先后顺序」出问题的地方。
//
// 色值取后台设计语言（admin theme.css 的亮色主题），让「后台长什么样、前台默认也长什么样」，
// 用户建站后看到的不是一套陌生的蓝，而是自己在后台熟悉的那身配色，改起来只是微调。
func DefaultThemeSettings() *ThemeSettings {
	return &ThemeSettings{
		Colors: ThemeColors{
			Primary:    "#3d444f", // --c-primary
			Secondary:  "#5b6572", // --c-primary-soft
			Accent:     "#6b7280", // --c-text-mute
			Success:    "#3f6b4f", // --c-success
			Warning:    "#8a6d3b", // --c-warning
			Danger:     "#b34a4a", // --c-danger
			Text:       "#1a1d21", // --c-text
			Heading:    "#1a1d21",
			Background: "#ffffff", // --c-bg
			Surface:    "#f4f5f6", // --c-bg-soft（卡片/面板底色）
			Border:     "#e5e7eb", // --c-border
		},
		Typography: ThemeTypography{
			Heading: ThemeHeadingStyle{
				Color:      "#1a1d21",
				FontWeight: "600",
				FontSize:   "32px",
				Spacing:    "12px",
			},
			Body: ThemeBodyStyle{
				Color:      "#1a1d21",
				FontSize:   "16px",
				LineHeight: "1.7",
			},
			Link: ThemeLinkStyle{
				Color:      "#3d444f",
				HoverColor: "#2f353d",
				Underline:  "hover",
			},
		},
		Button: ThemeButton{
			Background:      "#3d444f",
			Color:           "#ffffff",
			HoverBackground: "#2f353d",
			HoverColor:      "#ffffff",
			Radius:          "8px",
			FontWeight:      "600",
			PaddingY:        "10px",
			PaddingX:        "20px",
		},
		Surface: ThemeSurface{
			Radius:      "10px",
			BorderWidth: "1px",
			BorderColor: "#e5e7eb",
			Shadow:      "sm",
		},
	}
}

// DefaultThemeName 默认主题的名称（工程首次建立时创建的那一套）。
const DefaultThemeName = "默认主题（后台风格）"
