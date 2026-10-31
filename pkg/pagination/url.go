package pagination

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

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
