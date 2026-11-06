// 媒体 URL 只从可信媒体基址补全；外部 HTTP(S) 地址保留原域名。
export function normalizeMediaURL(value, baseURL) {
    var raw = String(value == null ? '' : value).trim();
    if (!raw) return '';
    if (/[\\\u0000-\u0020\u007f]/.test(raw)) return '';
    try {
        if (raw.startsWith('//')) raw = 'https:' + raw;
        if (/^[a-z][a-z0-9+.-]*:/i.test(raw)) {
            var external = new URL(raw);
            return /^(https?:)$/.test(external.protocol) && external.hostname && !external.username && !external.password ? external.href : '';
        }
        var base = new URL(baseURL);
        if (!/^(https?:)$/.test(base.protocol) || !base.hostname || base.username || base.password) return '';
        var relative = raw.replace(/^\/?storage\//, '');
        if (relative.startsWith('/') || relative.split(/[/?#]/).some(function (part) {
            try { return decodeURIComponent(part) === '..' || decodeURIComponent(part) === '.'; } catch (e) { return true; }
        })) return '';
        return new URL(relative, base.href.replace(/\/+$/, '') + '/').href;
    } catch (e) { return ''; }
}

export function mediaPreviewKind(value, path) {
    var raw = String(value || '').split(/[?#]/)[0].toLowerCase();
    if (/captionsSrc$/i.test(path || '') || /\.(vtt|srt|pdf|docx?|xlsx?|pptx?|txt|csv|zip)$/.test(raw)) return 'file';
    if (/\.(mp4|webm|mov|m4v|mkv|avi)$/.test(raw)) return 'video';
    if (/\.(mp3|wav|ogg|m4a|flac|aac)$/.test(raw)) return 'audio';
    return 'image';
}
