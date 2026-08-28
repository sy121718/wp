// Package unit 声明式 Controls（docs/02-C3）引擎的单测。
package unit

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/builder/components/heading"
	"go_wp/internal/builder/core"
)

// ctrlSample 带 ct tag 的测试 props 模板。
type ctrlSample struct {
	Title  string `json:"title" ct:"text,maxlen=10"`
	Level  string `json:"level" ct:"select,h1,h2,h3,default=h2"`
	Shade  string `json:"shade" ct:"safe,maxlen=50"`
	Times  int    `json:"times" ct:"int,min=1,max=5"`
	Enable bool   `json:"enable" ct:"bool"`
	Code   string `json:"code" ct:"regex" ctRegex:"^[a-z]{2,4}$"`
}

// TestParseControls 解析 ct tag：生成控件描述符表（含默认值与选项）。
func TestParseControls(t *testing.T) {
	controls, err := core.ParseControls(&ctrlSample{})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(controls) != 6 {
		t.Fatalf("控件数异常: %d", len(controls))
	}
	byKey := map[string]core.Control{}
	for _, c := range controls {
		byKey[c.Key] = c
	}
	if c := byKey["level"]; c.Kind != core.ControlSelect || c.Default != "h2" || len(c.Options) != 3 {
		t.Errorf("Level 控件异常: %+v", c)
	}
	if c := byKey["times"]; c.Kind != core.ControlInt || c.Min != 1 || c.Max != 5 {
		t.Errorf("Times 控件异常: %+v", c)
	}
	if c := byKey["code"]; c.Kind != core.ControlRegex || c.Pattern != "^[a-z]{2,4}$" {
		t.Errorf("Code 控件异常: %+v", c)
	}
}

// TestValidateSpec 反射校验：各控件类型的合法/非法值。
func TestValidateSpec(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want string // 空 = 期望通过
	}{
		{"合法整体", `{"title":"标题","level":"h3","shade":"#fff","times":3,"enable":true,"code":"abc"}`, ""},
		{"文本超长", `{"title":"一二三四五六七八九十十一"}`, "超长"},
		{"select 非法选项", `{"level":"h9"}`, "不在选项内"},
		{"safe 注入", `{"shade":"red}body{x:1}"}`, "值非法"},
		{"int 低于下限", `{"times":-1}`, "小于下限"},
		{"int 超上限", `{"times":9}`, "超出上限"},
		{"regex 不匹配", `{"code":"abcd1"}`, "不匹配模式"},
		{"空值+有默认放行", `{"level":""}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p ctrlSample
			if err := json.Unmarshal([]byte(tc.val), &p); err != nil {
				t.Fatalf("JSON 解析失败: %v", err)
			}
			err := core.ValidateSpec(&p, "node")
			if tc.want == "" {
				if err != nil {
					t.Errorf("期望通过，实际: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// TestSchemaJSON 面板 schema 输出：确定性排序与字段完整性。
func TestSchemaJSON(t *testing.T) {
	data, err := core.SchemaJSON(&ctrlSample{})
	if err != nil {
		t.Fatalf("schema 生成失败: %v", err)
	}
	var items []map[string]any
	if err = json.Unmarshal(data, &items); err != nil {
		t.Fatalf("schema 解析失败: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("schema 项数异常: %d", len(items))
	}
	first := items[0]
	if first["key"] != "title" || first["kind"] != "text" || first["maxLen"] != float64(10) {
		t.Errorf("首个 schema 项异常: %+v", first)
	}
	// 确定性：两次生成一致。
	data2, _ := core.SchemaJSON(&ctrlSample{})
	if string(data) != string(data2) {
		t.Error("schema 输出不确定")
	}
}

// TestHeadingSpecIntegration heading 组件接入声明式后的控件解析。
func TestHeadingSpecIntegration(t *testing.T) {
	controls, err := core.ParseControls(&heading.Props{})
	if err != nil {
		t.Fatalf("heading Props 解析失败: %v", err)
	}
	if len(controls) < 6 {
		t.Errorf("heading 声明式控件数异常: %d", len(controls))
	}
	// Tag 的默认值与选项完整性（Key 取 json tag：heading.Props.Tag 序列化为 tag）。
	var found bool
	for _, c := range controls {
		if c.Key == "tag" {
			found = true
			if c.Default != "h2" || len(c.Options) != 8 {
				t.Errorf("Tag 声明异常: %+v", c)
			}
		}
	}
	if !found {
		t.Error("Tag 控件缺失")
	}
}