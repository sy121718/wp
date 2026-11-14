package projectdto

type SettingsPanelView struct {
	Settings any
}

type SeoScorePanelView struct {
	Score any
}

type LocaleRowsView struct {
	Locales any
}

type GlobalPanelView struct {
	ThemeID string
	Groups  any

	// 保存走的是同一份主题设置端点：面板不渲染的绑定字段必须以服务端事实
	// 作为隐藏域随表单提交，否则「只在工作台改个颜色」会把页眉/页脚/公告槽解绑。
	HeaderBlockID       string
	FooterBlockID       string
	AnnouncementBlockID string
}
