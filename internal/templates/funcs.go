// Package templates — Jet 模板全局函数注入。
//
// 三个 Set（后台/工作台/组件）统一注入这些函数，模板里直接调用。
// 保持纯函数、无副作用，便于确定性构建与测试。
package templates

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/CloudyKit/jet/v6"
)

// injectGlobals 给 Jet Set 注入全局函数（所有 Set 共用同一套）。
func injectGlobals(set *jet.Set) {
	set.AddGlobalFunc("formatNumber", formatNumber)
	set.AddGlobalFunc("formatBytes", formatBytes)
	set.AddGlobalFunc("assetURL", assetURL)
}

// formatNumber 千分位格式化：1234567 → "1,234,567"
// 支持 int/float/数字字符串；非法输入原样返回。
func formatNumber(a jet.Arguments) reflect.Value {
	if !a.IsSet(0) {
		return reflect.ValueOf("")
	}
	v := a.Get(0)
	if !v.IsValid() {
		return reflect.ValueOf("")
	}
	// 解引用 nil 指针/interface（防御：避免解引用 panic）。
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.ValueOf("")
		}
		v = v.Elem()
	}
	var n int64
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n = v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := v.Uint()
		if u > math.MaxInt64 {
			return reflect.ValueOf(thousandsUint(u)) // uint 溢出 int64 时独立格式化
		}
		n = int64(u)
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return v // NaN/Inf 无法格式化，原样返回
		}
		n = int64(f)
	case reflect.String:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v.String()), 10, 64)
		if err != nil {
			return v // 非数字字符串原样返回
		}
		n = parsed
	default:
		return v
	}
	return reflect.ValueOf(thousands(n))
}

// formatBytes 文件大小格式化：1536 → "1.5 KB"（1000 进制，简洁可读）。
func formatBytes(a jet.Arguments) reflect.Value {
	if !a.IsSet(0) {
		return reflect.ValueOf("")
	}
	v := a.Get(0)
	if !v.IsValid() {
		return reflect.ValueOf("")
	}
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.ValueOf("")
		}
		v = v.Elem()
	}
	var n float64
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n = float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n = float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		n = v.Float()
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return v
		}
	default:
		return v
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for n >= 1000 && i < len(units)-1 {
		n /= 1000
		i++
	}
	if i == 0 {
		return reflect.ValueOf(fmt.Sprintf("%d B", int64(n)))
	}
	return reflect.ValueOf(fmt.Sprintf("%.1f %s", n, units[i]))
}

// assetURL 媒体地址生成（占位实现，待接入 media 契约 resolver）。
// 签名：assetURL(assetID, variant)。当前仅返回 assetID 原值，后续接入 builder/media。
func assetURL(a jet.Arguments) reflect.Value {
	if !a.IsSet(0) {
		return reflect.ValueOf("")
	}
	return a.Get(0)
}

// thousandsUint uint64 千分位（无符号，避免 int64 溢出）。
func thousandsUint(n uint64) string {
	return thousandsFromStr(strconv.FormatUint(n, 10))
}

// thousands 整数千分位。
func thousands(n int64) string {
	return thousandsFromStr(strconv.FormatInt(n, 10))
}

// thousandsFromStr 对已格式化的数字串加千分位分隔。
func thousandsFromStr(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
