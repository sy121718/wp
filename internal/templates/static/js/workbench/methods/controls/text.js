// workbench/methods/controls/text.js — 富文本/排版/图标/类名控件（从 methods/inspector.js 提取）。

import { commit, get, set } from './base.js';
import { unitInput } from './spacing.js';
import { csrfHeaders } from '../../core.js';

// 富文本编辑器（Trix 2.x，本地 vendor 资源 /static/vendor/trix/）：
// 工具条与构建期白名单对齐（加粗/斜体/删除线/列表/引用/代码/链接），
// 提交 HTML 在发布构建时经 sanitizeRichHTML 白名单清洗（h1~h5 原样保留，不做标题层级降级）。
// 正文内容由 Trix 自动同步进关联的 <input type="hidden">（见 Trix README
// 「Integrating with Forms」），因此不写任何表单同步 / 提交钩子 JS。
//
// 附件上传（见下方 bindRichAttachmentUpload）：Trix 2.x 没有「用属性指定上传 URL」
// 的内置能力，粘贴/拖入图片只产生 pending attachment，必须自行监听
// trix-attachment-add → XHR 上传 → 回填 url/href，否则保存的 HTML 里 src 会被
// 协议白名单剥离成空 figure。上传复用媒体模块 POST /api/media/upload。
var richTextFieldSeq = 0;

// RICH_UPLOAD_ENDPOINT 富文本附件上传端点：直接复用媒体模块 POST /api/media/upload
// （multipart 字段名 file，响应 {code,message,data:{url,...}}；与 methods/media.js 的
// uploadMedia 同一端点）。不新增代理端点、不重复实现存储与校验——类型/大小/魔数嗅探
// 全部由 media service 决定，前端只透传后端的错误消息。
var RICH_UPLOAD_ENDPOINT = '/api/media/upload';

// syncRichValue 把 Trix 写入隐藏 input 的 HTML 同步回 AST 节点属性。
// 工作台草稿以 AST JSON 提交（methods/api.js draft/save），不是表单提交，
// 因此必须写回节点属性；值未变化时跳过，避免初始化污染 undo 栈。
function syncRichValue(ctx, path, input) {
    var html = input.value || '';
    var prev = get(ctx, path) == null ? '' : String(get(ctx, path));
    if (html === prev) return;
    ctx.self.snapshot();
    set(ctx, path, html);
    ctx.self.renderTree(); ctx.self.refreshCanvas(); ctx.self.renderUI();
}

