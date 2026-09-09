// workbench/methods/controls/media.js — 媒体选择与图片列表控件（从 methods/inspector.js 提取）。

import { commit, get } from './base.js';

// mediaControl 媒体选择控件：缩略图预览 + 媒体库选择 + 清除 + 外链粘贴（统一回填 URL）。
export function mediaControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var val = get(ctx, path) == null ? '' : String(get(ctx, path));
    var box = document.createElement('div');
    box.className = 'wb-media-field' + (val ? ' has-image' : '');
    var img = document.createElement('img'); img.alt = '';
    var src = ctx.self.resolveAssetUrl(val);
    if (src) img.src = src; else img.classList.add('is-empty');
    var tip = document.createElement('span'); tip.className = 'wb-media-tip';
    tip.textContent = src ? '' : '点击选择图片';
    box.appendChild(img); box.appendChild(tip);
    var ext = document.createElement('input'); ext.type='text'; ext.placeholder='或粘贴图片地址（https://…）';
    ext.value = val;
    function set(v, url) {
        commit(ctx, path, v);
        ext.value = v;
        if (url) { img.src = url; img.classList.remove('is-empty'); tip.textContent = ''; }
        else { img.src = ''; img.classList.add('is-empty'); tip.textContent = '点击选择图片'; }
        box.classList.toggle('has-image', !!url);
    }
    box.addEventListener('click', function () {
        ctx.self.openMediaPicker(function (u) {
            set(u, u);
        });
    });
    wrap.appendChild(box);
    var row = document.createElement('div'); row.className = 'wb-media-row';
    var pick = document.createElement('button'); pick.type='button'; pick.className='wb-btn wb-btn-secondary wb-btn-sm'; pick.textContent='媒体库';
    pick.addEventListener('click', function(){ ctx.self.openMediaPicker(function(u){ set(u, u); }); });
    var clear = document.createElement('button'); clear.type='button'; clear.className='wb-btn wb-btn-ghost wb-btn-sm'; clear.textContent='清除';
    clear.addEventListener('click', function(){ set('', ''); });
    row.appendChild(pick); row.appendChild(clear);
    wrap.appendChild(row);
    ext.addEventListener('change', function(){
        var v = ext.value.trim();
        set(v, v);
    });
    wrap.appendChild(ext);
    ctx.panel.appendChild(wrap);
}

// mediaListControl 多图列表（后端 []string，媒体库逐张添加/移除；用于背景轮播）。
export function mediaListControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var list = get(ctx, path);
    if (!Array.isArray(list)) list = [];
    var box = document.createElement('div'); box.className = 'wb-medialist';
    list.forEach(function (url, idx) {
        var row = document.createElement('div'); row.className = 'wb-medialist-row';
        var img = document.createElement('img'); img.className = 'wb-medialist-thumb'; img.alt = '';
        img.src = ctx.self.resolveAssetUrl(url) || url;
        var del = document.createElement('button'); del.type = 'button';
        del.className = 'wb-btn wb-btn-ghost wb-btn-sm'; del.textContent = '移除';
        del.addEventListener('click', function () {
            var next = (Array.isArray(get(ctx, path)) ? get(ctx, path) : []).slice();
            next.splice(idx, 1);
            commit(ctx, path, next);
            ctx.self.syncInspector();
        });
        row.appendChild(img); row.appendChild(del); box.appendChild(row);
    });
    var add = document.createElement('button'); add.type = 'button';
    add.className = 'wb-btn wb-btn-secondary wb-btn-sm'; add.textContent = '+ 添加图片';
    add.addEventListener('click', function () {
        ctx.self.openMediaPicker(function (u) {
            var next = (Array.isArray(get(ctx, path)) ? get(ctx, path) : []).slice();
            next.push(u);
            commit(ctx, path, next);
            ctx.self.syncInspector();
        });
    });
    box.appendChild(add);
    wrap.appendChild(box);
    ctx.panel.appendChild(wrap);
}

