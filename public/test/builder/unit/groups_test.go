package unit

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// groupSample 分组与新增控件测试模板。
type groupSample struct {
	Content string `json:"content" ct:"text,maxlen=100"`                                  // 默认 content 组
	Level   string `json:"level" ct:"select,a,b,sec=style"`                                // style 组
	Opacity int    `json:"opacity" ct:"slider,min=0,max=100,step=5,sec=style"`             // slider
	Link    string `json:"link" ct:"url,sec=content"`                                      // url 协议白名单
}

// TestGroupSectionParse 分组解析：默认 content，sec=style 归入样式组。
func TestGroupSectionParse(t *testing.T) {
	controls, err := core.ParseControls(&groupSample{})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	byKey := map[string]core.Control{}
	for _, c := range controls {
		byKey[c.Key] = c
	}
	if byKey["Content"].Section != "content" {
		t.Errorf("默认分组异常: %+v", byKey["Content"])
	}
	if byKey["Level"].Section != "style" {
		t.Errorf("sec=style 未生效: %+v", byKey["Level"])
	}
	if c := byKey["Opacity"]; c.Kind != core.ControlSlider || c.Step != 5 || c.Min != 0 || c.Max != 100 {
		t.Errorf("slider 参数异常: %+v", c)
	}
}

// TestGroupSectionSchema schema 分组输出：content 在前 style 在后。
func TestGroupSectionSchema(t *testing.T) {
	data, err := core.SchemaJSON(&groupSample{})
	if err != nil {
		t.Fatalf("schema 失败: %v", err)
	}
	var items []map[string]any
	if err = json.Unmarshal(data, &items); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("项数异常: %d", len(items))
	}
	// 分组顺序：content（Content/Link）在前，style（Level/Opacity）在后。
	if items[0]["section"] != "content" || items[1]["section"] != "content" {
		t.Errorf("content 组未在前: %v", items)
	}
	if items[2]["section"] != "style" || items[3]["section"] != "style" {
		t.Errorf("style 组未在后: %v", items)
	}
	// step 透出。
	for _, it := range items {
		if it["key"] == "Opacity" && it["step"] != float64(5) {
			t.Errorf("step 未输出: %v", it)
		}
	}
}

// TestURLControl url 控件协议白名单。
func TestURLControl(t *testing.T) {
	cases := []struct {
		val  string
		want string // 空=通过
	}{
		{"https://example.com", ""},
		{"/internal-page", ""},
		{"#anchor", ""},
		{"mailto:a@b.com", ""},
		{"javascript:alert(1)", "协议非法"},
		{"data:text/html,<x>", "协议非法"},
		{"", ""}, // 空放行
	}
	for _, tc := range cases {
		p := groupSample{Link: tc.val}
		err := core.ValidateSpec(&p, "node")
		if tc.want == "" {
			if err != nil {
				t.Errorf("%q 应通过: %v", tc.val, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q 期望错误含 %q: %v", tc.val, tc.want, err)
		}
	}
}

// TestSliderValue slider 值域（与 int 同语义）。
func TestSliderValue(t *testing.T) {
	p := groupSample{Opacity: 120}
	if err := core.ValidateSpec(&p, "node"); err == nil || !strings.Contains(err.Error(), "超出上限") {
		t.Errorf("slider 超上限未拦截: %v", err)
	}
	p2 := groupSample{Opacity: 60}
	if err := core.ValidateSpec(&p2, "node"); err != nil {
		t.Errorf("slider 合法值被误拦: %v", err)
	}
}

// TestTextStyleGroup 共享排版组：校验与断点声明。
func TestTextStyleGroup(t *testing.T) {
	ts := core.TextStyle{
		Desktop: core.TextStyleValue{FontSize: "2rem", LineHeight: "1.2", TextAlign: "center"},
		Mobile:  core.TextStyleValue{FontSize: "clamp(1rem, 4vw, 1.5rem)"},
	}
	if err := core.ValidateTextStyle("node", &ts); err != nil {
		t.Fatalf("合法排版被拒: %v", err)
	}
	// 非法字号。
	bad := core.TextStyle{Desktop: core.TextStyleValue{FontSize: "2url"}}
	if err := core.ValidateTextStyle("node", &bad); err == nil || !strings.Contains(err.Error(), "字号") {
		t.Errorf("非法字号未拦截: %v", err)
	}
	// 断点声明。
	d := ts.BreakpointDecls(core.BreakpointDesktop)
	if len(d) != 3 || d[0] != "font-size: 2rem" {
		t.Errorf("桌面断点声明异常: %v", d)
	}
	m := ts.BreakpointDecls(core.BreakpointMobile)
	if len(m) != 1 || !strings.HasPrefix(m[0], "font-size: clamp(") {
		t.Errorf("手机断点声明异常: %v", m)
	}
	if ts.BreakpointDecls("unknown") != nil {
		t.Error("未知断点应返回 nil")
	}
}