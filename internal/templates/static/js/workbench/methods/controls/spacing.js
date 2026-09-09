// workbench/methods/controls/spacing.js — 间距/尺寸/响应式数值控件（从 methods/inspector.js 提取）。

import { commit, get } from './base.js';

// 单位值输入：解析 "16px" -> 值+单位；紧凑一行。
export function unitInput(ctx, label, path, units) {
    units = units || ['px', '%', 'em', 'rem', 'vw'];
    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-unit';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var row = document.createElement('div'); row.className = 'wb-unit-row';
    var input = document.createElement('input'); input.type = 'text'; input.className = 'wb-unit-value';
    var unitSel = document.createElement('select'); unitSel.className = 'wb-unit-select';
    units.forEach(function (u) {
        var o = document.createElement('option'); o.value = u; o.textContent = u; unitSel.appendChild(o);
    });
    var raw = get(ctx, path) == null ? '' : String(get(ctx, path)).trim();
    var m = raw.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i);
    input.value = m ? m[1] : (raw && !m ? raw : '');
    if (m) unitSel.value = m[2].toLowerCase(); else unitSel.value = units[0];
    function push() {
        var v = input.value.trim();
        if (v === '') { commit(ctx, path, ''); return; }
        // 已带单位(或 clamp()/字母开头)则原样存;纯数字才追加单位。
        commit(ctx, path, /^-?[0-9.]+$/.test(v) ? v + unitSel.value : v);
    }
    input.addEventListener('change', push);
    unitSel.addEventListener('change', function () { if (input.value.trim() !== '') push(); });
    row.appendChild(input); row.appendChild(unitSel); wrap.appendChild(row); ctx.panel.appendChild(wrap);
}

// 三端响应式单位输入(同类型合并):一个控件 + 设备图标切换,绑 Responsive{desktop,tablet,mobile}。
export function responsiveUnitField(ctx, label, objPath, units) {
    units = units || ['px', '%', 'em', 'rem', 'vw'];
    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-unit';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var row = document.createElement('div'); row.className = 'wb-unit-row';
    var input = document.createElement('input'); input.type = 'text'; input.className = 'wb-unit-value';
    var unitSel = document.createElement('select'); unitSel.className = 'wb-unit-select';
    units.forEach(function (u) {
        var o = document.createElement('option'); o.value = u; o.textContent = u; unitSel.appendChild(o);
    });
    var device = 'desktop';
    var devices = [['desktop', '🖥'], ['tablet', '▭'], ['mobile', '📱']];
    var devRow = document.createElement('div'); devRow.className = 'wb-device-row';
    function readVal(dev) {
        var v = get(ctx, objPath + '.' + dev);
        return v == null ? '' : String(v).trim();
    }
    function load(dev) {
        device = dev;
        var raw = readVal(dev);
        var m = raw.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i);
        input.value = m ? m[1] : (raw && !m ? raw : '');
        unitSel.value = m ? m[2].toLowerCase() : units[0];
        devBtns.forEach(function (b) { b.classList.toggle('is-active', b.dataset.dev === device); b.classList.toggle('has-val', !!readVal(b.dataset.dev)); });
    }
    function push() {
        var v = input.value.trim();
        var val = v === '' ? '' : (/^-?[0-9.]+$/.test(v) ? v + unitSel.value : v);
        commit(ctx, objPath + '.' + device, val);
        devBtns.forEach(function (b) { b.classList.toggle('has-val', !!readVal(b.dataset.dev)); });
    }
    input.addEventListener('change', push);
    unitSel.addEventListener('change', function () { if (input.value.trim() !== '') push(); });
    var devBtns = [];
    devices.forEach(function (d) {
        var b = document.createElement('button');
        b.type = 'button'; b.className = 'wb-dev-btn'; b.textContent = d[1]; b.title = d[0]; b.dataset.dev = d[0];
        b.addEventListener('click', function () { load(d[0]); });
        devBtns.push(b); devRow.appendChild(b);
    });
    row.appendChild(input); row.appendChild(unitSel);
    wrap.appendChild(row);
    var row2 = document.createElement('div'); row2.className = 'wb-unit-row';
    row2.appendChild(devRow);
    wrap.appendChild(row2); ctx.panel.appendChild(wrap);
    load(device);
}

