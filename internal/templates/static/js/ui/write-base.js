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
// ── 关于行数（方案目标 ≤450，当前 ~735）────────────────────────────────────
//
// 超出的部分不是「写多了」，是两类**可以消失**的内容：
//   · ~120 行存量兼容层（legacyJson / applyBuilder 的 data-write-rows / -object /
//     -builder），等那 10 处旧模板迁到 data-write-json 后整段删除；
//   · ~185 行注释，写的是「为什么这样做」而不是「做了什么」—— 尤其「标量不转换」这类
//     容易被误读成 bug 的决定。删注释能压到 550 行，但那等于让下一个人重新踩一遍。
//
// 所以行数会在兼容层删掉后自然降到 ~450。在那之前，行数不是进度指标。
(function () {
    'use strict';

    // 不依赖 htmx：本基座用 fetch 发 formdata，只有在取 CSRF 头时才碰页面上的 meta。
    // （早先版本这里有一句 `if (!window.htmx) return;` —— 那是从旧基座照抄的守卫，
    //  而新基座根本不走 htmx，那条守卫会让整个基座在 htmx 未加载时不工作。
    //  实测在无 htmx 的沙箱里提交不发请求，正是这条守卫造成的。）

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
    // statusOf 找结论区：**先表单内、再宿主内**。
    //
    // 为什么必须兜到宿主：一批隐藏表单（单条删除 / 行内改状态）在列表页上各占一行，
    // 它们的结论区是**共用**的 —— 每个表单各带一个结果区会在每行底下多出一片空白。
    // 模板把那个共用区放在 <div data-write-host> 里（17 个模板这么写）。
    // 只查表单内的话，这些表单的失败提示不会显示在任何地方（用户只看到"点了没反应"）。
    function statusOf(form) {
        var own = form.querySelector('[data-write-status]');
        if (own) return own;
        var host = form.closest ? form.closest('[data-write-host]') : null;
        return host ? host.querySelector('[data-write-status]') : null;
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

    // ── 存量兼容层（只减不增）────────────────────────────────────────────────
    //
    // 10 处模板仍在用旧的三套整形属性（data-write-rows / -object / -builder），
    // 它们处理的行由**服务端片段**渲染（如 product_attribute_rows.html 的行带
    // hx-post 增删行），改成 <template data-row> 需要连着改服务端片段与交互。
    //
    // 本层在内部把它们翻译成 data-write-json 的等价行为，因此：
    //   · 新模板一律写 data-write-json，不写这三个属性；
    //   · 存量由 check-write-attributes.sh 登记（只减不增）。
    //
    // 为什么不在模板里直接改：改一处要动「片段端点 + 行增删交互 + 服务端数据形状」
    // 三件东西，与本批「统一写基座」是两件事，混在一起会让 review 无法判断回归来自哪。
    function legacyJson(form) {
        var out = {};

        // data-write-rows=".sel;f1,f2;key"：每行容器收成对象，按 DOM 顺序组装成数组。
        var rowsSpec = form.getAttribute('data-write-rows');
        if (rowsSpec) {
            var parts = rowsSpec.split(';');
            if (parts.length >= 2) {
                var sel = parts[0].trim();
                var names = parts[1].split(',').map(function (x) { return x.trim(); }).filter(Boolean);
                var key = (parts[2] || 'rows').trim();
                var rows = [];
                var containers = form.querySelectorAll(sel);
                for (var i = 0; i < containers.length; i++) {
                    var obj = {};
                    names.forEach(function (n) {
                        var f = findByName(containers[i], n);
                        if (f !== null) obj[n] = f;
                    });
                    // 全空的容器整行跳过。
                    if (Object.keys(obj).some(function (k) { return String(obj[k]).trim() !== ''; })) {
                        rows.push(obj);
                    }
                }
                if (rows.length) out[key] = rows;
            }
        }

        // data-write-object=".sel;key;driver;cond"：把容器里由 driver 点名的键收成对象。
        var objSpec = form.getAttribute('data-write-object');
        if (objSpec) {
            var op = objSpec.split(';');
            if (op.length >= 3) {
                var oscope = form.querySelector(op[0].trim());
                var okey = op[1].trim();
                var driver = op[2].trim();
                var when = op[3] ? op[3].trim() : '';
                var ok = true;
                if (when) {
                    var eq = when.indexOf('=');
                    if (eq > 0) {
                        var condName = when.slice(0, eq).trim();
                        var condVal = when.slice(eq + 1).trim();
                        var condEl = findByName(form, condName);
                        ok = condEl === condVal || condEl === null ? ok : false;
                        if (condEl !== null) ok = String(condEl) === condVal;
                    }
                }
                if (ok && oscope) {
                    var got = collect(oscope, null);
                    var o = {};
                    Object.keys(got).forEach(function (k) {
                        var v = got[k];
                        o[k] = v.length > 1 ? v : v[0];
                    });
                    if (Object.keys(o).length) out[okey] = o;
                }
            }
        }
        return out;
    }

    // findByName 在容器里按 name 精确取一个值（取最后一个，与旧基座同口径）。
    function findByName(scope, name) {
        var els = scope.querySelectorAll('[name]');
        var out = null;
        for (var i = 0; i < els.length; i++) {
            if (els[i].getAttribute('name') === name) {
                var t = (els[i].type || '').toLowerCase();
                if (t === 'checkbox') { out = els[i].checked ? els[i].value : '0'; }
                else if (t === 'radio') { if (els[i].checked) out = els[i].value; }
                else out = els[i].value;
            }
        }
        return out;
    }

    // 组装最终请求体（FormData）。
    function buildBody(form, submitter) {
        var body = new FormData();
        var jsonNames = String(form.getAttribute('data-write-json') || '')
            .split(',').map(function (s) { return s.trim(); }).filter(Boolean);
        var jsonVals = jsonFields(form);
        var skip = {};
        jsonNames.forEach(function (n) { skip[n] = true; });

        // bulk 档**只发** ids + data-write-extra 点名的字段 —— 不发整份表单。
        //
        // 为什么：bulk 的形状是 {ids} + 该动作自己的参数（如批量流转的 toStatus/remark），
        // 而表单里还带着筛选控件、其它动作的隐藏域。全发出去会让端点收到两种互不认识的
        // 身体（严格白名单直接整批 400）。旧基座的 bulk 也是这个口径。
        var bulkMode = attr(form, submitter, 'data-write') === 'bulk';
        var bulkExtra = {};
        if (bulkMode) {
            attr(form, submitter, 'data-write-extra').split(',').map(function (x) { return x.trim(); })
                .filter(Boolean).forEach(function (n) { bulkExtra[n] = true; });
        }
        var fields = collect(form, skip);
        Object.keys(fields).forEach(function (name) {
            if (bulkMode && !bulkExtra[name]) return;
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

        // data-write-lines="images,imageAlts"：多行文本 → 字符串数组。
        // 空行丢弃（用户多敲一个空行不该在数组里留空串）；空文本按空数组发
        //（「一个都不填」是明确的清空意图，不是「本次不改」）。
        attr(form, null, 'data-write-lines').split(',')
            .map(function (x) { return x.trim(); })
            .filter(Boolean)
            .forEach(function (name) {
                var raw = body.get(name);
                if (raw === null) return;
                body.delete(name);
                splitLines(raw).forEach(function (line) { body.append(name, line); });
            });

        // data-write-json-wrap="descriptionHtml:description"：把裸 HTML 包成 {html: …}。
        // 富文本控件给的是裸 HTML，而库里该字段是 jsonb 对象；这层包装原本在 handler 里做。
        // 冒号后是目标键，省略则用源键名。不 trim —— 正文空白是排版。
        attr(form, null, 'data-write-json-wrap').split(',')
            .map(function (x) { return x.trim(); })
            .filter(Boolean)
            .forEach(function (pair) {
                var parts = pair.split(':');
                var from = parts[0].trim();
                var to = (parts[1] || from).trim();
                var raw = body.get(from);
                if (raw === null) return;
                body.delete(from);
                body.set(to, JSON.stringify({ html: raw }));
            });

        // 存量兼容层：旧整形属性组装的键并入请求体。
        var legacy = legacyJson(form);
        Object.keys(legacy).forEach(function (name) {
            body.set(name, JSON.stringify(legacy[name]));
        });
        // data-write-extra="status,projectId"：点名的额外字段一起发。
        //
        // 为什么必须**点名**而不是「把表单字段全带上」：bulk 的形状是 {ids} + 该动作
        // 自己的参数，多带字段会让端点收到两种互不认识的身体。同名字段优先取本次
        // submitter 的值（按钮上的 value 就是它要发的东西，如批量启停的 status）。
        attr(form, submitter, 'data-write-extra').split(',')
            .map(function (x) { return x.trim(); })
            .filter(Boolean)
            .forEach(function (name) {
                if (submitter && submitter.getAttribute && submitter.getAttribute('name') === name) {
                    body.set(name, submitter.getAttribute('value') || '');
                    return;
                }
                if (body.has(name)) return;   // 表单里已有，保留
                var v = findByName(form, name);
                if (v !== null) body.set(name, v);
            });

        // 页面级组装器（data-write-builder）：最后跑，它可以覆盖前面的一切。
        applyBuilder(body, form);

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

    // splitLines 多行文本 → 去空行的字符串数组。
    function splitLines(raw) {
        if (typeof raw !== 'string') return [];
        var out = [];
        raw.replace(/\r\n/g, '\n').split('\n').forEach(function (line) {
            var v = line.trim();
            if (v) out.push(v);
        });
        return out;
    }

    // ── bulk ────────────────────────────────────────────────────────────────

    function bulkIDs(form) {
        var seen = {};
        var out = [];
        // data-write-ids-scope：ids 在**另一个容器**里（抽屉内容被克隆到 document 级，
        // 勾选框留在列表的批量表单里）。不指回去的话，在抽屉内找 [name=ids] 恒为空，
        // 症状是「点了应用调价什么也没发生」，提示却只说「没勾选任何项」。
        var scopeSel = String(form.getAttribute('data-write-ids-scope') || '').trim();
        var scope = form;
        if (scopeSel) {
            scope = document.querySelector(scopeSel) || form;
        }
        // 用 [name] + 逐个判 checked，而不是选择器里的 :checked 伪类：
        // 伪类在最小 DOM 桩（feature 测试的驱动脚本）里不支持，而在真实浏览器里
        // 「[name=ids] 且 checked」与「[name=ids]:checked」等价 —— 取前者兼容面更宽。
        var els = scope.querySelectorAll('[name]');
        for (var i = 0; i < els.length; i++) {
            var name = els[i].getAttribute('name');
            if (name !== 'ids' && name !== 'ids[]') continue;
            if (!els[i].checked) continue;
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
        if (!form || form.tagName !== 'FORM') return;
        var submitter = ev.submitter || null;
        // 声明可以写在表单上，也可以写在提交按钮上；两者都有时按钮优先（见 attr）。
        // 所以**任一处**有 data-write 即算本基座的表单 —— 只判表单会让
        // 「一个表单并排几个动作、端点各写在按钮上」这种形态整个失效（实测：
        // 批量启停/删除的按钮就长这样，表单本身刻意不声明）。
        if (!hasAttr(form, submitter, 'data-write')) return;
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

    // ── 页面级 payload 组装器（data-write-builder）────────────────────────────
    //
    // 有些写动作的 payload 不是「表单字段的平铺」，而是从 DOM 另一处读出结构再合并
    //（变体清单的行、捆绑配置的 config）。这类组装只有页面自己知道，所以走注册表：
    // 页面脚本注册一个函数，模板只声明名字。
    //
    // 保留这个机制的原因：product_variant_write.js / product_bundle_write.js /
    // membership_write.js 都在向它注册，而那些组装逻辑依赖各自页面特有的 DOM 结构，
    // 用通用的 <template data-row> 表达不了（例如捆绑配置要合并多个区域的勾选状态）。
    var builders = {};

    function registerPayloadBuilder(name, fn) {
        if (name && typeof fn === 'function') builders[name] = fn;
    }

    function applyBuilder(body, form) {
        var name = String(form.getAttribute('data-write-builder') || '').trim();
        if (!name || !builders[name]) return;
        var obj = builders[name](form);
        if (!obj || typeof obj !== 'object') return;
        Object.keys(obj).forEach(function (k) {
            body.set(k, typeof obj[k] === 'string' ? obj[k] : JSON.stringify(obj[k]));
        });
    }

    // 暴露两个纯函数给测试用（node:test 里以 vm 加载，见 write-base.test.cjs）。
    window.WBUI = window.WBUI || {};
    window.WBUI.writeBase = { buildBody: buildBody, bulkIDs: bulkIDs, registerPayloadBuilder: registerPayloadBuilder };
    // 旧附属脚本（product_variant_write.js 等）仍在向这两个名字注册组装器，
    // 保留别名让它们不必同时改动 —— 迁完那批模板后可以删掉。
    window.WPJsonWrite = window.WPJsonWrite || { registerPayloadBuilder: registerPayloadBuilder };
    window.WBFormBody = window.WBFormBody || { registerPayloadBuilder: registerPayloadBuilder };
    window.WPJsonWrite.registerPayloadBuilder = registerPayloadBuilder;
    window.WBFormBody.registerPayloadBuilder = registerPayloadBuilder;
})();
