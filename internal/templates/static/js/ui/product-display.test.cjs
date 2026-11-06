const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');

function load(product, ratings = []) {
  let init, loadedHTML;
  const source = product ? {textContent: JSON.stringify(product)} : null;
  const ratingNodes = ratings.map(score => ({dataset: {productRating: String(score)}, textContent: ''}));
  const input = {id: 'rich-descriptionHtml-product', value: ''};
  const spec = {dataset: {productSpec: 'v1'}, textContent: ''};
  const document = {
    getElementById: id => id === 'product-display-data' ? source : input,
    querySelector: () => ({editor: {loadHTML: html => { loadedHTML = html; }}}),
    querySelectorAll: selector => selector === '[data-product-rating]' ? ratingNodes : [spec],
  };
  const window = {document, WBUI: {register: callback => {init = callback;}, markOnce: () => true}};
  vm.runInNewContext(fs.readFileSync(path.join(UI_SRC, 'product-display.js'), 'utf8'), {window});
  init();
  return {api: window.WBProductDisplay, input, spec, loadedHTML, ratingNodes};
}

test('编辑页从原始描述对象恢复富文本并保留规格名称为纯文本', () => {
  const state = load({description: {html: '<p>原始描述</p>'},
    attributes: [{key: 'color', name: '颜色', values: [{key: 'red', label: '<红色>'}]}],
    variants: [{id: 'v1', optionValues: {color: 'red'}}]});
  assert.equal(state.input.value, '<p>原始描述</p>');
  assert.equal(state.loadedHTML, state.input.value);
  assert.equal(state.spec.textContent, '颜色 <红色>');
});

test('无描述和已删除属性值保留可编辑状态与可识别的规格键', () => {
  const state = load({description: null, variants: [{id: 'v1', optionValues: {size: 'XL', color: 'old'}}]});
  assert.equal(state.input.value, '');
  assert.equal(state.loadedHTML, '');
  assert.equal(state.spec.textContent, 'color old · size XL');
  assert.equal(state.api.formatSpec({}, []), '—');
});

test('列表没有详情数据时仍格式化评分，零分和小数保持两位', () => {
  const state = load(null, [4.5, 4, 5, 0, 3.1950000000000003]);
  assert.deepEqual(state.ratingNodes.map(node => node.textContent), ['4.50', '4.00', '5.00', '0.00', '3.20']);
});