// 四向维度输入(WP 式):上右下左 + 链接联动 + 三端切换。
// 值形态为 CSS 简写字符串(单值/两值/四值),编译端 padding/margin 直接输出,
// 后端零改动。继承语义:某端未设置则回退桌面值。
export function dimensionsField(ctx, label, objPath, units) {
    units = units || ['px', '%', 'em', 'rem', 'vw'];
    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-dims';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var device = 'desktop';
    var linked = true;
    var inputs = [];  // [top, right, bottom, left]
    var unitSel = document.createElement('select'); unitSel.className = 'wb-unit-select';
    units.forEach(function (u) {
        var o = document.createElement('option'); o.value = u; o.textContent = u; unitSel.appendChild(o);
    });
    var linkBtn = document.createElement('button');
    linkBtn.type = 'button'; linkBtn.className = 'wb-dims-link is-linked'; linkBtn.textContent = '🔗'; linkBtn.title = '四边联动';
    linkBtn.addEventListener('click', function () {
        linked = !linked;
        linkBtn.classList.toggle('is-linked', linked);
        linkBtn.textContent = linked ? '🔗' : '⛓️‍💥';
    });
    function parseShorthand(raw) {
        var parts = String(raw || '').trim().split(/\s+/).filter(Boolean);
        if (!parts.length) return ['', '', '', ''];
        function strip(v) { var m = v.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i); return m ? m[1] : v; }
        var unitSeen = null;
        parts.forEach(function (v) { var m = v.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i); if (m) { unitSeen = m[2].toLowerCase(); } });
        if (unitSeen) unitSel.value = unitSeen;
        var t = strip(parts[0]);
        var r2 = parts.length > 1 ? strip(parts[1]) : t;
        var b = parts.length > 2 ? strip(parts[2]) : t;
        var l = parts.length > 3 ? strip(parts[3]) : r2;
        return [t, r2, b, l];
    }
    function readRaw(dev) {
        var v = get(ctx, objPath + '.' + dev);
        return v == null ? '' : String(v).trim();
    }
    var devBtns = [];
    var grid = document.createElement('div'); grid.className = 'wb-dims-grid';
    function buildInputs() {
        grid.innerHTML = '';
        inputs = [];
        var sides = ['上', '右', '下', '左'];
        for (var i = 0; i < 4; i++) {
            var inp = document.createElement('input');
            inp.type = 'text'; inp.className = 'wb-dims-input'; inp.placeholder = sides[i];
            inp.dataset.side = String(i);
            inp.addEventListener('change', function (ev) { onInput(ev.target.dataset.side); });
            inputs.push(inp); grid.appendChild(inp);
        }
        grid.appendChild(linkBtn);
        grid.appendChild(unitSel);
    }
    function syncSides() {
        // 联动:任一输入变化同步其余(仅链接开时)。
        if (linked) {
            var v = inputs[0].value;
            for (var i = 1; i < 4; i++) inputs[i].value = v;
        }
    }
    function onInput(sideIdx) {
        if (linked) syncSides();
        write();
    }
    function write() {
        var t = inputs[0].value.trim(), r2 = inputs[1].value.trim(), b = inputs[2].value.trim(), l = inputs[3].value.trim();
        var u = unitSel.value;
        function fmt(v) { if (v === '') return ''; return isNaN(parseFloat(v)) ? v : v + u; }
        var ft = fmt(t), fr = fmt(r2), fb = fmt(b), fl = fmt(l);
        var out;
        if (linked || (t === b && r2 === l)) out = ft || (fr || fb || fl);
        else if (t === b) out = ft + ' ' + fr;
        else out = [ft, fr, fb, fl].join(' ');
        if (out === undefined) out = '';
        commit(ctx, objPath + '.' + device, out.trim());
        devBtns.forEach(function (btn) { btn.classList.toggle('has-val', !!readRaw(btn.dataset.dev)); });
    }
    function load(dev) {
        device = dev;
        buildInputs();
        var four = parseShorthand(readRaw(dev));
        for (var i = 0; i < 4; i++) inputs[i].value = four[i];
        devBtns.forEach(function (btn) { btn.classList.toggle('is-active', btn.dataset.dev === device); btn.classList.toggle('has-val', !!readRaw(btn.dataset.dev)); });
    }
    var devRow = document.createElement('div'); devRow.className = 'wb-device-row';
    [['desktop', '🖥'], ['tablet', '▭'], ['mobile', '📱']].forEach(function (d) {
        var b = document.createElement('button');
        b.type = 'button'; b.className = 'wb-dev-btn'; b.textContent = d[1]; b.title = d[0]; b.dataset.dev = d[0];
        b.addEventListener('click', function () { load(d[0]); });
        devBtns.push(b); devRow.appendChild(b);
    });
    var row = document.createElement('div'); row.className = 'wb-unit-row';
    row.appendChild(grid);
    wrap.appendChild(row);
    var row2 = document.createElement('div'); row2.className = 'wb-unit-row';
    row2.appendChild(devRow);
    wrap.appendChild(row2);
    ctx.panel.appendChild(wrap);
    load(device);
}

