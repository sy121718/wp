package builder

import (
	"reflect"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestAltPropsMustBeTranslatable(t *testing.T) {
	checked := 0
	for _, typeName := range core.Types() {
		comp, err := core.Lookup(typeName)
		if err != nil || comp == nil {
			continue
		}
		spec, ok := comp.(core.SpecProvider)
		if !ok || spec.PropsSpec() == nil {
			continue
		}
		rv := reflect.ValueOf(spec.PropsSpec())
		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				continue
			}
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			continue
		}
		declared := map[string]bool{}
		if tp, ok := comp.(core.TranslatableProvider); ok {
			for _, f := range tp.Translatable() {
				declared[f] = true
			}
		}
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if field.Type.Kind() != reflect.String {
				continue
			}
			jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
			if jsonName == "" || !strings.HasSuffix(strings.ToLower(jsonName), "alt") {
				continue
			}
			checked++
			if !declared[jsonName] {
				t.Errorf("%s 的 Props.%s（json=%s）是文本字段却没声明可翻译：英文站点上它会保持中文",
					typeName, field.Name, jsonName)
			}
		}
	}

	if checked == 0 {
		t.Fatal("没有扫到任何 alt 字段，判据可能已失效（检查 json 标签命名）")
	}
}
