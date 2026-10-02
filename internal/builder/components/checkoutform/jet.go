// checkoutform 的 Jet 渲染路径辅助导出（与其余组件同形：Go 侧准备视图数据，模板只拼 HTML）。
package checkoutform

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	ordercontract "go_wp/internal/module/order/contract"
)

// FragmentPath 结算片段地址（构建期烘进表单 action / hx-post）。
//
// 片段端点只按 type 路由（POST /_fragments/{type}），type 就是订单侧的结算能力名
// （见 runtimefragment/cart.go 的 Register(Spec{Type: "checkout", Method: "POST", ...})）。
// 写错这里是**静默失效**：表单照常渲染、提交后 404，页面上看不出任何异常。
const FragmentPath = "/_fragments/checkout"

// DefaultSubmitLabel 提交按钮的默认文字（Props 未填时用）。
//
// 本批不做多语言表单文案（组件固定文案的 sys_i18n 取词不在本批范围），
// 因此这里直接给中文；产物按当前构建语言输出同一份文字。
const DefaultSubmitLabel = "提交订单"

// noticeNoProject 缺站点工程时的提示（编辑器画布 / 单测编译等无工程上下文的场景）。
//
// 为什么是提示而不是报错：组件库的「每个条目插入后都能编译」是既有契约
// （见 builder 的 palette 契约测试），而那些编译路径本来就没有工程上下文。
// 报错会让「插入即合法」这条契约失效，且失败的是一条与作者配置无关的路径。
const noticeNoProject = "结算表单暂不可用（未取到站点工程）"

// 分组标题（Go 侧文案，见 DefaultSubmitLabel 的说明）。
var groupTitles = map[string]string{
	ordercontract.CheckoutGroupContact:  "联系信息",
	ordercontract.CheckoutGroupShipping: "收货地址",
	ordercontract.CheckoutGroupBilling:  "账单地址",
}

// groupTitleOf 取分组标题；未知分组回退分组键（绝不输出空串）。
func groupTitleOf(group string) string {
	if t, ok := groupTitles[group]; ok {
		return t
	}
	return group
}

// HiddenView 隐藏字段（projectId / lang：片段端 POST 只读表单体，URL 上的 query 到不了处理器）。
type HiddenView struct {
	Name  string
	Value string
}

// OptionView 下拉选项（国家字段用：选项在构建期烘焙，运行时零 JS）。
type OptionView struct {
	Code     string
	Label    string
	Selected bool
}

// FieldView 单个字段的渲染视图。
type FieldView struct {
	// Key 提交参数名（表单 name 与返回给片段端的键）。
	Key string
	// Label 标签文案（作者覆盖优先，否则清单默认）。
	Label string
	// Type 输入形态：text / email / tel / textarea / country。
	Type string
	// Required 是否必填（渲染 required 属性；标记为必填的字段不可关）。
	Required bool
	// Placeholder 占位提示（空则不输出属性）。
	Placeholder string
	// Wide 是否整行（textarea 独占一行；栅格按 auto-fit 自适应，窄屏自然变一列）。
	Wide bool
	// Options 国家字段的选项（其余类型为空）。
	Options []OptionView
}

// GroupView 一个展示分组（联系信息 / 收货地址 / 账单地址）。
type GroupView struct {
	// Key 分组键（CheckoutGroup* 常量，模板输出 data-group 便于站点主题定位）。
	Key string
	// Title 分组标题。
	Title string
	// Fields 本组字段（顺序即作者配置的顺序）。
	Fields []FieldView
}

// View 结算表单渲染视图（供 checkoutform.jet 使用）。
type View struct {
	// Notice 非空表示本组件处于「不可用」状态（缺站点工程）：只渲染提示，不渲染表单。
	Notice string
	// Action 提交地址（原生 POST 的降级路径，无 JS 时整页提交）。
	Action string
	// SubmitLabel 提交按钮文字。
	SubmitLabel string
	// Groups 分组后的字段。
	Groups []GroupView
	// Hidden 隐藏字段（projectId / lang）。
	Hidden []HiddenView
}