// bindRichAttachmentUpload 接管 Trix 附件上传（Trix 官方约定，见 README
// 「Storing Attachments」；本地 vendor 为 trix 2.1.19）。
//
// 用到的 Trix 附件 API（已按 2.1.19 源码核对，ManagedAttachment 逐项代理）：
//   - 事件 trix-attachment-add，event.attachment 为 ManagedAttachment
//     （Trix 把 detail 平铺到事件对象上，同时保留 event.detail.attachment 兜底）
//   - attachment.setUploadProgress(0~100)：进度是百分数，不是 0~1
//   - attachment.setAttributes({ url, href })：上传成功后回填，Trix 随即渲染 figure/img
//   - attachment.remove()：失败时移除 pending 附件（留着会写进正文变成空 figure）
function bindRichAttachmentUpload(editor, wrap, ctx, path, input) {
    var hintTimer = null;

    // 可见提示：上传中/成功/失败都在字段下方显示一行文字，8 秒后自动清空。
    function hint(text, isError) {
        var box = wrap.querySelector('.wb-richtext-upload-hint');
        if (!box) {
            box = document.createElement('div');
            box.className = 'wb-richtext-upload-hint';
            box.style.marginTop = '6px';
            box.style.fontSize = '12px';
            box.style.lineHeight = '1.5';
            wrap.appendChild(box);
        }
        box.style.color = isError ? '#cf1322' : '#8c8c8c';
        box.textContent = text;
        if (hintTimer) { clearTimeout(hintTimer); hintTimer = null; }
        if (text) {
            hintTimer = setTimeout(function () { box.textContent = ''; }, 8000);
        }
    }

    editor.addEventListener('trix-attachment-add', function (event) {
        var attachment = event.attachment || (event.detail && event.detail.attachment);
        // 非文件附件（内容附件 / 粘贴的富 HTML）没有 file，保持 Trix 默认行为。
        if (!attachment || !attachment.file) return;

        var file = attachment.file;
        var form = new FormData();
        form.append('file', file);   // 字段名对齐 media 模块的 c.FormFile("file")

        var xhr = new XMLHttpRequest();
        xhr.open('POST', RICH_UPLOAD_ENDPOINT, true);
        // CSRF：写接口必须带 X-CSRF-Token（builtin.CSRFMiddleware 读请求头或 csrf_token
        // 表单域）。不设置 Content-Type——浏览器自动补 multipart/form-data + boundary。
        var headers = csrfHeaders({});
        Object.keys(headers).forEach(function (key) { xhr.setRequestHeader(key, headers[key]); });

        attachment.setUploadProgress(0);
        xhr.upload.onprogress = function (e) {
            if (!e.lengthComputable || !e.total) return;
            attachment.setUploadProgress(Math.round(e.loaded / e.total * 100));
        };

        function fail(message) {
            // 失败必须移除 pending 附件：留着会被写进正文并渲染成空 figure。
            try { attachment.remove(); } catch (e) { /* 已不在文档中 */ }
            hint((message || '上传失败') + '（已移除该附件）', true);
        }

        xhr.onload = function () {
            var payload = null;
            try { payload = JSON.parse(xhr.responseText); } catch (e) { payload = null; }
            var url = (payload && payload.data && payload.data.url) ? payload.data.url : '';
            var failed = xhr.status < 200 || xhr.status >= 300 || (payload && payload.code >= 400) || !url;
            if (failed) {
                fail((payload && payload.message) || ('上传失败（HTTP ' + xhr.status + '）'));
                return;
            }
            attachment.setUploadProgress(100);
            attachment.setAttributes({ url: url, href: url });
            hint('已上传：' + (file.name || '附件'), false);
            // attachment-add 只更新隐藏 input 的 value、不派发 trix-change，
            // 故这里主动回写 AST；延到下一帧，等 Trix 渲染完 figure 再读 value。
            setTimeout(function () { syncRichValue(ctx, path, input); }, 0);
        };
        xhr.onerror = function () { fail('网络错误，上传失败'); };
        xhr.onabort = function () { fail('上传已取消'); };

        hint('上传中：' + (file.name || '附件'), false);
        xhr.send(form);
    });
}

export function richTextField(ctx, label, path) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-richtext';
    var caption = document.createElement('label'); caption.textContent = label + '（富文本）'; wrap.appendChild(caption);

    // 关联隐藏 input：Trix 随编辑实时写入 value，表单 / htmx 序列化即可取到正文。
    var inputId = 'wb-richtext-' + (++richTextFieldSeq);
    var input = document.createElement('input');
    input.type = 'hidden';
    input.id = inputId;
    input.name = path.split('.').join('-');
    input.value = get(ctx, path) == null ? '' : String(get(ctx, path));
    wrap.appendChild(input);
    ctx.panel.appendChild(wrap);

    if (!window.Trix) {
        // vendor 资源缺失：退化为 textarea，保证正文仍可编辑（不静默丢内容）。
        var ta = document.createElement('textarea'); ta.rows = 8; ta.value = input.value;
        ta.addEventListener('change', function () { commit(ctx, path, ta.value); });
        wrap.appendChild(ta);
        return;
    }

    var editor = document.createElement('trix-editor');
    editor.setAttribute('input', inputId);
    editor.setAttribute('placeholder', '输入正文…');
    editor.className = 'wb-richtext-editor';
    wrap.appendChild(editor);

    // AST 回写：正文变化（含附件上传成功后的 figure 更新）统一走 syncRichValue。
    editor.addEventListener('trix-change', function () { syncRichValue(ctx, path, input); });

    // 图片/文件附件上传：粘贴、拖入、工具条插图都产生 pending attachment，
    // 由这里接管上传并回填 URL。
    bindRichAttachmentUpload(editor, wrap, ctx, path, input);
}

