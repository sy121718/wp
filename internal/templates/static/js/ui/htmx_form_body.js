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

    // ── data-write-array：同名多值永远收成数组（哪怕一个都没勾） ──
    //
    // 浏览器对「一个都没勾的 checkbox 组」**完全不提交该键**，于是 DTO 的
    // `AttributeIDs []string` 保持 nil —— 语义从「解绑全部」反转成「本次不改」，
    // 用户以为解绑了。声明过的字段因此在一个都没勾时补一个空数组。
    function applyArrayFields(body, el) {
        var names = nameSet(el, 'data-write-array');
        Object.keys(names).forEach(function (name) {
            if (body.get && body.get(name) !== null) return;   // 有值就不动
            body.set(name, '[]');
        });
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
    // 都不进顶层标量 —— 同一个 name 交给端点两次，严格白名单只能整条请求拒掉。
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
    //      同一个 name 交给端点两次（一次扁平、一次在对象里），严格白名单只能整条请求拒掉。
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

        // 容器里的**全部**具名控件都不进顶层（判据用容器而不是点名的字段名：
        // 容器里没点名的字段若漏到顶层，端点按严格白名单只能整条拒掉）。
        var boxes = spec.host.querySelectorAll(spec.selector);
        for (var i = 0; i < boxes.length; i++) {
            var els = boxes[i].querySelectorAll('[name]');
            for (var j = 0; j < els.length; j++) {
                body.delete(els[j].getAttribute('name'));
            }
        }

        body.set(spec.key, JSON.stringify(rows));
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
    });
})();
