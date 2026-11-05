package view

import (
	"reflect"
)

const paginationKey = PaginationKey

// MapOf 把视图结构投影成模板可用的 map。
//
// 纯结构映射：只处理结构体、结构体指针、map[string]any 与 gin.H 形态，
// 不做权限/菜单查询，不解释业务语义。标签 key 为 `view`。
func MapOf(v any) map[string]any {
	switch value := v.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return value
	}

	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return map[string]any{}
		}
		rv = rv.Elem()
	}

	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = iter.Value().Interface()
		}
		return out
	}

	if rv.Kind() != reflect.Struct {
		panic("view.MapOf: 视图必须是结构体、结构体指针或 map[string]any，收到 " + rv.Kind().String())
	}
	return mapOfStruct(rv)
}

// AttachPagination 把视图自带的 Page 注入到 Pagination 键（纯事实投影）。
func AttachPagination(data map[string]any, v any) {
	if data == nil {
		return
	}
	page, ok := PageListOf(v)
	if !ok {
		return
	}
	if _, exists := data[paginationKey]; exists {
		return
	}
	data[paginationKey] = page
}

func mapOfStruct(rv reflect.Value) map[string]any {
	rt := rv.Type()
	out := make(map[string]any, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Anonymous {
			ev := rv.Field(i)
			for ev.Kind() == reflect.Pointer {
				if ev.IsNil() {
					ev = reflect.Value{}
					break
				}
				ev = ev.Elem()
			}
			if ev.IsValid() && ev.Kind() == reflect.Struct {
				for k, v := range mapOfStruct(ev) {
					if _, exists := out[k]; !exists {
						out[k] = v
					}
				}
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		name := field.Name
		if tag := field.Tag.Get("view"); tag != "" {
			name = tag
		}
		out[name] = rv.Field(i).Interface()
	}
	return out
}
