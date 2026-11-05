package templates

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// 判据：主题设置页三个全局块槽位（页眉 / 页脚 / 公告条）的「自定义」入口。
// 有候选内选中项 → 链接到该块的工作台画布（/workbench?block=<id>，与块列表行内编辑同一条链路）；
// 无选中 / 选中项不在候选内 → 不可点击（无 href、不进 Tab 序列）。
//
// 回显字段必须是后端真实字段 HeaderBlockID / FooterBlockID / AnnouncementBlockID
// （internal/module/project/inbound/http/project_page.go 的 themeSettingsData，经 adminui.ViewMap 投影）：
// 写错字段名时 Jet 直接报 "there is no field or method"，不会静默。

func themeGlobalBlockPageData(t *testing.T) map[string]any {
	t.Helper()
	data := siteStructureData()
	data["ThemeName"] = "春季促销版"
	data["ProjectID"] = "p1"
	data["ThemeID"] = "t1"
	data["ThemeSettings"] = "{}"
	data["Groups"] = []map[string]any{{
		"TitleKey": "", "TitleFallback": "颜色",
		"Fields": []map[string]any{
			{"Kind": "text", "LabelKey": "", "LabelFallback": "主色", "Name": "colors.primary", "Value": "", "Options": []map[string]any{}},
		},
	}}
	// 三个槽位的当前绑定：后端 DTO 恒带这三个键，测试数据也要齐（默认未设置 = 空串）。
	data["HeaderBlockID"] = ""
	data["FooterBlockID"] = ""
	data["AnnouncementBlockID"] = ""
	// 候选第一项是后端给的「未设置」项（ID 为空串）。
	data["HeaderBlocks"] = []map[string]any{
		{"ID": "", "NameKey": "", "NameFallback": "（未设置）"},
		{"ID": "blk-h", "NameKey": "", "NameFallback": "站点页眉"},
	}
	data["FooterBlocks"] = []map[string]any{
		{"ID": "", "NameKey": "", "NameFallback": "（未设置）"},
		{"ID": "blk-f", "NameKey": "", "NameFallback": "站点页脚"},
	}
	data["AnnouncementBlocks"] = []map[string]any{
		{"ID": "", "NameKey": "", "NameFallback": "（未设置）"},
		{"ID": "blk-a", "NameKey": "", "NameFallback": "公告条"},
	}
	data["HeaderTemplateOptions"] = []map[string]any{}
	data["FooterTemplateOptions"] = []map[string]any{}
	return data
}

func renderThemeGlobalBlockPage(t *testing.T, data map[string]any) string {
	t.Helper()
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	out, err := render(t, set, "admin/project/theme_settings", data)
	if err != nil {
		t.Fatalf("主题设置页渲染失败: %v", err)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("输出里残留模板语法字面量")
	}
	if !strings.Contains(out, "</html>") {
		t.Fatalf("页面没有渲染到布局尾部（渲染在中途中断）")
	}
	return out
}

// 每个槽位的入口宿主节点：data-block-customize="<select 的 name>"。
var themeCustomizeHostRe = regexp.MustCompile(
	`(?s)<div class="form-inline" data-block-customize="([^"]+)"[^>]*>(.*?)</div>`)

func themeCustomizeHosts(t *testing.T, out string) map[string]string {
	t.Helper()
	hosts := map[string]string{}
	for _, m := range themeCustomizeHostRe.FindAllStringSubmatch(out, -1) {
		hosts[m[1]] = m[2]
	}
	return hosts
}

func themeSelectedOption(t *testing.T, out, slot string) string {
	t.Helper()
	selRe := regexp.MustCompile(`(?s)<select id="[^"]*" name="` + regexp.QuoteMeta(slot) + `"[^>]*>(.*?)</select>`)
	m := selRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("找不到槽位 %s 的 select（提交键改名？）", slot)
	}
	o := regexp.MustCompile(`<option value="([^"]*)"\s+selected`).FindStringSubmatch(m[1])
	if o == nil {
		return ""
	}
	return o[1]
}

