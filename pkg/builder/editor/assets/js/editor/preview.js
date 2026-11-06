// 模拟状态只活在编辑器内存与预览query，保存/发布载荷不携带它。
export function previewRequestURL(path, state) {
    var url = new URL(path, window.location.href);
    if (state && state !== 'default') url.searchParams.set('previewState', state);
    else url.searchParams.delete('previewState');
    return url.pathname + url.search;
}

export const previewMethods = {
    syncPreviewControls() {
        var frame = document.getElementById('wb-canvas');
        var wrapper = document.getElementById('wb-preview-states');
        var select = document.getElementById('wb-preview-state');
        if (!frame || !wrapper || !select) return;
        var helper = frame.contentWindow && frame.contentWindow.WPSitePreview;
        var data = frame.contentWindow && frame.contentWindow.WPSiteData;
        var states = helper && helper.active && data && data.supportedPreviewStates ? data.supportedPreviewStates() : [];
        this._previewStates = states;
        wrapper.hidden = states.length < 2;
        Array.from(select.options).forEach(function (option) { option.hidden = states.indexOf(option.value) === -1; option.disabled = option.hidden; });
        select.value = this.previewState || 'default';
        var link = document.getElementById('wb-preview-link');
        if (link) link.setAttribute('href', previewRequestURL(link.getAttribute('href'), this.previewState));
    },
    setPreviewState(state) {
        if ((this._previewStates || []).indexOf(state) === -1 || state === this.previewState) return;
        this.previewState = state;
        this.flushCanvas();
    },
    initPreviewControls() {
        var self = this;
        var select = document.getElementById('wb-preview-state');
        if (select) select.addEventListener('change', function () { self.setPreviewState(select.value); });
        self.syncPreviewControls();
    }
};
