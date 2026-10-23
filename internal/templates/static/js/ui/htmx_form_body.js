/* ui/htmx_form_body.js — htmx 写表单的**请求体整形**（一处实现，全站生效）。
 *
 * 它替 ui/json_write.js 承担两件「formdata 与 JSON 语义不一样」的事：
 *
 *   ① 删空值键 —— JSON 客户端可以**省略**字段（省略 = 本次不改），HTML 表单做不到：
 *      `<input name="sort" type="number" value="">` 一定发 `sort=`，后端 `*int` 会被绑成
 *      `0`（不是 nil），于是「什么都没填」变成「把排序改成 0」—— 静默改数据。
 *
 *   ② 扁平字段 → 嵌套对象 —— 有些端点的身体里有 `{ruleParams:{days:30}}` 这种子对象，
 *      而 DOM 里只有扁平的 `name=days`。模板用 `data-write-object` 声明这件事。
 *
 * 实测确认过三种形态（独立探针，gin 同版本）：
 *   不发该键   → *int = nil  （本次不改）✅
 *   sort=      → *int = 0    （会写库）  ⚠️
 *   sort=0     → *int = 0    （正确）    ✅
 *
 * 为什么用 htmx:config:request 而不是 hx-params：hx-params 在 htmx 4 里**已删除**
 *（官方 whats-new 列出）。v4 的正解就是事件式配置 —— 在 config:request 里改 ctx.request.body。
 */