// 排版组：组级设备切换，字号/行高/对齐绑 TextStyle 三端
// (fontSize 校验支持 clamp() 流式字号，输入以字母开头时不追加单位)。
// image 样式：固定高度三端（结构字段 schema 管不到）。
// 图片高度、标题对齐/宽度、副标题排版均已由 schema 驱动（ct tag / rtext 控件）渲染。
export function typographyPanel(ctx, typoPath) {
    var device = 'desktop';
    var devices = [['desktop', '🖥'], ['tablet', '▭'], ['mobile', '📱']];
    var devRow = document.createElement('div'); devRow.className = 'wb-device-row';
    var devBtns = [];
    devices.forEach(function (d) {
        var b = document.createElement('button');
        b.type = 'button'; b.className = 'wb-dev-btn'; b.textContent = d[1]; b.title = d[0]; b.dataset.dev = d[0];
        b.addEventListener('click', function () {
            device = d[0];
            devBtns.forEach(function (x) { x.classList.toggle('is-active', x.dataset.dev === device); });
            rebuild();
        });
        devBtns.push(b); devRow.appendChild(b);
    });
    var host = document.createElement('div');
    var wrapAll = document.createElement('div'); wrapAll.className = 'wb-field wb-typo-group';
    var head = document.createElement('div'); head.className = 'wb-typo-head';
    var cap = document.createElement('label'); cap.textContent = '排版';
    head.appendChild(cap); head.appendChild(devRow);
    wrapAll.appendChild(head); wrapAll.appendChild(host);
    ctx.panel.appendChild(wrapAll);

    function base() { return typoPath + '.' + device; }
    function rebuild() {
        host.innerHTML = '';
        devBtns.forEach(function (x) { x.classList.toggle('is-active', x.dataset.dev === device); });
        var save = ctx.panel;
        ctx.panel = host;
        unitInput(ctx, '字号', base() + '.fontSize', ['px', 'rem', 'em', 'vw']);
        unitInput(ctx, '行高', base() + '.lineHeight', ['', 'px', 'rem']);
        var alignSeg = document.createElement('div'); alignSeg.className = 'wb-seg';
        var aligns = [['left', '左'], ['center', '中'], ['right', '右'], ['justify', '两端']];
        var cur = get(ctx, base() + '.textAlign') || '';
        var btns = [];
        aligns.forEach(function (a) {
            var b = document.createElement('button');
            b.type = 'button'; b.className = 'wb-seg-btn'; b.textContent = a[1];
            if (cur === a[0]) b.classList.add('is-active');
            b.addEventListener('click', function () {
                commit(ctx, base() + '.textAlign', a[0]);
                btns.forEach(function (x) { x.classList.toggle('is-active', x === b); });
            });
            btns.push(b); alignSeg.appendChild(b);
        });
        var aw = document.createElement('div'); aw.className = 'wb-field wb-field-seg';
        var ac = document.createElement('label'); ac.textContent = '对齐';
        aw.appendChild(ac); aw.appendChild(alignSeg); host.appendChild(aw);
        ctx.panel = save;
    }
    rebuild();
}

// renderIcon 安全渲染图标：仅白名单（WPIcons.names）名走 innerHTML（SVG 来自常量），
// 其余（自定义/未知名）退回 textContent，杜绝非白名单值注入。
export function renderIcon(ctx, el, name, fallback) {
    if (name && window.WPIcons && window.WPIcons.names && window.WPIcons.names.indexOf(name) >= 0) {
        el.innerHTML = window.WPIcons.svg(name);
    } else {
        el.textContent = (name || fallback || '');
    }
}

