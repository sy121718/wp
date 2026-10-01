package templates

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// 公共入口迁移必须经过整页渲染；仅片段测试看不到编辑器外壳丢失。
func TestWorkbenchShellLoadsSharedUI(t *testing.T) {
	renderer := NewJetHTMLRender(".", true)
	for _, tc := range []struct {
		isBlock, isTemplate bool
		previewQS           string
	}{
		{false, false, ""},
		{true, false, ""},
		{false, true, "template=t1&entityType=product&entityId=e1&editor=1"},
	} {
		recorder := httptest.NewRecorder()
		err := renderer.Instance("workbench/layout.html", map[string]any{
			"title": "控件检查", "csrf_token": "test", "jsVer": "test",
			"isBlock": tc.isBlock, "isTemplate": tc.isTemplate, "previewQS": tc.previewQS,
			"pageId": "test-page", "document": `{}`, "meta": `{}`, "schemas": `{}`,
		}).Render(recorder)
		if err != nil {
			t.Fatal(err)
		}
		html := recorder.Body.String()
		for _, want := range []string{`<body class="wb-body">`, `id="wb-main"`, `id="inspector-panel"`, `id="wb-canvas"`, `id="wb-save-draft"`, `id="wb-bootstrap"`, `/static/css/ui.css`} {
			if !strings.Contains(html, want) {
				t.Errorf("工作台整页缺少 %s", want)
			}
		}
		entry := `/static/js/ui/index.js`
		if strings.Count(html, entry) != 1 || strings.Index(html, entry) > strings.Index(html, `/static/js/workbench/index.js`) {
			t.Fatal("工作台必须在自身入口前加载一次完整控件基座")
		}
	}
}

// 公共入口中的资产必须存在且顺序正确，所有控件都按普通脚本解析。
func TestUIAssetEntry(t *testing.T) {
	for _, path := range []string{"admin/layout.html", "workbench/layout.html"} {
		shell, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(shell), `{{include "../partials/ui_scripts.html"}}`) != 1 || strings.Contains(string(shell), `/static/js/ui/`) {
			t.Fatalf("%s 必须只经公共入口加载控件，不能另抄脚本清单", path)
		}
	}
	entry, err := os.ReadFile("partials/ui_scripts.html")
	if err != nil {
		t.Fatal(err)
	}
	// 名字里允许点与数字：htmx.min.js 这类**前置库**同样是入口的一部分
	//（原先从 CDN 引它，CDN 不可达时后台的局部刷新会静默退化成整页刷新）。
	matches := regexp.MustCompile(`/static/js/(ui/[a-z0-9_.-]*\.js)`).FindAllStringSubmatch(string(entry), -1)
	if len(matches) < 3 || matches[len(matches)-1][1] != "ui/index.js" {
		t.Fatal("控件入口必须按助手、控件、扫描入口的顺序加载（扫描入口放最后）")
	}
	// 断言的是**顺序的性质**而不是固定清单：_util.js 必须在 index.js 之前，
	// 前置库（htmx）必须在 _util.js 之前 —— 控件入口监听 htmx:afterSwap，反了就静默失效。
	idx := func(name string) int {
		for i, m := range matches {
			if m[1] == name {
				return i
			}
		}
		return -1
	}
	utilIdx, indexIdx := idx("ui/_util.js"), idx("ui/index.js")
	if utilIdx < 0 {
		t.Fatal("控件入口缺少 ui/_util.js（控件靠它注册）")
	}
	if utilIdx > indexIdx {
		t.Fatal("控件入口顺序不对：_util.js 必须在 index.js 之前")
	}
	if htmxIdx := idx("ui/htmx.min.js"); htmxIdx >= 0 && htmxIdx > utilIdx {
		t.Fatal("前置库（htmx）必须排在控件助手之前")
	}
	node, nodeErr := exec.LookPath("node")
	seen := map[string]bool{}
	for _, match := range matches {
		name := match[1]
		if seen[name] {
			t.Fatalf("控件重复加载: %s", name)
		}
		seen[name] = true
		src, err := StaticJS(name)
		if err != nil {
			t.Fatal(err)
		}
		if nodeErr == nil {
			cmd := exec.Command(node, "--input-type=commonjs", "--check")
			cmd.Stdin = strings.NewReader(src)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s 普通脚本解析失败: %v\n%s", name, err, out)
			}
		}
	}
	if nodeErr != nil && os.Getenv("GOWP_REQUIRE_NODE") == "1" {
		t.Fatal("完整控件检查要求 Node")
	}
	files, err := staticJSFS.ReadDir("static/js/ui")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".js") && !seen["ui/"+file.Name()] {
			t.Errorf("控件未接入公共入口: %s", file.Name())
		}
	}
}