(function () {
    'use strict';

    if (!window.htmx || typeof window.htmx.on !== 'function') return;

    // 会被整理的动词。GET/HEAD 没有 body，不在此列。
    var WRITE_VERBS = { post: 1, put: 1, patch: 1, delete: 1 };

    // ── ② data-write-object 的规格解析 ──
    //
    // 格式："<容器选择器>;<payload 键名>;<驱动控件名>[;<条件 name=值>]"
    //
    // 例子（product 的标签表单）：
    //   data-write-object=".tag-rule-fields;ruleParams;ruleType;kind=rule"
    //   → 把 .tag-rule-fields 里、由 ruleType 当前选项点名的键收成 ruleParams 对象，
    //     仅当 kind=rule 时执行。
    //
    // 为什么「哪些键进对象」由**驱动控件的 option** 决定（data-rule-keys）而不是容器里的全部输入框：
    // 同一个容器里的输入框服务不同规则子类型，多发一个键会被服务端白名单整条拒掉
    //（product 的 rejectUnknownRuleKeys：新品只要 days、价格区间只要 minPrice/maxPrice）。
    function specOf(el) {
        var host = el.closest ? el.closest('[data-write-object]') : null;
        if (!host) return null;
        var parts = String(host.getAttribute('data-write-object') || '').split(';');
        var selector = String(parts[0] || '').trim();
        var key = String(parts[1] || '').trim();
        var driver = String(parts[2] || '').trim();
        var when = String(parts[3] || '').trim();
        if (!selector || !key || !driver) return null;
        var cond = null;
        if (when) {
            var eq = when.indexOf('=');
            if (eq > 0) cond = { name: when.slice(0, eq).trim(), value: when.slice(eq + 1).trim() };
        }
        return { host: host, selector: selector, key: key, driver: driver, when: cond };
    }

    function fieldNamed(scope, name) {
        var out = null;
        var els = scope.querySelectorAll('[name]');
        for (var i = 0; i < els.length; i++) {
            if (els[i].getAttribute('name') === name) out = els[i];
        }
        return out;
    }

    function selectedOption(el) {
        if (el.selectedOptions && el.selectedOptions.length) return el.selectedOptions[0];
        var opts = el.options || [];
        for (var i = 0; i < opts.length; i++) if (opts[i].selected) return opts[i];
        return null;
    }

    // 嵌套对象里的值按 JSON 数字发（能完整解析成十进制数才转）。
    //
    // ruleParams 这类结构参数的值天然是数字：发 "30" 会被服务端的 ruleInt 当字符串拒掉，
    // 症状是「保存没反应」。但也不一律 Number()：认不出的取值（"abc"、"1e3"）原样发字符串，
    // 由严格 API 报参数错误 —— 页面替调用方猜一次类型，就是替它造了一个它没说的值。
    function scalar(raw) {
        var s = String(raw == null ? '' : raw).trim();
        if (s === '') return null;
        if (!/^-?\d+(\.\d+)?$/.test(s)) return s;
        var n = Number(s);
        if (!isFinite(n) || Math.abs(n) > Number.MAX_SAFE_INTEGER) return s;
        return n;
    }

    // objectOf 把容器里「驱动控件当前选项点名的键」收成一个对象；返回 null 表示不发。
    function objectOf(body, spec) {
        if (spec.when) {
            var condEl = fieldNamed(spec.host, spec.when.name);
            var condValue = condEl ? String(condEl.value == null ? '' : condEl.value).trim() : '';
            if (condValue !== spec.when.value) return null;
        }
        var box = spec.host.querySelector(spec.selector);
        if (!box) return null;
        var driver = fieldNamed(box, spec.driver) || fieldNamed(spec.host, spec.driver);
        if (!driver) return null;

        var option = selectedOption(driver);
        var keys = option ? String(option.getAttribute('data-rule-keys') || '') : '';
        var out = {};
        keys.split(',').forEach(function (rawKey) {
            var name = String(rawKey || '').trim();
            if (!name) return;
            var el = fieldNamed(box, name);
            if (!el) return;
            var v = scalar(el.value);
            // 空值不进对象（与顶层同口径）：价格区间只填一个价时另一个键根本不该出现 ——
            // 发 "" 会被解码成 0，命中集合整个错掉而没有任何报错。
            if (v === null) return;
            out[name] = v;
        });
        return { driver: spec.driver, driverValue: String(driver.value == null ? '' : driver.value).trim(), value: out };
    }

    // ── data-write-lines：多行文本 → 字符串数组 ──
    //
    // 图集控件的隐藏域是**换行拼接的单值**（media-gallery.js 的 join('\n')），
    // 而 DTO 的 `Images []string` 期望多个元素。不转换时 formdata 只给出
    // 一个元素、内含换行 —— 实测 len=1 ["a.jpg\nb.jpg"]，等于存了一个坏 URL。
    //
    // 空行丢弃（与旧基座的 splitLines 同口径）：用户多敲一个空行不该在数组里留空串。
    function splitLines(raw) {
        if (typeof raw !== 'string') return [];
        var out = [];
        raw.replace(/\r\n/g, '\n').split('\n').forEach(function (line) {
            var v = line.trim();
            if (v) out.push(v);
        });
        return out;
    }

    // ── data-write-html："源键:目标键" —— 裸 HTML 包成 {html: …} ──
    //
    // 富文本控件提交的是裸 HTML 字符串，而入库形态是 jsonb `{"html": …}`。
    // 声明形如 `data-write-html="descriptionHtml:description"`：读 descriptionHtml，
    // 包成 {"html": 原值} 写到 description，原键消失。
    //
    // 不 trim：富文本的尾随空格可能是内容的一部分（旧基座对此有专门断言）。
    function applyHTMLFields(body, el) {
        var host = el && el.closest ? el.closest('[data-write-html]') : null;
        if (!host) return;
        String(host.getAttribute('data-write-html') || '').split(',').forEach(function (pair) {
            var parts = String(pair || '').split(':');
            if (parts.length !== 2) return;
            var from = parts[0].trim(), to = parts[1].trim();
            if (!from || !to) return;
            var raw = body.get ? body.get(from) : null;
            if (raw === null || raw === undefined) return;
            body.delete(from);
            body.set(to, JSON.stringify({ html: String(raw) }));
        });
    }

    // ── data-write-float：点名的字段按小数发 ──
    //
    // 后端 *float64 收字符串也能绑（实测），所以这一档**不是必需的**；
    // 保留它是为了与旧基座的行为逐字一致：JSON 侧原本就发数字，
    // 改成字符串会让 `{"defaultPrice":"9.9"}` 与 `{"defaultPrice":9.9}` 在日志/审计里不同形。
    // 认不出的取值原样发字符串，交给严格 API 报错（不猜）。
    function applyFloatFields(body, el) {
        var names = nameSet(el, 'data-write-float');
        Object.keys(names).forEach(function (name) {
            var raw = body.get ? body.get(name) : null;
            if (typeof raw !== 'string' || raw.trim() === '') return;
            if (!/^-?\d+(\.\d+)?$/.test(raw.trim())) return;
            var n = Number(raw.trim());
            if (!isFinite(n)) return;
            body.set(name, String(n));
        });
    }

    // ── data-write-lines：换行拼接的单值展开成数组 ──
    //
    // 必须在 dropEmpty **之后**做：dropEmpty 会把「整串为空」的键删掉，
    // 而这里有值时才有展开的意义。展开本身对空串是安全的（splitLines 返回空数组）。
    function applyLinesFields(body, el) {
        var names = nameSet(el, 'data-write-lines');
        Object.keys(names).forEach(function (name) {
            var raw = body.get ? body.get(name) : null;
            if (raw === null || raw === undefined) return;
            body.delete(name);
            splitLines(raw).forEach(function (line) { body.append(name, line); });
        });
    }

    // ── data-write-array：同名多值收成数组 ──
    //
    // 浏览器对「一个都没勾的 checkbox 组」**完全不提交该键**，于是 DTO 的
    // `AttributeIDs []string` 保持 nil —— 语义从「解绑全部」反转成「本次不改」，
    // 用户以为解绑了。
    //
    // 但**不能靠发 "[]" 表达空数组**：gin 的 form 绑定把字符串 "[]" 当成**一个元素**
    //（实测 `attributeIds=[]` → `[]string{"[]"}`），那个 "[]" 会被当成 uuid 去查库 →
    // PG 报 22P02 → 500（审查 P1）。空串同样是 `[]string{""}`。
    //
    // 所以这里的职责改成：**删掉空值**，把「全都没勾」表达为「不发这个键」。
    // 「解绑全部」由 DTO 自己的 BindForm 按「键存在但为空」识别 —— 见
    // product_edit.html 的 keep-empty 声明与各 DTO 的 BindForm。
    function applyArrayFields(body, el) {
        var names = nameSet(el, 'data-write-array');
        Object.keys(names).forEach(function (name) {
            var v = body.get ? body.get(name) : null;
            if (v === null) return;   // 键本来就不在（浏览器没提交）：不动
            // 只删**字面量 "[]"**：那是历史模板把空数组写成字符串的遗留形态，
            // 到后端会变成 `[]string{"[]"}`、被当 uuid 查库 → PG 22P02 → 500。
            //
            // **空串不删**。两类字段的语义正好相反，而前端分不出来：
            //   · 翻译工作台的 rowTarget 是**按行对齐的并行数组**（rowTarget /
            //     rowContext / rowHash 三组同长），某一行为空时必须保留占位，
            //     剔掉会让后面所有行错位一格（service 判长度不等 → 整表报错）；
            //   · 商品的 attributeIds 是纯 id 数组，空串不会被当 id —— service 侧的
            //     resolveAttributeIDs / resolveTagIDs 都有 `if id == "" { continue }`。
            // 所以「跳过空 id」由**消费方**负责（它知道自己在读什么），
            // 前端只做它唯一能确定的一件事：把字面量 "[]" 这个明显错误形态清掉。
            if (v.trim() === '[]') body.delete(name);
        });
    }

    // ── data-write-builder：页面级 payload 组装器 ──
    //
    // 有些写动作的 payload 不是「表单字段的平铺」，而是从 DOM 另一处读出结构再合并
    // （变体清单的 rows、捆绑配置的 config）。这类组装只有页面自己知道，
    // 所以走**注册表**：页面脚本注册一个函数，模板只声明名字。
    //
    // 契约与旧基座 WPJsonWrite.registerPayloadBuilder 一致：
    //   builder(form, submitter, payload) → 返回改写后的 plain object
    // 但这里的 payload 是**对象**而不是 FormData：组装器要嵌套数组/对象，
    // 最后由 applyBuilderPayload 逐键写回 body（简单值直接 set，对象/数组 stringify）。
    //
    // 为什么走 JSON 字符串而不是展开成 formdata 键：gin 的 form 映射**不支持点号嵌套**
    //（实测 config.maxOptions=3 绑完仍是 0），而 Rows / Config 这类字段在 DTO 侧
    // 都已实现 BindForm 自己解析 JSON 字符串 —— 这条路是通的。
    var payloadBuilders = {};

    function registerPayloadBuilder(name, fn) {
        if (!name || typeof fn !== 'function') {
            throw new TypeError('registerPayloadBuilder: 需要 name 与 function');
        }
        payloadBuilders[name] = fn;
    }

    // applyBuilderPayload 把组装器的产物写回 body。
    //
    // 规则：组装器返回的**全部键**都要进 body（它已经删掉了不该发的中间键，
    // 例如 memberRows / maxOptions）。值是对象或数组时 JSON.stringify，
    // 其它按字符串写 —— DTO 侧用 BindForm 解 JSON、用 form tag 接标量。
    function applyBuilderPayload(body, payload) {
        // **先收集键再删**：真 FormData 的 forEach 在删除当前项后会跳过下一个
        // （实测 4 个键 a,b,c,d → 遍历删除后只剩 b,d），于是组装器想删掉的中间键
        // 会漏进请求体。改成先取快照再逐键删。
        var existing = [];
        body.forEach(function (_v, key) { existing.push(key); });
        for (var i = 0; i < existing.length; i++) body.delete(existing[i]);
        Object.keys(payload).forEach(function (key) {
            var v = payload[key];
            if (v === null || v === undefined) return;
            if (typeof v === 'object') {
                body.set(key, JSON.stringify(v));
                return;
            }
            body.set(key, String(v));
        });
    }

    // applyBuilder 若表单声明了 data-write-builder，就跑一次组装器。
    function applyBuilder(body, el) {
        if (!el || !el.closest) return;
        var host = el.closest('[data-write-builder]');
        if (!host) return;
        var name = String(host.getAttribute('data-write-builder') || '').trim();
        if (!name) return;
        var builder = payloadBuilders[name];
        if (!builder) return;   // 页面没注册：不动 body，交给端点按缺字段拒绝

        // body → plain object（同名多值收成数组，与 FormData 的语义一致）
        var payload = {};
        body.forEach(function (value, key) {
            if (Object.prototype.hasOwnProperty.call(payload, key)) {
                if (!Array.isArray(payload[key])) payload[key] = [payload[key]];
                payload[key].push(value);
                return;
            }
            payload[key] = value;
        });
        var out = builder(host, null, payload);
        if (out && typeof out === 'object') applyBuilderPayload(body, out);
    }

    window.WBFormBody = { registerPayloadBuilder: registerPayloadBuilder };

    // ── data-write-ids-scope：ids 装在**别的容器**里 ──
    //
    // 现有唯一用例是商品列表的「按规则改价」抽屉：抽屉内容被克隆到 document 级，
    // 而勾选框留在页面那个批量表单里 —— 在抽屉表单内部找 [name=ids] 永远是空数组，
    // 表现是「点了应用调价什么都没发生」，而基座只回一句「没勾选任何项」，
    // 把人指向错误的原因。
    //
    // 不声明时什么都不做（ids 由浏览器原样提交）；声明了但选择器打错 / 目标还没渲染时
    // **也什么都不做** —— 不静默变成空数组，让端点按自己的规则拒绝。
    function applyIDsScope(body, el) {
        if (!el || !el.closest) return;
        var host = el.closest('[data-write-ids-scope]');
        if (!host) return;
        var sel = String(host.getAttribute('data-write-ids-scope') || '').trim();
        if (!sel) return;
        var scope = document.querySelector(sel);
        if (!scope) return;   // 目标没渲染：不动，交给端点拒绝

        var name = String(host.getAttribute('data-write-ids') || 'ids').trim() || 'ids';
        var seen = {};
        var ids = [];
        var els = scope.querySelectorAll('[name="' + name + '"]');
        for (var i = 0; i < els.length; i++) {
            var node = els[i];
            var type = (node.type || '').toLowerCase();
            // 只认两种载体：已勾选的复选框（表格行选择）与隐藏域（页面预置的 id 列表）。
            // 其它类型即使撞名也不是 id 载体 —— 把用户敲的文本当记录 id 发出去，
            // 请求长得完全合法，错得安静。
            if (type === 'checkbox' ? !node.checked : type !== 'hidden') continue;
            var v = String(node.value == null ? '' : node.value).trim();
            if (v === '' || seen[v]) continue;
            seen[v] = true;
            ids.push(v);
        }
        if (ids.length === 0) return;   // 一个都没勾：不发键，由端点的「没勾选」分支给结论

        body.delete(name);
        for (var j = 0; j < ids.length; j++) body.append(name, ids[j]);
    }

    // dropEmpty 删掉「空值 = 没填」的顶层字段。
    //
    // ⚠ 不是所有空串都该删：有些字段的空串**是有语义的**（解绑某处关联），
    // 例如分类/商品的 parentId、primaryCategoryId —— 后端 DTO 的注释写着
    // 「nil 表示本次不改，指向空串表示解绑 / 提升为顶级」。把这类字段的空串删掉，
    // 「提升为顶级」就变成了「不改父级」，用户点了没反应还以为保存成功。
    //
    // 判据因此分两类：
    //   · data-write-keep-empty="parentId,..." 点名的字段：**空串照发**；
    //   · 其余字段：空串删掉（formdata 的 `sort=` 会让后端 *int 绑成 0，
    //     而 JSON 侧省略字段 = 本次不改）。
    function dropEmpty(body, el) {
        var keep = nameSet(el, 'data-write-keep-empty');

        // data-write-array 点名的字段**不参与删空值**。
        //
        // 它们是「按位置对齐的数组元素」：翻译工作台的 rowTarget / rowContext / rowHash
        // 三组同长，某一行译文为空时必须保留一个空串占位 —— 剔掉会让后面所有行错位一格，
        // service 判「三个数组长度不等」就把整表拒了（而用户只是没填那一行）。
        // id 类数组（attributeIds 等）里的空串由 service 侧跳过，见 applyArrayFields 的注释。
        var arrayNames = nameSet(el, 'data-write-array');
        Object.keys(arrayNames).forEach(function (n) { keep[n] = true; });

        // 先把每个键的**全部值**收集起来再判断。
        //
        // 为什么不能「见到一个空值就删这个键」：FormData.delete(key) 删的是该键的**所有值**，
        // 而不是那一个空值。同名多值字段（多选 checkbox、多个 hidden）里只要混进一个空值，
        // `tagIds=a&tagIds=&tagIds=c` 就会把 a 和 c 一起删掉 —— 用户勾了两项，存进去零项，
        // 而页面显示保存成功。这类丢数据的缺陷在界面上完全看不出来。
        //
        // 语义因此定成：**只有该键的全部值都为空，才算「没填」并删掉**；
        // 只要有一个非空值，整组原样保留（空值也保留 —— 它可能是占位，删了会改变顺序语义）。
        var all = {};
        var order = [];
        body.forEach(function (value, key) {
            if (!all[key]) { all[key] = []; order.push(key); }
            all[key].push(value);
        });

        for (var i = 0; i < order.length; i++) {
            var key = order[i];
            if (keep[key]) continue;   // 空串有语义的字段：照发

            var values = all[key];
            var everyEmpty = true;
            for (var j = 0; j < values.length; j++) {
                var v = values[j];
                // File 永远保留：空文件输入根本不会出现在 FormData 里，这里只是防御。
                if (typeof v !== 'string') { everyEmpty = false; break; }
                if (v.trim() !== '') { everyEmpty = false; break; }
            }
            if (everyEmpty) body.delete(key);
        }
    }

    // numericValue 十进制非负整数才转数字：认不出的一律原样发，由严格 API 报参数错误
    //（页面替调用方猜一次类型，就是替它造了一个它没说的值）。
    function numericValue(raw) {
        if (typeof raw !== 'string') return raw;
        if (!/^\d+$/.test(raw)) return raw;
        var n = Number(raw);
        if (!isFinite(n) || n > Number.MAX_SAFE_INTEGER) return raw;
        return n;
    }

    // boolValue 只认 1/true/on/yes 与 0/false/off/no（大小写不敏感；空串按 false）。
    function boolValue(raw) {
        if (typeof raw !== 'string') return raw;
        var v = raw.trim().toLowerCase();
        if (v === '') return false;
        if (v === '1' || v === 'true' || v === 'on' || v === 'yes') return true;
        if (v === '0' || v === 'false' || v === 'off' || v === 'no') return false;
        return raw;
    }

    // ── ③ data-write-rows：行容器 → 对象数组 ──
    //
    // 格式："<行容器选择器>;<字段名逗号分隔>;<payload 键名>"
    // 例子：data-write-rows=".attr-value-row;id,key,label,sort,enabled;values"
    //
    // 一个容器 = 一行 = 一个对象；每行按点名的字段顺序取值。容器里的**全部**具名控件
    // 都不进顶层标量 —— 同一个 name 交给端点两次会产生两份真相（binding.JSON 本身
    // 对未知键是容忍的，见 applyRows 的同一段说明）。
    function rowsSpecOf(el) {
        var host = el.closest ? el.closest('[data-write-rows]') : null;
        if (!host) return null;
        var parts = String(host.getAttribute('data-write-rows') || '').split(';');
        var selector = String(parts[0] || '').trim();
        var fields = String(parts[1] || '').split(',').map(function (n) { return String(n || '').trim(); }).filter(Boolean);
        var key = String(parts[2] || '').trim() || 'rows';
        if (!selector || fields.length === 0) return null;
        return { host: host, selector: selector, fields: fields, key: key };
    }

    // 一行里取一个字段：先按**精确名**找，找不到再按「.后缀」找
    //（values[0].key 这类带索引的扁平名每行下标都不同，固定 name 匹配不到）。
    //
    // 命中多个时取**最后一个有效值**：hidden(0) + checkbox(1) 同名打底时，勾选后 checkbox
    // 在 DOM 末尾，于是勾选得 "1"、未勾选时 checkbox 返回 null 而 hidden 的 "0" 留下。
    function rowFieldValue(box, name) {
        var suffix = '.' + name;
        var value = null;
        var els = box.querySelectorAll('[name]');
        for (var i = 0; i < els.length; i++) {
            var n = els[i].getAttribute('name') || '';
            if (n !== name && n.slice(-suffix.length) !== suffix) continue;
            var v = controlValue(els[i]);
            if (v !== null) value = v;
        }
        return value;
    }

    // controlValue 与「表单会提交什么」同口径：disabled 与未勾选的 checkbox 不给值
    //（返回 null = 不进对象）。
    function controlValue(el) {
        if (el.disabled || (el.closest && el.closest('fieldset[disabled]'))) return null;
        var type = (el.type || '').toLowerCase();
        if ((type === 'checkbox' || type === 'radio') && !el.checked) return null;
        return el.value;
    }

    function rowsOf(spec, numberNames, boolNames) {
        var rows = [];
        var boxes = spec.host.querySelectorAll(spec.selector);
        for (var i = 0; i < boxes.length; i++) {
            var row = {};
            var found = false;
            for (var j = 0; j < spec.fields.length; j++) {
                var name = spec.fields[j];
                var v = rowFieldValue(boxes[i], name);
                if (v === null) continue;
                if (numberNames[name]) v = numericValue(v);
                else if (boolNames[name]) v = boolValue(v);
                row[name] = v;
                found = true;
            }
            if (found) rows.push(row);
        }
        return rows;
    }

    // applyObject 把嵌套对象写进 body，并删掉容器里那些**已进对象**的扁平键。
    //
    // 两条语义都是从 ui/json_write.js 的 objectNamesOf / objectOf 原样接过来的：
    //
    //   ① 容器里**全部**具名控件都不进顶层（与 conditions 无关）。
    //      同一个 name 交给端点两次（一次扁平、一次在对象里）会产生两份真相。
    //      注意这一条是**无条件**的 —— 即使下面的条件不满足、对象根本不发，
    //      容器里的字段也不该以扁平形态漏出去。
    //   ② 条件不满足时，整个对象**连同驱动字段**都不发。
    //      手工标签带 ruleType 会被服务端明确拒绝（product 的 normalizeTagShapeOnCreate），
    //      而类型是运行期选的，模板决定不了。
    function applyObject(body, spec) {
        var box = spec.host.querySelector(spec.selector);
        if (!box) return;

        // ① 先无条件清掉容器内的扁平键（含驱动控件本身）。
        var inBox = box.querySelectorAll('[name]');
        for (var i = 0; i < inBox.length; i++) {
            body.delete(inBox[i].getAttribute('name'));
        }
        body.delete(spec.driver);

        // ② 条件不满足 → 到此为止：对象与驱动字段都不发。
        var got = objectOf(body, spec);
        if (!got) return;

        body.set(spec.key, JSON.stringify(got.value));
        // 驱动字段（如 ruleType）单独再发一次：端点用它判「用哪条规则」。
        body.set(got.driver, got.driverValue);
    }

    // applyRows 把行数组写进 body，并清掉行容器里的全部扁平键。
    //
    // 行内字段吃 data-write-number / data-write-bool 的点名：DOM 里的值都是字符串，
    // 而 dto 里 values[].sort 是 int、enabled 是 *bool —— 顶层转、行内不转，端点收到的
    // 是 "0" 与 "1"，严格解码整条 400（症状是「保存没反应」）。
    function applyRows(body, spec, src) {
        var rows = rowsOf(spec, numberSet(src, 'data-write-number'), boolSet(src, 'data-write-bool'));

        // 容器里的**全部**具名控件都不进顶层。
        //
        // 注意：这**不是**为了绕过什么白名单 —— 实测 binding.JSON 对未知键完全容忍
        // （多一个顶层键不会报错）。真正的理由是**语义**：同一个字段既在顶层出现、
        // 又在嵌套对象里出现时，消费方看到的是两份不同的真相，而哪一份生效
        // 取决于遍历顺序 —— 那种歧义不该留在请求体里。
        var boxes = spec.host.querySelectorAll(spec.selector);
        for (var i = 0; i < boxes.length; i++) {
            var els = boxes[i].querySelectorAll('[name]');
            for (var j = 0; j < els.length; j++) {
                body.delete(els[j].getAttribute('name'));
            }
        }

        // 逐行 append **单个 JSON 对象**，不是整体一个数组字符串。
        //
        // 这条是实测出来的（gin v1.12.0 的 form_mapping.go：`reflect.Struct` 分支对每个值
        // 逐元素 `json.Unmarshal`，要求**单个对象**）：
        //
        //	values=[{...},{...}]（整体数组字符串）→ 400 cannot unmarshal array into Go value
        //	values={"..."}&values={"..."}（同键多次） → 200，Values 长度 2
        //
        // 受影响的是所有 slice-of-struct 字段：属性值整表保存、属性组新建的 Values ——
        // 而属性值保存是「整表全量替换」的唯一入口，整体数组会让那个抽屉**完全不可用**。
        //
        // 0 行时**删键而不是发 "[]"**：空数组字符串同样绑不上（实测 400），
        // 而「不发」在 DTO 侧是 nil = 本次不改 / 空表，语义正确。
        body.delete(spec.key);
        for (var r = 0; r < rows.length; r++) {
            body.append(spec.key, JSON.stringify(rows[r]));
        }
    }

    // numberSet / boolSet 读 data-write-number / data-write-bool 点名的字段（逗号分隔）。
    function numberSet(el, attr) { return nameSet(el, attr); }
    function boolSet(el, attr) { return nameSet(el, attr); }

    function nameSet(el, attr) {
        var host = el.closest ? el.closest('[' + attr + ']') : null;
        var out = {};
        if (!host) return out;
        String(host.getAttribute(attr) || '').split(',').forEach(function (raw) {
            var n = String(raw || '').trim();
            if (n) out[n] = true;
        });
        return out;
    }

    window.htmx.on('htmx:config:request', function (evt) {
        var ctx = (evt && evt.detail && evt.detail.ctx) || {};
        var req = ctx.request || {};
        var method = String(req.method || '').toLowerCase();
        if (!WRITE_VERBS[method]) return;

        var body = req.body;
        // v4 的 body 是 FormData 或 URLSearchParams，两者都有 forEach / delete / set。
        if (!body || typeof body.forEach !== 'function' || typeof body.delete !== 'function') return;

        var src = ctx.sourceElement;

        // 顺序要紧：先删顶层空值，再组装对象。
        //   · 反过来会出问题：applyObject 之后 body 里的 ruleParams 是 "{}"（非空串），
        //     本身不会被删；但驱动字段（ruleType）若为空会被 dropEmpty 删掉，
        //     而它是端点判「用哪条规则」的依据 —— 那种情况下整条请求本来就该被拒，
        //     少发一个键只会让错误信息更难读。
        //   · 更关键的是：dropEmpty 要作用在**原始表单值**上，才能在组装前把
        //     「没填的参数输入框」排除掉；组装时再判一次空值（scalar 返回 null）。
        // ids-scope 要在删空值**之前**：勾选框里没有空值，但这一步读的是页面别处的 DOM，
        // 与请求体里现有的键无关。
        applyIDsScope(body, src);

        dropEmpty(body, src);

        // 其余整形：与顺序无关的逐字段转换，放在结构组装之前。
        applyLinesFields(body, src);
        applyHTMLFields(body, src);
        applyFloatFields(body, src);
        applyArrayFields(body, src);

        // 再组装行数组（必须在删空值之前读容器里的原始值 —— 它要取 id 这类隐藏字段）。
        var rowsSpec = src ? rowsSpecOf(src) : null;
        if (rowsSpec) applyRows(body, rowsSpec, src);

        // 最后组装嵌套对象（读容器里的原始值；它自己也跳过空值）。
        var spec = src ? specOf(src) : null;
        if (spec) applyObject(body, spec);

        // builder 放在最后：它读的是 DOM 里已渲染的行，与前面的键级整形无关。
        applyBuilder(body, src);
    });
})();
