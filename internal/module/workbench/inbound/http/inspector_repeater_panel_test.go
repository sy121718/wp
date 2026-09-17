package workbenchhttp

// inspector_repeater_panel_test.go — 重复项面板的服务端渲染（折叠项 / 页签）。
//
// 链路：节点 props → Go 骨架生成器（inspector_repeater.go）→ Jet 模板
// {{f.HTML | unsafe}} → 客户端绑定函数（repeater.js 的 bindRepeaterPanel）认得的属性。
//
// 单元测试钉的是生成器的输出；这里钉的是「它确实进了模板，并且按原样输出」——
// 少了 | unsafe 的话 HTML 会被转义成文本，页面上看得见一串标签、客户端一个属性都读不到，
// 而生成器自身的单测照样全绿。
//
// 复用 inspector_panel_test.go 的 newInspectorRouter（只装这一个端点，不依赖数据库）。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postInspector 提交一次面板请求并返回响应体。
func postInspector(t *testing.T, doc, nodeID, tab string) string {
	t.Helper()
	router := newInspectorRouter(t)
	form := url.Values{"nodeId": {nodeID}, "document": {doc}, "tab": {tab}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	return recorder.Body.String()
}

const accordionDoc = `{"settings":{},"root":[{"id":"acc1","type":"core.accordion",` +
	`"props":{"items":[{"title":"第一项","open":true},{"title":"第二项"}]},` +
	`"children":[{"id":"c1","type":"core.text","props":{}},{"id":"c2","type":"core.text","props":{}}]}]}`

// TestInspectorPanelRendersAccordionRepeater 折叠项面板的骨架出现在片段里，且未被转义。
func TestInspectorPanelRendersAccordionRepeater(t *testing.T) {
	body := postInspector(t, accordionDoc, "acc1", "content")
	for _, want := range []string{
		`data-wb-rep="core.accordion"`,
		`data-wb-rep-field="title"`,
		`data-wb-rep-input="0"`,
		`value="第一项"`,
		`placeholder="折叠项1 标题"`,
		`data-wb-rep-extra="open" data-wb-rep-index="0" checked`,
		`data-wb-rep-op="move" data-wb-rep-index="0" data-wb-rep-to="1"`,
		`data-wb-rep-op="remove" data-wb-rep-index="1"`,
		`data-wb-rep-op="add"`,
		`折叠项与面板数量一致（2）`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("面板缺少 %q\n%s", want, body)
		}
	}
	// 原样输出：转义了的话页面上会显示一串标签，而生成器的单测仍然全绿。
	if strings.Contains(body, "&lt;div class=") || strings.Contains(body, "data-wb-rep=&quot;") {
		t.Errorf("骨架被转义了 —— 模板分支少了 | unsafe：\n%s", body)
	}
}

// TestInspectorPanelRepeaterCountMismatch 条目数与画布面板数不一致时给红色提示。
//
// 不一致是真实会发生的（历史脏数据、或直接在画布上增删面板）：用户要在保存前就看到。
func TestInspectorPanelRepeaterCountMismatch(t *testing.T) {
	doc := `{"settings":{},"root":[{"id":"acc1","type":"core.accordion",` +
		`"props":{"items":[{"title":"唯一一项"}]},` +
		`"children":[{"id":"c1","type":"core.text","props":{}},{"id":"c2","type":"core.text","props":{}}]}]}`
	body := postInspector(t, doc, "acc1", "content")
	if !strings.Contains(body, "数量不一致（折叠项 1 个 / 面板 2 个）") {
		t.Errorf("应有数量不一致提示：\n%s", body)
	}
	if !strings.Contains(body, "var(--c-danger") {
		t.Error("不一致时应标红")
	}
}

// TestInspectorPanelRepeaterTabs 页签面板走同一套服务端骨架（共用生成器与绑定函数）。
func TestInspectorPanelRepeaterTabs(t *testing.T) {
	doc := `{"settings":{},"root":[{"id":"t1","type":"core.tabs",` +
		`"props":{"tabs":[{"label":"概览"},{"label":"参数"}]},` +
		`"children":[{"id":"p1","type":"core.text","props":{}},{"id":"p2","type":"core.text","props":{}}]}]}`
	body := postInspector(t, doc, "t1", "content")
	for _, want := range []string{
		`data-wb-rep="core.tabs"`,
		`data-wb-rep-field="label"`,
		`value="概览"`,
		`placeholder="页签1 标签"`,
		`页签与面板数量一致（2）`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("页签面板缺少 %q\n%s", want, body)
		}
	}
	// 页签没有额外字段（折叠项的「默认展开」是它独有的）。
	if strings.Contains(body, "data-wb-rep-extra") {
		t.Error("页签面板不该有额外字段控件")
	}
}

// TestInspectorPanelRepeaterHiddenOnStyleTab 样式页签不挂重复项面板（它属于内容）。
func TestInspectorPanelRepeaterHiddenOnStyleTab(t *testing.T) {
	if body := postInspector(t, accordionDoc, "acc1", "style"); strings.Contains(body, "data-wb-rep=") {
		t.Errorf("样式页签不该出现重复项面板：\n%s", body)
	}
}