// dimensionControl 数值+单位。
export function dimensionControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-unit';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var row = document.createElement('div'); row.className = 'wb-unit-row';
    var input = document.createElement('input'); input.type='text'; input.className='wb-unit-value';
    var unitSel = document.createElement('select'); unitSel.className='wb-unit-select';
    // 单位按字段语义收敛：字号不用 %，字间距/行高不用 vw/%，
    // 避免「选了 % 却算不出来」的无效组合。
    var UNIT_SETS = {
        fontSize: ['px', 'rem', 'em', 'vw'],
        letterSpacing: ['px', 'em', 'rem'],
        lineHeight: ['', 'px', 'em', 'rem'],
        borderRadius: ['px', '%', 'em', 'rem'],
        radius: ['px', '%', 'em', 'rem']
    };
    var units = ctl.unit ? [ctl.unit] : (UNIT_SETS[ctl.key] || ['px', '%', 'em', 'rem']);
    units.forEach(function(u){ var o=document.createElement('option'); o.value=u; o.textContent=u; unitSel.appendChild(o); });
    var raw = get(ctx, path)==null ? '' : String(get(ctx, path)).trim();
    var m = raw.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i);
    input.value = m ? m[1] : (raw && !m ? raw : '');
    if (m) unitSel.value = m[2].toLowerCase(); else unitSel.value = units[0];
    // 始终拼接单位：此前 px 被省略（提交纯数字「99」），
    // 编译端直接输出 font-size: 99 属于无效 CSS，表现为「改字号不生效」。
    function push(){ var v=input.value.trim(); if(!v){ commit(ctx, path,''); return; } commit(ctx, path, v + (unitSel.value||'')); }
    input.addEventListener('change', push);
    unitSel.addEventListener('change', push);
    row.appendChild(input); row.appendChild(unitSel);
    wrap.appendChild(row);
    ctx.panel.appendChild(wrap);
}

// marginControl 四向边距：上右下左 + 联动 + 单位。
export function marginControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var unit = ctl.unit || 'px';
    var sides = [['top','上'],['right','右'],['bottom','下'],['left','左']];
    var raw = get(ctx, path) == null ? '' : String(get(ctx, path)).trim();
    // 支持简写：拆成四边或联动值。
    var linked = '';
    var m = raw.match(/^([0-9.]+)(px|em|rem|%|vw)?$/);
    if (m) linked = m[1] + (m[2] || unit);
    var row = document.createElement('div'); row.className = 'wb-margin-row';
    var inputs = {};
    var linkBtn = document.createElement('button'); linkBtn.type='button';
    linkBtn.className = 'wb-margin-link' + (linked ? ' is-on' : '');
    linkBtn.textContent = '🔗';
    linkBtn.title = '四边联动';
    sides.forEach(function(sd){
        var box = document.createElement('div'); box.className='wb-margin-side';
        var lb = document.createElement('span'); lb.textContent = sd[1];
        var inp = document.createElement('input'); inp.type='text'; inp.placeholder='—';
        inp.value = linked ? '' : (function(){ var mm=raw.match(new RegExp(sd[0]+':([^;]+)')); return mm?mm[1]:''; })();
        inp.addEventListener('change', function(){
            var vals = sides.map(function(sd2){ return inputs[sd2[0]].value.trim(); });
            var all = vals.every(function(v){ return v==='' || v===vals[0]; });
            if (all && vals[0]!=='') { commit(ctx, path, vals[0]); }
            else { commit(ctx, path, sides.map(function(sd2,i){ return inputs[sd2[0]].value.trim()? sd2[0]+':'+inputs[sd2[0]].value.trim() : (vals[0]?sd2[0]+':'+vals[0]:''); }).filter(Boolean).join(';')); }
        });
        box.appendChild(lb); box.appendChild(inp);
        inputs[sd[0]] = inp;
        row.appendChild(box);
    });
    // 联动值输入（四边一致）。
    var linkedInput = document.createElement('input'); linkedInput.type='text';
    linkedInput.className='wb-margin-linked'; linkedInput.placeholder='四边统一（如 16px）';
    linkedInput.value = linked;
    linkedInput.addEventListener('change', function(){ commit(ctx, path, linkedInput.value.trim()); });
    row.appendChild(linkBtn);
    wrap.appendChild(row);
    ctx.panel.appendChild(wrap);
    var lw = document.createElement('div'); lw.className='wb-margin-linked-row';
    lw.appendChild(linkedInput);
    wrap.appendChild(lw);
}

