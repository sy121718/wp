// 检查器媒体字段共用视图；数据写入由调用方提交。
import { mediaPreviewKind } from '../../media-url.js';
import { normalizeAssetURL } from '../../core.js';

// mediaControl 用固定缩略图与文件信息，保留完整地址和键盘可用的媒体选择。
export function createMediaField(ctx, label, path, val, applyValue) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-media';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var current = normalizeAssetURL(val);
    val = val == null ? '' : String(val);
    var header = document.createElement('div'); header.className = 'wb-media-header';
    var box = document.createElement('button'); box.type = 'button'; box.className = 'wb-media-field';
    box.setAttribute('aria-label', '从媒体库选择：' + label);
    var img = document.createElement('img'); img.alt = '';
    var tip = document.createElement('span'); tip.className = 'wb-media-tip';
    box.appendChild(img); box.appendChild(tip);
    var info = document.createElement('div'); info.className = 'wb-media-info';
    var name = document.createElement('span'); name.className = 'wb-media-name';
    var row = document.createElement('div'); row.className = 'wb-media-row';
    var pick = document.createElement('button'); pick.type='button'; pick.className='wb-btn wb-btn-secondary wb-btn-sm'; pick.textContent='媒体库';
    var clear = document.createElement('button'); clear.type='button'; clear.className='wb-btn wb-btn-ghost wb-btn-sm'; clear.textContent='清除';
    row.appendChild(pick); row.appendChild(clear); info.appendChild(name); info.appendChild(row);
    header.appendChild(box); header.appendChild(info); wrap.appendChild(header);
    var ext = document.createElement('input'); ext.type='url'; ext.placeholder='完整媒体地址（https://…）';
    ext.setAttribute('aria-label', label + '完整地址'); ext.spellcheck = false;
    ext.value = current || val;
    img.addEventListener('error', function () { img.classList.add('is-empty'); box.classList.remove('has-image'); tip.textContent = '预览不可用'; });
    function preview(url) {
        var kind = mediaPreviewKind(url, path);
        img.removeAttribute('src'); img.classList.add('is-empty');
        box.classList.toggle('has-image', !!url && kind === 'image');
        if (url && kind === 'image') { img.src = url; img.classList.remove('is-empty'); }
        tip.textContent = url ? ({image:'',video:'视频',audio:'音频',file:'文件'}[kind]) : '选择';
        var filename = '';
        try { filename = decodeURIComponent(new URL(url).pathname.split('/').pop()); } catch (e) { /* 未选资源 */ }
        name.textContent = filename || '尚未选择媒体'; name.title = url || '';
        clear.disabled = !url;
    }
    function apply(value) {
        var url = value ? normalizeAssetURL(value) : '';
        if (value && !url) {
            ext.setCustomValidity('请填写有效的完整 HTTP(S) 媒体地址'); ext.reportValidity(); return;
        }
        ext.setCustomValidity('');
        applyValue(url); ext.value = url; current = url; preview(url);
    }
    function choose() { ctx.self.openMediaPicker(function (url) { apply(url); }); }
    box.addEventListener('click', choose); pick.addEventListener('click', choose);
    clear.addEventListener('click', function () { apply(''); });
    ext.addEventListener('input', function () { ext.setCustomValidity(''); });
    ext.addEventListener('change', function () { apply(ext.value.trim()); });
    wrap.appendChild(ext); preview(current); ctx.panel.appendChild(wrap);
}
