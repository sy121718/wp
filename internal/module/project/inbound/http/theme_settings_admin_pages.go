package projecthttp

// theme_settings_admin_pages.go — 单主题设置页（自 dashboard/inbound/http/theme_settings_handle.go 迁入）。
//
// 页面：/admin/themes/settings（GET 表单 + POST 保存）。
// block 契约仅做只读取数（页眉/页脚候选块列表）；block 的 stale 传播接线已由装配层接管，
// 本文件不编排。

import (
	"encoding/json"
	"net/http"
	"strings"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// 主题设置页文案（i18n key，与 dashboard enums 迁移前同值）。
//
// 校验失败的文案已收进 projectenums.MsgThemeSettingsInvalid —— 它会进 ?err= 通道，
// 而读侧白名单要够得着它（本地常量让白名单只能靠抄字面量，抄错就静默失配）。
const (
	themeSettingsMsgTitle = "MsgThemeSettingsTitle"
)

// themeSettingsData 单主题设置页数据（全局颜色/字体/页眉页脚块绑定）。
type themeSettingsData struct {
	Title     string
	Menu      string
	ThemeID   string
	ThemeName string
	ProjectID string
	// 以下 5 色 + 字体为旧字段，供 admin/theme_settings.html 回显（向后兼容）。
	PColor     string
	TColor     string
	BgColor    string
	SColor     string
	BdColor    string
	FontFamily string
	// ThemeSettingsJSON 完整 ThemeSettings JSON 字符串（colors 11 色 + typography +
	// button + surface + motion），供前端面板回显/扩展使用。
	ThemeSettingsJSON string
	// Groups 主题设置字段分组（与工作台全局设置面板共用同一份字段表）：
	// 颜色走取色器、固定档位走下拉、字体走可填可选的 datalist ——
	// 让主题设置页的控件类型由数据决定，而不是手写一堆文本框让用户猜格式。
	Groups []themeFieldGroupView
	// HeaderBlockID/FooterBlockID 全局页眉/页脚块绑定（编译期内联装配）。
	HeaderBlockID string
	FooterBlockID string
	// HeaderBlocks/FooterBlocks 该工程页眉/页脚候选块列表。
	HeaderBlocks []blockOption
	FooterBlocks []blockOption
	// AnnouncementBlocks 公告条候选块；AnnouncementBlockID 当前绑定（审计 VIS-012）。
	AnnouncementBlocks  []blockOption
	AnnouncementBlockID string
	// HeaderTemplateID / FooterTemplateID 页眉 / 页脚绑定的结构模板（可空 = 用块绑定）。
	//
	// 本页暂只做**原样回传**（渲染成隐藏域，保存时写回）：结构模板下拉与「生效」徽标
	// 属模板列表侧的后台改造，届时把隐藏域换成 select 即可，落库口径不用变。
	HeaderTemplateID string
	FooterTemplateID string
	// SlotTemplates 其余槽位的模板绑定（槽位名 → 模板 ID），同样原样回传。
	SlotTemplates map[string]string
	// HeaderTemplateOptions / FooterTemplateOptions 结构模板候选（页眉 / 页脚各一组）。
	//
	// 「不绑定」那一项由模板固定渲染（value 为空串），服务端只给真实候选 ——
	// 候选为空时下拉仍有一项可选，不会退化成「没有这个字段」（那才是清空绑定）。
	HeaderTemplateOptions []structureTemplateOptionView
	FooterTemplateOptions []structureTemplateOptionView
	// Err 上一次保存失败的提示（?err= 经读侧白名单，空 = 无提示）。
	//
	// 保存失败（校验不通过 / service 拒绝 / 整站刷新失败）后 303 回到本页并带 ?err=；
	// 本页原先对此**没有任何出口** —— 失败是一块纯文本错误页，表单与页头全没了。
	Err string
}

// structureTemplateOptionView 结构模板下拉项（selected 由服务端算好，前端不认识这组数据）。
type structureTemplateOptionView struct {
	ID       string
	Label    string
	Selected bool
}

// blockOption 页眉/页脚绑定候选下拉项。
type blockOption struct {
	ID   string
	Name string
	Kind string
}

// templateMap 转 Jet 模板键 map。
func (d *themeSettingsData) templateMap() gin.H {
	return gin.H{
		"title":                 d.Title,
		"menu":                  d.Menu,
		"ThemeID":               d.ThemeID,
		"ThemeName":             d.ThemeName,
		"ProjectID":             d.ProjectID,
		"PColor":                d.PColor,
		"TColor":                d.TColor,
		"BgColor":               d.BgColor,
		"SColor":                d.SColor,
		"BdColor":               d.BdColor,
		"FontFamily":            d.FontFamily,
		"ThemeSettings":         d.ThemeSettingsJSON,
		"Groups":                d.Groups,
		"HeaderBlock":           d.HeaderBlockID,
		"FooterBlock":           d.FooterBlockID,
		"HeaderBlocks":          d.HeaderBlocks,
		"FooterBlocks":          d.FooterBlocks,
		"AnnouncementBlocks":    d.AnnouncementBlocks,
		"AnnouncementBlock":     d.AnnouncementBlockID,
		"HeaderTemplate":        d.HeaderTemplateID,
		"FooterTemplate":        d.FooterTemplateID,
		"SlotTemplates":         d.SlotTemplates,
		"HeaderTemplateOptions": d.HeaderTemplateOptions,
		"FooterTemplateOptions": d.FooterTemplateOptions,
		"Err":                   d.Err,
	}
}

// themeSettingsJSON 与主题设置结构约定对齐（020_themes.sql / 021_blocks.sql 方案 C）：
// themes.settings 存储的 JSON = 完整 ThemeSettings（colors 11 色 + typography +
// button + surface + motion，经 builder.ParseThemeSettings 校验）+ 顶层
// headerBlockId/footerBlockId（全局块槽位绑定，不属于 ThemeSettings 模型，
// 但同存一份方便 themeSnapshots 分离快照）。
//
// 嵌入 builder.ThemeSettings 使 json.Marshal 扁平化输出完整主题字段；
// headerBlockId/footerBlockId 为顶层结构绑定。
type themeSettingsJSON struct {
	builder.ThemeSettings
	HeaderBlockID string `json:"headerBlockId,omitempty"`
	FooterBlockID string `json:"footerBlockId,omitempty"`
	// Slots 其余结构槽位的绑定（公告条 / 侧边栏等）：主题设置保存时与两个历史字段一起落库。
	Slots map[string]string `json:"slots,omitempty"`
	// HeaderTemplateID/FooterTemplateID/SlotTemplates 结构模板绑定（页眉 / 页脚的
	// 「多套存着、单套生效」之选），与块绑定同层、同一份快照里落库。
	//
	// **保存路径必须原样写回**：漏掉就是「在别处配好的结构模板，来这个页面保存一次
	// 主题设置就被清空」—— 站点上页眉悄悄回到旧块绑定，而页面上看不出任何异常。
	HeaderTemplateID string            `json:"headerTemplateId,omitempty"`
	FooterTemplateID string            `json:"footerTemplateId,omitempty"`
	SlotTemplates    map[string]string `json:"slotTemplates,omitempty"`
}

// ThemeSettings 单主题设置页。
//
// 缺 id / 主题不存在时 303 回主题列表并带提示：本页**不知道自己该显示什么**（没有主题），
// 停在原地只能给一块错误页，而用户要的是回到能重新选主题的地方（列表页）。
func (h *themeAdminHandle) ThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.Query("id"))
	if themeID == "" {
		projectErrRedirect(c, "/admin/themes", response.TranslateMessage(c, projectenums.ErrThemeIDRequired))
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		projectErrRedirect(c, "/admin/themes", response.TranslateMessage(c, projectenums.ErrThemeNotFound))
		return
	}
	data.Err = projectPageErrText(c, c.Query("err"))
	c.HTML(http.StatusOK, "admin/project/theme_settings", shell.Prepare(c, data.templateMap()))
}

