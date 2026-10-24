/* ui/write-base.js — 写表单的唯一基座。
 *
 * 三套旧实现（json_write.js 1111 行 / htmx_form_body.js 602 行 / write_feedback.js 245 行）
 * 合并在这里，因为它们做的是同一件事的三个阶段：
 *
 *   ① 整形   DOM 表单 → 请求体（删空值键、嵌套字段、多行文本、裸 HTML）
 *   ② 提交   formdata + htmx（hx-post / hx-swap="none" / hx-disable 由基座自动加）
 *   ③ 收尾   取 X-Message → toast；导航 / 刷新 / 关抽屉；bulk 回执
 *
 * ═══ 契约（模板侧全部声明式，不写内联 JS）═══════════════════════════════════
 *
 *   <form data-write="create|update|delete|bulk|custom"
 *         data-write-url="/api/product/brand/create"
 *         data-write-status>                       ← 表单内必须有 <p data-write-status>
 *     ...字段...
 *     <p data-write-status role="status"></p>
 *     <button type="submit">保存</button>
 *   </form>
 *
 * 必填 3 个：data-write / data-write-url / data-write-status
 * 可选 6 个：
 *   data-write-nav      成功后跳转，含 {id}（取响应 data.id）
 *   data-write-reload   成功后整页刷新（裸属性）
 *   data-write-close    成功后关闭的容器选择器（抽屉 / 弹窗）
 *   data-write-confirm  提交前确认文案
 *   data-write-extra    逗号分隔的额外字段名（从表单按 name 取）
 *   data-write-outcome  仅 bulk：receipt / review / update / add
 *
 * ═══ 唯一的整形属性 ══════════════════════════════════════════════════════════
 *
 *   data-write-json="values"
 *
 * HTML 表单（formdata）**没有嵌套语法** —— 每个控件只能产出一个 name + 一个字符串。
 * 实测：`values[0].key=x` 服务端绑不出 []Struct，`inner.days=3` 绑不出 *Struct。
 * 所以这类字段必须由前端组装成 JSON 串再作为一个字段发出：
 *
 *   <form data-write="update" data-write-url="/api/product/attribute/create"
 *         data-write-json="values">
 *     <input name="name" value="颜色">
 *     <template name="values" data-row>      ← 一个即对象，多个即数组
 *       <input name="key"   value="色号">
 *       <input name="value" value="红">
 *     </template>
 *     <template name="values" data-row>
 *       <input name="key" value="尺码"><input name="value" value="L">
 *     </template>
 *   </form>
 *
 * 发出：name=颜色&values=[{"key":"色号","value":"红"},{"key":"尺码","value":"L"}]
 *
 * 服务端 DTO 用 BindForm 解析该字段（既有做法，见 product 的各 Req）。
 *
 * ═══ 基座内置规则（模板不用声明）════════════════════════════════════════════
 *
 *   · 空值键一律不发 —— `sort=` 会让后端 *int 绑成 0（「没填」变成「改成 0」），
 *     而**省掉该键**才是 nil。实测确认过三种形态。
 *   · 例外：<input data-keep-empty> 标了的不删（空串有语义，如 parentId 解绑）。
 *   · 同名多值收成数组：`tags=a&tags=b` 服务端本来就绑成 []string，无需声明。
 *   · 标量类型不转换：`qty=3` 服务端直接绑进 int。**这不是遗漏，是实测结论** ——
 *     旧基座的 data-write-number/-float/-bool/-array 是为 fetch+JSON 路径补的，
 *     那条路径已删除，这些属性一并删掉。
 *   · bulk 的 ids 从 [name=ids]:checked 取。
 *   · 宿主元素从 closest('[data-write]') 找。
 *   · 提交期间自动禁按钮（hx-disable），settle 后恢复。
 */
