// workbench/methods/controls/corners.js — 四角圆角控件（从 methods/inspector.js 提取）。

import { commit, get } from './base.js';

// cornersControl 四角圆角（radiusTL/TR/BR/BL 合并渲染：四输入 + 单位 + 联动锁）。
export function cornersControl(ctx, label, ctl, prefix, CORNERS) {
    var unit = ctl.unit || 'px';
    var linked = false;
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);

    var unitRow = document.createElement('div'); unitRow.className = 'wb-unit-row';
    var unitSel = document.createElement('select'); unitSel.className = 'wb-unit-select';
    ['px', '%', 'em', 'rem'].forEach(function (u) {
        var o = document.createElement('option'); o.value = u; o.textContent = u; unitSel.appendChild(o);
    });
    var linkBtn = document.createElement('button'); linkBtn.type = 'button';
    linkBtn.className = 'wb-margin-link' + (linked ? ' is-on' : '');
    linkBtn.textContent = '🔗';
    linkBtn.title = linked ? '已联动：四角一起改' : '未联动：四角独立';
    linkBtn.addEventListener('click', function () { linked = !linked; render(); });
    unitRow.appendChild(unitSel); unitRow.appendChild(linkBtn);
    wrap.appendChild(unitRow);

    var body = document.createElement('div');
    wrap.appendChild(body);

    function render() {
        body.innerHTML = '';
        var row = document.createElement('div'); row.className = 'wb-margin-row';
        CORNERS.forEach(function (cn) {
            var box = document.createElement('div'); box.className = 'wb-margin-side';
            var lb = document.createElement('span'); lb.textContent = cn[1];
            var inp = document.createElement('input'); inp.type = 'text'; inp.placeholder = '—';
            var raw = get(ctx, prefix + cn[0]);
            raw = raw == null ? '' : String(raw);
            var m = raw.match(/^(-?[0-9.]+)\s*([a-z%]*)$/i);
            inp.value = m ? m[1] : raw;
            if (m && m[2]) unit = m[2];
            inp.addEventListener('change', function () {
                var val = inp.value.trim();
                var full = val === '' ? '' : (/[a-z%]/i.test(val) ? val : val + unit);
                if (linked) {
                    CORNERS.forEach(function (c2) { commit(ctx, prefix + c2[0], full); });
                } else {
                    commit(ctx, prefix + cn[0], full);
                }
            });
            box.appendChild(lb); box.appendChild(inp);
            row.appendChild(box);
        });
        body.appendChild(row);
        unitSel.value = unit;
    }
    render();
    ctx.panel.appendChild(wrap);
}

