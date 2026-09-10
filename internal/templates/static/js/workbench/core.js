// workbench/core.js — 工作台前端共享内核：状态来源、CSRF、通用控件与组件清单。
// 由 workbench/index.js 组装（docs/09 §3 拆分）；ES module，无副作用（仅解析注入的 JSON）。
// 无 DOM 环境（node 契约测试直接 import 本模块求值纯函数）下不抛错：
// 缺 document 时 meta/initialDoc 退化为空对象，浏览器行为不变。
const wbMetaEl = typeof document !== 'undefined' ? document.getElementById('wb-meta') : null;
export const meta = wbMetaEl && wbMetaEl.textContent ? JSON.parse(wbMetaEl.textContent) : {};
export let initialDoc = null;
    if (typeof document !== 'undefined') {
        try { initialDoc = JSON.parse(document.getElementById('wb-bootstrap').textContent || '{}'); } catch (e) { /* 空文档兜底 */ }
    }
    if (!initialDoc) initialDoc = { settings: {}, root: [] };
    if (!Array.isArray(initialDoc.root)) initialDoc.root = [];
    if (!initialDoc.settings) initialDoc.settings = {};

    /**
     * CSRF token 统一获取：优先服务端注入（<meta name="csrf-token">，每次页面渲染新鲜，
     * 由 workbench/layout.html 输出 withCSRF 注入的 token）；兜底登录时写入的 sessionStorage
     * （login.html 登录成功回调里存的 csrf_token，适用于 meta 缺失/旧的场景）。
     */
export function getCSRFToken() {
        var m = document.querySelector('meta[name="csrf-token"]');
        if (m && m.getAttribute('content')) return m.getAttribute('content');
        try { return sessionStorage.getItem('csrf_token') || ''; } catch (e) { return ''; }
    }

    /** 给写请求附上 X-CSRF-Token 头；extra 中的既有头保留（调用方负责 Content-Type）。 */
export function csrfHeaders(extra) {
        var h = extra || {};
        var t = getCSRFToken();
        if (t) h['X-CSRF-Token'] = t;
        return h;
    }

    /**
     * morphHTML 用 idiomorph 做 DOM morphing 替换元素内部内容（docs/06-C §四）。
     *
     * 与 el.innerHTML = html 的区别：idiomorph 按 id 集合 + 软匹配（同 nodeType/tagName）
     * 复用未变节点，只更新差异部分，因此面板重渲染后滚动位置、输入焦点、展开的分组
     * 都不会复位。morphStyle:'innerHTML' 表示只 morph 子节点，元素自身（含 data-wb-bound
     * 之类的一次性绑定标记）不动。
     *
     * 降级：idiomorph 未加载（vendor 缺失 / 脚本被拦 / 未来换构建）或 morph 抛错时，
     * 退回整块 innerHTML，行为与改造前完全一致——本函数只做替换方式升级，不改调用方语义。
     */
export function morphHTML(el, html) {
        if (!el) return;
        var lib = window.Idiomorph;
        if (lib && typeof lib.morph === 'function') {
            // <details> 折叠态保留：检查器分组是原生 <details open>，服务端 open 由
            // s.Open（分组内有自定义值）决定。用户手动折叠后重渲染时，若分组键
            // （data-wb-section）没变，就拦下 open 属性更新，让用户的折叠态留住；
            // 分组键变了（切到另一组件、分组集合不同）则不拦，服务端默认展开语义照常生效，
            // 避免「新组件该展开的分组因为上个组件的折叠态而藏起来」。
            var keepClosed = new WeakSet();
            var options = {
                morphStyle: 'innerHTML',
                callbacks: {
                    beforeNodeMorphed: function (oldNode, newNode) {
                        if (oldNode && newNode && oldNode.nodeName === 'DETAILS' && newNode.nodeName === 'DETAILS'
                            && oldNode.getAttribute && oldNode.getAttribute('data-wb-section')
                            && oldNode.getAttribute('data-wb-section') === newNode.getAttribute('data-wb-section')
                            && !oldNode.open) {
                            keepClosed.add(oldNode);
                        }
                        return true;
                    },
                    beforeAttributeUpdated: function (name, node) {
                        if (name === 'open' && keepClosed.has(node)) return false;
                        return true;
                    }
                }
            };
            try {
                lib.morph(el, html, options);
                return;
            } catch (e) {
                // morph 失败不让面板空掉：记录原因后走 innerHTML 兜底。
                console.error('[workbench] idiomorph morph 失败，回退 innerHTML', e);
            }
        }
        el.innerHTML = html;
    }

    /**
     * 自定义下拉（替换面板内原生 select）。
     *
     * 根因：Linux/Chromium 原生 select 为「按下展开、松开即选」交互，弹层首项
     * 恰好压在 select 原位置，单击的松开动作会立即选中首项并收起——表现为
     * 「下拉一出来瞬间就选中了，无法做出选择」。本组件用 click 展开、click
     * 选项、点击外部关闭，三端交互一致。
     *
     * choices: [[value, label], ...]；current: 当前值；
     * opts.onChange(value) 仅在用户点选时回调（程序化赋值不触发，对齐原生 change 语义）。
     *
     * 返回对象带 value getter/setter：主题面板 allInputs 收集只用 .value；
     * root 是渲染 DOM（.wb-dd），样式见 workbench.css。
     */
