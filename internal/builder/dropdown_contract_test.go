// dropdown_contract_test.go — 工作台自定义下拉（core.js wbDropdown）交互契约测试。
//
// 背景（用户报障）：面板下拉「点开一瞬间就自动选择/关闭，无法选择」。真实浏览器
// 实测定位到两件事：
//  1) 面板里 schema 驱动的下拉是服务端渲染的原生 <select>（fragments/inspector_panel.html），
//     原生 select 在 Linux/Chromium 是「按下展开、松开即选」，正是 wbDropdown 当初
//     存在的理由；修复是 controls/selects.js 把原生 select 升级为 wb-dd；
//  2) wb-dd 的展开态只存在 DOM（is-open class）里，而面板由 idiomorph morph 整体替换
//     （客户端创建的 wb-dd 不在服务端片段里，必然被删掉重建），所以任何一次面板重渲染
//     都会把用户刚点开的下拉强制收起。修复是 core.js 的 wbOpenDropdownKeys /
//     wbRestoreDropdowns 按稳定 key 搬运展开态。
//
// 本测试用 node + 极简 DOM stub 求值 core.js 的同一份实现（不抄逻辑），断言：
// 展开 → 选择 → 关闭、document 级监听不会误关（stopPropagation 生效）、
// 以及展开态可以跨「DOM 被替换」恢复。环境没有 node 时自动跳过。

package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// dropdownProbe node 侧下拉交互探针结果。
type dropdownProbe struct {
	Opened        bool     `json:"opened"`
	OpenAfterDoc  bool     `json:"openAfterDoc"`  // 点按钮后 document 监听是否被触发（true = 未被误关）
	ItemValue     string   `json:"itemValue"`     // 点选后的值
	ChangeCalls   []string `json:"changeCalls"`   // onChange 回调收到的值序列
	ClosedAfter   bool     `json:"closedAfter"`   // 点选后是否收起
	BtnText       string   `json:"btnText"`       // 点选后按钮文案
	KeysBefore    []string `json:"keysBefore"`    // 重渲染前记录的展开 key
	RestoredOpen  bool     `json:"restoredOpen"`  // 新节点（模拟 morph 重建）恢复后是否展开
	RecreatedNode bool     `json:"recreatedNode"` // 恢复用的节点确实是新对象
	ClosedByDoc   bool     `json:"closedByDoc"`   // 点击下拉外部后是否收起
}

// runDropdownProbe 用 node 求值 core.js 的 wbDropdown 并跑完交互场景。
func runDropdownProbe(t *testing.T) dropdownProbe {
	t.Helper()
	abs, err := filepath.Abs(coreJSRel)
	if err != nil {
		t.Fatalf("解析 core.js 路径失败: %v", err)
	}
	if _, err = os.Stat(abs); err != nil {
		t.Fatalf("core.js 不存在: %v", err)
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未找到 node，跳过下拉交互契约测试")
	}
	url := "file://" + filepath.ToSlash(abs)
	out, err := exec.Command(nodeBin, "--input-type=module", "--eval", fmt.Sprintf(dropdownProbeScript, url)).CombinedOutput()
	if err != nil {
		t.Fatalf("node 求值 wbDropdown 失败: %v\n%s", err, out)
	}
	var p dropdownProbe
	if err = json.Unmarshal(out, &p); err != nil {
		t.Fatalf("解析下拉探针 JSON 失败: %v\n%s", err, out)
	}
	return p
}

