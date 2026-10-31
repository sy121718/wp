package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductCreateFormEnhancesOnDOMReady(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/product-create-form.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)

	for _, want := range []string{
		"document.addEventListener('DOMContentLoaded'",

		"input.dataset.skuEnhanced === '1'",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("product-create-form.js 缺少 %q", want)
		}
	}

	for _, gone := range []string{"wbui:drawer-open", "htmx:afterSwap", "htmx:after:swap"} {
		if strings.Contains(js, gone) {
			t.Errorf("product-create-form.js 仍监听 %q —— 该入口对应的提交机制已撤掉，监听留着只是在掩盖回归", gone)
		}
	}
}

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
    dataset: {}, value: value, required: false, readOnly: false, textContent: '',
    getAttribute: n => (n in attrs ? attrs[n] : null),
    setAttribute: (n, v) => { attrs[n] = v; },
    removeAttribute: n => { delete attrs[n]; },
    addEventListener: () => { bound++; },
    focus: () => {},
    style: {}
  };
}

const input = makeTextField('Abc');
const preview = makeTextField('');
preview.dataset.previewLabel = '将生成：';
const box = {
  value: 'wh-1', checked: true, dataset: { code: 'SZ', default: '1' },
  addEventListener: () => { bound++; }
};
const form = {
  dataset: {},
  querySelector: sel => {
    if (sel === '[data-sku-input]') { return input; }
    if (sel === '[data-sku-preview]') { return preview; }
    return null;
  },
  querySelectorAll: sel => (sel === '[data-warehouse-check]' ? [box] : [])
};
const document = {
  addEventListener: (type, fn) => { handlers[type] = fn; },
  querySelector: () => form
};

vm.runInNewContext(fs.readFileSync('static/js/product-create-form.js', 'utf8'), { document });
assert.ok(handlers['DOMContentLoaded'], '没有注册 DOMContentLoaded 监听');

handlers['DOMContentLoaded']({});
assert.equal(input.dataset.skuEnhanced, '1', '页面就绪后表单没有被增强');
assert.equal(preview.textContent, '将生成：SZ_Abc', '应按勾选仓的短码预览主体 SKU（裸码原样保留，只加仓前缀）');
assert.equal(input.required, false, '主体 SKU 不再必填（留空由服务端按 URL 段派生）');

const first = bound;
handlers['DOMContentLoaded']({});
assert.equal(bound, first, '同一段内容被重复增强（监听器叠加了）');
`
	if out, err := exec.Command(node, "--eval", script).CombinedOutput(); err != nil {
		t.Fatalf("建表单增强契约失败: %v\n%s", err, out)
	}
}
