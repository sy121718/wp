package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductCreateFormUsesSharedScan(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("static/js/product-create-form.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, want := range []string{"WBUI.register(scan)", "WBUI.ready(", "input.dataset.skuEnhanced === '1'"} {
		if !strings.Contains(js, want) {
			t.Errorf("商品新建控件缺少 %q", want)
		}
	}
	for _, gone := range []string{"DOMContentLoaded", "wbui:drawer-open", "htmx:", "previewContainerSKU", "toUpperCase", "fetch("} {
		if strings.Contains(js, gone) {
			t.Errorf("控件不应重复接线、命名业务规则或提交请求：%q", gone)
		}
	}
}

func TestProductCreateFormSharedScanAndOptionalInputs(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("该契约要求 Node")
		}
		t.Skip("缺少 Node")
	}
	script := `
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const controls = [];
let bound = 0, ready;
function field(value = '') {
  const events = {};
  return {dataset: {}, value, checked: false, disabled: false, required: false,
    addEventListener(type, fn) { events[type] = fn; bound++; },
    change() { events.change(); }
  };
}
function makeForm() {
  const input = field('Abc'), quantity = field(''), track = field(), priceSwitch = field(), price = field('');
  const wh1 = field('warehouse-1'), wh2 = field('warehouse-2');
  const options = [{dataset: {warehouse: wh1.value}}, {dataset: {warehouse: wh2.value}}];
  const fields = {'[data-sku-input]': input, '[data-track-quantity]': track,
    '[data-quantity-input]': quantity, '[data-default-price-switch]': priceSwitch,
    '[data-default-price-input]': price, '[data-sku-candidates]': {querySelectorAll: () => options}};
  const form = {dataset: {}, matches: sel => sel === 'form[data-product-create-form]',
    querySelector: sel => fields[sel] || null,
    querySelectorAll: sel => sel === '[data-warehouse-check]' ? [wh1, wh2] : []};
  return {form, fields, input, quantity, track, priceSwitch, price, wh1, wh2, options};
}
const first = makeForm();
const document = {querySelectorAll: () => [first.form]};
const WBUI = {register: fn => controls.push(fn), ready: fn => {ready = fn;}};
vm.runInNewContext(fs.readFileSync('static/js/product-create-form.js', 'utf8'), {document, window: {WBUI}});
assert.equal(controls.length, 1);
ready();
assert.equal(first.input.dataset.skuEnhanced, '1');
assert.equal(first.quantity.disabled, true);
assert.equal(first.price.disabled, true, '未设置默认价时不得提交空数字');
assert.equal(first.price.required, false);
assert.ok(first.options.every(option => !option.disabled), '未选仓库不在浏览器重算默认仓');
const initialBound = bound;
controls[0](document);
controls[0](first.form);
assert.equal(bound, initialBound, '首屏与根节点重扫不得叠加监听');
first.wh1.checked = true;
first.wh1.change();
assert.equal(first.options[0].disabled, false);
assert.equal(first.options[1].disabled, true);
first.track.checked = true;
first.track.change();
first.quantity.value = '0';
assert.equal(first.quantity.disabled, false, '显式零库存可提交');
first.track.checked = false;
first.track.change();
assert.equal(first.quantity.disabled, true);
assert.equal(first.quantity.value, '');
first.priceSwitch.checked = true;
first.priceSwitch.change();
assert.equal(first.price.disabled, false);
assert.equal(first.price.required, true, '启用后空价由原生表单校验阻断');
first.price.value = '0';
first.priceSwitch.checked = false;
first.priceSwitch.change();
assert.equal(first.price.disabled, true);
assert.equal(first.price.value, '0', '暂时取消设置不应丢弃已输入值');
const dynamic = makeForm();
controls[0](dynamic.form);
assert.equal(dynamic.input.dataset.skuEnhanced, '1', '直接插入的表单根节点必须增强');
const replacement = field('new-content');
dynamic.fields['[data-sku-input]'] = replacement;
controls[0](dynamic.form);
assert.equal(replacement.dataset.skuEnhanced, '1', '表单复用而内容换新时必须重新增强');
assert.equal(first.input.value, 'Abc', '控件不得改写用户 SKU 或替 Service 添加仓库前缀');
`
	if out, err := exec.Command(node, "--eval", script).CombinedOutput(); err != nil {
		t.Fatalf("建表单统一扫描与可选输入回归失败: %v\n%s", err, out)
	}
}