// spacer 三端高度已由 schema 驱动（ct:"rtext"）渲染。
// gallery 图片列表 repeater:逐项媒体库选图/alt/删除,底部添加。
export function galleryItemsPanel(ctx, itemsPath) {
    if (!Array.isArray(get(ctx, itemsPath))) set(ctx, itemsPath, []);
    var items = get(ctx, itemsPath);
    function save() {
        commit(ctx, itemsPath, items);
    }
    items.forEach(function (item, idx) {
        var row = document.createElement('div'); row.className = 'wb-repeater-row';
        var thumb = document.createElement('img');
        thumb.className = 'wb-repeater-thumb';
        var res = ctx.self.resolveAssetUrl(item.url);
        if (res) thumb.src = res;
        row.appendChild(thumb);
        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
        var pick = document.createElement('button');
        pick.type = 'button'; pick.className = 'wb-btn wb-btn-secondary wb-btn-sm';
        pick.textContent = '选图';
        pick.addEventListener('click', function () {
            ctx.self.openMediaPicker(function (url) {
                item.url = url;
                save();
            });
        });
        mid.appendChild(pick);
        var alt = document.createElement('input');
        alt.type = 'text'; alt.placeholder = '替代文字'; alt.value = item.alt || '';
        alt.addEventListener('change', function () { item.alt = alt.value; save(); });
        mid.appendChild(alt);
        // 单图加载策略（空=继承组件级 → 主题「图片管理」默认）
        var loadSel = document.createElement('select');
        loadSel.className = 'wb-unit-select'; loadSel.title = '单图加载策略';
        [['', '继承组件'], ['on', '懒加载'], ['off', '立即加载']].forEach(function (o) {
            var opt = document.createElement('option');
            opt.value = o[0]; opt.textContent = o[1];
            if ((item.loading || '') === o[0]) opt.selected = true;
            loadSel.appendChild(opt);
        });
        loadSel.addEventListener('change', function () { item.loading = loadSel.value; save(); });
        mid.appendChild(loadSel);
        // 单图资源提示（空=auto；轮播未设置时第 1 张自动 high、其余 low）
        var fpSel = document.createElement('select');
        fpSel.className = 'wb-unit-select'; fpSel.title = '单图加载优先级';
        [['', '优先级自动'], ['high', '高优先'], ['low', '低优先']].forEach(function (o) {
            var opt2 = document.createElement('option');
            opt2.value = o[0]; opt2.textContent = o[1];
            if ((item.fetchPriority || '') === o[0]) opt2.selected = true;
            fpSel.appendChild(opt2);
        });
        fpSel.addEventListener('change', function () { item.fetchPriority = fpSel.value; save(); });
        mid.appendChild(fpSel);
        row.appendChild(mid);
        var del = document.createElement('button');
        del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕'; del.title = '删除此项';
        del.addEventListener('click', function () {
            items.splice(idx, 1);
            save();
            ctx.self.syncInspector();
        });
        row.appendChild(del);
        ctx.panel.appendChild(row);
    });
    var add = document.createElement('button');
    add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
    add.textContent = '+ 添加图片';
    add.addEventListener('click', function () {
        items.push({ url: '', alt: '', caption: '', link: '' });
        save();
        ctx.self.syncInspector();
    });
    ctx.panel.appendChild(add);
}

// 轮播首图优先加载开关（默认开启：首图立即加载 + fetchpriority=high，压 LCP）
export function carouselFirstEagerControl(ctx) {
    var label = document.createElement('label');
    label.className = 'wb-field wb-field-check';
    var box = document.createElement('input');
    box.type = 'checkbox';
    box.checked = !get(ctx, 'props.carousel.firstEagerOff');
    box.addEventListener('change', function () {
        var c = get(ctx, 'props.carousel') || {};
        c.firstEagerOff = !box.checked;
        commit(ctx, 'props.carousel', c);
    });
    var txt = document.createElement('span');
    txt.textContent = '首图优先加载（LCP）';
    label.appendChild(box); label.appendChild(txt);
    ctx.panel.appendChild(label);
}

