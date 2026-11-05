package core

import (
	"fmt"
	"reflect"
	"strings"

	"go_wp/pkg/upload"
)

func normalizeMediaProps(props any) error {
	return normalizeMediaValue(reflect.ValueOf(props), "")
}

func normalizeMediaValue(v reflect.Value, parent string) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return normalizeMediaValue(v.Elem(), parent)
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		for i := 0; i < v.Len(); i++ {
			if err := normalizeMediaValue(v.Index(i), fmt.Sprintf("%s[%d]", parent, i)); err != nil {
				return err
			}
		}
		return nil
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		spec := v.Type().Field(i)
		if !field.CanSet() {
			continue
		}
		name := strings.Split(spec.Tag.Get("json"), ",")[0]
		if name == "" {
			name = spec.Name
		}
		if name == "-" {
			continue
		}
		path := name
		if parent != "" {
			path = parent + "." + name
		}
		kind := strings.ToLower(strings.Split(spec.Tag.Get("ct"), ",")[0])
		if kind == "media" && field.Kind() == reflect.String {
			raw := field.String()
			if strings.TrimSpace(raw) == "" {
				field.SetString("")
				continue
			}
			resolved := upload.StorageURL(raw)
			if resolved == "" {
				return fmt.Errorf("媒体字段 %s 必须提供完整 HTTP(S) 地址；本地媒体需配置媒体基址", path)
			}
			field.SetString(resolved)
		} else if kind == "medialist" && field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.String {
			for j := 0; j < field.Len(); j++ {
				raw := field.Index(j).String()
				if strings.TrimSpace(raw) == "" {
					field.Index(j).SetString("")
					continue
				}
				resolved := upload.StorageURL(raw)
				if resolved == "" {
					return fmt.Errorf("媒体字段 %s[%d] 必须提供完整 HTTP(S) 地址；本地媒体需配置媒体基址", path, j)
				}
				field.Index(j).SetString(resolved)
			}
		} else if err := normalizeMediaValue(field, path); err != nil {
			return err
		}
	}
	return nil
}
