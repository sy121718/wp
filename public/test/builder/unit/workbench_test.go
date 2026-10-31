package unit

import (
	"strings"
	"testing"
)

func TestWorkbenchMetadata(t *testing.T) {
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[
		{"id":"sec1","type":"core.container","name":"首屏 Banner 容器","hidden":true,"locked":true,
		 "props":{"tag":"section","layout":{"engine":"flex","flex":{}}}},
		{"id":"head1","type":"core.heading","props":{"text":"标题"}}
	]}`
	c, err := compile(t, mustParse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	if strings.Contains(c.HTML, "首屏") || strings.Contains(c.HTML, `id="`) {
		t.Errorf("编辑元数据不应进入产物: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, `<section class="sky-c-sec1 sky-section">`) {
		t.Errorf("容器输出异常: %s", c.HTML)
	}
}

func TestWorkbenchPosition(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"absolute", `{"tag":"section","layout":{"engine":"flex","flex":{}},"position":{"type":"absolute","top":"40px","left":"16px"}}`, "position: absolute"},
		{"sticky", `{"tag":"header","layout":{"engine":"flex","flex":{}},"position":{"type":"sticky"}}`, "position: sticky"},
		{"drawer", `{"tag":"aside","layout":{"engine":"flex","flex":{}},"position":{"type":"drawer","drawerSide":"right","drawerOverlay":true,"drawerTriggerId":"trig-1"}}`, "data-drawer-side=\"right\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sec1","type":"core.container","props":` + tc.props + `}]}`
			c, err := compile(t, mustParse(t, doc))
			if err != nil {
				t.Fatalf("编译失败: %v", err)
			}
			if !strings.Contains(c.HTML, tc.want) && !strings.Contains(c.CSS, tc.want) {
				t.Errorf("输出缺少 %q", tc.want)
			}
		})
	}

	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"d1","type":"core.container","props":{"tag":"aside","layout":{"engine":"flex","flex":{}},"position":{"type":"drawer","drawerSide":"right","drawerTriggerId":"trig-1"}}}]}`
	c, err := compile(t, mustParse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.CSS, ".sky-c-d1:target") || !strings.Contains(c.CSS, "translateX(100%)") {
		t.Errorf("drawer 零 JS 协议缺失:\n%s", c.CSS)
	}
}

func TestWorkbenchStyleEx(t *testing.T) {
	props := `{"tag":"section","layout":{"engine":"flex","flex":{}},"styleEx":{
		"backgroundHover":"#111111",
		"overlay":"rgba(0,0,0,0.4)",
		"shapeDivider":"wave","shapeDividerPosition":"bottom",
		"order":2,
		"groupParent":true,
		"attributes":[{"key":"data-track","value":"banner"},{"key":"aria-label","value":"首屏"}]
	}}`
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sec1","type":"core.container","props":` + props + `}]}`
	c, err := compile(t, mustParse(t, doc))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{

		`data-sky-group="true"`,
		`data-track="banner"`,
		`aria-label="首屏"`,

		`<span class="sky-shape sky-shape-bottom">`,
		"<svg viewBox=\"0 0 1440 120\"",

		".sky-c-sec1:hover", "background: #111111",
		".sky-c-sec1::before", "rgba(0,0,0,0.4)",
		"order: 2",
		".sky-shape-bottom", "bottom: 0",
	} {
		if !strings.Contains(c.HTML, want) && !strings.Contains(c.CSS, want) {
			t.Errorf("输出缺少 %q", want)
		}
	}
}

func TestWorkbenchValidate(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"绝对定位缺坐标", `{"tag":"div","layout":{"engine":"flex","flex":{}},"position":{"type":"absolute"}}`, "必须提供至少一个坐标"},
		{"抽屉缺方向", `{"tag":"div","layout":{"engine":"flex","flex":{}},"position":{"type":"drawer"}}`, "滑出方向"},
		{"抽屉非法触发ID", `{"tag":"div","layout":{"engine":"flex","flex":{}},"position":{"type":"drawer","drawerSide":"left"}}`, "触发 ID"},
		{"非法形状", `{"tag":"div","layout":{"engine":"flex","flex":{}},"styleEx":{"shapeDivider":"zigzag"}}`, "无效的形状分隔线"},
		{"顺序越界", `{"tag":"div","layout":{"engine":"flex","flex":{}},"styleEx":{"order":500}}`, "顺序值"},
		{"非法属性key", `{"tag":"div","layout":{"engine":"flex","flex":{}},"styleEx":{"attributes":[{"key":"onclick","value":"x"}]}}`, "无效的自定义属性 key"},
		{"元数据Name过长", `{"tag":"div","layout":{"engine":"flex","flex":{}}}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sec1","type":"core.container","props":` + tc.props + `}]}`
			_, err := compile(t, mustParse(t, doc))
			if tc.want == "" {
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

func TestWorkbenchNodeName(t *testing.T) {
	long := strings.Repeat("很", 101)
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sec1","type":"core.container","name":"` + long + `","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}]}`
	_, err := compile(t, mustParse(t, doc))
	if err == nil || !strings.Contains(err.Error(), "节点名称过长") {
		t.Errorf("超长名称应拒绝: %v", err)
	}
}
