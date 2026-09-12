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
	for _, isBlock := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		err := renderer.Instance("workbench/layout.html", map[string]any{
			"title": "控件检查", "csrf_token": "test", "jsVer": "test",
			"isBlock": isBlock, "pageId": "test-page", "document": `{}`, "meta": `{}`, "schemas": `{}`,
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
	matches := regexp.MustCompile(`/static/js/(ui/[a-z_]+\.js)`).FindAllStringSubmatch(string(entry), -1)
	if len(matches) < 3 || matches[0][1] != "ui/_util.js" || matches[len(matches)-1][1] != "ui/index.js" {
		t.Fatal("控件入口必须按助手、控件、扫描入口的顺序加载")
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
