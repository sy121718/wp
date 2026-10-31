package builder

import (
	"reflect"
	"strings"
	"testing"

	productlist "go_wp/internal/builder/components/productlist"
	"go_wp/internal/builder/core"
)

func TestProductListComplexConfigUsesStructuredControls(t *testing.T) {
	props := (&productlist.Component{}).PropsSpec()
	controls, err := core.ParseControls(props)
	if err != nil {
		t.Fatalf("解析控件 schema 失败: %v", err)
	}
	byKey := map[string]core.Control{}
	for _, c := range controls {
		byKey[c.Key] = c
	}

	tags, ok := byKey["filterTagIds"]
	if !ok {
		t.Fatalf("未找到 filterTagIds 控件")
	}
	if tags.Kind != core.ControlMultiEntityRef {
		t.Fatalf("filterTagIds 应为多选实体控件，实际 %s", tags.Kind)
	}
	if len(tags.Options) == 0 || tags.Options[0].Value != "tag" {
		t.Fatalf("filterTagIds 应声明实体类型 tag，实际 %v", tags.Options)
	}

	ranges, ok := byKey["priceRanges"]
	if !ok {
		t.Fatalf("未找到 priceRanges 控件")
	}
	if ranges.Kind != core.ControlRangeList {
		t.Fatalf("priceRanges 应为区间列表控件，实际 %s", ranges.Kind)
	}

	typ := reflect.TypeOf(props)
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	for _, field := range []string{"FilterTagIDs", "PriceRanges"} {
		f, found := typ.FieldByName(field)
		if !found {
			t.Fatalf("未找到字段 %s", field)
		}
		if f.Type.Kind() != reflect.String {
			t.Fatalf("字段 %s 应仍是 string（读写兼容），实际 %s", field, f.Type.Kind())
		}
	}
}

func TestMultiEntityRefRequiresEntityKind(t *testing.T) {
	type badProps struct {
		Broken string `json:"broken" ct:"multientityref,label=标签"`
	}
	if _, err := core.ParseControls(&badProps{}); err == nil {
		t.Fatalf("multientityref 未声明实体类型时应报错")
	} else if !strings.Contains(err.Error(), "multientityref") {
		t.Fatalf("错误信息应指出 multientityref，实际: %v", err)
	}
}

func TestProductListMultiValueFilterControls(t *testing.T) {
	props := (&productlist.Component{}).PropsSpec()
	controls, err := core.ParseControls(props)
	if err != nil {
		t.Fatalf("解析控件 schema 失败: %v", err)
	}
	byKey := map[string]core.Control{}
	for _, c := range controls {
		byKey[c.Key] = c
	}
	for field, entity := range map[string]string{
		"filterCategoryIds": "category",
		"filterBrandIds":    "brand",
		"filterTagIds":      "tag",
	} {
		ctrl, ok := byKey[field]
		if !ok {
			t.Fatalf("未找到 %s 控件", field)
		}
		if ctrl.Kind != core.ControlMultiEntityRef {
			t.Fatalf("%s 应为多选实体控件，实际 %s", field, ctrl.Kind)
		}
		if len(ctrl.Options) == 0 || ctrl.Options[0].Value != entity {
			t.Fatalf("%s 应声明实体类型 %s，实际 %v", field, entity, ctrl.Options)
		}
	}

	typ := reflect.TypeOf(props)
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	for _, field := range []string{"FilterCategoryIDs", "FilterBrandIDs", "FilterTagIDs"} {
		f, found := typ.FieldByName(field)
		if !found {
			t.Fatalf("未找到字段 %s", field)
		}
		if f.Type.Kind() != reflect.String {
			t.Fatalf("字段 %s 应仍是 string（读写兼容），实际 %s", field, f.Type.Kind())
		}
	}
}