// spacingControl 三端 × 四向间距（后端 ResponsiveSpacing：{desktop:{top,right,bottom,left}, tablet, mobile}）。
export function spacingControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);

    var DEVICES = [['desktop', '桌面'], ['tablet', '平板'], ['mobile', '手机']];
    var SIDES = [['top', '上'], ['right', '右'], ['bottom', '下'], ['left', '左']];
    var device = 'desktop';
    var unit = ctl.unit || 'px';
    var linked = true;   // 默认联动（WP 式），点 🔗 解除

    var devBar = document.createElement('div'); devBar.className = 'wb-subtabs wb-device-tabs';
    DEVICES.forEach(function (d) {
        var b = document.createElement('button'); b.type = 'button';
        b.className = 'wb-subtab' + (device === d[0] ? ' is-active' : '');
        b.textContent = d[1];
        b.addEventListener('click', function () {
            device = d[0];
            // 同步三端按钮高亮：此前只重绘下方四向框，按钮高亮不变，
            // 看起来像「切不动」（新端本来没值 → 四个框都空，视觉无变化）。
            Array.prototype.forEach.call(devBar.children, function (x, i) {
                x.classList.toggle('is-active', DEVICES[i][0] === device);
            });
            render();
        });
        devBar.appendChild(b);
    });
    wrap.appendChild(devBar);

    var body = document.createElement('div');
    wrap.appendChild(body);

    function readSide(side) {
        var v = get(ctx, path + '.' + device + '.' + side);
        return v == null ? '' : String(v);
    }
    function writeSide(side, val) {
        commit(ctx, path + '.' + device + '.' + side, val);
    }
    function render() {
        body.innerHTML = '';
        // WP 式：四向框与联动锁同一行（单位由输入值自带，如 10px / 2rem）。
        var linkBtn = document.createElement('button'); linkBtn.type = 'button';
        linkBtn.className = 'wb-margin-link' + (linked ? ' is-on' : '');
        linkBtn.textContent = '🔗';
        linkBtn.title = linked ? '已联动：四向一起改' : '未联动：四向独立';
        linkBtn.addEventListener('click', function () { linked = !linked; render(); });

        var row = document.createElement('div'); row.className = 'wb-margin-row';
        SIDES.forEach(function (sd) {
            var box = document.createElement('div'); box.className = 'wb-margin-side';
            var lb = document.createElement('span'); lb.textContent = sd[1];
            var inp = document.createElement('input'); inp.type = 'text'; inp.placeholder = '—';
            var raw = readSide(sd[0]);
            var m = raw.match(/^(-?[0-9.]+)\s*([a-z%]*)$/i);
            inp.value = m ? m[1] : raw;
            if (m && m[2]) unit = m[2];
            inp.addEventListener('change', function () {
                var val = inp.value.trim();
                var full = val === '' ? '' : (/[a-z%]/i.test(val) ? val : val + unit);
                if (linked) {
                    SIDES.forEach(function (s2) { writeSide(s2[0], full); });
                } else {
                    writeSide(sd[0], full);
                }
            });
            box.appendChild(lb); box.appendChild(inp);
            row.appendChild(box);
        });
        row.appendChild(linkBtn);
        body.appendChild(row);
    }
    render();
    ctx.panel.appendChild(wrap);
}