// loadThemeSettings 组装单主题设置页数据；主题不存在返回 nil。
func (h *themeAdminHandle) loadThemeSettings(c *gin.Context, themeID string) *themeSettingsData {
	ctx := c.Request.Context()
	theme, err := h.projects.GetTheme(ctx, themeID)
	if err != nil || theme == nil {
		return nil
	}
	data := &themeSettingsData{
		Title: themeSettingsMsgTitle, Menu: "themes",
		ThemeID: theme.ID, ThemeName: theme.Name, ProjectID: theme.ProjectID,
	}
	// 完整 ThemeSettings：解析 + 校验（向后兼容旧 5 色 + 顶层 fontFamily 格式）。
	// 旧字段回显：PColor 等取自 ThemeColors；FontFamily 取自 typography.body.fontFamily
	// （旧顶层 fontFamily 被 ParseThemeSettings 忽略）。
	ts, perr := builder.ParseThemeSettings(theme.Settings)
	if perr != nil {
		logger.Scene("page").With("theme_id", themeID).Error(perr, "解析主题设置 JSON 失败")
	} else {
		data.PColor = ts.Colors.Primary
		data.TColor = ts.Colors.Text
		data.BgColor = ts.Colors.Background
		data.SColor = ts.Colors.Surface
		data.BdColor = ts.Colors.Border
		data.FontFamily = ts.Typography.Body.FontFamily
		if b, err := json.Marshal(ts); err == nil {
			data.ThemeSettingsJSON = string(b)
		}
	}
	// 结构绑定：顶层 headerBlockId/footerBlockId（不属于 ThemeSettings）。
	var s themeSettingsJSON
	if len(theme.Settings) > 0 {
		if err := json.Unmarshal(theme.Settings, &s); err != nil {
			logger.Scene("page").With("theme_id", themeID).Error(err, "解析主题结构绑定 JSON 失败")
		}
	}
	data.HeaderBlockID = s.HeaderBlockID
	data.FooterBlockID = s.FooterBlockID
	// 结构模板绑定：本页只做原样回传（隐藏域），不清空、不改写。
	data.HeaderTemplateID = s.HeaderTemplateID
	data.FooterTemplateID = s.FooterTemplateID
	data.SlotTemplates = s.SlotTemplates
	// 结构模板候选（页眉 / 页脚）：主题设置页的「选结构模板」下拉。取不到候选不是致命错误 ——
	// 下拉退化成只有「不绑定」一项，页面其余字段照常可保存（缺候选在启动日志里有 Warn）。
	if opts, oerr := h.projects.StructureTemplateOptions(ctx, theme.ProjectID); oerr != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(oerr, "读取结构模板候选失败（下拉候选为空，页面其余部分照常）")
	} else {
		for _, o := range opts {
			// 标签里带「当前生效」：多套模板并存时，下拉必须能看出哪一套是每次构建真正生效的那套，
			// 否则「选了另一套没生效」会被当成 bug（生效与否由模板列表的「设为生效」决定）。
			label := o.Name
			if o.IsDefault {
				label += "（当前生效）"
			}
			if o.EntityType == "footer" {
				data.FooterTemplateOptions = append(data.FooterTemplateOptions, structureTemplateOptionView{
					ID: o.ID, Label: label, Selected: o.ID == data.FooterTemplateID,
				})
				continue
			}
			data.HeaderTemplateOptions = append(data.HeaderTemplateOptions, structureTemplateOptionView{
				ID: o.ID, Label: label, Selected: o.ID == data.HeaderTemplateID,
			})
		}
	}
	// 其余槽位（目前是公告条）从 slots 映射里取：加新槽位时这里与模板各加一行。
	data.AnnouncementBlockID = s.Slots["announcement"]
	// 字段分组：以原始 JSON 为准（保真，不经过结构体丢掉历史/未来的键）。
	var rawSettings map[string]any
	if len(theme.Settings) > 0 {
		_ = json.Unmarshal(theme.Settings, &rawSettings)
	}
	data.Groups = buildThemeGroups(rawSettings)
	// 页眉/页脚绑定候选：本工程的页眉/页脚类全局块。
	if blocks, err := h.blocks.List(ctx, &blockcontract.ListReq{ProjectID: theme.ProjectID}); err == nil {
		data.HeaderBlocks = []blockOption{{ID: "", Name: "（未设置）"}}
		data.FooterBlocks = []blockOption{{ID: "", Name: "（未设置）"}}
		data.AnnouncementBlocks = []blockOption{{ID: "", Name: "（未设置）"}}
		for _, b := range blocks {
			opt := blockOption{ID: b.ID, Name: b.Name, Kind: b.Kind}
			switch b.Kind {
			case "header":
				data.HeaderBlocks = append(data.HeaderBlocks, opt)
			case "footer":
				data.FooterBlocks = append(data.FooterBlocks, opt)
			case "announcement":
				data.AnnouncementBlocks = append(data.AnnouncementBlocks, opt)
			}
		}
	}
	return data
}

