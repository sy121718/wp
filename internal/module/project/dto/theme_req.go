package projectdto

import (
	json "go_wp/pkg/jsonx"
)

type ThemeCreateReq struct {
	ProjectID string          `json:"projectId" form:"projectId" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Settings  json.RawMessage `json:"settings" form:"settings"`
}

type ThemeUpdateReq struct {
	ID       string          `json:"id" form:"id" binding:"required"`
	Name     string          `json:"name" form:"name" binding:"required,max=100"`
	Settings json.RawMessage `json:"settings" form:"settings"`
}

type ThemeActivateReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	// ProjectID 只用于回跳筛选（列表页按工程过滤）；激活本身以主题自身归属为准。
	ProjectID string `json:"projectId" form:"projectId"`
}

// ThemeSettingsFormReq 是「主题设置」页主表单的显式字段表。
//
// 字段名与模板控件逐字一致：colors.* / typography.* / button.* / surface.* /
// motion.* / images.* 都是**字面量点号键**。gin 的 form tag 按字面匹配，不把点号
// 折成嵌套路径（已用 go test 样例验证 colors.primary 能从 colors.primary=v 绑上），
// 所以这里用扁平字段逐字登记，不引入嵌套结构。
//
// 本 DTO 只负责字段映射；脚本白名单（builder.ValidateThemeSettings 的 IsSafeCSSValue）、
// 结构模板哨兵语义仍在控制器侧。字段语义由调用处逐字保持：空串 = 跟随主题内置默认，
// 懒加载骨架屏只有字面量 "true" 为真。
//
// 额外槽位的模板绑定用两个**静态同名数组**提交：slotTemplateSlot（槽位名）与
// slotTemplateID（模板 id），按出现顺序配对。槽位名是值而不是字段名，因此不需要
// 编译期枚举库内槽位，字段门禁与契约也能对账。slots.* 仍只有公告条一个具名控件。
type ThemeSettingsFormReq struct {
	ID string `form:"id"`

	// 额外槽位模板绑定的静态平行数组（按 DOM 顺序配对）。
	SlotTemplateSlot []string `form:"slotTemplateSlot"`
	SlotTemplateID   []string `form:"slotTemplateID"`

	HeaderBlockID       string `form:"headerBlockId"`
	FooterBlockID       string `form:"footerBlockId"`
	AnnouncementBlockID string `form:"slots.announcement"`

	HeaderTemplateID        string `form:"headerTemplateId"`
	FooterTemplateID        string `form:"footerTemplateId"`
	StructureTemplateFields string `form:"structureTemplateFields"`

	ImagesLazyLoad string `form:"images.lazyLoad"`
	ImagesSkeleton string `form:"images.skeleton"`

	ColorPrimary    string `form:"colors.primary"`
	ColorSecondary  string `form:"colors.secondary"`
	ColorAccent     string `form:"colors.accent"`
	ColorSuccess    string `form:"colors.success"`
	ColorWarning    string `form:"colors.warning"`
	ColorDanger     string `form:"colors.danger"`
	ColorText       string `form:"colors.text"`
	ColorHeading    string `form:"colors.heading"`
	ColorBackground string `form:"colors.background"`
	ColorSurface    string `form:"colors.surface"`
	ColorBorder     string `form:"colors.border"`

	HeadingColor      string `form:"typography.heading.color"`
	HeadingFontWeight string `form:"typography.heading.weight"`
	HeadingFontSize   string `form:"typography.heading.size"`
	HeadingSpacing    string `form:"typography.heading.spacing"`
	HeadingFontFamily string `form:"typography.heading.font"`

	BodyColor      string `form:"typography.body.color"`
	BodyFontSize   string `form:"typography.body.size"`
	BodyLineHeight string `form:"typography.body.line"`
	BodyFontFamily string `form:"typography.body.font"`

	LinkColor      string `form:"typography.link.color"`
	LinkHoverColor string `form:"typography.link.hover"`
	LinkUnderline  string `form:"typography.link.underline"`

	ButtonBackground      string `form:"button.background"`
	ButtonColor           string `form:"button.color"`
	ButtonRadius          string `form:"button.radius"`
	ButtonFontWeight      string `form:"button.weight"`
	ButtonPaddingY        string `form:"button.py"`
	ButtonPaddingX        string `form:"button.px"`
	ButtonHoverBackground string `form:"button.hoverBg"`
	ButtonHoverColor      string `form:"button.hoverColor"`
	ButtonBorderWidth     string `form:"button.borderWidth"`
	ButtonBorderStyle     string `form:"button.borderStyle"`
	ButtonBorderColor     string `form:"button.borderColor"`
	ButtonShadow          string `form:"button.shadow"`

	SurfaceRadius      string `form:"surface.radius"`
	SurfaceBorderWidth string `form:"surface.borderWidth"`
	SurfaceBorderColor string `form:"surface.borderColor"`
	SurfaceShadow      string `form:"surface.shadow"`

	MotionTransitionDuration string `form:"motion.duration"`
	MotionEasing             string `form:"motion.easing"`
	MotionDefaultEntrance    string `form:"motion.entrance"`
}