func TestThemeGlobalBlockCustomizeFollowsRealFields(t *testing.T) {
	data := themeGlobalBlockPageData(t)
	// 真实字段（与后端 themeSettingsData 同名）：三个槽位各选一个候选内的块。
	data["HeaderBlockID"] = "blk-h"
	data["FooterBlockID"] = "blk-f"
	data["AnnouncementBlockID"] = "blk-a"

	out := renderThemeGlobalBlockPage(t, data)

	want := map[string]string{
		"headerBlockId":      "blk-h",
		"footerBlockId":      "blk-f",
		"slots.announcement": "blk-a",
	}
	hosts := themeCustomizeHosts(t, out)
	if len(hosts) != len(want) {
		t.Fatalf("三个槽位应各有一个自定义入口，实际 %d 个: %v", len(hosts), keysOfStringMap(hosts))
	}
	for slot, id := range want {
		body, ok := hosts[slot]
		if !ok {
			t.Fatalf("槽位 %s 缺少自定义入口（宿主节点 data-block-customize=%q 不存在）", slot, slot)
		}
		if !strings.Contains(body, `href="/workbench?block=`+id+`"`) {
			t.Fatalf("槽位 %s 的自定义入口应指向选中的块 %q，实际: %s", slot, id, body)
		}
		if !strings.Contains(body, "data-block-customize-link") || strings.Contains(body, "disabled") {
			t.Fatalf("槽位 %s 有选中项，入口应是可点击链接: %s", slot, body)
		}
		for otherSlot, otherID := range want {
			if otherSlot != slot && strings.Contains(body, otherID) {
				t.Fatalf("槽位 %s 的入口指向了 %s 的块: %s", slot, otherSlot, body)
			}
		}
		// 服务端 selected 也走真实字段：选中的 option 必须就是绑定的那个 id。
		if got := themeSelectedOption(t, out, slot); got != id {
			t.Fatalf("槽位 %s 的选中项应是 %q，实际 %q（selected 未生效 —— 回显字段与后端不一致）", slot, id, got)
		}
	}
}

func TestThemeGlobalBlockCustomizeDisabledWithoutCandidate(t *testing.T) {
	cases := []struct {
		name   string
		data   map[string]any
		linked map[string]string
	}{
		{
			name:   "全空字符串",
			data:   map[string]any{"HeaderBlockID": "", "FooterBlockID": "", "AnnouncementBlockID": ""},
			linked: map[string]string{},
		},
		{
			name: "候选外的 id 不给链接",
			// 后端还存着这个绑定，但块已不在候选里（删了 / 别的工程）：
			// 入口必须不可点击，不能拿绑定 id 拼 URL 去工作台猜。
			data:   map[string]any{"HeaderBlockID": "blk-gone", "FooterBlockID": "blk-f"},
			linked: map[string]string{"footerBlockId": "blk-f"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := themeGlobalBlockPageData(t)
			for k, v := range tc.data {
				data[k] = v
			}
			hosts := themeCustomizeHosts(t, renderThemeGlobalBlockPage(t, data))

			for _, slot := range []string{"headerBlockId", "footerBlockId", "slots.announcement"} {
				body, ok := hosts[slot]
				if !ok {
					t.Fatalf("槽位 %s 缺少自定义入口", slot)
				}
				if id, linked := tc.linked[slot]; linked {
					if !strings.Contains(body, `href="/workbench?block=`+id+`"`) {
						t.Fatalf("槽位 %s 有候选内选中项，入口应指向 %q: %s", slot, id, body)
					}
					continue
				}
				if strings.Contains(body, "href=") || strings.Contains(body, "data-block-customize-link") {
					t.Fatalf("槽位 %s 没有候选内选中项，入口不该有链接: %s", slot, body)
				}
				if !strings.Contains(body, "<button") || !strings.Contains(body, "disabled") {
					t.Fatalf("槽位 %s 没有候选内选中项，入口应是 disabled 的 button: %s", slot, body)
				}
				if !strings.Contains(body, `aria-disabled="true"`) || !strings.Contains(body, `type="button"`) {
					t.Fatalf("槽位 %s 的不可点击入口缺 aria-disabled / type=button: %s", slot, body)
				}
			}
		})
	}
}

// 页面脚本的同步逻辑：沿用本仓已有的 node --eval + 最小 DOM 桩做法
// （ui_assets_test.go / select_contract_test.go），执行的是页面 <script> 原文。
func TestThemeGlobalBlockCustomizeScriptSyncsHref(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("自定义入口脚本检查要求 Node")
		}
		t.Skip("缺少 Node")
	}

	data := themeGlobalBlockPageData(t)
	data["HeaderBlockID"] = "blk-h"
	data["FooterBlockID"] = ""

	out := renderThemeGlobalBlockPage(t, data)
	script := themePageScript(t, out)
	if strings.TrimSpace(script) == "" {
		t.Fatal("主题设置页没有 script（回填与自定义入口同步已丢失）")
	}

	cmd := exec.Command(node, "--eval", themeCustomizeProbe)
	cmd.Env = append(os.Environ(), "THEME_PAGE_SCRIPT="+script)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("自定义入口同步契约失败: %v\n%s", err, o)
	}
}

