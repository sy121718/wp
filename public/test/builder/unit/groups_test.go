package unit

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

type groupSample struct {
	Content string `json:"content" ct:"text,maxlen=100"`
	Level   string `json:"level" ct:"select,a,b,sec=style"`
	Opacity int    `json:"opacity" ct:"slider,min=0,max=100,step=5,sec=style"`
	Link    string `json:"link" ct:"url,sec=content"`
}

func TestGroupSectionParse(t *testing.T) {
	controls, err := core.ParseControls(&groupSample{})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	byKey := map[string]core.Control{}
	for _, c := range controls {
		byKey[c.Key] = c
	}

	if byKey["content"].Section != "content" {
		t.Errorf("默认分组异常: %+v", byKey["content"])
	}
	if byKey["level"].Section != "style" {
		t.Errorf("sec=style 未生效: %+v", byKey["level"])
	}
	if c := byKey["opacity"]; c.Kind != core.ControlSlider || c.Step != 5 || c.Min != 0 || c.Max != 100 {
		t.Errorf("slider 参数异常: %+v", c)
	}
}

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

	if items[0]["section"] != "content" || items[1]["section"] != "content" {
		t.Errorf("content 组未在前: %v", items)
	}
	if items[2]["section"] != "style" || items[3]["section"] != "style" {
		t.Errorf("style 组未在后: %v", items)
	}

	for _, it := range items {
		if it["key"] == "opacity" && it["step"] != float64(5) {
			t.Errorf("step 未输出: %v", it)
		}
	}
}

func TestURLControl(t *testing.T) {
	cases := []struct {
		val  string
		want string
	}{
		{"https://example.com", ""},
		{"/internal-page", ""},
		{"#anchor", ""},
		{"mailto:a@b.com", ""},
		{"javascript:alert(1)", "协议非法"},
		{"data:text/html,<x>", "协议非法"},
		{"", ""},
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

func TestTextStyleGroup(t *testing.T) {
	ts := core.TextStyle{
		Desktop: core.TextStyleValue{FontSize: "2rem", LineHeight: "1.2", TextAlign: "center"},
		Mobile:  core.TextStyleValue{FontSize: "clamp(1rem, 4vw, 1.5rem)"},
	}
	if err := core.ValidateTextStyle("node", &ts); err != nil {
		t.Fatalf("合法排版被拒: %v", err)
	}

	bad := core.TextStyle{Desktop: core.TextStyleValue{FontSize: "2url"}}
	if err := core.ValidateTextStyle("node", &bad); err == nil || !strings.Contains(err.Error(), "字号") {
		t.Errorf("非法字号未拦截: %v", err)
	}

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