func TestUIScanReportsFailureAndContinues(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("完整控件检查要求 Node")
		}
		t.Skip("缺少 Node")
	}
	script := `
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const events = [], logs = [];
const context = {
  window: {getComputedStyle: () => ({transitionDuration:'0.1s, 200ms', transitionDelay:'50ms'})},
  document: {dispatchEvent: e => events.push(e)},
  console: {error: (...args) => logs.push(args)},
  CustomEvent: class { constructor(type, options) { this.type=type; this.detail=options.detail; } }
};
vm.runInNewContext(fs.readFileSync('static/js/ui/_util.js','utf8'), context);
const ui=context.window.WBUI, scope={}, failure=new Error('故意损坏一个控件');
let called=0;
ui.register(() => {throw failure;});
ui.register(got => {assert.equal(got,scope); called++;});
const errors=ui.scan(scope);
assert.equal(errors.length,1); assert.equal(errors[0],failure);
assert.equal(called,1); assert.equal(logs.length,1);
assert.equal(events[0].type,'wbui:error'); assert.equal(events[0].detail.error,failure);
assert.equal(ui.transitionTime({}),250);
context.window.getComputedStyle=()=>({transitionDuration:'0s',transitionDelay:'0s'});
assert.equal(ui.transitionTime({}),0);
`
	if out, err := exec.Command(node, "--eval", script).CombinedOutput(); err != nil {
		t.Fatalf("基座异常可观测性/动效时间契约失败: %v\n%s", err, out)
	}
}

// TestProductCategoryTreeSelectionSync 分类树脚本只剩勾选态与父级搜索：
// 树由服务端一次渲染完（没有展开 / 折叠），勾选态仍要正确汇总到批量条。
func TestProductCategoryTreeSelectionSync(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("分类树勾选态检查要求 Node")
		}
		t.Skip("缺少 Node")
	}
	script := `
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const handlers = {};
const bar = {hidden: true};
const count = {dataset: {bulkTemplate: '已选 {n} 项'}, textContent: ''};
const all = {checked: false, indeterminate: false};
const marked = [];
const row = {classList: {toggle: (cls, on) => marked.push([cls, on])}};
const makeBox = checked => ({
  checked, disabled: false,
  matches: selector => selector.includes('data-check-item'),
  closest: selector => selector === '[data-category-tree-form]' ? form : (selector === '[data-category-row]' ? row : null)
});
const boxes = [makeBox(true), makeBox(false)];
const form = {
  querySelectorAll: selector => selector === '[data-check-item]' ? boxes : [],
  querySelector: selector => selector === '[data-check-all]' ? all
    : (selector === '[data-bulk-bar]' ? bar : (selector === '[data-bulk-count]' ? count : null))
};
const document = {addEventListener: (type, fn) => { handlers[type] = fn; }};
vm.runInNewContext(fs.readFileSync('static/js/ui/product-category-tree.js', 'utf8'), {document, window: {}});
assert.ok(!handlers.keydown, '展开 / 折叠已退役，键盘监听不该还在');
handlers.change({target: boxes[0], stopPropagation: () => {}});
assert.equal(count.textContent, '已选 1 项', '批量条计数没有跟着勾选走');
assert.equal(bar.hidden, false, '勾选后批量条应显示');
assert.equal(all.indeterminate, true, '部分勾选应是半选态');
assert.deepEqual(marked[0], ['is-selected', true], '选中行没有标记');
`
	if out, err := exec.Command(node, "--eval", script).CombinedOutput(); err != nil {
		t.Fatalf("分类树勾选态契约失败: %v\n%s", err, out)
	}
}