func themePageScript(t *testing.T, out string) string {
	t.Helper()
	const open = "<script>"
	i := strings.LastIndex(out, open)
	if i < 0 {
		return ""
	}
	rest := out[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func keysOfStringMap(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// 探针脚本：最小 DOM 桩执行页面脚本。
const themeCustomizeProbe = `
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const pageScript = process.env.THEME_PAGE_SCRIPT || '';

class El {
  constructor(tag) {
    this.tagName = tag.toUpperCase();
    this.attrs = {};
    this.children = [];
    this.textContent = '';
    this._value = '';
    this.listeners = {};
  }
  get className() { return this.attrs['class'] || ''; }
  set className(v) { this.attrs['class'] = v; }
  // option 的 value 就是 value 属性（页面脚本靠它比对候选）；select 等仍走 _value。
  get value() { return this.tagName === 'OPTION' ? (this.attrs['value'] || '') : this._value; }
  set value(v) { this._value = String(v); this.attrs['value'] = String(v); }
  get options() { return this.children.filter(c => c.tagName === 'OPTION'); }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; }
  removeAttribute(k) { delete this.attrs[k]; }
  hasAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k); }
  appendChild(n) { this.children.push(n); return n; }
  set innerHTML(v) { if (v === '') this.children = []; }
  addEventListener(type, fn) { (this.listeners[type] || (this.listeners[type] = [])).push(fn); }
  fire(type) { (this.listeners[type] || []).slice().forEach(fn => fn.call(this, {type, target: this})); }
  all() { const out = []; const walk = n => n.children.forEach(c => { out.push(c); walk(c); }); walk(this); return out; }
  querySelector(sel) { return this.all().find(n => matches(n, sel)) || null; }
  querySelectorAll(sel) { return this.all().filter(n => matches(n, sel)); }
}

// 复合选择器：纯 tag / 纯 [attr] / tag[attr=value]。页面脚本只用这三类。
function matchesCompound(node, one) {
  if (one.startsWith('[') && one.endsWith(']')) {
    const body = one.slice(1, -1), eq = body.indexOf('=');
    if (eq < 0) return node.hasAttribute(body);
    let val = body.slice(eq + 1);
    if (val.length > 1 && (val[0] === val[val.length - 1]) && (val[0] === '"' || val[0] === "'")) val = val.slice(1, -1);
    return node.getAttribute(body.slice(0, eq)) === val;
  }
  if (/^[a-zA-Z]+$/.test(one)) return node.tagName === one.toUpperCase();
  const m = /^([a-zA-Z]+)\[([^\]=]+)=(["']?)(.*)\3\]$/.exec(one);
  if (m) return node.tagName === m[1].toUpperCase() && node.getAttribute(m[2]) === m[4];
  throw new Error('探针 DOM 桩不支持的选择器: ' + one);
}

function matches(node, sel) {
  return String(sel).split(',').map(s => s.trim()).filter(Boolean).some(one => matchesCompound(node, one));
}

// select 自带服务端渲染好的选项与选中态；入口是服务端那一版（链接或空态 button）。
function buildPage(slots) {
  const root = new El('div'), form = new El('form');
  form.attrs['id'] = 'theme-settings-form';
  const json = new El('script');
  json.attrs['id'] = 'theme-settings-json';
  json.textContent = JSON.stringify({headerBlockId: 'blk-h', footerBlockId: '', slots: {announcement: ''}});
  root.appendChild(json); root.appendChild(form);
  slots.forEach(slot => {
    const select = new El('select');
    select.attrs['name'] = slot.name;
    select.value = slot.selected;
    // 真 DOM 里 option.value 读的是 value 属性；桩要映射，否则 optionExists 恒 false。
    slot.ids.forEach(id => { const o = new El('option'); o.setAttribute('value', id); select.appendChild(o); });
    form.appendChild(select);

    const wrap = new El('div');
    wrap.attrs['class'] = 'form-inline';
    wrap.attrs['data-block-customize'] = slot.name;
    wrap.attrs['data-customize-empty-title'] = '先选一个块再自定义';
    const node = new El(slot.serverLink ? 'a' : 'button');
    node.attrs['class'] = 'btn btn-sm btn-secondary';
    node.textContent = '自定义';
    if (slot.serverLink) {
      node.attrs['data-block-customize-link'] = '';
      node.attrs['href'] = slot.serverLink;
    } else {
      node.attrs['type'] = 'button';
      node.attrs['data-block-customize-empty'] = '';
      node.attrs['disabled'] = '';
      node.attrs['aria-disabled'] = 'true';
    }
    wrap.appendChild(node);
    form.appendChild(wrap);
  });
  return {root, form};
}

const page = buildPage([
  {name: 'headerBlockId', ids: ['', 'blk-h', 'blk/f x&y'], selected: 'blk-h', serverLink: '/workbench?block=blk-h'},
  {name: 'footerBlockId', ids: ['', 'blk-f'], selected: '', serverLink: ''},
  {name: 'slots.announcement', ids: ['', 'blk-a'], selected: '', serverLink: ''},
]);
const document = {
  getElementById: id => page.root.all().find(n => n.getAttribute('id') === id) || null,
  createElement: tag => new El(tag),
};
const ctx = {document, window: {}, console, encodeURIComponent, JSON, String, Object, Array};

vm.runInNewContext(pageScript, ctx);
assert.equal(page.form.getAttribute('data-theme-settings-ready'), '1', '初始化标记没有落到表单上');

const entry = name => {
  const host = page.form.querySelector('[data-block-customize="' + name + '"]');
  return host.querySelector('[data-block-customize-link]') || host.querySelector('[data-block-customize-empty]');
};
const selectOf = name => page.form.querySelectorAll('select[name="' + name + '"]')[0];

// 1) 初始化后：服务端 selected=blk-h，入口是可点击链接，指向选中块
let header = entry('headerBlockId');
assert.equal(header.tagName, 'A', '有选择时入口应是 a');
assert.equal(header.getAttribute('href'), '/workbench?block=blk-h', '初始化后 href 没指向选中块');
assert.equal(header.hasAttribute('disabled'), false, '有选择时入口不该带 disabled');
assert.equal(header.textContent, '自定义', '入口文案应在改写时保留');

// 2) 空选择：button + disabled + 无 href
['footerBlockId', 'slots.announcement'].forEach(name => {
  const e = entry(name);
  assert.equal(e.tagName, 'BUTTON', name + ' 空选择时入口应是 button');
  assert.equal(e.hasAttribute('disabled'), true, name + ' 空选择时入口应 disabled');
  assert.equal(e.getAttribute('href'), null, name + ' 空选择时入口不该有 href');
});

// 3) 换选新值：href 跟着变，id 走 encodeURIComponent
const headerSelect = selectOf('headerBlockId');
// 初始化没绑 change 时这条会失败：初始 href 由服务端数据给出，掩盖不了后面的交互。
headerSelect.value = 'blk/f x&y';
headerSelect.fire('change');
header = entry('headerBlockId');
assert.equal(header.getAttribute('href'), '/workbench?block=' + encodeURIComponent('blk/f x&y'),
  '换选后 href 没有跟着变 / 没有编码 id');

// 4) 选到候选之外：不给链接
headerSelect.value = 'blk-not-in-list';
headerSelect.fire('change');
header = entry('headerBlockId');
assert.equal(header.tagName, 'BUTTON', '候选外的值不该渲染成链接');
assert.equal(header.getAttribute('type'), 'button', '候选外的值生成的 button 也必须 type=button');
assert.equal(header.getAttribute('href'), null, '候选外的值不该给出 href');

// 5) 清空：变回不可点击，且必须是 type=button（默认 submit 会误触保存）
headerSelect.value = '';
headerSelect.fire('change');
header = entry('headerBlockId');
assert.equal(header.tagName, 'BUTTON', '清空选择后入口应变回 button');
assert.equal(header.getAttribute('type'), 'button', '脚本生成的 button 必须显式 type=button');
assert.equal(header.hasAttribute('disabled'), true, '清空选择后入口应 disabled');
assert.equal(header.getAttribute('href'), null, '清空选择后入口不该保留 href');
assert.equal(header.getAttribute('data-block-customize-link'), null, '清空选择后不该还挂着链接标记');

// 6) 三个槽位各自独立
const annSelect = selectOf('slots.announcement');
annSelect.value = 'blk-a';
annSelect.fire('change');
assert.equal(entry('slots.announcement').getAttribute('href'), '/workbench?block=blk-a', '公告条入口没跟着选中值走');
assert.equal(entry('footerBlockId').hasAttribute('disabled'), true, '公告条换选不该影响页脚槽位');

// 7) 重复执行：同步逻辑不翻倍、状态不漂移
vm.runInNewContext(pageScript, ctx);
annSelect.value = '';
annSelect.fire('change');
assert.equal(entry('slots.announcement').hasAttribute('disabled'), true, '重复执行后同步逻辑异常');
console.log('ok');
`
