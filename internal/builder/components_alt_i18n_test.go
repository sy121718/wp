package builder

// components_alt_i18n_test.go — alt 类文本字段的可翻译覆盖检查（审计 I18N-008）。
//
// 为什么值得一条机制测试：Translatable 白名单是**手工维护**的清单，新增组件、或给已有
// 组件加一个 alt 时没人会记得回来补一行，而症状是「英文站的图片 alt 还是中文」——
// 只在非默认语言站点上可见，默认语言站点完全看不出问题，评审时几乎不可能发现。
// infobox 的 mediaAlt 就是这么漏的（image / gallery 声明了，它没有）。
//
// 判据刻意宽松：只看 json 名以 alt 结尾的 string 字段。宁可漏判结构字段，
// 也不要把样式字段误报成文案 —— 误报会让这条测试被当噪音关掉，那才是最坏的结果。

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
	// 一条都没扫到说明判据失效（字段改名 / 标签写错），测试会变成永远通过的空壳。
	if checked == 0 {
		t.Fatal("没有扫到任何 alt 字段，判据可能已失效（检查 json 标签命名）")
	}
}