// TestDropdownInteractionContract 展开 / 选择 / 关闭 / 展开态跨重建恢复。
func TestDropdownInteractionContract(t *testing.T) {
	p := runDropdownProbe(t)

	if !p.Opened {
		t.Error("点击下拉按钮后没有展开（is-open 缺失）")
	}
	if !p.OpenAfterDoc {
		t.Error("点开下拉后 document 级 click 监听把它关掉了（stopPropagation 失效）")
	}
	if p.ItemValue != "b" {
		t.Errorf("点选选项后值不对: got %q want %q", p.ItemValue, "b")
	}
	if len(p.ChangeCalls) != 1 || p.ChangeCalls[0] != "b" {
		t.Errorf("onChange 回调不符: %v", p.ChangeCalls)
	}
	if !p.ClosedAfter {
		t.Error("点选后下拉没有收起")
	}
	if p.BtnText != "选项B" {
		t.Errorf("点选后按钮文案没更新: %q", p.BtnText)
	}
	if len(p.KeysBefore) != 1 || p.KeysBefore[0] != "core.button.value" {
		t.Errorf("展开 key 记录不符: %v", p.KeysBefore)
	}
	if !p.RecreatedNode {
		t.Error("探针没有真正替换节点，展开态恢复测试无效")
	}
	if !p.RestoredOpen {
		t.Error("面板重渲染（DOM 被替换）后下拉没有恢复展开态")
	}
	if !p.ClosedByDoc {
		t.Error("点击下拉外部没有收起（document 级监听失效）")
	}
}

