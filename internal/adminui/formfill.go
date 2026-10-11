package adminui

// formfill.go — 表单回显的唯一实现：按结构体字段从表单值填。
//
// 为什么需要它：失败后原地留住输入（见 internal/templates/CLAUDE.md §写表单失败时原地留住输入）
// 原先靠两样东西 —— 一份**手写的字段名数组**（`productCreateFormFields`）加一个按名字补零值键的助手。
// 那份数组是「模板与 handler 之间的协议」，漏一个字段的后果不是「少显示一格」，而是渲染期读不到键 →
// 整块片段 500（htmx 对 5xx 不 swap，用户点了保存**什么都看不到**）。
//
// 换成本函数之后，字段清单就是**视图结构体本身**：漏字段在编译期不存在（模板读的字段要么在结构体里，
// 要么渲染测试当场红）。原来的 `FormEcho / FormEchoChecked / FormEchoMulti` 三个前缀键也随之取消 ——
// 那套前缀是为了在 gin.H 的**单一键空间**里躲开页面自己的键（如列表页的 `Form` 结构体）；
// 结构体视图天然分空间，不需要前缀。
//
// 取值口径（`*T` 与 `T` 的区别是这条机制的关键）：
//   - string        ← 表单值（缺键即空串）
//   - bool          ← 键存在且值为 1/on/true/yes 即 true（复选框形态）
//   - int/int64/float64 ← 解析失败即零值（校验属 service，这里不报错）
//   - []string      ← 同名多值，保序（多选 / 批量勾选）
//   - *string / *int / *int64 / *float64 ← **键不存在则留 nil**，键存在但空则零值
//     （「没填数量」与「明确填了 0」是两回事，见 product 迁移 261 的口径）

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// FillForm 按视图结构体的字段从表单值回填（导出字段才填）。
//
// 字段名默认取字段名转小驼峰（`WarehouseSKU` → `warehouseSKU`），可用 `form:"名字"` 标签覆盖。
// view 必须是**非 nil 的结构体指针**：传错是编程错误，直接 panic（写对了永不触发，写错了第一次跑到就炸）。
func FillForm(view any, values url.Values) {
	rv := reflect.ValueOf(view)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		panic("adminui.FillForm: 视图必须是非 nil 的结构体指针（表单回显按字段填，不要手工拼 map）")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		panic("adminui.FillForm: 视图必须是结构体指针，收到 " + rv.Kind().String())
	}

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		name := formFieldName(field)
		if name == "-" {
			continue
		}
		fillField(rv.Field(i), name, values)
	}
}

// formFieldName 解析字段的表单名：优先 `form:"名字"` 标签，否则字段名首字母小写。
//
// **含连续大写的字段名必须写标签**（`ProjectID` / `WarehouseSKU` / `AttributeIDs`）：这类名字
// 转表单名是有歧义的（`projectId`？`projectID`？），而歧义猜错的后果是**静默**的 —— 回填读不到值，
// 用户填的内容在失败后丢光，不报错、测试也可能不红。所以这里直接 panic 要求写清标签，
// 与 ViewMap / FillForm 的其它编程错误同一判据：写对了永不触发，写错了第一次跑到就炸。
func formFieldName(field reflect.StructField) string {
	name := field.Tag.Get("form")
	if name != "" {
		return name
	}
	if hasAcronymRun(field.Name) {
		panic("adminui.FillForm: 字段 " + field.Name + " 含连续大写，表单名有歧义 —— " +
			`请写 form:"xxx" 标签（默认推断只处理首字母小写这一种）`)
	}
	if field.Name == "" {
		return ""
	}
	return strings.ToLower(field.Name[:1]) + field.Name[1:]
}

// hasAcronymRun 是否含连续两个大写字母（缩写形态）。
func hasAcronymRun(name string) bool {
	run := 0
	for i := 0; i < len(name); i++ {
		if name[i] >= 'A' && name[i] <= 'Z' {
			run++
			if run >= 2 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

func fillField(dst reflect.Value, name string, values url.Values) {
	raw, present := values[name]

	switch dst.Kind() {
	case reflect.String:
		dst.SetString(firstValue(raw))
	case reflect.Bool:
		dst.SetBool(truthy(firstValue(raw)))
	case reflect.Int, reflect.Int64:
		dst.SetInt(parseInt(firstValue(raw)))
	case reflect.Float64:
		dst.SetFloat(parseFloat(firstValue(raw)))
	case reflect.Slice:
		if dst.Type().Elem().Kind() != reflect.String {
			panic("adminui.FillForm: 只支持 []string 切片字段，字段 " + name)
		}
		dst.Set(reflect.ValueOf(append([]string(nil), raw...)))
	case reflect.Pointer:
		fillPointer(dst, name, raw, present)
	default:
		panic("adminui.FillForm: 不支持的字段类型 " + dst.Kind().String() + "（字段 " + name + "）")
	}
}

// fillPointer 指针字段：键不存在留 nil（「没填」与「填了空/0」是两回事）。
func fillPointer(dst reflect.Value, name string, raw []string, present bool) {
	if !present {
		return
	}
	ptr := reflect.New(dst.Type().Elem())
	switch ptr.Elem().Kind() {
	case reflect.String:
		ptr.Elem().SetString(firstValue(raw))
	case reflect.Int, reflect.Int64:
		ptr.Elem().SetInt(parseInt(firstValue(raw)))
	case reflect.Float64:
		ptr.Elem().SetFloat(parseFloat(firstValue(raw)))
	case reflect.Bool:
		ptr.Elem().SetBool(truthy(firstValue(raw)))
	default:
		panic("adminui.FillForm: 不支持的指针字段类型 " + ptr.Elem().Kind().String() + "（字段 " + name + "）")
	}
	dst.Set(ptr)
}

func firstValue(raw []string) string {
	if len(raw) == 0 {
		return ""
	}
	return raw[0]
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

func parseInt(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseFloat(v string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0
	}
	return f
}
