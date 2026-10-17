// product_form_bind.go — 表单字段的**绑定与归一化原语**（HTML 表单路径专用，显式、无反射）。
package productdto

// 处理链（dd.txt §7.1）：表单 / JSON 解析 → **字段归一化（本文件）** → DTO 业务输入校验
// （service）→ Service 用例。
//
// 为什么只给「少量显式方法」而不是一个反射型绑定框架：本模块的表单字段各有**不同的**绑定
// 语义（空串算不填还是算清空、0 是否有效、非法数字是报错还是按未填处理），反射框架只能表达
// 一种统一口径 —— 那正是这批要消除的模糊。显式方法把每个字段的语义写在注释里，并由
// product_form_bind_test.go 逐条钉住；调用点读一行就知道这个字段的规矩。
//
// 三条铁律（每个 helper 都遵守）：
//
//	① **未提交 与 空串 / 纯空白 等价** —— 浏览器在输入框为空时根本不提交该字段，
//	   表单路径无法区分两者，所以空串一律按「这次没填」处理，绝不解释成「明确填了空」；
//	② **"0" 是有效值**，返回指向 0 的指针（与 JSON 里显式写 0 同义），永不与「未提交」混同 ——
//	   这正是字段用 *float64 / *int 而不是 float64 / int 的理由；
//	③ **非法数字只报事实，不决定文案**：返回 *FieldValueError（字段名 + 种类），
//	   由 HTTP 层按该字段的既有分档决定「报错」还是「按未填处理」（各调用点注释写明）。
//	   NaN / 正负无穷一律算非法（不是数字，进了金额运算会污染整批数据）。
//
// **不在这里做的事**（做了就是第二份真相，迟早与 service 分叉）：
//   - 精度与上限：金额的小数位由 money 包与列类型决定，值域由各 service 用例判定；
//   - 跨字段业务约束（如 minPrice <= maxPrice、minTotalQty <= maxTotalQty）：一律归 service；
//     本文件只表达「同一组表单控件之间的**绑定**约束」（例如开关没勾时数量框不提交这个 HTML 事实）。
//     商品建表单迁到 JSON 写基座后，「跟踪数量」开关与数量框的绑定归到了 service.Create
//     （JSON 里两种意图都表现为 quantity 缺失，区别只在 TrackQuantity 上），本文件不再有那一档。

import (
	"math"
	"strconv"
	"strings"
)

// FieldKind 字段原始值不合法的种类（**类型事实**，不是文案）。
//
// 与 pkg/bulkids 的 IDsError 同一取舍：判据是类型而不是比对错误原文 ——
// 上游改措辞或换语言时，按文案比对的判定会静默失配。
type FieldKind string

const (
	// FieldKindNumber 不是一个可用的十进制数字（含 NaN / 正负无穷 / 空串以外的一切垃圾）。
	FieldKindNumber FieldKind = "number"
	// FieldKindRange 数字本身合法，但落在该字段允许的区间外（如负数数量）。
	FieldKindRange FieldKind = "range"
)

// FieldValueError 某个表单字段的原始值不合法（属于**绑定期**错误，不是业务规则错误）。
//
// Error() 的原文只用于日志与测试断言：它不是可展示文案，HTTP 层按字段挑受控词条
// （如产品新建页的「数量必须是非负整数」）。
type FieldValueError struct {
	// Field 表单字段名（与模板里的 name 属性逐字一致，便于定位）。
	Field string
	// Kind 不合法的种类。
	Kind FieldKind
}

func (e *FieldValueError) Error() string {
	if e == nil {
		return ""
	}
	return "form field " + e.Field + ": " + string(e.Kind)
}

// FormText 文本字段归一：去首尾空白。
//
// 未提交与空串等价（都得到 ""），调用方据此决定「空 = 不改」还是「空 = 清空」：
// 那属于该字段的业务语义（见 UpdateReq 各字段注释），本函数不替它选。
func FormText(raw string) string {
	return strings.TrimSpace(raw)
}

// OptFloat 可选数值字段（金额 / 重量 / 价格）。
//
//	未提交 / 空串 / 纯空白 → (nil, nil)    与 JSON 省略该字段同义（本次不改）
//	"0"                    → (&0, nil)     0 是有效值，不是缺省
//	负数 / 超出精度         → 原样返回       是否允许由 service 按字段判定（价格不许、调价差额可以）
//	非法 / NaN / ±Inf       → (nil, *FieldValueError{Kind: FieldKindNumber})
//
// field 是表单字段名，只用于错误事实里的定位信息。
func OptFloat(field, raw string) (*float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || isNotFinite(v) {
		return nil, &FieldValueError{Field: field, Kind: FieldKindNumber}
	}
	return &v, nil
}

// OptInt 可选整数字段：语义与 OptFloat 逐条对应（空 → nil，"0" → &0，非法 → FieldValueError）。
//
// 只接受十进制整数：小数（"1.5"）判非法，而不是悄悄截断 —— 截断会让「填了 1.5 个」
// 在页面上看起来成功，实际存了 1。
func OptInt(field, raw string) (*int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, &FieldValueError{Field: field, Kind: FieldKindNumber}
	}
	return &v, nil
}

// FormInt 非可选整数字段：空串 → def；非法 → (def, *FieldValueError)。
//
// 与 OptInt 的分工：用于「字段总会有一个值，读不出来时用既有默认值」的输入框
// （如捆绑配置的 minTotalQty / maxTotalQty：非法一律 0，由 service 的配置校验统一给出
// 可读原因 —— 前端只负责搬运，不在这里造第二套报错）。
func FormInt(field, raw string, def int) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return def, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def, &FieldValueError{Field: field, Kind: FieldKindNumber}
	}
	return v, nil
}

// isNotFinite NaN 与正负无穷：都不是可参与金额 / 数量运算的数字，因此与「不是数字」同档。
//
// 单列一个函数而不是在每个调用点写两次 math.Is*：漏掉一次的表现是 NaN 悄悄进了金额，
// 而 NaN 参与的每一次比较都是 false —— 后面所有的「小于 0 就拒绝」都不再拦得住它。
func isNotFinite(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0)
}
