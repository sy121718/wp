package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// product_create_form_swap_test.go — 商品建表单增强脚本的「第三触发点」守门。
//
// 为什么必须钉住：htmx 把片段（写失败返的「错误槽 + 回填后的表单」）swap 进抽屉 / 页面时，
// **既不派发 wbui:drawer-open 也不派发 DOMContentLoaded**；而 WBUI.scan 只跑 WBUI.controls
// 里登记过的控件 —— 本脚本不是基座控件。少挂这一个监听，增强（SKU 建议值预填 / 预览行 /
// 多仓候选过滤）会静默失效，**没有任何测试会红**，只有人打开抽屉才看得出来。
//
// 与 admin_htmx_feedback_test.go 同法：读静态 JS 源 + 到 htmx 运行时里核对事件名真实存在
//（事件名写错不会报错，只会永远不触发）。

// 增强脚本必须同时挂住三个入口，并保留内容节点级的幂等判据。
func TestProductCreateFormEnhancesAfterHtmxSwap(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/product-create-form.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)

	for _, want := range []string{
		// 三个触发点（缺任一处都会让某条入口静默失去增强）。
		"document.addEventListener('wbui:drawer-open'", // 抽屉：模板克隆进 document
		"document.addEventListener('DOMContentLoaded'", // 新建整页
		"document.addEventListener('htmx:afterSwap'",   // 片段 swap 进来（写失败回填表单）
		// 幂等判据落在**内容节点**上：hx-swap=innerHTML 打在 form 自身时 form 节点被复用、
		// 内容已换新，只看 form.dataset 会把新 input 当成已增强。
		"input.dataset.skuEnhanced === '1'",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("product-create-form.js 缺少 %q —— 对应入口的增强会静默失效", want)
		}
	}
}

// 监听的事件名必须在 htmx 运行时里真实存在。
func TestHtmxRuntimeDispatchesAfterSwap(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/ui/htmx.min.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "htmx:afterSwap") {
		t.Error("htmx 运行时里没有 htmx:afterSwap —— 增强脚本的监听永远不会触发")
	}
}

// 增强脚本必须在 htmx:afterSwap 后**真的重跑增强**（不是只登记了一个监听）。
//
// 文本断言只能证明「监听见了」，证明不了「换进来的新表单真的被增强」—— 而那正是本批要修的
// 静默失效。所以这里用桩 DOM 把脚本真跑一遍，并复现那个关键场景：**form 节点被复用**
// （hx-swap=innerHTML 打在 form 上）但内容已整体换新，form.dataset 上还留着上一轮的幂等标记。
// 与 ui_assets_test.go 的 TS 检查同法（node + vm，缺 Node 则跳过）。
func TestProductCreateFormReEnhancesAfterHtmxSwap(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("该契约要求 Node")
		}
		t.Skip("缺少 Node")
	}
	script := `
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');

const handlers = {};
const bound = { count: 0 };
function makeTextField(value) {
  const attrs = { placeholder: '留空自动生成' };
  return {
    dataset: {}, value: value, required: false, readOnly: false,
    getAttribute: n => (n in attrs ? attrs[n] : null),
    setAttribute: (n, v) => { attrs[n] = v; },
    removeAttribute: n => { delete attrs[n]; },
    addEventListener: () => { bound.count++; },
    focus: () => {}
  };
}

// 片段 swap 进了 form 自身：节点复用、内容换新 —— form.dataset 上仍是上一轮的 '1'，
// 而 input / select 都是新节点（没有标记）。
const input = makeTextField('');
const slug = makeTextField('abc');
const typeSelect = { value: 'bundle', addEventListener: () => {} };
const form = {
  dataset: { skuEnhanced: '1' },
  querySelector: sel => {
    if (sel === '[data-sku-input]') { return input; }
    if (sel === 'select[name="type"]') { return typeSelect; }
    if (sel === 'input[name="slug"]') { return slug; }
    return null;
  },
  querySelectorAll: () => []
};
const document = {
  addEventListener: (type, fn) => { handlers[type] = fn; },
  querySelector: () => form
};

vm.runInNewContext(fs.readFileSync('static/js/product-create-form.js', 'utf8'), { document });
assert.ok(handlers['htmx:afterSwap'], '没有注册 htmx:afterSwap 监听');

handlers['htmx:afterSwap']({ target: form });
assert.equal(input.dataset.skuEnhanced, '1', 'afterSwap 后新换进来的表单没有被增强');
assert.equal(input.required, true, '捆绑商品的主体 SKU 必须变成必填');
assert.equal(input.value, 'ABC_B', '建议值应按 URL 段预填');

const first = bound.count;
handlers['htmx:afterSwap']({ target: form });
assert.equal(bound.count, first, '同一段内容被重复增强（监听器叠加了）');
`
	if out, err := exec.Command(node, "--eval", script).CombinedOutput(); err != nil {
		t.Fatalf("htmx swap 后重增强契约失败: %v\n%s", err, out)
	}
}