// iconFilterBar 图标选择器工具栏：搜索框 + 分类标签。
// names 为可用图标名全集（可能是 opts.names 子集）；onchange 在筛选状态变化时回调。
// 返回 { el, filtered() }：el 为工具栏 DOM，filtered() 返回按当前分类+关键词筛选后的图标名。
export function iconFilterBar(ctx, names, onchange) {
    var state = { category: '', style: '', keyword: '' };
    var bar = document.createElement('div');
    bar.className = 'wb-icon-toolbar';
    var search = document.createElement('input');
    search.type = 'text';
    search.className = 'wb-icon-search';
    search.placeholder = '搜索图标…';
    search.addEventListener('input', function () { state.keyword = search.value; onchange(); });
    bar.appendChild(search);
    var styles = document.createElement('div');
    styles.className = 'wb-icon-styles';
    function styleBtn(v, label) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'wb-icon-cat';
        b.textContent = label;
        b.addEventListener('click', function () {
            state.style = v;
            Array.prototype.forEach.call(styles.children, function (x) { x.classList.toggle('is-active', x === b); });
            onchange();
        });
        styles.appendChild(b);
        return b;
    }
    styleBtn('', '全部').classList.add('is-active');
    styleBtn('outlined', '描边');
    styleBtn('filled', '实心');
    bar.appendChild(styles);
    var cats = document.createElement('div');
    cats.className = 'wb-icon-cats';
    function catBtn(c, label) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'wb-icon-cat';
        b.textContent = label;
        b.addEventListener('click', function () {
            state.category = c;
            Array.prototype.forEach.call(cats.children, function (x) { x.classList.toggle('is-active', x === b); });
            onchange();
        });
        cats.appendChild(b);
        return b;
    }
    catBtn('', '全部').classList.add('is-active');
    (window.WPIcons && window.WPIcons.categories || []).forEach(function (c) {
        catBtn(c, window.WPIcons.categoryLabel(c));
    });
    bar.appendChild(cats);
    return {
        el: bar,
        filtered: function () {
            if (!window.WPIcons || !window.WPIcons.filter) return names;
            var out = window.WPIcons.filter(state.category, state.style, state.keyword);
            // opts.names 限定的子集取交集，保持调用方约束（如按钮只允许箭头图标）。
            if (names !== window.WPIcons.names) {
                var set = {};
                names.forEach(function (n) { set[n] = true; });
                out = out.filter(function (n) { return set[n]; });
            }
            return out;
        }
    };
}

// iconPopupPicker 弹层图标选择：点击小图标按钮弹出 SVG 网格，选后回填。
// onPick(value) 回调；当前值 current。
export function iconPopupPicker(ctx, triggerLabel, current, onPick, opts) {
    opts = opts || {};
    var names = opts.names || (window.WPIcons ? window.WPIcons.names : []);
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'wb-icon-pop';
    renderIcon(btn, current, '＋');
    btn.title = '选择图标';
    // 浮层。
    var pop = document.createElement('div');
    pop.className = 'wb-icon-pop-menu';
    pop.style.display = 'none';
    var grid = document.createElement('div');
    grid.className = 'wb-icon-grid';
    var pager = document.createElement('div');
    pager.className = 'wb-icon-pager';
    // 筛选条件变化时回到第一页（否则可能停在一个不存在的页码）。
    var toolbar = iconFilterBar(names, function () { page = 1; buildGrid(); });
    // 命名 closer：仅在浮层打开时挂载，关闭即解绑，避免每次构建累积 document 级监听器。
    function onDocClick(ev) {
        if (!pop.contains(ev.target) && ev.target !== btn) closePop();
    }
    function closePop() {
        pop.style.display = 'none';
        document.removeEventListener('click', onDocClick);
    }
    // 分页：图标上千个，一次渲染既慢又难找；每页固定数量 + 底部翻页。
    var PAGE_SIZE = 60;
    var page = 1;
    function buildGrid() {
        grid.innerHTML = '';
        pager.innerHTML = '';
        var cur = current;
        var all = toolbar.filtered();
        var totalPages = Math.max(1, Math.ceil(all.length / PAGE_SIZE));
        if (page > totalPages) page = totalPages;
        if (page < 1) page = 1;
        var start = (page - 1) * PAGE_SIZE;
        var slice = all.slice(start, start + PAGE_SIZE);

        // 「无」（清空图标）只在第一页首位显示，避免每页都占一格。
        if (opts.allowEmpty && page === 1) {
            var n0 = document.createElement('button'); n0.type='button'; n0.className='wb-icon-cell'+(cur===''?' is-active':'');
            n0.textContent='无'; n0.addEventListener('click', function(){ closePop(); onPick(''); });
            grid.appendChild(n0);
        }
        slice.forEach(function(nm){
            var b=document.createElement('button'); b.type='button';
            b.className='wb-icon-cell'+(cur===nm?' is-active':'');
            b.title=window.WPIcons?window.WPIcons.label(nm):nm;
            renderIcon(b, nm, '');
            b.addEventListener('click', function(){ closePop(); onPick(nm); });
            grid.appendChild(b);
        });

        // 分页条：总数提示 + 上一页 / 页码 / 下一页。
        var info = document.createElement('span');
        info.className = 'wb-icon-page-info';
        info.textContent = all.length ? (all.length + ' 个 · 第 ' + page + '/' + totalPages + ' 页') : '没有匹配的图标';
        pager.appendChild(info);
        if (totalPages > 1) {
            var nav = document.createElement('span');
            nav.className = 'wb-icon-page-nav';
            function pageBtn(label, target, disabled) {
                var b = document.createElement('button');
                b.type = 'button'; b.className = 'wb-icon-page-btn';
                b.textContent = label;
                if (disabled) { b.disabled = true; b.classList.add('is-disabled'); }
                b.addEventListener('click', function () {
                    if (disabled) return;
                    page = target;
                    buildGrid();
                    grid.scrollTop = 0;
                });
                nav.appendChild(b);
            }
            pageBtn('‹', page - 1, page <= 1);
            // 页码窗口：当前页前后各 2 页 + 首尾。
            var win = [];
            for (var i = 1; i <= totalPages; i++) {
                if (i === 1 || i === totalPages || Math.abs(i - page) <= 2) win.push(i);
            }
            var last = 0;
            win.forEach(function (i) {
                if (last && i - last > 1) {
                    var dot = document.createElement('span');
                    dot.className = 'wb-icon-page-dot'; dot.textContent = '…';
                    nav.appendChild(dot);
                }
                var b = document.createElement('button');
                b.type = 'button';
                b.className = 'wb-icon-page-btn' + (i === page ? ' is-active' : '');
                b.textContent = String(i);
                b.addEventListener('click', function () { page = i; buildGrid(); grid.scrollTop = 0; });
                nav.appendChild(b);
                last = i;
            });
            pageBtn('›', page + 1, page >= totalPages);
            pager.appendChild(nav);
        }
    }
    // 不在创建时构建网格：图标上千个，预渲染会让检查器 DOM 膨胀到数百 KB。
    // 改为点击打开时才构建（buildGrid 在下方 click 处理里调用）。
    pop.appendChild(toolbar.el);
    pop.appendChild(grid);
    pop.appendChild(pager);
    btn.addEventListener('click', function(){
        if (pop.style.display === 'block') { closePop(); return; }
        pop.style.display = 'block';
        buildGrid();
        document.addEventListener('click', onDocClick);
    });
    return { btn: btn, pop: pop, refresh: function(v){ current=v; renderIcon(btn, v, '＋'); } };
}