// boxSpacingControl 三端 CSS 简写 → 「一行四向 + 联动」编辑
// （container 的 box.padding/margin）。数据仍是 CSS 简写字符串，
// 编译端零改动；四向解析/回写在这里完成，默认联动、点锁解除。
export function boxSpacingControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);

    var DEVICES = [['desktop', '桌面'], ['tablet', '平板'], ['mobile', '手机']];
    var SIDES = [['top', '上'], ['right', '右'], ['bottom', '下'], ['left', '左']];
    var device = 'desktop';
    var linked = true;   // 默认联动（WP 式），点 🔗 解除
    var unit = ctl.unit || 'px';

    var devBar = document.createElement('div'); devBar.className = 'wb-subtabs wb-device-tabs';
    DEVICES.forEach(function (d) {
        var b = document.createElement('button'); b.type = 'button';
        b.className = 'wb-subtab' + (device === d[0] ? ' is-active' : '');
        b.textContent = d[1];
        b.addEventListener('click', function () {
            device = d[0];
            // 同步三端按钮高亮：此前只重绘下方四向框，按钮高亮不变，
            // 看起来像「切不动」（新端本来没值 → 四个框都空，视觉无变化）。
            Array.prototype.forEach.call(devBar.children, function (x, i) {
                x.classList.toggle('is-active', DEVICES[i][0] === device);
            });
            render();
        });
        devBar.appendChild(b);
    });
    wrap.appendChild(devBar);
    var body = document.createElement('div');
    wrap.appendChild(body);

    // parseShorthand：CSS 简写 → [上, 右, 下, 左]（1/2/3/4 值规则）。
    function parseShorthand(v) {
        var parts = String(v == null ? '' : v).trim().split(/\s+/).filter(Boolean);
        if (!parts.length) return ['', '', '', ''];
        if (parts.length === 1) return [parts[0], parts[0], parts[0], parts[0]];
        if (parts.length === 2) return [parts[0], parts[1], parts[0], parts[1]];
        if (parts.length === 3) return [parts[0], parts[1], parts[2], parts[1]];
        return [parts[0], parts[1], parts[2], parts[3]];
    }
    // buildShorthand：四向 → 最短等价简写（全空回退空串）。
    function buildShorthand(t, r, b, l) {
        if (!t && !r && !b && !l) return '';
        if (t === b && r === l) {
            if (t === r) return t;
            return t + ' ' + r;
        }
        return [t, r, b, l].join(' ');
    }
    function render() {
        body.innerHTML = '';
        var raw = get(ctx, path + '.' + device);
        var sides = parseShorthand(raw == null ? '' : String(raw));
        var row = document.createElement('div'); row.className = 'wb-margin-row';
        var inputs = [];
        SIDES.forEach(function (sd, i) {
            var box = document.createElement('div'); box.className = 'wb-margin-side';
            var lb = document.createElement('span'); lb.textContent = sd[1];
            var inp = document.createElement('input'); inp.type = 'text'; inp.placeholder = '—';
            inp.value = sides[i] || '';
            inp.addEventListener('change', function () {
                var val = inp.value.trim();
                if (linked) { inputs.forEach(function (x) { x.value = val; }); }
                var vals = inputs.map(function (x) { return x.value.trim(); });
                commit(ctx, path + '.' + device, buildShorthand(vals[0], vals[1], vals[2], vals[3]));
            });
            box.appendChild(lb); box.appendChild(inp);
            row.appendChild(box);
            inputs.push(inp);
        });
        var linkBtn = document.createElement('button'); linkBtn.type = 'button';
        linkBtn.className = 'wb-margin-link' + (linked ? ' is-on' : '');
        linkBtn.textContent = '🔗';
        linkBtn.title = linked ? '已联动：四向一起改' : '未联动：四向独立';
        linkBtn.addEventListener('click', function () { linked = !linked; render(); });
        row.appendChild(linkBtn);
        body.appendChild(row);
    }
    render();
    ctx.panel.appendChild(wrap);
}

// rtextControl 三端文本值（后端 Responsive{desktop,tablet,mobile}，如容器内距 "10px 20px"）。
export function rtextControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var device = 'desktop';
    var holder = document.createElement('div');
    wrap.appendChild(holder);
    function render() {
        holder.innerHTML = '';
        // DEVICES 必须是本函数内的局部常量：此前误用全局名，点击会抛
        // ReferenceError，表现为三端按钮点了没反应（高亮与值都不变）。
        var DEVICES = [['desktop', '桌面'], ['tablet', '平板'], ['mobile', '手机']];
        var devBar = document.createElement('div'); devBar.className = 'wb-subtabs wb-device-tabs';
        DEVICES.forEach(function (d) {
            var b = document.createElement('button'); b.type = 'button';
            b.className = 'wb-subtab' + (device === d[0] ? ' is-active' : '');
            b.textContent = d[1];
            b.addEventListener('click', function () {
                device = d[0];
                Array.prototype.forEach.call(devBar.children, function (x, i) {
                    x.classList.toggle('is-active', DEVICES[i][0] === device);
                });
                render();
            });
            devBar.appendChild(b);
        });
        holder.appendChild(devBar);
        var input = document.createElement('input'); input.type = 'text';
        input.placeholder = 'CSS 值，如 10px 20px / auto';
        var cur = get(ctx, path + '.' + device);
        input.value = cur == null ? '' : String(cur);
        input.addEventListener('change', function () { commit(ctx, path + '.' + device, input.value.trim()); });
        holder.appendChild(input);
    }
    render();
    ctx.panel.appendChild(wrap);
}

