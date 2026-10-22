package pagination

// url.go — 分页链接的**取址**：把已知事实串成 href。
//
// 这两个函数从 internal/templates/funcs.go 搬来。搬的理由不是「更通用」，而是
// 「谁该拥有这段算法」：窗口与取址是同一个整体（窗口说出要第 3 页，取址负责把第 3 页
// 写成串），拆在两个包里时任何一方改口径都要跨包对齐。将来访客侧的商品筛选、站内搜索
// 片段也要分页，那时反向依赖 internal/templates 是不允许的（pkg 不能 import internal）。
//
// 仍然保持**纯函数**：不读请求上下文、不看当前语言、不查库。语言相关的取词不在这里。

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

// QueryURL 把成对的键值串到基地址后面：QueryURL("/admin/x", "page", 2, "limit", 20)。
//
// 值**不预转义** —— Jet 渲染时会转义恰好一次。Go 侧若先 url.QueryEscape / html.EscapeString，
// 页面上就会出现 `&amp;amp;`：不报错、不 500，只是筛选链接静默失效（这是分页最容易坏的地方）。
// 键与值各自做 QueryEscape 是正确的（那是 URL 编码，不是 HTML 转义）。
//
// base 为空（含全空白）回空串：调用点据此不渲染按钮，而不是渲染出 `?page=2` 这种半截链接。
// 末尾落单的键直接忽略（写成奇数个参数是调用点笔误，但没必要因此 500）。
// 键不是字符串时 **panic** —— 静默丢一个筛选条件，表现是「用户点下一页后筛选没了」，
// 比 500 难发现得多；panic 会被模板层转成渲染错误，当场暴露。
func QueryURL(base string, kv ...any) string {
	if strings.TrimSpace(base) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(base)
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			panic(fmt.Errorf("QueryURL：键必须是字符串（页码参数名），实际 %T（值 %v）", kv[i], kv[i]))
		}
		b.WriteString(sep)
		sep = "&"
		b.WriteString(url.QueryEscape(key))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(DisplayString(kv[i+1])))
	}
	return b.String()
}

// PageBase 把请求串压成「翻页基址」：保留全部筛选参数，只丢掉分页自己会写回的参数。
//
// 为什么需要它：分页链接是在**当前请求串**后面追加 page/limit，而那个串里已经带着
// page/limit —— 直接追加会出现两个同名参数，浏览器取第一个，症状是「点了翻页没反应」。
// 去掉之后再追加，语义与旧实现（控制器先拼好不含分页参数的基址）一致：
// 同样走 url.Values.Encode 的字典序。
//
// raw 为空或解析失败时返回空串（调用点据此不渲染分页条）。drop 里写错的键名不报错：
// 它只是「本来就不该保留的参数」，多删一个不存在的键没有副作用。
func PageBase(raw string, drop ...string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, k := range drop {
		q.Del(k)
	}
	u.RawQuery = q.Encode()
	if u.RawQuery == "" {
		return u.Path
	}
	return u.Path + "?" + u.RawQuery
}

// DisplayString 把任意值收敛成链接里用的展示串（nil → 空串，其余走 fmt.Sprint）。
//
// 单独导出而不是内联 fmt.Sprint：**typed nil 也要当空串**。可空字段（*int64 / *time.Time）
// 走 fmt.Sprint 会打出 `<nil>`，链接里就成了 `?page=<nil>`。分页入参本身不会是可空字段，
// 但 QueryURL 是公开的，调用点不该因为传了个可空值就产出坏链接。
func DisplayString(v any) string {
	if v == nil {
		return ""
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		if rv.IsNil() {
			return ""
		}
	}
	return fmt.Sprint(v)
}
