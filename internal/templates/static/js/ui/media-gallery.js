/* ui/media-gallery.js — 多图字段控件（缩略图网格 + 媒体库多选 + 逐张 alt）。

 * 为什么要有它：图集原来是「一行一个地址」的 textarea —— 运营看不到自己填的图长什么样，
 * 也没有从媒体库挑的入口，敲错地址要等发布后才发现。单图那条路（ui/mediafield.js）早就有
 * 缩略图 + 媒体库 + 清除，多图一直缺席。
 *
 * 唯一真值来源是**两个隐藏域**（图集字段 + alt 字段）：DOM 只是这两串文本的视图，
 * 每次变更都从视图写回它们。这样后端的字段形态（每行一个）完全不用动，
 * 页面既有的表单收集逻辑一行都不用改 —— 与单图控件同一条约定。
 *
 * 上传与选择**只走媒体库**：本控件不持有任何文件输入，唯一入口是
 * WBUI.openMediaPickerMulti（媒体库弹窗的多选模式）。要加图就先在媒体库里传。
 *
 * alt 与图集**逐行对应**：写回时空 alt 也要占位（不能 filter 掉空行，否则后面所有 alt 错位）。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});

    function lines(value) {
        return String(value || '').split('\n').map(function (s) { return s.trim(); }).filter(Boolean);
    }

    function itemsOf(root) {
        var values = root.querySelector('[data-gallery-values]');
        var alts = root.querySelector('[data-gallery-alts]');
        var urls = lines(values ? values.value : '');
        var altList = lines(alts ? alts.value : '');
        return urls.map(function (url, i) { return { url: url, alt: altList[i] || '' }; });
    }

    function writeItems(root, items) {
        var values = root.querySelector('[data-gallery-values]');
        var alts = root.querySelector('[data-gallery-alts]');
        if (values) {
            values.value = items.map(function (it) { return it.url; }).join('\n');
        }
        if (alts) {
            // 不 filter：空 alt 占位，位置才对得上上一栏的那张图。
            alts.value = items.map(function (it) { return it.alt || ''; }).join('\n');
        }
    }


    // appendAddBox 在网格末尾放一个「＋」框，点击与标题行的按钮同一动作。
    function appendAddBox(root, grid) {
        var box = root.querySelector('[data-gallery-addbox]');
        if (!box) {
            box = document.createElement('button');
            box.type = 'button';
            box.className = 'media-gallery-addbox';
            box.setAttribute('data-gallery-addbox', '');
            box.textContent = '＋';
        }
        grid.appendChild(box);
    }

    function render(root) {
        var grid = root.querySelector('[data-gallery-grid]');
        var empty = root.querySelector('[data-gallery-empty]');
        if (!grid) {
            return;
        }
        var items = itemsOf(root);
        grid.innerHTML = '';
        if (empty) {
            empty.hidden = items.length > 0;
        }
        items.forEach(function (item, index) {
            var cell = document.createElement('figure');
            cell.className = 'media-gallery-cell';
            // url 记在节点上：拖拽结束后按 **DOM 顺序**回写（DOM 是隐藏域的视图，顺序以它为准）。
            cell.setAttribute('data-gallery-url', item.url);

            var handle = document.createElement('span');
            handle.className = 'media-gallery-handle';
            handle.setAttribute('data-gallery-drag', '');
            handle.setAttribute('title', '拖动排序');
            handle.setAttribute('aria-hidden', 'true');
            handle.textContent = '⠿';
            cell.appendChild(handle);

            var img = document.createElement('img');
            img.alt = item.alt || '';
            img.loading = 'lazy';
            img.src = item.url;
            cell.appendChild(img);

            var alt = document.createElement('input');
            alt.type = 'text';
            alt.className = 'form-input media-gallery-alt';
            alt.value = item.alt || '';
            alt.setAttribute('data-gallery-alt', '');
            // 输入时只回写隐藏域、**不重渲染**：重渲染会丢焦点与光标位置。
            alt.addEventListener('input', function () {
                var all = itemsOf(root);
                if (all[index]) {
                    all[index].alt = alt.value;
                    writeItems(root, all);
                }
            });
            cell.appendChild(alt);

            var row = document.createElement('div');
            row.className = 'media-gallery-cell-actions';

            // 键盘等价：拖动在只读键盘/无指针设备上没有对应操作，所以每格带左右移动。
            // 它们操作 DOM 再按 DOM 顺序回写 —— 与拖拽走同一条落盘路径。
            var left = document.createElement('button');
            left.type = 'button';
            left.className = 'btn btn-sm';
            left.textContent = '←';
            left.setAttribute('aria-label', '前移一位');
            left.addEventListener('click', function () { moveCell(cell, -1); });
            row.appendChild(left);

            var right = document.createElement('button');
            right.type = 'button';
            right.className = 'btn btn-sm';
            right.textContent = '→';
            right.setAttribute('aria-label', '后移一位');
            right.addEventListener('click', function () { moveCell(cell, 1); });
            row.appendChild(right);

            var del = document.createElement('button');
            del.type = 'button';
            del.className = 'btn btn-sm media-gallery-del';
            del.textContent = '移除';
            del.addEventListener('click', function () {
                cell.remove();
                syncFromDOM(root);
                render(root);
            });
            row.appendChild(del);
            cell.appendChild(row);

            attachDrag(root, cell);

            grid.appendChild(cell);
        });
        // 「＋」框放在**所有格子之后** —— 它必须先被清掉再重建，
        // 否则 render 之后它会停在第一格（先插入的排前面）。
        appendAddBox(root, grid);
    }


    // syncFromDOM 按当前 DOM 顺序回写两个隐藏域。
    //
    // 顺序以 **DOM 为准**而不是内存里的数组：拖拽期间我们是直接移动节点的（不重渲染，
    // 否则每次 pointermove 都要重建一遍 DOM、光标与滚动位置全丢），DOM 就是这一刻的事实。
    function syncFromDOM(root) {
        var cells = Array.prototype.slice.call(root.querySelectorAll('.media-gallery-cell'));
        var items = cells.map(function (cell) {
            var alt = cell.querySelector('[data-gallery-alt]');
            return { url: cell.getAttribute('data-gallery-url') || '', alt: alt ? alt.value : '' };
        });
        writeItems(root, items);
        var empty = root.querySelector('[data-gallery-empty]');
        if (empty) {
            empty.hidden = items.length > 0;
        }
    }

    // moveCell 把一格向前/向后挪一位（键盘等价的落点）。
    function moveCell(cell, delta) {
        var root = cell.closest ? cell.closest('[data-media-gallery]') : null;
        if (!root) {
            return;
        }
        if (delta < 0 && cell.previousElementSibling) {
            cell.parentNode.insertBefore(cell, cell.previousElementSibling);
        } else if (delta > 0 && cell.nextElementSibling) {
            cell.parentNode.insertBefore(cell.nextElementSibling, cell);
        }
        syncFromDOM(root);
    }

    // nearestCell 找指针最近的一格（用于判断往哪儿插）。
    function nearestCell(root, x, y) {
        var cells = Array.prototype.slice.call(root.querySelectorAll('.media-gallery-cell'));
        var best = null;
        var bestDist = Infinity;
        cells.forEach(function (cell) {
            var r = cell.getBoundingClientRect();
            var dx = Math.max(r.left - x, 0, x - r.right);
            var dy = Math.max(r.top - y, 0, y - r.bottom);
            var d = dx * dx + dy * dy;
            if (d < bestDist) {
                bestDist = d;
                best = cell;
            }
        });
        return best;
    }

    // attachDrag 拖拽排序（Pointer Events：鼠标 / 触屏 / 触控笔同一条路径）。
    //
    // 用 pointer* 而不是 HTML5 的 drag*：后者在触屏上不触发，而「多端可用」是硬规则。
    // 落点判断只比**左右半格**（网格是等宽列，左右就够）：上下半格会把「往右挪一格」
    // 误判成「换行到下一行」—— 那在自适应列数的网格里是常态，不是特例。
    function attachDrag(root, cell) {
        var handle = cell.querySelector('[data-gallery-drag]');
        if (!handle || !handle.setPointerCapture) {
            return;
        }
        handle.addEventListener('pointerdown', function (e) {
            // 阻止默认：否则触屏上会先触发滚动/文本选择，拖不起来。
            e.preventDefault();
            try { handle.setPointerCapture(e.pointerId); } catch (err) { /* 捕获失败不致命 */ }
            cell.classList.add('is-dragging');

            var onMove = function (ev) {
                var target = nearestCell(root, ev.clientX, ev.clientY);
                if (!target || target === cell) {
                    return;
                }
                var rect = target.getBoundingClientRect();
                if (ev.clientX < rect.left + rect.width / 2) {
                    target.parentNode.insertBefore(cell, target);
                } else {
                    target.parentNode.insertBefore(cell, target.nextElementSibling);
                }
            };
            var onUp = function () {
                handle.removeEventListener('pointermove', onMove);
                handle.removeEventListener('pointerup', onUp);
                handle.removeEventListener('pointercancel', onUp);
                cell.classList.remove('is-dragging');
                syncFromDOM(root);
            };
            handle.addEventListener('pointermove', onMove);
            handle.addEventListener('pointerup', onUp);
            handle.addEventListener('pointercancel', onUp);
        });
    }

    WBUI.register(function (scope) {
        WBUI.$$('[data-media-gallery]', scope).forEach(function (root) {
            if (!WBUI.markOnce(root, 'MediaGallery')) {
                return;
            }
            render(root);
            var addTo = function () {
                if (typeof WBUI.openMediaPickerMulti !== 'function') {
                    return; // 控件基座没加载：装配问题，不弹提示（点了没反应比乱提示好查）
                }
                WBUI.openMediaPickerMulti(function (urls) {
                    var items = itemsOf(root);
                    (urls || []).forEach(function (url) {
                        // 去重：同一张图连点两次不该在图集里出现两行。
                        var dup = items.some(function (it) { return it.url === url; });
                        if (!dup) {
                            items.push({ url: url, alt: '' });
                        }
                    });
                    writeItems(root, items);
                    render(root);
                });
            };
            // 两个入口同一动作：标题行右上角的按钮 + 网格末尾的「＋」框。
            // 事件委托到容器上 —— 「＋」框在每次 render 时被重建，逐个绑会漏。
            root.addEventListener('click', function (e) {
                var t = e.target;
                if (!t || !t.closest) { return; }
                if (t.closest('[data-gallery-add]') || t.closest('[data-gallery-addbox]')) {
                    addTo();
                }
            });
        });
    });
})(window);