// dropdownProbeScript node 侧探针：极简 DOM stub + 真实 core.js 的 wbDropdown。
const dropdownProbeScript = `
// ---- 极简 DOM stub：只实现 wbDropdown / wbOpenDropdownKeys / wbRestoreDropdowns 用到的 API ----
function parseSel(sel) {
  var attr = null;
  var m = /\[([^=\]]+)="([^"]*)"\]/.exec(sel);
  if (m) { attr = { name: m[1], value: m[2] }; sel = sel.replace(/\[[^\]]*\]/g, ''); }
  var parts = sel.split('.').filter(function (x) { return x !== ''; });
  var tag = '';
  if (sel.charAt(0) !== '.') tag = (parts.shift() || '').toLowerCase();
  return { tag: tag, classes: parts, attr: attr };
}
function camel(name) { return name.replace(/^data-/, '').replace(/-(\w)/g, function (_, c) { return c.toUpperCase(); }); }
function matches(el, sel) {
  var p = parseSel(sel);
  if (p.tag && el.nodeName.toLowerCase() !== p.tag) return false;
  for (var i = 0; i < p.classes.length; i++) if (!el.classList.contains(p.classes[i])) return false;
  if (p.attr) {
    var v = el.attrs[p.attr.name];
    if (v === undefined) v = el.dataset[camel(p.attr.name)];
    if (String(v) !== p.attr.value) return false;
  }
  return true;
}
function El(tag) {
  this.nodeName = String(tag).toUpperCase();
  this.children = []; this.parentNode = null; this._cls = {}; this._listeners = {};
  this.dataset = {}; this.attrs = {}; this.textContent = '';
  var self = this;
  this.classList = {
    add: function () { for (var i = 0; i < arguments.length; i++) self._cls[arguments[i]] = true; },
    remove: function () { for (var i = 0; i < arguments.length; i++) delete self._cls[arguments[i]]; },
    contains: function (c) { return !!self._cls[c]; },
    toggle: function (c, force) { var on = force === undefined ? !self._cls[c] : !!force; if (on) self._cls[c] = true; else delete self._cls[c]; return on; }
  };
}
Object.defineProperty(El.prototype, 'className', {
  get: function () { return Object.keys(this._cls).join(' '); },
  set: function (v) { var self = this; this._cls = {}; String(v || '').split(/\s+/).filter(Boolean).forEach(function (c) { self._cls[c] = true; }); }
});
El.prototype.setAttribute = function (k, v) { this.attrs[k] = String(v); };
El.prototype.getAttribute = function (k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; };
El.prototype.appendChild = function (c) { c.parentNode = this; this.children.push(c); return c; };
El.prototype.insertBefore = function (c, ref) {
  c.parentNode = this;
  var i = this.children.indexOf(ref);
  if (i < 0) this.children.push(c); else this.children.splice(i, 0, c);
  return c;
};
El.prototype.addEventListener = function (t, fn) { (this._listeners[t] = this._listeners[t] || []).push(fn); };
El.prototype.dispatchEvent = function (ev) {
  if (typeof ev === 'string') ev = { type: ev };
  ev.target = this; ev._stopped = false;
  ev.stopPropagation = function () { ev._stopped = true; };
  ev.preventDefault = function () {};
  var node = this;
  while (node) {
    var ls = node._listeners && node._listeners[ev.type];
    if (ls) for (var i = 0; i < ls.length; i++) ls[i].call(node, ev);
    if (ev._stopped) break;
    node = node.parentNode;
  }
  return true;
};
El.prototype.click = function () { this.dispatchEvent({ type: 'click' }); };
El.prototype.descendants = function () {
  var out = [];
  (function walk(n) { n.children.forEach(function (c) { out.push(c); walk(c); }); })(this);
  return out;
};
El.prototype.querySelectorAll = function (sel) { return this.descendants().filter(function (n) { return matches(n, sel); }); };
El.prototype.querySelector = function (sel) { return this.querySelectorAll(sel)[0] || null; };

var docListeners = {};
var documentStub = {
  nodeName: '#document', _listeners: docListeners,
  body: new El('body'),
  createElement: function (t) { return new El(t); },
  getElementById: function () { return null; },
  addEventListener: function (t, fn) { (docListeners[t] = docListeners[t] || []).push(fn); },
  querySelectorAll: function (sel) { return documentStub.body.querySelectorAll(sel); },
  querySelector: function (sel) { return documentStub.body.querySelector(sel); }
};
documentStub.body.parentNode = documentStub;
globalThis.document = documentStub;

const m = await import(%q);
const scope = documentStub.body;

// ---- 场景 1：展开 → 选择 → 关闭 ----
var changes = [];
var dd = m.wbDropdown([['a', '选项A'], ['b', '选项B']], 'a', {
  key: 'core.button.value',
  onChange: function (v) { changes.push(v); }
});
scope.appendChild(dd.root);
var btn = dd.root.querySelector('.wb-dd-btn');
btn.click();
var opened = dd.root.classList.contains('is-open');
var openAfterDoc = dd.root.classList.contains('is-open');   // document 监听若触发会立刻置 false
var itemB = dd.root.querySelectorAll('.wb-dd-item')[1];
itemB.click();
var closedAfter = !dd.root.classList.contains('is-open');

// ---- 场景 2：展开态跨 DOM 重建恢复（模拟 morph 整体替换）----
var dd2 = m.wbDropdown([['a', '选项A'], ['b', '选项B']], 'b', { key: 'core.button.value' });
scope.appendChild(dd2.root);
dd2.root.querySelector('.wb-dd-btn').click();
var keysBefore = m.wbOpenDropdownKeys(scope);
// 模拟 morph：旧节点被移除、增强阶段重建了一个全新节点（同 key）
dd2.root.parentNode.children.length = 0;
var dd3 = m.wbDropdown([['a', '选项A'], ['b', '选项B']], 'b', { key: 'core.button.value' });
scope.appendChild(dd3.root);
m.wbRestoreDropdowns(scope, keysBefore);
var restoredOpen = dd3.root.classList.contains('is-open');

// ---- 场景 3：点击下拉外部（document 级监听）关闭 ----
documentStub.body.click();
var closedByDoc = !dd3.root.classList.contains('is-open');

console.log(JSON.stringify({
  opened: opened,
  openAfterDoc: openAfterDoc,
  itemValue: dd.value,
  changeCalls: changes,
  closedAfter: closedAfter,
  btnText: btn.textContent,
  keysBefore: keysBefore,
  restoredOpen: restoredOpen,
  recreatedNode: dd3.root !== dd2.root,
  closedByDoc: closedByDoc
}));
`