// iconPicker 视觉化图标选择：SVG 网格点选（无文字按钮）。
// opts.names 可用图标名列表（默认 WPIcons 全部）；opts.allowEmpty 首位加「无」。
export function iconPicker(ctx, label, path, opts) {
    opts = opts || {};
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var grid = document.createElement('div'); grid.className = 'wb-icon-grid';
    var names = opts.names || (window.WPIcons ? window.WPIcons.names : []);
    var current = get(ctx, path) == null ? '' : String(get(ctx, path));
    var toolbar = iconFilterBar(names, paint);
    function paint() {
        grid.innerHTML = '';
        if (opts.allowEmpty) {
            var none = document.createElement('button');
            none.type = 'button'; none.className = 'wb-icon-cell' + (current === '' ? ' is-active' : '');
            none.textContent = '无';
            none.addEventListener('click', function () { current = ''; commit(ctx, path, ''); paint(); });
            grid.appendChild(none);
        }
        toolbar.filtered().forEach(function (name) {
            var b = document.createElement('button');
            b.type = 'button';
            b.className = 'wb-icon-cell' + (current === name ? ' is-active' : '');
            b.title = window.WPIcons ? window.WPIcons.label(name) : name;
            renderIcon(b, name, '');
            b.addEventListener('click', function () {
                current = name;
                commit(ctx, path, name);
                paint();
            });
            grid.appendChild(b);
        });
    }
    paint();
    wrap.appendChild(toolbar.el);
    wrap.appendChild(grid);
    ctx.panel.appendChild(wrap);
}

// classesControl 多类名（后端 []string，前端逗号/空格分隔编辑）。
export function classesControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var input = document.createElement('input'); input.type = 'text';
    input.placeholder = '多个类名用空格或逗号分隔，如 hero-box accent';
    var cur = get(ctx, path);
    input.value = Array.isArray(cur) ? cur.join(' ') : (cur == null ? '' : String(cur));
    input.addEventListener('change', function () {
        var list = input.value.split(/[,\s]+/).filter(function (x) { return x !== ''; });
        commit(ctx, path, list);
    });
    wrap.appendChild(input);
    ctx.panel.appendChild(wrap);
}

