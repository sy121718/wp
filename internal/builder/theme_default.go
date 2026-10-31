package builder

func DefaultThemeSettings() *ThemeSettings {
	return &ThemeSettings{
		Colors: ThemeColors{
			Primary:    "#3d444f",
			Secondary:  "#5b6572",
			Accent:     "#6b7280",
			Success:    "#3f6b4f",
			Warning:    "#8a6d3b",
			Danger:     "#b34a4a",
			Text:       "#1a1d21",
			Heading:    "#1a1d21",
			Background: "#ffffff",
			Surface:    "#f4f5f6",
			Border:     "#e5e7eb",
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

const DefaultThemeName = "默认主题（后台风格）"
