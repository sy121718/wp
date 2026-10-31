package adminui

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

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