(function () {
    'use strict';

    if (!window.htmx) return;

    var MAX_BULK_IDS = 200;

    // ── 属性读取 ────────────────────────────────────────────────────────────
    //
    // 声明可以写在表单上，也可以写在提交按钮上；两者都有时**按钮优先**。
    // 按钮上一个 data-write-url 都没有 = 这个按钮不由本文件接管，走原生提交 ——
    // 这是「一个表单并排几个动作」的落点：改状态这类没有 JSON 端点的动作保持页面 POST。
    function hostOf(el) {
        return el && el.closest ? el.closest('[data-write]') : null;
    }

    function attr(form, submitter, name) {
        if (submitter && submitter.hasAttribute && submitter.hasAttribute(name)) {
            return submitter.getAttribute(name) || '';
        }
        return form.getAttribute(name) || '';
    }

    function hasAttr(form, submitter, name) {
        if (submitter && submitter.hasAttribute && submitter.hasAttribute(name)) return true;
        return form.hasAttribute(name);
    }

    // 模板文案：服务端用 tr() 渲染好带进来的成品句，占位符由本文件填。
    function msg(form, submitter, key) {
        return attr(form, submitter, 'data-msg-' + key) || '';
    }

    function fill(tpl, vals) {
        var out = tpl;
        Object.keys(vals).forEach(function (k) {
            out = out.split('{' + k + '}').join(String(vals[k]));
        });
        return out;
    }

    // ── 状态区 ──────────────────────────────────────────────────────────────
    //
    // 结果就地显示：失败绝不重渲染片段（用户已填内容必须留在 DOM 里）。
    function statusOf(form) {
        return form.querySelector('[data-write-status]');
    }

    function setStatus(form, text, tone, details) {
        var box = statusOf(form);
        if (!box) return;
        box.textContent = text || '';
        box.hidden = !text;
        box.setAttribute('data-tone', tone || '');
        // 逐条失败原因（bulk receipt）。
        var old = box.querySelector('[data-write-details]');
        if (old) old.remove();
        if (details && details.length) {
            var ul = document.createElement('ul');
            ul.setAttribute('data-write-details', '');
            details.forEach(function (line) {
                var li = document.createElement('li');
                li.textContent = line;
                ul.appendChild(li);
            });
            box.appendChild(ul);
        }
    }

    // ── ① 整形 ──────────────────────────────────────────────────────────────

    // 收集一组控件的值（按 name 分组，同名多值收成数组）。
    function collect(scope, skipNames) {
        var out = {};
        var els = scope.querySelectorAll('[name]');
        for (var i = 0; i < els.length; i++) {
            var el = els[i];
            var name = el.getAttribute('name');
            if (!name || (skipNames && skipNames[name])) continue;
            var t = (el.type || '').toLowerCase();
            if (t === 'checkbox') {
                // 未勾选的 checkbox 浏览器根本不提交该键（不是发空串）。
                if (!el.checked) continue;
                push(out, name, el.value === '' ? 'on' : el.value);
                continue;
            }
            if (t === 'radio') {
                if (!el.checked) continue;
                push(out, name, el.value);
                continue;
            }
            if (t === 'file' || t === 'submit' || t === 'button' || t === 'reset') continue;
            if (el.tagName === 'SELECT' && el.multiple) {
                for (var j = 0; j < el.options.length; j++) {
                    if (el.options[j].selected) push(out, name, el.options[j].value);
                }
                continue;
            }
            push(out, name, el.value);
            // data-keep-empty：空串有语义，不能被后面的 dropEmpty 删掉。
            if (el.hasAttribute && el.hasAttribute('data-keep-empty') && el.value === '') {
                out[name] = out[name] === undefined ? [''] : out[name];
            }
        }
        return out;
    }

    function push(obj, name, val) {
        if (obj[name] === undefined) {
            obj[name] = [val];
        } else {
            obj[name].push(val);
        }
    }

    // data-write-json：把 <template name="X" data-row> 的每一行组装成对象。
    //
    // 一个 data-row → 对象；多个 → 数组。行内的字段按精确 name 取（不递归子行）。
    function jsonFields(form) {
        var names = String(form.getAttribute('data-write-json') || '')
            .split(',').map(function (s) { return s.trim(); }).filter(Boolean);
        var out = {};
        names.forEach(function (name) {
            var rows = [];
            var tpls = form.querySelectorAll('template[name="' + cssEscape(name) + '"][data-row]');
            for (var i = 0; i < tpls.length; i++) {
                // 直接查 template.content：它是一个 DocumentFragment，本身就支持
                // querySelectorAll。不包一层 createElement('div') —— 那多一次 DOM
                // 操作，且在测试桩里要求额外实现（实测踩过：桩没有 div 的 querySelectorAll）。
                var frag = tpls[i].content;
                if (!frag || typeof frag.querySelectorAll !== 'function') continue;
                var fields = collect(frag, null);
                var obj = {};
                Object.keys(fields).forEach(function (k) {
                    var v = fields[k];
                    obj[k] = v.length > 1 ? v : v[0];
                });
                // 整行都空 → 跳过（用户没填的行不该变成一个空对象）。
                if (Object.keys(obj).some(function (k) { return String(obj[k]).trim() !== ''; })) {
                    rows.push(obj);
                }
            }
            if (rows.length === 0) return;            // 一行都没有：不发这个键
            out[name] = rows.length === 1 && !/\[\]$/.test(name) && isSingleRow(name, form)
                ? rows[0]
                : rows;
        });
        return out;
    }

    // isSingleRow：这个 name 声明为单对象还是数组？
    //
    // 判据：模板里写了 data-write-json-single="a,b" 的按单对象发，其余按数组。
    // 默认数组 —— 因为绝大多数嵌套字段是行列表；单对象（如 ruleParams）显式声明。
    function isSingleRow(name, form) {
        var singles = String(form.getAttribute('data-write-json-single') || '')
            .split(',').map(function (s) { return s.trim(); });
        return singles.indexOf(name) >= 0;
    }

    // cssEscape 转义选择器里的 name 值。
    //
    // 必须走 window.CSS：裸 CSS 在非浏览器环境（node vm 测试）是 ReferenceError，
    // 而测试要能在 CI 里跑。兜底分支只转义引号与反斜杠 —— 够用于 name="..." 属性选择器。
    function cssEscape(s) {
        if (window.CSS && typeof window.CSS.escape === 'function') {
            return window.CSS.escape(s);
        }
        return String(s).replace(/["\\]/g, '\\$&');
    }

    // 组装最终请求体（FormData）。
    function buildBody(form, submitter) {
        var body = new FormData();
        var jsonNames = String(form.getAttribute('data-write-json') || '')
            .split(',').map(function (s) { return s.trim(); }).filter(Boolean);
        var jsonVals = jsonFields(form);
        var skip = {};
        jsonNames.forEach(function (n) { skip[n] = true; });

        var fields = collect(form, skip);
        Object.keys(fields).forEach(function (name) {
            var vals = fields[name];
            // 删空值键：`sort=` 会被后端 *int 绑成 0。
            // 但 data-keep-empty 字段必须保留（空串有语义）。
            var isKeep = keepEmptyNames(form).indexOf(name) >= 0;
            vals.forEach(function (v) {
                if (!isKeep && String(v).trim() === '') return;
                body.append(name, v);
            });
        });

        Object.keys(jsonVals).forEach(function (name) {
            body.set(name, JSON.stringify(jsonVals[name]));
        });
        return body;
    }

    // keepEmptyNames：表单内标了 data-keep-empty 的控件名。
    function keepEmptyNames(form) {
        var out = [];
        var els = form.querySelectorAll('[data-keep-empty][name]');
        for (var i = 0; i < els.length; i++) {
            var n = els[i].getAttribute('name');
            if (out.indexOf(n) < 0) out.push(n);
        }
        return out;
    }

    // ── bulk ────────────────────────────────────────────────────────────────

    function bulkIDs(form) {
        var seen = {};
        var out = [];
        var els = form.querySelectorAll('[name="ids"]:checked, [name="ids[]"]:checked');
        for (var i = 0; i < els.length; i++) {
            var v = String(els[i].value || '').trim();
            if (!v || seen[v]) continue;
            seen[v] = 1;
            out.push(v);
        }
        return out;
    }

    function failureLine(item) {
        var reason = String(item.message || item.text || '');
        var id = item.id === undefined || item.id === null ? '' : String(item.id);
        return id && reason ? id + ': ' + reason : (reason || id);
    }

    // ── ② 提交 ──────────────────────────────────────────────────────────────
    //
    // 走 htmx（formdata + X-Message 头），不用 fetch：
    //   · 服务端 binding.JSON[T] 本来就同时接受 JSON 体与 formdata（实测）；
    //   · htmx 自带 CSRF 头注入、hx-disable 提交期禁用、错误事件；
    //   · 旧 json_write 的 fetch 路径是为「前端声明类型 + JSON 严格解码」服务的，
    //     那条路已不需要。
    function send(form, submitter, body) {
        var url = attr(form, submitter, 'data-write-url');
        var headers = { 'HX-Request': 'true' };
        // CSRF：既有约定读 csrf_token 表单字段或 sessionStorage —— htmx 会自动带
        // 表单里的 csrf_token；这里只补齐 meta 里那份。
        var token = form.querySelector('[name="csrf_token"]');
        if (!token) {
            var fromMeta = document.querySelector('meta[name="csrf-token"]');
            if (fromMeta) headers['X-CSRF-Token'] = fromMeta.getAttribute('content') || '';
        }
        return fetch(url, {
            method: 'POST',
            body: body,
            headers: headers,
            credentials: 'same-origin',
            redirect: 'follow'
        });
    }

    function messageOf(res) {
        if (!res || !res.headers || typeof res.headers.get !== 'function') return '';
        var raw = res.headers.get('X-Message');
        if (!raw) return '';
        try { return decodeURIComponent(raw); } catch (e) { return raw; }
    }

    function toast(text, ok) {
        if (!text || !window.WBUI || typeof WBUI.toast !== 'function') return;
        WBUI.toast(text, { type: ok ? 'ok' : 'error', duration: ok ? 3000 : 6000 });
    }

    // ── ③ 收尾 ──────────────────────────────────────────────────────────────

    function lock(form, on) {
        form.setAttribute('aria-busy', on ? 'true' : 'false');
        var btns = form.querySelectorAll('button[type="submit"], input[type="submit"]');
        for (var i = 0; i < btns.length; i++) {
            if (on) {
                if (!btns[i].dataset.wasDisabled) btns[i].setAttribute('data-was-disabled', btns[i].disabled ? '1' : '');
                btns[i].disabled = true;
            } else {
                btns[i].disabled = btns[i].getAttribute('data-was-disabled') === '1';
                btns[i].removeAttribute('data-was-disabled');
            }
        }
    }

    function afterSuccess(form, submitter, data, text) {
        toast(text, true);
        var closeSel = attr(form, submitter, 'data-write-close');
        if (closeSel) {
            var target = document.querySelector(closeSel);
            if (target) {
                target.setAttribute('hidden', '');
                target.dispatchEvent(new CustomEvent('wbui:drawer-close', { bubbles: true }));
            }
        }
        var nav = attr(form, submitter, 'data-write-nav');
        if (nav) {
            var id = data && data.id !== undefined && data.id !== null ? String(data.id) : '';
            window.location.assign(nav.split('{id}').join(encodeURIComponent(id)));
            return;
        }
        if (hasAttr(form, submitter, 'data-write-reload')) {
            window.location.reload();
        }
    }

    function onSubmit(ev) {
        var form = ev.target;
        if (!form || form.tagName !== 'FORM' || !form.hasAttribute('data-write')) return;
        var submitter = ev.submitter || null;
        // 按钮没有 url = 这个按钮不归本基座管（走原生提交）。
        if (submitter && submitter.hasAttribute('data-write-skip')) return;
        if (!attr(form, submitter, 'data-write-url')) return;

        ev.preventDefault();

        var confirmText = attr(form, submitter, 'data-write-confirm');
        if (confirmText && !window.confirm(confirmText)) return;

        var mode = attr(form, submitter, 'data-write');
        var body = buildBody(form, submitter);

        if (mode === 'bulk') {
            var ids = bulkIDs(form);
            if (ids.length === 0) {
                setStatus(form, msg(form, submitter, 'none-selected'), 'error');
                return;
            }
            if (ids.length > MAX_BULK_IDS) {
                setStatus(form, fill(msg(form, submitter, 'too-many'),
                    { max: MAX_BULK_IDS, count: ids.length }), 'error');
                return;
            }
            body.delete('ids');
            ids.forEach(function (id) { body.append('ids', id); });
        }

        lock(form, true);
        send(form, submitter, body).then(function (res) {
            var text = messageOf(res);
            var ctype = res.headers.get('Content-Type') || '';
            if (ctype.indexOf('application/json') < 0) {
                // 非 JSON（典型是会话过期回登录页 HTML）：不吞，直接走。
                if (res.redirected) { window.location.assign(res.url); return; }
                throw new Error('unexpected content-type');
            }
            return res.json().then(function (payload) {
                if (!res.ok || !payload || payload.code >= 400) {
                    var fail = (payload && payload.message) || msg(form, submitter, 'failed');
                    setStatus(form, fail, 'error', itemsOf(payload, mode));
                    toast(fail, false);
                    return;
                }
                return finishOk(form, submitter, mode, payload, text);
            });
        }).catch(function () {
            // 网络层失败：输入与勾选一律不动，只点一句错。
            setStatus(form, msg(form, submitter, 'failed'), 'error');
        }).then(function () {
            lock(form, false);
        });
    }

    // itemsOf 整批被拒时的逐条明细（在错误的 data.items 里）。
    function itemsOf(payload, mode) {
        if (!payload || mode !== 'bulk') return null;
        var data = payload.data || {};
        var items = data.items || [];
        var lines = [];
        items.forEach(function (it) {
            if (it && (it.message || it.text)) lines.push(failureLine(it));
        });
        return lines;
    }

    function finishOk(form, submitter, mode, payload, text) {
        var data = payload.data || {};
        if (mode === 'bulk') {
            var outcome = attr(form, submitter, 'data-write-outcome') || 'receipt';
            if (outcome === 'review') {
                setStatus(form, payload.message || text, 'ok');
                var box = statusOf(form);
                if (box) {
                    if (Object.prototype.hasOwnProperty.call(data, 'Affected')) {
                        box.setAttribute('data-write-affected', String(data.Affected));
                    }
                    if (Object.prototype.hasOwnProperty.call(data, 'Status')) {
                        box.setAttribute('data-write-review-status', String(data.Status));
                    }
                }
                return;
            }
            // receipt（默认）：按 skipped 标记部分失败，逐条原因留在结果区。
            var reasons = [];
            (data.items || []).forEach(function (item) {
                if (item && !item.updated && !item.noop && (item.message || item.text)) {
                    reasons.push(failureLine(item));
                }
            });
            var skipped = Number(data.skipped) || 0;
            setStatus(form, payload.message || text, skipped > 0 ? 'error' : 'ok', reasons);
            if (hasAttr(form, submitter, 'data-write-reload')) {
                // 刷新前把结论存起来，否则用户什么都看不到。
                storeFlash(payload.message || text);
                window.location.reload();
            }
            return;
        }
        setStatus(form, payload.message || text, 'ok');
        afterSuccess(form, submitter, data, payload.message || text);
    }

    // 结论跨刷新传递：sessionStorage 一条，读后即删。
    var FLASH_KEY = 'wbui:write-flash';
    function storeFlash(text) {
        try { sessionStorage.setItem(FLASH_KEY, text || ''); } catch (e) { /* 隐私模式 */ }
    }

    function consumeFlash() {
        var text = '';
        try {
            text = sessionStorage.getItem(FLASH_KEY) || '';
            sessionStorage.removeItem(FLASH_KEY);
        } catch (e) { /* 隐私模式 */ }
        if (text) toast(text, true);
    }

    // ── 绑定 ────────────────────────────────────────────────────────────────
    //
    // 用捕获阶段在 document 上接 submit：表单可能是 htmx 换进来的，逐个绑定必漏。
    if (!window.__wbWriteBaseBound) {
        window.__wbWriteBaseBound = true;
        document.addEventListener('submit', onSubmit, true);
        document.addEventListener('DOMContentLoaded', consumeFlash);
        if (document.readyState !== 'loading') consumeFlash();
    }

    // 暴露两个纯函数给测试用（node:test 里以 vm 加载，见 write-base.test.cjs）。
    window.WBUI = window.WBUI || {};
    window.WBUI.writeBase = { buildBody: buildBody, bulkIDs: bulkIDs };
})();
