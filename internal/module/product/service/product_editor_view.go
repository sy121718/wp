package productservice

// product_editor_view.go — 商品新建 / 编辑 / 详情三页的**派生值**（页面要哪些字段、怎么填）。
//
// 控制器只做绑定 → 调读模型 → 出口；派生值放这里是因为它由业务数据决定（选中哪一档状态、
// 哪些属性组被引用、输入框回填什么文本），而不是由这一屏怎么显示决定 —— 显示形态（徽标、
// 文案、格式）在模板。

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// ProductListPath 商品列表页路径（详情页 / 编辑页的返回地址都以它打底）。
const ProductListPath = "/admin/products"

// ProductListFilterURL 列表页的筛选基地址（**不含** page/limit）。
//
// 只服务「返回列表」这类链接；分页条的地址由模板自己从请求串压出来（pageBase 去掉 page/limit），
// 服务端不再参与。
func ProductListFilterURL(projectID, keyword, status string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if k := strings.TrimSpace(keyword); k != "" {
		q.Set("keyword", k)
	}
	if s := strings.TrimSpace(status); s != "" {
		q.Set("status", s)
	}
	if enc := q.Encode(); enc != "" {
		return ProductListPath + "?" + enc
	}
	return ProductListPath
}

// ProductStatusOptions 编辑页状态下拉的**值域与顺序**（含「全部状态」这一档）。
//
// 显示名由模板按取值取词：后端带成品串就会把这一句锁死在一种语言，英文界面上仍是中文，
// 不报错、门禁也不红。状态维度只有这一种入口（不另配徽章筛选 —— 同一维度两套控件时，
// 用户会怀疑它们是否等价）。
func ProductStatusOptions(current string) []productdto.StatusOptionResp {
	values := []string{"", productenums.StatusDraft, productenums.StatusPublished, productenums.StatusArchived}
	out := make([]productdto.StatusOptionResp, 0, len(values))
	current = strings.TrimSpace(current)
	for _, value := range values {
		out = append(out, productdto.StatusOptionResp{Value: value, Selected: value == current})
	}
	return out
}

// NullableNumberText 可空数值 → 输入框文本（未设置给空串，不是 0）。
//
// 与列表展示用的 pkg/money.FormatYuanOrDash 分开：— 是展示符号，填进 input 的 value 会变成
// 一个待提交的非法值。
func NullableNumberText(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// AttributeChecks 属性组勾选列表：给每组补一个 Checked（商品是否引用它）。
func AttributeChecks(options []productdto.AttributeOptionResp, selected []string) []productdto.AttributeCheckOptionResp {
	out := make([]productdto.AttributeCheckOptionResp, 0, len(options))
	for _, o := range options {
		out = append(out, productdto.AttributeCheckOptionResp{
			AttributeOptionResp: o,
			Checked:             slices.Contains(selected, o.ID),
		})
	}
	return out
}

// FillProductEditFormEcho 编辑表单的回显值（状态选中项、属性组勾选态、图集与数值字段的文本形态、
// 捆绑容器标记）。
//
// 商品读不出来时页面仍要完整：状态按「全部状态」那一档给默认值，其余回显位保持零值，
// 模板据此显示空表单而不是中断。
func FillProductEditFormEcho(view *productdto.ProductEditPageResp) {
	view.Statuses = ProductStatusOptions("")
	if !view.HasProduct || view.Detail == nil {
		return
	}
	view.Statuses = ProductStatusOptions(view.Detail.Status)
	// 候选读失败：撤掉保存入口（EditBlocked），勾选态保持为空 ——「选择器缺失」与「用户主动
	// 取消了全部引用」在 POST 上同形，读故障时不能装作后者。
	if view.AttributeOptionsErr != nil {
		view.EditBlocked = true
		return
	}
	view.AttributeChecks = AttributeChecks(view.AttributeOptions, view.Detail.AttributeIDs)
	view.ImagesInput = strings.Join(view.Detail.Images, "\n")
	view.ImageAltsInput = strings.Join(view.Detail.ImageAlts, "\n")
	view.WeightInput = NullableNumberText(view.Detail.Weight)
	view.DefaultPriceInput = NullableNumberText(view.Detail.DefaultPrice)
}
