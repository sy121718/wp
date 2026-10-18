// product_form_text.go — 表单回显用的**文本化**（数值 → 输入框 value 字符串）。
//
// 为什么在 service：输入框里的初值是「业务数据的一个表示」而不是业务数据本身，
// 换算规则必须只有一份 —— 控制器各写一遍 strconv，就会在两屏之间出现
// 「一处 12.50、一处 12.5」这类差异（同一个商品，两个页面看起来价格不同）。
//
// 与展示口径分开：列表展示用 pkg/money.FormatYuanOrDash（空显示为 —），— 是**展示**符号，
// 填进 input 的 value 会变成一个待提交的非法值。
package productservice

import (
	"strconv"

	productdto "go_wp/internal/module/product/dto"
)

// NullableNumberText 可空数值 → 输入框文本（未设置给空串，不是 0）。
func NullableNumberText(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// FormSortText 排序号 → 输入框文本（属性值行的 sort 列）。
func FormSortText(v int) string { return strconv.Itoa(v) }

// AttrValueFormRows 属性值行 → 属性值编辑片段的表单行（序号 + 排序号的文本化都在这里）。
//
// 序号是**本次渲染的位次**而不是业务数据，所以由本函数给出；排序号是业务数据，
// 只负责转成输入框文本（见 FormSortText）。
func AttrValueFormRows(rows []productdto.AttributeValueResp) []productdto.AttrValueFormRow {
	out := make([]productdto.AttrValueFormRow, 0, len(rows))
	for i, row := range rows {
		out = append(out, productdto.AttrValueFormRow{
			Index: i, ID: row.ID, Key: row.Key, Label: row.Label,
			Sort: FormSortText(row.Sort), Enabled: row.Enabled,
		})
	}
	return out
}
