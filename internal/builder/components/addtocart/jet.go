// Package addtocart — Jet 渲染路径（视图组装）。
//
// 规格标签与变体行的组装**复用 core.product 导出的同名函数**：加购按钮旁边的
// 「红色 · L ¥99.00」必须与详情页规格选择器、商品卡上显示的完全一致 ——
// 各写一份解析，迟早在某一次字段改动后分叉，而分叉的表现为「按钮加购的是另一档规格」。
package addtocart

import (
	"fmt"
	"strings"

	product "go_wp/internal/builder/components/product"
	"go_wp/internal/builder/core"
)

// Row 一个加购行（逐变体模式下一行一个变体；单变体模式只有一行）。
type Row struct {
	// VariantID 变体 id（构建期烘进 hidden input —— 客户端无法伪造成别的变体，
	// 但即使伪造也只会买到他自己选的那件，价格仍由服务端现算）。
	VariantID string
	// Label 规格标签（如「颜色 红色 · 尺寸 L」）；单变体模式为空。
	Label string
	// Price 展示价（含货币符号）。
	Price string
	// ComparePrice 划线价（空则不输出）。
	ComparePrice string
}

// View 加购组件渲染视图。
type View struct {
	// Action 加购片段端点（固定路径）。
	Action string
	// Target 加购后刷新的购物车容器选择器（HTMX；无 JS 时忽略）。
	Target string
	// ProjectID 站点工程 id（构建上下文提供）。
	ProjectID string
	// Lang 本次编译的目标语言（core.RenderContext.Lang），输出为表单 hidden input。
	//
	// 加购是 POST：片段端点收集参数时只读 PostForm，**忽略 query** —— 语言只能走表单域，
	// 不能像 GET 片段那样拼在 action 的查询串里。空语言（单语言站点）不输出该域。
	Lang string
	// ButtonText 按钮文字（参与内容翻译）。
	ButtonText string
	// QtyAria 数量输入框的无障碍名（构建期按当前语言填充，多语言 P4）。
	//
	// 此前硬编码在模板里（aria-label="数量"）：读屏器在英文站点上念中文，
	// 而页面上根本看不到这串字 —— 只有看产物源码或用读屏器才会发现。
	QtyAria string
	// ShowQuantity 是否输出数量输入。
	ShowQuantity bool
	// Rows 加购行。
	Rows []Row
	// ShowRowPrice 是否在行内显示价格。
	//
	// 只在**多行**时为真：行内价格的作用是区分「这一行是哪件、多少钱」，
	// 多规格商品里每行价格不同、非显示不可；单 SKU 只有一行，
	// 而价格已经由商品详情区在标题下展示过了 —— 再显示一次是同一屏里的重复信息。
	ShowRowPrice bool
	// Notice 无法加购时的提示（商品没有启用变体）。空表示正常渲染。
	//
	// 用提示而不是构建失败：商品暂时没上架变体是**数据状态**，不是配置错误 ——
	// 为它让整页构建失败，等于一次运营操作把页面打没了。
	Notice string
	// noticeKey Notice 对应的词条 key（非导出：模板只消费文案本身）。
	//
	// 按 key 取词而不是「拿当前值反查属于哪条词条」：两条提示的译文一旦相同，
	// 值比较就分不清是哪一条，改译文会改错地方。
	noticeKey string
}

// 界面文案键（多语言 P5b）。
//
// 与 Translatable 的分工：作者**没填**按钮文字时用这里的译文兜底；
// 作者填了自定义文案，那条文案属于内容，走 sys_translation 的内容翻译链路。
// 两条链路各管一段，互不覆盖。
const (
	TextKeyButton = "site.component.addToCart.button"
	// TextKeyQtyAria 数量输入框的无障碍名（模板里此前硬编码「数量」）。
	TextKeyQtyAria = "site.component.addToCart.qtyAria"
	// TextKeyNoticeNoProject / TextKeyNoticeNoVariant 两条降级提示的词条
	//（都只在异常数据状态下出现，见 BuildView）。
	TextKeyNoticeNoProject = "site.component.addToCart.notice.noProject"
	TextKeyNoticeNoVariant = "site.component.addToCart.notice.noVariant"
)