// BuildView 生成结算表单视图。
//
// 降级与错误的分工（两者后果不同，判据是「这条路径是不是真实产物」）：
//
//   - **缺站点工程** → 渲染提示、不报错。编辑器画布与组件库契约测试本来就没有工程上下文，
//     报错会让「每个组件插入后都能编译」这条既有契约失效，而那不是作者的配置问题；
//   - **表单里有国家字段、但构建期没注入国家清单** → **明确报错**（阻断构建）。
//     真实产物上这只能是装配缺陷（sys_area 没数据 / 注入点漏了）：降级成「只有一个默认国家的
//     下拉」会把访客锁死在一个国家，而页面上完全看不出异常；构建失败是可见的，也与
//     NavigationResolver「未注入时显式失败、不静默产出空菜单」是同一口径。
//   - 表单里**没有**国家字段时不检查国家清单：不需要的数据缺失不构成缺陷。
func BuildView(p *Props, ctx *core.RenderContext) (View, error) {
	projectID := ""
	lang := ""
	var countries []core.CheckoutCountry
	defaultCountry := ""
	if ctx != nil {
		projectID = strings.TrimSpace(ctx.ProjectID)
		lang = strings.TrimSpace(ctx.Lang)
		countries = ctx.Checkout.Countries
		defaultCountry = strings.TrimSpace(ctx.Checkout.DefaultCountry)
	}
	if projectID == "" {
		return View{Notice: noticeNoProject}, nil
	}

	selected, err := resolveSelection(p)
	if err != nil {
		return View{}, err
	}

	needCountries := false
	for _, f := range selected {
		if f.Type == ordercontract.CheckoutFieldCountry {
			needCountries = true
			break
		}
	}
	if needCountries && len(countries) == 0 {
		return View{}, fmt.Errorf("结算表单缺少国家选项：构建期未注入国家清单（core.checkoutForm 消费 RenderContext.Checkout.Countries）")
	}

	overrides := make(map[string]Field, len(p.Fields))
	for _, f := range p.Fields {
		overrides[strings.TrimSpace(f.Key)] = f
	}

	groups := make([]GroupView, 0, 3)
	index := make(map[string]int, 3)
	for _, f := range selected {
		if f.Group == ordercontract.CheckoutGroupBilling && !p.CollectBilling {
			continue // 不收集账单地址：整组不渲染（账单缺省与收货地址相同）
		}
		gi, ok := index[f.Group]
		if !ok {
			gi = len(groups)
			index[f.Group] = gi
			groups = append(groups, GroupView{Key: f.Group, Title: groupTitleOf(f.Group)})
		}
		groups[gi].Fields = append(groups[gi].Fields, fieldViewOf(f, overrides[f.Key], countries, defaultCountry))
	}

	submit := strings.TrimSpace(p.SubmitLabel)
	if submit == "" {
		submit = DefaultSubmitLabel
	}
	hidden := []HiddenView{{Name: "projectId", Value: projectID}}
	if lang != "" {
		hidden = append(hidden, HiddenView{Name: "lang", Value: lang})
	}
	return View{
		Action:      FragmentPath,
		SubmitLabel: submit,
		Groups:      groups,
		Hidden:      hidden,
	}, nil
}

// fieldViewOf 单个字段 → 渲染视图（作者覆盖优先，清单默认兜底）。
func fieldViewOf(f ordercontract.CheckoutField, override Field, countries []core.CheckoutCountry, defaultCountry string) FieldView {
	label := f.Label
	if override.Label = strings.TrimSpace(override.Label); override.Label != "" {
		label = override.Label
	}
	// 必填只能加严：清单必填的字段在这里恒为 true（校验阶段已经拦住「从表单移除」，
	// 但直接构造 Props 的调用方仍可能把 Required 留空，这里再兜一次）。
	required := f.Required || override.Required
	view := FieldView{
		Key:         f.Key,
		Label:       label,
		Type:        string(f.Type),
		Required:    required,
		Placeholder: strings.TrimSpace(override.Placeholder),
		Wide:        f.Type == ordercontract.CheckoutFieldTextarea,
	}
	if f.Type == ordercontract.CheckoutFieldCountry {
		view.Options = countryOptionsOf(countries, defaultCountry)
	}
	return view
}

// countryOptionsOf 国家下拉选项（默认国家预选中；不在清单里时全都不选，浏览器取第一项）。
func countryOptionsOf(countries []core.CheckoutCountry, defaultCountry string) []OptionView {
	out := make([]OptionView, 0, len(countries))
	for _, c := range countries {
		code := strings.TrimSpace(c.Code)
		if code == "" {
			continue
		}
		label := strings.TrimSpace(c.Label)
		if label == "" {
			label = code // 装配层已经兜过一层，这里再兜：绝不输出空选项
		}
		out = append(out, OptionView{
			Code:     code,
			Label:    label,
			Selected: defaultCountry != "" && strings.EqualFold(code, defaultCountry),
		})
	}
	return out
}

// CompileCSS 导出样式编译（供 builder 的渲染分派调用）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：表单输出 hx-post /
// hx-target / hx-swap 三个属性，产物据此注入 htmx 与片段基座样式。
//
// 「不可用」提示分支（Notice）不输出任何 hx-*，因此一个字节都不该带。
// 原生 select 刻意**不**加 data-ui-select：那是控件基座的 JS 替身，而本组件的
// 访问面约束是零客户端 JS。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.Notice != "" {
		return nil, nil
	}
	return []string{"hx-post", "hx-target", "hx-swap"}, nil
}