// SaveThemeSettings 保存单主题设置（POST /admin/themes/settings/save）：
// 写回 themes.settings（完整 ThemeSettings + 页眉/页脚块绑定）；主题设置批量合入
// 该主题下全部页面文档（settings.theme 快照），结构绑定合入 settings.structure；
// 保存后该主题下页面全部标待重建（新颜色/结构与块内容需重新构建生效）。
//
// PostForm 键名约定（扁平点分命名，前端 workbench.js 严格按此提交）：
//   - 颜色 11 个：colors.primary / colors.secondary / colors.accent / colors.success /
//     colors.warning / colors.danger / colors.text / colors.heading /
//     colors.background / colors.surface / colors.border
//   - 标题排版：typography.heading.color / typography.heading.weight（→ Heading.FontWeight）/
//     typography.heading.size（→ Heading.FontSize）/ typography.heading.spacing /
//     typography.heading.font（→ Heading.FontFamily）
//   - 正文排版：typography.body.color / typography.body.size（→ Body.FontSize）/
//     typography.body.line（→ Body.LineHeight）/ typography.body.font（→ Body.FontFamily）
//   - 链接：typography.link.color / typography.link.hover（→ Link.HoverColor）/
//     typography.link.underline
//   - 按钮：button.background / button.color / button.radius /
//     button.weight（→ Button.FontWeight）/ button.py（→ Button.PaddingY）/
//     button.px（→ Button.PaddingX）/ button.hoverBg（→ Button.HoverBackground）/
//     button.hoverColor
//   - 表面：surface.radius / surface.borderWidth / surface.borderColor / surface.shadow
//   - 动效：motion.duration（→ Motion.TransitionDuration）/ motion.easing /
//     motion.entrance（→ Motion.DefaultEntrance）
//   - 结构绑定（顶层）：headerBlockId / footerBlockId
func (h *themeAdminHandle) SaveThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.PostForm("id"))
	if themeID == "" {
		projectErrRedirect(c, "/admin/themes", response.TranslateMessage(c, projectenums.ErrThemeIDRequired))
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		projectErrRedirect(c, "/admin/themes", response.TranslateMessage(c, projectenums.ErrThemeNotFound))
		return
	}
	// 失败后一律回到本页（PRG）：用户刚在这一屏调完颜色 / 字体 / 结构绑定，
	// 停在原地才能接着改 —— 这与「表单内容丢失」是两个问题（后者要回填表单值，见 02-O 的任务单）。
	backURL := "/admin/themes/settings?id=" + themeID
	// 从 PostForm（点分键名）组装完整 ThemeSettings；空值直接透传为字段零值，
	// 序列化时经 omitempty 省略（未设置字段不输出 CSS 变量，组件回退自身默认）。
	ts := &builder.ThemeSettings{
		// 图片管理：懒加载默认策略 + 骨架屏开关（组件级三态为「默认」时继承这里）。
		Images: builder.ThemeImages{
			LazyLoad: strings.TrimSpace(c.PostForm("images.lazyLoad")),
			Skeleton: strings.TrimSpace(c.PostForm("images.skeleton")) == "true",
		},
		Colors: builder.ThemeColors{
			Primary:    strings.TrimSpace(c.PostForm("colors.primary")),
			Secondary:  strings.TrimSpace(c.PostForm("colors.secondary")),
			Accent:     strings.TrimSpace(c.PostForm("colors.accent")),
			Success:    strings.TrimSpace(c.PostForm("colors.success")),
			Warning:    strings.TrimSpace(c.PostForm("colors.warning")),
			Danger:     strings.TrimSpace(c.PostForm("colors.danger")),
			Text:       strings.TrimSpace(c.PostForm("colors.text")),
			Heading:    strings.TrimSpace(c.PostForm("colors.heading")),
			Background: strings.TrimSpace(c.PostForm("colors.background")),
			Surface:    strings.TrimSpace(c.PostForm("colors.surface")),
			Border:     strings.TrimSpace(c.PostForm("colors.border")),
		},
		Typography: builder.ThemeTypography{
			Heading: builder.ThemeHeadingStyle{
				Color:      strings.TrimSpace(c.PostForm("typography.heading.color")),
				FontWeight: strings.TrimSpace(c.PostForm("typography.heading.weight")),
				FontSize:   strings.TrimSpace(c.PostForm("typography.heading.size")),
				Spacing:    strings.TrimSpace(c.PostForm("typography.heading.spacing")),
				FontFamily: strings.TrimSpace(c.PostForm("typography.heading.font")),
			},
			Body: builder.ThemeBodyStyle{
				Color:      strings.TrimSpace(c.PostForm("typography.body.color")),
				FontSize:   strings.TrimSpace(c.PostForm("typography.body.size")),
				LineHeight: strings.TrimSpace(c.PostForm("typography.body.line")),
				FontFamily: strings.TrimSpace(c.PostForm("typography.body.font")),
			},
			Link: builder.ThemeLinkStyle{
				Color:      strings.TrimSpace(c.PostForm("typography.link.color")),
				HoverColor: strings.TrimSpace(c.PostForm("typography.link.hover")),
				Underline:  strings.TrimSpace(c.PostForm("typography.link.underline")),
			},
		},
		Button: builder.ThemeButton{
			Background:      strings.TrimSpace(c.PostForm("button.background")),
			Color:           strings.TrimSpace(c.PostForm("button.color")),
			Radius:          strings.TrimSpace(c.PostForm("button.radius")),
			FontWeight:      strings.TrimSpace(c.PostForm("button.weight")),
			PaddingY:        strings.TrimSpace(c.PostForm("button.py")),
			PaddingX:        strings.TrimSpace(c.PostForm("button.px")),
			HoverBackground: strings.TrimSpace(c.PostForm("button.hoverBg")),
			HoverColor:      strings.TrimSpace(c.PostForm("button.hoverColor")),
			BorderWidth:     strings.TrimSpace(c.PostForm("button.borderWidth")),
			BorderStyle:     strings.TrimSpace(c.PostForm("button.borderStyle")),
			BorderColor:     strings.TrimSpace(c.PostForm("button.borderColor")),
			Shadow:          strings.TrimSpace(c.PostForm("button.shadow")),
		},
		Surface: builder.ThemeSurface{
			Radius:      strings.TrimSpace(c.PostForm("surface.radius")),
			BorderWidth: strings.TrimSpace(c.PostForm("surface.borderWidth")),
			BorderColor: strings.TrimSpace(c.PostForm("surface.borderColor")),
			Shadow:      strings.TrimSpace(c.PostForm("surface.shadow")),
		},
		Motion: builder.ThemeMotion{
			TransitionDuration: strings.TrimSpace(c.PostForm("motion.duration")),
			Easing:             strings.TrimSpace(c.PostForm("motion.easing")),
			DefaultEntrance:    strings.TrimSpace(c.PostForm("motion.entrance")),
		},
	}
	// 完整存储 JSON = 校验后的 ThemeSettings + 顶层结构绑定。
	settingsJSON, err := json.Marshal(themeSettingsJSON{
		ThemeSettings: *ts,
		HeaderBlockID: strings.TrimSpace(c.PostForm("headerBlockId")),
		FooterBlockID: strings.TrimSpace(c.PostForm("footerBlockId")),
		Slots:         themeSlotFormValues(c),
		// 结构模板绑定：见 structureTemplateFormValue —— 带哨兵域时按提交值写回（空值 = 主动解绑），
		// 不带时保持已存值（旧表单/缺字段不能让保存一次颜色就把页眉模板清空）。
		HeaderTemplateID: structureTemplateFormValue(c, "headerTemplateId", data.HeaderTemplateID),
		FooterTemplateID: structureTemplateFormValue(c, "footerTemplateId", data.FooterTemplateID),
		SlotTemplates:    themeSlotTemplateFormValues(c, data.SlotTemplates),
	})
	if err != nil {
		projectErrRedirect(c, backURL, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err))
		return
	}
	// ParseThemeSettings 校验（IsSafeCSSValue 白名单，防 CSS 注入）；非法回本页并给提示。
	if _, err := builder.ParseThemeSettings(settingsJSON); err != nil {
		// CSS 值白名单不通过：原文（哪个字段、期望什么形状）只进日志，对外一句归口文案。
		// 逐字段就近提示属 02-O 的 theme_settings 任务单（本域 P0 集中页），不在本批范围。
		logger.Scene("theme").With("theme_id", themeID).Error(err, "主题设置校验失败")
		projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.MsgThemeSettingsInvalid))
		return
	}
	if _, err := h.projects.UpdateTheme(c.Request.Context(), &projectcontract.ThemeUpdateReq{
		ID: themeID, Name: data.ThemeName, Settings: settingsJSON,
	}); err != nil {
		projectErrRedirect(c, backURL, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err))
		return
	}
	// 颜色/字体快照合入 settings.theme + 页眉/页脚绑定合入 settings.structure，
	// 再标记待重建（新颜色/结构与块内容需重新构建生效）。
	if code := h.refreshThemePages(c, themeID, settingsJSON); code != 0 {
		// 部分成功：设置**已经落库**，失败的只是「合入页面文档 + 标记待重建」这一步。
		// 提示必须说清这层区别（原先是 500 + 一行纯文本），否则用户以为白填了一遍又填一次。
		projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.MsgThemeSettingsRefreshFailed))
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/themes/settings?id="+themeID)
}