// 中文兜底：取词函数为 nil（未接入 i18n）时用这些值，产物与接入前逐字一致。
const (
	// textFallbackButton 按钮文字的中文兜底。
	textFallbackButton = defaultButtonText
	// textFallbackQtyAria 数量输入框无障碍名的中文兜底。
	textFallbackQtyAria = "数量"
	// noticeNoProject / noticeNoVariant 提示语的中文兜底：BuildView 把它们写进
	// View.Notice 作为初值，ApplyI18n 再按 View 记下的 key 覆盖（不做值比较）。
	noticeNoProject = "加购暂不可用（未取到站点工程）"
	noticeNoVariant = "暂无可购买的规格"
)

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// 三段**各自独立**判定，不能整函数早退：按钮文字「作者是否自定义」与数量框
// 无障碍名「有没有译文」是两件事 —— 早退会让作者一填自定义按钮文字，
// aria-label 就永远是中文（读屏器在英文站点念中文，页面上看不出来）。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		// BuildView 已落中文兜底；这里补上零值构造（测试直连 View）的情况。
		v.QtyAria = textFallbackQtyAria
		return
	}
	v.QtyAria = text(TextKeyQtyAria, textFallbackQtyAria)
	// 按钮文字：作者填过的那条走内容翻译（sys_translation），这里只管缺省值。
	if v.ButtonText == textFallbackButton {
		v.ButtonText = text(TextKeyButton, textFallbackButton)
	}
	// 提示语：只在异常数据状态下非空，按 BuildView 记下的 key 取词。
	if v.noticeKey != "" {
		v.Notice = text(v.noticeKey, v.Notice)
	}
}

// CompileCSS 导出样式编译（复用组件内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// BuildView 生成加购视图。
//
// content 为构建期注入的解析器（商品实体解析器）：两个槽位字段都必须取到值 ——
// 取不到说明字段写错或数据源不对，构建期报错比产出一个点不动的按钮好。
//
// lang 为本次编译的目标语言（core.RenderContext.Lang）：加购是 POST，语言只能经表单域传
// （片段端点收集参数时只读 PostForm），不带它就恒回落工程默认语言。
func BuildView(p *Props, content core.ContentResolver, projectID, lang string) (View, error) {
	view := View{
		Action:       CartAddPath,
		Target:       effectiveTarget(p),
		ProjectID:    strings.TrimSpace(projectID),
		Lang:         strings.TrimSpace(lang),
		ButtonText:   effectiveButtonText(p),
		QtyAria:      textFallbackQtyAria,
		ShowQuantity: p.ShowQuantity,
	}
	if content == nil {
		return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析商品变体")
	}
	if view.ProjectID == "" {
		// 没有工程 id 的表单提交不了（片段端直接返回「缺少站点工程」）。
		//
		// 但**不构建失败**：并非所有编译路径都带工程 id —— pipeline.DefaultCompile 的
		// 注释明确写着「不解析站点级资源」。为它让整页构建失败，等于「一个按钮坏了，
		// 整页发布不了」，而页面其余部分明明好好的。
		// 所以降级成一句**看得见**的提示：访客看到「暂不可用」，页面作者能顺着它去查，
		// 而不是静默渲染一个点了没反应的按钮。
		view.Notice, view.noticeKey = noticeNoProject, TextKeyNoticeNoProject
		return view, nil
	}

	rawOptions, err := content.ResolveString(strings.TrimSpace(p.OptionsField))
	if err != nil {
		return View{}, fmt.Errorf("解析规格维度字段 %q 失败: %w", p.OptionsField, err)
	}
	rawVariants, err := content.ResolveString(strings.TrimSpace(p.VariantsField))
	if err != nil {
		return View{}, fmt.Errorf("解析变体组合字段 %q 失败: %w", p.VariantsField, err)
	}

	groups := product.ParseOptionGroups(rawOptions)
	options := product.ParseVariantOptions(rawVariants, groups, effectiveCurrency(p), view.ProjectID, view.Lang)
	if len(options) == 0 {
		// 有规格维度、但没有可买的组合（未上架 / 全部停用）：留一句提示，
		// 不做成一个点了没反应的按钮。
		view.Notice, view.noticeKey = noticeNoVariant, TextKeyNoticeNoVariant
		return view, nil
	}

	if effectiveMode(p) == ModePerVariant {
		rows := make([]Row, 0, len(options))
		for _, o := range options {
			rows = append(rows, Row{
				VariantID:    o.ID,
				Label:        o.Labels,
				Price:        o.Price,
				ComparePrice: o.ComparePrice,
			})
		}
		view.Rows = rows
		view.ShowRowPrice = len(rows) > 1
		return view, nil
	}

	// 单变体模式取**第一个**启用变体，不猜「哪个是默认变体」——
	// 商品数据里没有这个概念，凭空造一个会与规格选择器的首选项打架。
	// 多变体商品想在卡片上一键加购，请用逐变体模式（或接受「加的是第一档」）。
	first := options[0]
	view.Rows = []Row{{VariantID: first.ID, Price: first.Price, ComparePrice: first.ComparePrice}}
	// 单行：价格已在商品详情区展示过，行内不再重复（见 ShowRowPrice 注释）。
	view.ShowRowPrice = false
	return view, nil
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：加购表单按变体行渲染，
// 每行一个 hx-post 表单（无 HTMX 时降级为原生 POST 到同一端点）。没有可购买变体时
// View.Rows 为空、模板只出提示文案，此时不该登记任何 hx-* 属性。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if len(v.Rows) == 0 {
		return nil, nil
	}
	return []string{"hx-post", "hx-target", "hx-swap"}, nil
}
