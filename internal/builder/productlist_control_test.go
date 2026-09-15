package builder

// productlist_control_test.go — productList 复杂配置项的结构化控件（审计 EDT-007）。
//
// 此前 filterTagIds / priceRanges 是「一行逗号分隔字符串」：作者要记住
// `0-199,200-399,799+` 的写法、id 要去别处查、写错要等构建期才报错。
//
// 本用例守两件事：
//  1. 这两个字段确实换成了结构化控件（multientityref / rangelist）；
//  2. **Go 字段类型仍是 string** —— 控件只改编辑方式，存储格式不变。
//     这一条才是关键：格式一变，既存页面文档就要迁移，构建期解析也得跟着改。

import (
	"reflect"
	"strings"
	"testing"

	productlist "go_wp/internal/builder/components/productlist"
	"go_wp/internal/builder/core"
)

// TestProductListComplexConfigUsesStructuredControls 复杂配置项已换成结构化控件。
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

	// 值型不变：控件是编辑方式，不是存储格式。
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

// TestMultiEntityRefRequiresEntityKind 多选实体控件必须声明实体类型。
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