// structureTemplateFormValue 结构模板绑定的表单取值。
//
// 「没提交这个字段」与「提交了空值」必须分开：前者是旧表单 / 缺字段（保持原值 ——
// 不能让「保存一次颜色」把配好的页眉模板清空），后者是用户在下拉里选了「不绑定」
// （必须真的解绑）。判据是表单里的哨兵域 structureTemplateFields：新版表单渲染了
// 结构模板区块就带它。
//
// 为什么不用「表单里有 headerTemplateId 键」当判据：多套模板并存时下拉本身就带空值项，
// select 永远会提交这个键；而旧版表单根本没有这个键 —— 两件事用一个哨兵说清楚比猜更可靠。
func structureTemplateFormValue(c *gin.Context, key, current string) string {
	if strings.TrimSpace(c.PostForm("structureTemplateFields")) != "1" {
		return firstNonEmpty(strings.TrimSpace(c.PostForm(key)), current)
	}
	return strings.TrimSpace(c.PostForm(key))
}

// firstNonEmpty 取第一个非空串（表单没提交时回落到已存值，避免保存即清空）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// themeSlotTemplateFormValues 收集表单里的槽位模板绑定（点分键 slotTemplates.<槽位名>）。
//
// 与 themeSlotFormValues 同构：前缀扫描，将来加槽位不必改这里。表单没有提交任何
// slotTemplates 键时**原样返回已存值**（同上：不丢数据优先）。
func themeSlotTemplateFormValues(c *gin.Context, current map[string]string) map[string]string {
	const prefix = "slotTemplates."
	_ = c.Request.ParseForm()
	out := map[string]string{}
	for key, values := range c.Request.PostForm {
		if !strings.HasPrefix(key, prefix) || len(values) == 0 {
			continue
		}
		slot := strings.TrimSpace(strings.TrimPrefix(key, prefix))
		if id := strings.TrimSpace(values[0]); slot != "" && id != "" {
			out[slot] = id
		}
	}
	if len(out) == 0 {
		return current
	}
	return out
}

// themeSlotFormValues 收集表单里的结构槽位绑定（点分键 slots.<槽位名>）。
//
// 用前缀扫描而不是逐个 c.PostForm("slots.announcement")：主题设置页加一个槽位只需
// 多一个 select，这里不必改 —— 而漏改一处就是「配了不生效」，且页面上看不出任何异常。
func themeSlotFormValues(c *gin.Context) map[string]string {
	const prefix = "slots."
	_ = c.Request.ParseForm()
	out := map[string]string{}
	for key, values := range c.Request.PostForm {
		if !strings.HasPrefix(key, prefix) || len(values) == 0 {
			continue
		}
		slot := strings.TrimSpace(strings.TrimPrefix(key, prefix))
		if id := strings.TrimSpace(values[0]); slot != "" && id != "" {
			out[slot] = id
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