export function wbDropdown(choices, current, opts) {
        opts = opts || {};
        var root = document.createElement('div'); root.className = 'wb-dd';
        // 稳定 key（字段路径）：面板重渲染后据此恢复展开态，见 wbOpenDropdownKeys。
        if (opts.key) root.dataset.wbDdKey = String(opts.key);
        var btn = document.createElement('button'); btn.type = 'button'; btn.className = 'wb-dd-btn';
        var list = document.createElement('div'); list.className = 'wb-dd-list';
        var value = String(current == null ? '' : current);
        function labelOf(v) {
            for (var i = 0; i < choices.length; i++) {
                if (String(choices[i][0]) === v) return choices[i][1];
            }
            return opts.placeholder || (choices.length ? choices[0][1] : '');
        }
        function close() { root.classList.remove('is-open'); }
        btn.textContent = labelOf(value);
        choices.forEach(function (ch) {
            var item = document.createElement('button'); item.type = 'button'; item.className = 'wb-dd-item';
            item.textContent = ch[1];
            if (String(ch[0]) === value) item.classList.add('is-active');
            item.addEventListener('click', function (e) {
                e.stopPropagation();
                value = String(ch[0]);
                btn.textContent = labelOf(value);
                Array.from(list.querySelectorAll('.wb-dd-item')).forEach(function (x) { x.classList.toggle('is-active', x === item); });
                close();
                if (opts.onChange) opts.onChange(value);
            });
            list.appendChild(item);
        });
        btn.addEventListener('click', function (e) {
            e.stopPropagation();
            var wasOpen = root.classList.contains('is-open');
            closeAllDropdowns();
            if (!wasOpen) root.classList.add('is-open');
        });
        root.appendChild(btn); root.appendChild(list);
        return {
            root: root,
            get value() { return value; },
            set value(v) { value = String(v == null ? '' : v); btn.textContent = labelOf(value); }
        };
    }

    /** 关闭页面上所有已展开的自定义下拉（点击外部 / 打开另一个前调用）。 */
export function closeAllDropdowns() {
        if (typeof document === 'undefined') return;
        Array.from(document.querySelectorAll('.wb-dd.is-open')).forEach(function (d) { d.classList.remove('is-open'); });
    }
    if (typeof document !== 'undefined') document.addEventListener('click', function () { closeAllDropdowns(); });

    /**
     * wbOpenDropdownKeys / wbRestoreDropdowns：下拉展开态跨面板重渲染保留。
     *
     * 根因：检查器面板由服务端片段经 idiomorph morph 整体替换，而 wb-dd 是客户端
     * 创建的节点（服务端片段里没有），morph 必然删掉旧节点、增强阶段再建一个新的
     * （实测：重渲染后旧节点 isConnected=false，.wb-dd.is-open 数量归零）。
     * 于是「面板重渲染」= 「下拉被强制收起」。这里用稳定 key（字段路径）在
     * 重渲染前后做状态搬运，而不是靠 setTimeout 之类时序补丁。
     */
export function wbOpenDropdownKeys(scope) {
        var root = scope || (typeof document !== 'undefined' ? document : null);
        if (!root || !root.querySelectorAll) return [];
        return Array.prototype.map.call(root.querySelectorAll('.wb-dd.is-open'), function (d) {
            return (d.dataset && d.dataset.wbDdKey) || '';
        }).filter(function (k) { return !!k; });
    }
