package core

import (
	"fmt"
	"go_wp/internal/builder/source"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

type TranslatableProvider interface {
	Translatable() []string
}

var translatableFieldRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

func ValidateTranslatable(spec any, fields []string) (err error) {
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !translatableFieldRe.MatchString(f) {
			return fmt.Errorf("字段名 %q 非法（仅允许字母/数字/下划线，不含 . 与 [ ]）", f)
		}
		if seen[f] {
			return fmt.Errorf("字段名 %q 重复声明", f)
		}
		seen[f] = true
	}
	if spec == nil || len(fields) == 0 {
		return nil
	}
	names := jsonFieldNames(spec)
	for _, f := range fields {
		if !names[f] {
			return fmt.Errorf("字段 %q 不在组件 Props 中（白名单拼错即拒绝，docs/06-D §7.5 规则 2）", f)
		}
	}
	return nil
}

func ContentContextFor(entityType, field string) string {
	return source.ContentContextFor(entityType, field)
}

func jsonFieldNames(spec any) map[string]bool {
	out := map[string]bool{}
	collectJSONFieldNames(reflect.TypeOf(spec), out, map[reflect.Type]bool{})
	return out
}

func collectJSONFieldNames(t reflect.Type, out map[string]bool, seen map[reflect.Type]bool) {
	for t != nil && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice ||
		t.Kind() == reflect.Array || t.Kind() == reflect.Map) {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || seen[t] || t == reflect.TypeOf(time.Time{}) {
		return
	}
	seen[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := translatableJSONFieldName(f)
		if name == "" {
			continue
		}
		out[name] = true
		collectJSONFieldNames(f.Type, out, seen)
	}
}

func translatableJSONFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if i := strings.Index(tag, ","); i >= 0 {
		tag = tag[:i]
	}
	if tag == "-" {
		return ""
	}
	if tag != "" {
		return tag
	}
	return f.Name
}

func TranslatableFields(typeName string) map[string]bool {
	translatableCacheMu.RLock()
	cached, ok := translatableCache[typeName]
	translatableCacheMu.RUnlock()
	if ok {
		return cached
	}
	comp, err := Lookup(typeName)
	if err != nil {
		return nil
	}
	tp, ok := comp.(TranslatableProvider)
	if !ok {
		return nil
	}
	fields := tp.Translatable()
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[f] = true
	}
	translatableCacheMu.Lock()
	translatableCache[typeName] = out
	translatableCacheMu.Unlock()
	return out
}

var (
	translatableCacheMu sync.RWMutex
	translatableCache   = map[string]map[string]bool{}
)

func resetTranslatableFields(typeName string) {
	translatableCacheMu.Lock()
	delete(translatableCache, typeName)
	translatableCacheMu.Unlock()
}
