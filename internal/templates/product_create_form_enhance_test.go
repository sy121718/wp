package templates

// product_create_form_enhance_test.go — 商品建表单增强脚本的入口与幂等守门。
//
// 提交迁到 JSON 写基座之后，表单一直在 /admin/products/new 这一页的文档里：片段不再被
// swap 进换出、抽屉也已撤掉。于是触发点只剩 DOMContentLoaded —— 反过来，只要有人把
// 抽屉 / htmx 监听加回来，就说明提交路径又被接回了旧机制（fetch 与 htmx 同时提交会建两个商品）。
//
// 文本断言只能证明「监听见了」，证明不了「增强真的跑了」，所以第二条用桩 DOM 跑一遍脚本。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 只有一个入口（DOMContentLoaded），且幂等标记落在内容节点上。
func TestProductCreateFormEnhancesOnDOMReady(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/product-create-form.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)

	for _, want := range []string{
		"document.addEventListener('DOMContentLoaded'",
		// 幂等判据落在**内容节点**上：form 节点可能被复用，内容却是新的。
		"input.dataset.skuEnhanced === '1'",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("product-create-form.js 缺少 %q", want)
		}
	}
	for _, gone := range []string{"wbui:drawer-open", "htmx:afterSwap"} {
		if strings.Contains(js, gone) {
			t.Errorf("product-create-form.js 仍监听 %q —— 该入口对应的提交机制已撤掉，监听留着只是在掩盖回归", gone)
		}
	}
}

// 桩 DOM 真跑一遍：DOMContentLoaded 之后捆绑商品的主体 SKU 被预填，重复派发不叠加监听。
func TestProductCreateFormEnhanceRunsOnLoad(t *testing.T) {
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
let bound = 0;

function makeTextField(value) {
  const attrs = { placeholder: '留空自动生成' };
  return {
    dataset: {}, value: value, required: false, readOnly: false,
    getAttribute: n => (n in attrs ? attrs[n] : null),
    setAttribute: (n, v) => { attrs[n] = v; },
    removeAttribute: n => { delete attrs[n]; },
    addEventListener: () => { bound++; },
    focus: () => {},
    style: {}
  };
}

const input = makeTextField('');
const slug = makeTextField('abc');
const name = makeTextField('半价套餐');
const typeSelect = { value: 'bundle', addEventListener: () => {} };
const form = {
  dataset: {},
  querySelector: sel => {
    if (sel === '[data-sku-input]') { return input; }
    if (sel === 'select[name="type"]') { return typeSelect; }
    if (sel === 'input[name="slug"]') { return slug; }
    if (sel === 'input[name="name"]') { return name; }
    return null;
  },
  querySelectorAll: () => []
};
const document = {
  addEventListener: (type, fn) => { handlers[type] = fn; },
  querySelector: () => form
};

vm.runInNewContext(fs.readFileSync('static/js/product-create-form.js', 'utf8'), { document });
assert.ok(handlers['DOMContentLoaded'], '没有注册 DOMContentLoaded 监听');

handlers['DOMContentLoaded']({});
assert.equal(input.dataset.skuEnhanced, '1', '页面就绪后表单没有被增强');
assert.equal(input.required, true, '捆绑商品的主体 SKU 必须变成必填');
assert.equal(input.value, 'ABC_B', '建议值应按 URL 段预填');

const first = bound;
handlers['DOMContentLoaded']({});
assert.equal(bound, first, '同一段内容被重复增强（监听器叠加了）');
`
	if out, err := exec.Command(node, "--eval", script).CombinedOutput(); err != nil {
		t.Fatalf("建表单增强契约失败: %v\n%s", err, out)
	}
}