export function wbRestoreDropdowns(scope, keys) {
        if (!keys || !keys.length) return;
        var root = scope || (typeof document !== 'undefined' ? document : null);
        if (!root || !root.querySelectorAll) return;
        keys.forEach(function (k) {
            var sel = '.wb-dd[data-wb-dd-key="' + String(k).replace(/["\\]/g, '') + '"]';
            var el = root.querySelector(sel);
            if (el) el.classList.add('is-open');
        });
    }

    /**
     * wbActionValue — core.button「点击动作 → 动作值」联动（纯函数，无 DOM）。
     *
     * 根因：action 与 value 是两个独立字段，切换 action 时 value 不联动，于是
     * 「internal + /shop」切成 native 后 value 仍是 /shop，构建期校验直接失败
     * （button.go validateExtra 的 native 分支只收 tel:/mailto:）。
     *
     * 这里把 value 自动调整为新动作的合法形态；规则逐条镜像 Go 侧校验（校验本身
     * 是唯一真源，不改）：
     *   internal  ^/[A-Za-z0-9/-]{0,200}$                      （站内路径）
     *   external  前缀 http://|https:// 且字符集 [A-Za-z0-9./:?=&%~#+_@-]
     *   anchor    ^[A-Za-z][A-Za-z0-9_-]{0,63}$                （裸 ID，渲染时 jet.go 补 #）
     *   modal     同 anchor
     *   native    ^(tel:|mailto:)[^\s]{3,200}$
     *
     * 返回 { value, hint }：hint 非空表示「无法从旧值推出合法值」，已退化为该动作的
     * 合法前缀骨架 + 面板提示（不伪造号码/邮箱），用户在值里补全即可。
     */
export function wbActionValue(action, raw) {
        var value = String(raw == null ? '' : raw).trim();
        switch (String(action || '')) {
            case 'internal': {
                if (/^\/[A-Za-z0-9/-]{0,200}$/.test(value)) return { value: value, hint: '' };
                return { value: wbInternalPathOf(value), hint: '' };
            }
            case 'external': {
                if (/^https?:\/\/[A-Za-z0-9.\/:?=&%~#+_@-]*$/.test(value)) return { value: value, hint: '' };
                var bare = value.replace(/^\/+/, '');
                if (bare && /^[A-Za-z0-9.\/:?=&%~#+_@-]+$/.test(bare)) return { value: 'https://' + bare, hint: '' };
                return { value: 'https://', hint: '外链需为 http(s):// 开头的完整地址，请补全域名' };
            }
            case 'anchor':
            case 'modal': {
                var id = wbAnchorIDOf(value);
                if (id) return { value: id, hint: '' };
                return { value: '', hint: (action === 'modal' ? '弹窗' : '锚点') + '需填写页内元素 ID（字母开头，不含 #），如 contact' };
            }
            case 'native': {
                if (/^(tel:|mailto:)\S{3,200}$/.test(value)) return { value: value, hint: '' };
                var mail = value.match(/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/);
                if (mail) return { value: 'mailto:' + mail[0], hint: '' };
                var phone = value.replace(/[^0-9+]/g, '');
                if (phone.replace(/[^0-9]/g, '').length >= 3) return { value: 'tel:' + phone, hint: '' };
                return { value: 'tel:', hint: '电话/邮件需补全 tel:号码 或 mailto:邮箱（补全前预览会编译失败）' };
            }
            case 'link':
                // 动态绑定动作不需要 value，保持原值不误删用户输入。
                return { value: value, hint: '' };
            default:
                return { value: value, hint: '' };
        }
    }
    /** wbInternalPathOf 从任意旧值里抽出合法站内路径（URL → 取 pathname；无则回退 /）。 */
export function wbInternalPathOf(raw) {
        var s = String(raw == null ? '' : raw).trim();
        if (!s || /^[A-Za-z][A-Za-z0-9+.-]*:/.test(s)) {
            var m = s.match(/^https?:\/\/[^/]*(\/[^\s?#]*)?/);
            if (!m) return '/';
            s = m[1] || '/';
        }
        s = s.replace(/^#/, '');
        if (s.charAt(0) !== '/') s = '/' + s;
        s = s.replace(/[^A-Za-z0-9/-]/g, '-').replace(/-{2,}/g, '-').slice(0, 200);
        return s || '/';
    }
    /** wbAnchorIDOf 锚点/弹窗目标 ID：合法直接用；带 # 或路径/URL 时取末段派生。 */
export function wbAnchorIDOf(raw) {
        var s = String(raw == null ? '' : raw).trim().replace(/^#/, '');
        if (/^[A-Za-z][A-Za-z0-9_-]{0,63}$/.test(s)) return s;
        var seg = s.replace(/[?#].*$/, '').split('/').filter(function (t) { return !!t; }).pop() || '';
        seg = seg.replace(/[^A-Za-z0-9_-]/g, '');
        if (/^[A-Za-z][A-Za-z0-9_-]{0,63}$/.test(seg)) return seg;
        return '';
    }
    /**
     * WB_FIELD_LINKS 字段联动表（声明式）：源字段变化 → 目标字段按 resolver 归一。
     * 新增联动只加一行，不用改提交路径；resolve(源值, 目标旧值) → { value, hint }。
     */
export const WB_FIELD_LINKS = {
        'core.button': {
            action: [{ path: 'value', resolve: wbActionValue }]
        }
    };
    /**
     * wbLinkedPatch 计算联动结果（纯函数）：changedPath 形如 'props.action'。
     * 返回 null 表示该字段没有声明联动；否则返回 [{ path, value, changed }]。
     */
export function wbLinkedPatch(node, changedPath) {
        if (!node || !node.type) return null;
        var spec = WB_FIELD_LINKS[node.type];
        if (!spec) return null;
        var m = /^props\.([A-Za-z0-9_.]+)$/.exec(String(changedPath || ''));
        if (!m) return null;
        var links = spec[m[1]];
        if (!links) return null;
        var props = node.props || {};
        var src = props[m[1]];
        return links.map(function (link) {
            var old = props[link.path];
            var res = link.resolve(src, old);
            var oldStr = old == null ? '' : String(old);
            return { path: 'props.' + link.path, value: res.value, changed: oldStr !== res.value };
        });
    }
    /**
     * wbFieldHints 当前节点上「已声明联动但值仍非法」的提示（面板就地展示）。
     * 用同一份 wbActionValue 判定，避免提示与联动规则漂移。
     */
export function wbFieldHints(node) {
        var out = [];
        if (!node || !node.type) return out;
        var spec = WB_FIELD_LINKS[node.type];
        if (!spec) return out;
        var props = node.props || {};
        Object.keys(spec).forEach(function (srcKey) {
            spec[srcKey].forEach(function (link) {
                var res = link.resolve(props[srcKey], props[link.path]);
                if (res.hint) out.push({ path: 'props.' + link.path, text: res.hint });
            });
        });
        return out;
    }

    /** 解析颜色值：支持 #rgb/#rgba/#rrggbb/#rrggbbaa、rgb()/rgba()、transparent；不可解析返回 null。 */
export function wbParseColor(raw) {
        var str = String(raw == null ? '' : raw).trim().toLowerCase();
        if (!str) return null;
        if (str === 'transparent' || str === 'none') return { h: 0, s: 0, v: 0, a: 0 };
        var hsv, m = str.match(/^#([0-9a-f]{3,8})$/);
        if (m) {
            var x = m[1];
            if (x.length === 3 || x.length === 4) x = x.split('').map(function (c) { return c + c; }).join('');
            if (x.length !== 6 && x.length !== 8) return null;
            hsv = wbRgbToHsv(parseInt(x.slice(0, 2), 16), parseInt(x.slice(2, 4), 16), parseInt(x.slice(4, 6), 16));
            return { h: hsv[0], s: hsv[1], v: hsv[2], a: x.length === 8 ? parseInt(x.slice(6, 8), 16) / 255 : 1 };
        }
        var m2 = str.match(/^rgba?\(([^)]+)\)$/);
        if (m2) {
            var p = m2[1].split(/[,\s\/]+/).filter(function (t) { return t !== ''; });
            if (p.length < 3) return null;
            var ch = function (t) { return t.indexOf('%') >= 0 ? parseFloat(t) / 100 * 255 : parseFloat(t); };
            hsv = wbRgbToHsv(ch(p[0]), ch(p[1]), ch(p[2]));
            var al = p.length > 3 ? (p[3].indexOf('%') >= 0 ? parseFloat(p[3]) / 100 : parseFloat(p[3])) : 1;
            if (isNaN(al)) al = 1;
            return { h: hsv[0], s: hsv[1], v: hsv[2], a: Math.max(0, Math.min(1, al)) };
        }
        var m3 = str.match(/^hsla?\(([^)]+)\)$/);
        if (m3) {
            var q = m3[1].split(/[,\s\/]+/).filter(function (t) { return t !== ''; });
            if (q.length < 3) return null;
            var hh = parseFloat(q[0]), ss = parseFloat(q[1]), ll = parseFloat(q[2]);
            if (isNaN(hh) || isNaN(ss) || isNaN(ll)) return null;
            var hsv3 = wbHslToHsv(((hh % 360) + 360) % 360, Math.max(0, Math.min(100, ss)), Math.max(0, Math.min(100, ll)));
            var al3 = q.length > 3 ? (q[3].indexOf('%') >= 0 ? parseFloat(q[3]) / 100 : parseFloat(q[3])) : 1;
            if (isNaN(al3)) al3 = 1;
            return { h: hsv3[0], s: hsv3[1], v: hsv3[2], a: Math.max(0, Math.min(1, al3)) };
        }
        return null;
    }
    /** HSL(0-360, 0-100, 0-100) → HSV(0-360, 0-1, 0-1)。 */
export function wbHslToHsv(hDeg, sPct, lPct) {
        var s = sPct / 100, l = lPct / 100;
        var v = l + s * Math.min(l, 1 - l);
        var sv = v === 0 ? 0 : 2 * (1 - l / v);
        return [hDeg, sv, v];
    }
export function wbHsvToRgb(h, s, v) {
        h = ((h % 360) + 360) % 360;
        var c = v * s, x = c * (1 - Math.abs(((h / 60) % 2) - 1)), m = v - c, r = 0, g = 0, b = 0;
        if (h < 60) { r = c; g = x; } else if (h < 120) { r = x; g = c; }
        else if (h < 180) { g = c; b = x; } else if (h < 240) { g = x; b = c; }
        else if (h < 300) { r = x; b = c; } else { r = c; b = x; }
        return [Math.round((r + m) * 255), Math.round((g + m) * 255), Math.round((b + m) * 255)];
    }
export function wbRgbToHsv(r, g, b) {
        r /= 255; g /= 255; b /= 255;
        var max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min, h = 0;
        if (d) {
            if (max === r) h = 60 * (((g - b) / d) % 6);
            else if (max === g) h = 60 * ((b - r) / d + 2);
            else h = 60 * ((r - g) / d + 4);
        }
        if (h < 0) h += 360;
        return [h, max ? d / max : 0, max];
    }
export const WB_CP_CHECKER = 'repeating-conic-gradient(#d0d5dd 0% 25%, #fff 0% 50%)';

    /**
     * wbColorPicker 颜色控件（WP 风格内联取色器）。
     *
     * 结构：[文本输入框][色块按钮] + 浮层（饱和度/明度方块 + 色相条 + 透明度条 + 预览 + 预设色 + 透明按钮）。
     * 透明与 WP 一致：直接把不透明度拉到 0（或点「透明」），输出 transparent；
     * 半透明输出 rgba(r, g, b, a)；不透明输出 #rrggbb。文本框接受任意 CSS 值（含 var(--token)）。
     *
     * opts: { value, placeholder, onInput(value, final) }
     * 返回带 value getter/setter 的 root（可放进主题面板 allInputs 收集，对齐 wbDropdown）。
     */
export function wbColorPicker(opts) {
        opts = opts || {};
        var root = document.createElement('div'); root.className = 'wb-color-row';
        var input = document.createElement('input'); input.type = 'text'; input.className = 'wb-color-text';
        input.placeholder = opts.placeholder || '#rrggbb / transparent / var(--token)';
        input.value = opts.value == null ? '' : String(opts.value);
        var swatch = document.createElement('button'); swatch.type = 'button'; swatch.className = 'wb-color-swatch';
        swatch.title = '选择颜色 / 调整透明度';

        var panel = document.createElement('div'); panel.className = 'wb-cp'; panel.hidden = true;
        var sv = document.createElement('div'); sv.className = 'wb-cp-sv';
        var svDot = document.createElement('span'); svDot.className = 'wb-cp-dot'; sv.appendChild(svDot);
        var hue = document.createElement('div'); hue.className = 'wb-cp-strip wb-cp-hue';
        var hueDot = document.createElement('span'); hueDot.className = 'wb-cp-dot'; hue.appendChild(hueDot);
        var alpha = document.createElement('div'); alpha.className = 'wb-cp-strip wb-cp-alpha';
        var alphaDot = document.createElement('span'); alphaDot.className = 'wb-cp-dot'; alpha.appendChild(alphaDot);
        var row = document.createElement('div'); row.className = 'wb-cp-row';
        var preview = document.createElement('span'); preview.className = 'wb-cp-preview';
        var hex = document.createElement('input'); hex.type = 'text'; hex.className = 'wb-cp-hex';
        var clearBtn = document.createElement('button'); clearBtn.type = 'button'; clearBtn.className = 'wb-cp-btn';
        clearBtn.textContent = '透明';
        row.appendChild(preview); row.appendChild(hex); row.appendChild(clearBtn);
        // 格式切换 + 吸管 + 收藏（WP 式）
        var toolsRow = document.createElement('div'); toolsRow.className = 'wb-cp-tools';
        ['hexa', 'rgba', 'hsla'].forEach(function (f) {
            var b = document.createElement('button'); b.type = 'button';
            b.className = 'wb-cp-fmt' + (fmt === f ? ' is-active' : '');
            b.textContent = f.toUpperCase();
            b.addEventListener('click', function (e) {
                e.stopPropagation();
                fmt = f;
                Array.from(toolsRow.querySelectorAll('.wb-cp-fmt')).forEach(function (x) { x.classList.toggle('is-active', x === b); });
                sync();
            });
            toolsRow.appendChild(b);
        });
        if (window.EyeDropper) {
            var eyeBtn = document.createElement('button'); eyeBtn.type = 'button'; eyeBtn.className = 'wb-cp-btn';
            eyeBtn.textContent = '吸管';
            eyeBtn.title = '从屏幕取色';
            eyeBtn.addEventListener('click', function (e) {
                e.stopPropagation();
                new window.EyeDropper().open().then(function (res) {
                    applyRaw(res.sRGBHex); sync(); emit(true);
                }).catch(function () { /* 用户取消 */ });
            });
            toolsRow.appendChild(eyeBtn);
        }
        var favBtn = document.createElement('button'); favBtn.type = 'button'; favBtn.className = 'wb-cp-btn';
        favBtn.textContent = '收藏';
        favBtn.title = '把当前颜色加入收藏';
        favBtn.addEventListener('click', function (e) { e.stopPropagation(); addFav(value()); });
        toolsRow.appendChild(favBtn);
        var favRow = document.createElement('div'); favRow.className = 'wb-cp-presets';
        var presets = document.createElement('div'); presets.className = 'wb-cp-presets';
        panel.appendChild(sv); panel.appendChild(hue); panel.appendChild(alpha); panel.appendChild(row);
        panel.appendChild(toolsRow); panel.appendChild(favRow); panel.appendChild(presets);
        root.appendChild(input); root.appendChild(swatch); root.appendChild(panel);

        var h = 214, s = 0.83, v = 0.93, a = 1;
        var init = wbParseColor(opts.value);
        if (init) { h = init.h; s = init.s; v = init.v; a = init.a; }

        var fmt = 'hexa';   // 输出格式：hex / hexa / rgba / hsla
        function rgb() { return wbHsvToRgb(h, s, v); }
        function pad(n) { return ('0' + Math.round(n).toString(16)).slice(-2); }
        function alphaStr() { return String(Math.round(a * 100) / 100); }
        function value() {
            var c = rgb();
            if (a <= 0.002) return 'transparent';
            if (fmt === 'rgba') {
                return 'rgba(' + c[0] + ', ' + c[1] + ', ' + c[2] + ', ' + alphaStr() + ')';
            }
            if (fmt === 'hsla') {
                // HSV → HSL：亮度 L = V(1 - S/2)，饱和度 S_hsl = (V - L) / min(L, 1-L)
                var l = v * (1 - s / 2);
                var sh = (l <= 0 || l >= 1) ? 0 : (v - l) / Math.min(l, 1 - l);
                return 'hsla(' + Math.round(h) + ', ' + Math.round(sh * 100) + '%, ' + Math.round(l * 100) + '%, ' + alphaStr() + ')';
            }
            if (fmt === 'hex' || a >= 0.998) return '#' + pad(c[0]) + pad(c[1]) + pad(c[2]);
            // hexa：#rrggbbaa
            return '#' + pad(c[0]) + pad(c[1]) + pad(c[2]) + pad(a * 255);
        }
        function sync() {
            var c = rgb(), solid = 'rgb(' + c.join(',') + ')', soft = 'rgba(' + c.join(',') + ',' + a + ')';
            sv.style.background = 'hsl(' + Math.round(h) + ', 100%, 50%)';
            svDot.style.left = (s * 100) + '%';
            svDot.style.top = ((1 - v) * 100) + '%';
            svDot.style.background = solid;
            hueDot.style.left = (h / 360 * 100) + '%';
            alphaDot.style.left = (a * 100) + '%';
            alpha.style.backgroundImage = 'linear-gradient(to right, rgba(' + c.join(',') + ',0), ' + solid + '), ' +
                WB_CP_CHECKER;
            // 预览/色块必须用 background-image 叠加棋盘格：CSS 里这两个元素的
            // background-image 是棋盘格（透明底衬），只设 backgroundColor 会被盖住，
            // 表现为「有颜色值但预览全透明」。
            preview.style.backgroundImage = 'linear-gradient(' + soft + ',' + soft + '), ' + WB_CP_CHECKER;
            swatch.style.backgroundImage = 'linear-gradient(' + soft + ',' + soft + '), ' + WB_CP_CHECKER;
            hex.value = value();
        }
        function emit(final) {
            var v2 = value();
            if (input.value !== v2) input.value = v2;
            if (opts.onInput) opts.onInput(v2, !!final);
        }
        function applyRaw(raw) {
            var p = wbParseColor(raw);
            if (p) { h = p.h; s = p.s; v = p.v; a = p.a; }
            sync();
        }
        function drag(el, handler) {
            el.addEventListener('pointerdown', function (e) {
                e.preventDefault(); e.stopPropagation();
                try { el.setPointerCapture(e.pointerId); } catch (err) { /* 忽略 */ }
                var move = function (ev) { handler(ev, false); };
                var up = function (ev) {
                    el.removeEventListener('pointermove', move);
                    el.removeEventListener('pointerup', up);
                    el.removeEventListener('pointercancel', up);
                    handler(ev, true);
                };
                el.addEventListener('pointermove', move);
                el.addEventListener('pointerup', up);
                el.addEventListener('pointercancel', up);
                handler(e, false);
            });
        }
        drag(sv, function (ev, final) {
            var r = sv.getBoundingClientRect();
            s = Math.max(0, Math.min(1, (ev.clientX - r.left) / Math.max(1, r.width)));
            v = Math.max(0, Math.min(1, 1 - (ev.clientY - r.top) / Math.max(1, r.height)));
            sync(); emit(final);
        });
        drag(hue, function (ev, final) {
            var r = hue.getBoundingClientRect();
            h = Math.max(0, Math.min(359.9, (ev.clientX - r.left) / Math.max(1, r.width) * 360));
            sync(); emit(final);
        });
        drag(alpha, function (ev, final) {
            var r = alpha.getBoundingClientRect();
            a = Math.max(0, Math.min(1, (ev.clientX - r.left) / Math.max(1, r.width)));
            sync(); emit(final);
        });
        hex.addEventListener('change', function () {
            var p = wbParseColor(hex.value);
            if (!p) { hex.value = value(); return; }
            h = p.h; s = p.s; v = p.v; a = p.a; sync(); emit(true);
        });
        clearBtn.addEventListener('click', function (e) { e.stopPropagation(); a = 0; sync(); emit(true); });
        ['#2563eb', '#0ea5e9', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#111827', '#6b7280', '#ffffff', 'transparent'].forEach(function (c) {
            var chip = document.createElement('button'); chip.type = 'button'; chip.className = 'wb-cp-chip';
            chip.title = c === 'transparent' ? '透明' : c;
            chip.style.backgroundColor = c;
            chip.addEventListener('click', function (e) { e.stopPropagation(); applyRaw(c); emit(true); });
            presets.appendChild(chip);
        });
        input.addEventListener('change', function () {
            applyRaw(input.value);
            if (opts.onInput) opts.onInput(input.value, true);
        });
        function place() {
            panel.hidden = false;
            var r = swatch.getBoundingClientRect(), w = 236, hh = panel.offsetHeight || 250;
            panel.style.left = Math.max(8, Math.min(r.right - w, window.innerWidth - w - 8)) + 'px';
            var top = r.bottom + 6;
            if (top + hh > window.innerHeight - 8) top = Math.max(8, r.top - hh - 6);
            panel.style.top = top + 'px';
        }
        swatch.addEventListener('click', function (e) {
            e.stopPropagation();
            if (panel.hidden) { sync(); place(); } else { panel.hidden = true; }
        });
        panel.addEventListener('pointerdown', function (e) { e.stopPropagation(); });
        // 自清理监听：控件 DOM 被移除（检查器重渲染）后，本监听器自行注销。
        // 原来每次渲染一个颜色控件都会向 document 挂一个永不释放的监听器，
        // 闭包同时持有 panel/root，编辑会话越久泄漏越多。
        document.addEventListener('pointerdown', function onDocDown(e) {
            if (!document.documentElement.contains(root)) {
                document.removeEventListener('pointerdown', onDocDown);
                return;
            }
            if (!panel.hidden && !root.contains(e.target)) panel.hidden = true;
        });

        // 收藏色（localStorage 持久化，跨组件共用）
        var FAV_KEY = 'wb.colorFavs';
        function readFavs() {
            try {
                var l = JSON.parse(localStorage.getItem(FAV_KEY) || '[]');
                return Array.isArray(l) ? l : [];
            } catch (e) { return []; }
        }
        function renderFavs() {
            favRow.innerHTML = '';
            readFavs().forEach(function (c) {
                var chip = document.createElement('button'); chip.type = 'button'; chip.className = 'wb-cp-chip';
                chip.title = c; chip.style.backgroundColor = c;
                chip.addEventListener('click', function (e) { e.stopPropagation(); applyRaw(c); sync(); emit(true); });
                favRow.appendChild(chip);
            });
        }
        function addFav(c) {
            if (!c) return;
            var list = readFavs().filter(function (x) { return x !== c; });
            list.unshift(c);
            try { localStorage.setItem(FAV_KEY, JSON.stringify(list.slice(0, 14))); } catch (e) { /* 忽略配额错误 */ }
            renderFavs();
        }
        renderFavs();

        sync();
        Object.defineProperty(root, 'value', {
            configurable: true,
            get: function () { return input.value; },
            set: function (v2) { input.value = v2 == null ? '' : String(v2); applyRaw(input.value); }
        });
        return root;
    }

    /**
     * 主题主色（工作台色板默认值）。
     * 组件未设置颜色时，色板回退到当前主题的主色，而不是硬编码的蓝色——
     * 与「主题设置」保持一致，避免「改了主题色，组件面板还是默认蓝」。
     */
export function themePrimary() {
        try {
            var ts = meta && meta.themeSettings;
            var c = ts && ts.colors && ts.colors.primary;
            if (c && /^#[0-9a-fA-F]{3,8}$/.test(String(c).trim())) return String(c).trim();
        } catch (e) { /* 兜底 */ }
        return '#3d444f';   // 项目默认主色（theme.css --c-primary）
    }

    /** 组件 Inspector 面板 schema（docs/02-C3 声明式 Controls，后端 ComponentSchemas 注入）。 */
export let componentSchemas = {};
    if (typeof document !== 'undefined') {
        try { componentSchemas = JSON.parse(document.getElementById('wb-schemas').textContent || '{}'); } catch (e) { componentSchemas = {}; }
    }

    /** schema key → 面板中文标签（缺省回退 key 本身）。 */
export const CONTROL_LABELS = {
        container: '容器', heading: '标题', text2: '文本', image: '图片', gallery: '图集',
        button: '按钮', divider: '分隔线', spacer: '间隔', globalref: '全局块',
        text: '文本内容', tag: '语义标签', color: '颜色', weight: '字重',
        letterSpacing: '字间距', transform: '大小写转换', lineClamp: '多行截断',
        textShadow: '文字阴影', fontSize: '字号', fontWeight: '字重',
        background: '背景色', border: '边框颜色', shadow: '阴影级别',
        variant: '外观风格', radius: '圆角', hoverLift: '悬停上浮', spacing: '间距',
        hoverShift: '悬停位移', action: '点击动作', value: '跳转地址', target: '打开方式',
        rel: '链接关系', source: '图标来源', name: '名称',
        position: '位置', kind: '类型', iconName: '图标样式', align: '对齐',
        style: '样式', mode: '展示模式', aspectRatio: '宽高比', aspectRatioValue: '自定义宽高比',
        objectFit: '填充方式', borderWidth: '边框宽度', borderColor: '边框颜色',
        clickAction: '点击行为', defaultLink: '默认链接', captionMode: '说明方式',
        caption: '说明文字', fallback: '兜底文本', title: '标题', sizes: '响应式尺寸',
        src: '图片地址', alt: '替代文字', width: '宽度', height: '高度',
        inlineSvg: '内联 SVG', sticky: '滚动吸顶', stickyTop: '吸顶偏移',
        entrance: '入场动画', bgGradient: '背景渐变', bgImage: '背景图',
        borderStyle: '边框样式', overlay: '遮罩强度', columns: '栅格列数',
        headers: '表头列', rows: '数据行', striped: '斑马纹', bordered: '边框',
        question: '问题', answer: '回答', open: '默认展开', author: '作者',
        targetDate: '目标时间', showDays: '显示天', submitLabel: '提交文字',
        method: '提交方式', action2: '提交地址', fields: '表单字段',
        placeholder: '占位提示', required: '必填', options: '选项', label2: '字段标签'
    };
export function controlLabel(key) { return CONTROL_LABELS[key] || key; }

    /** 枚举值 → 面板显示名(分段按钮用);缺省回退原值。 */
export const OPTION_LABELS = {
        h1: 'H1', h2: 'H2', h3: 'H3', h4: 'H4', h5: 'H5', h6: 'H6',
        div: '容器', section: '区块', article: '文章', header: '页头', footer: '页脚', main: '主体',
        solid: '实线', dashed: '虚线', dotted: '点线', double: '双线',
        column: '纵向', row: '横向',
        'flex-start': '起始', center: '居中', 'flex-end': '末端',
        'space-between': '两端', 'space-around': '环绕', stretch: '拉伸',
        internal: '站内', external: '外链', anchor: '锚点', native: '电话邮件', modal: '弹窗', link: '链接',
        self: '当前页', blank: '新窗口', none: '无', nofollow: 'nofollow',
        grid: '网格', carousel: '轮播', original: '原始', cover: '填充', contain: '包含', fill: '拉伸满',
        lightbox: '灯箱', sm: '小', md: '中', lg: '大', xl: '特大', xs: '特小',
        solid2: '', underline: '下划线', 'line-through': '删除线',
        uppercase: '大写', lowercase: '小写', capitalize: '首字母',
        richtext: '富文本', plaintext: '纯文本',
        'fade-in': '淡入', 'slide-up': '上滑'
    };
export function optionLabel(v) { return OPTION_LABELS[v] || v; }

    /** 深拷贝（结构化克隆不可用时的兜底）。 */
export function clone(v) { return v === undefined ? undefined : JSON.parse(JSON.stringify(v)); }

    /** 组件库清单、插入默认内容与插入节点构造：全部在 palette.js。
     *  抽成无 DOM 依赖的纯数据/纯函数模块后，node 可以直接 import 求值，
     *  让「组件库条目插入后必须可编译」这条不变量可被自动测试（见
     *  internal/builder/defaults_contract_test.go）；此处 re-export 保持既有
     *  `import { paletteItems, paletteGroups } from '../core.js'` 写法不变。 */
export { paletteItems, paletteGroups, DEFAULT_CONTENT, buildInsertNode,
         alignKeyOf, buildDefaultChild, buildDefaultAlignEntry, alignMutation,
         alignFromChildren } from './palette.js';

    /** 区块预设（预组合的全局 section，一键插入整个容器）。
     *  结构与 Page Document 一致：JSON AST 片段。 */

    /** 容器嵌套最大深度（含节点自身），超出拒绝拖放/插入，防止无限嵌套。 */
export const MAX_NEST_DEPTH = 8;

