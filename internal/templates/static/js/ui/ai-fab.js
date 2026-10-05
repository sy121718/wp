/* ai-fab.js — 全局 AI 悬浮球。
 *
 * 从 htmx 换成了 fetch + ReadableStream：htmx 的模型是「换一段 HTML」，
 * 而流式要的是「往同一个节点里持续追加」—— 用 htmx 表达流式要么轮询、
 * 要么把每个增量都当成一次 swap（每次 swap 都要重建 DOM，几百个增量会让
 * 输入框与滚动位置不断重置）。
 *
 * 三条与体验直接相关的约定：
 *   1. 发出去就清空输入框（草稿留在内存里，失败时填回）。不清的话用户会
 *      以为没发出去而再点一次，于是同一个问题跑两遍。
 *   2. 面板一打开就显示「正在思考」并禁用发送 —— 跑一次工具任务可能几十秒，
 *      那段时间里界面上必须有「它在干活」的信号。
 *   3. 「停止」能真正中断：AbortController 断开 HTTP 连接，服务端从 ctx 上
 *      收到取消，上游调用随之结束（不是只把界面藏起来）。
 */
(function (global) {
    'use strict';

    function fabOf(root) { return root; }

    function panelOf(root) { return root.querySelector('[data-ai-fab-panel]'); }
    function buttonOf(root) { return root.querySelector('[data-ai-fab-toggle]'); }
    function inputOf(root) { return root.querySelector('[data-ai-fab-input]'); }
    function sendOf(root) { return root.querySelector('[data-ai-fab-send]'); }
    function stopOf(root) { return root.querySelector('[data-ai-fab-stop]'); }
    function stateOf(root) { return root.querySelector('[data-ai-fab-state]'); }
    function answerOf(root) { return root.querySelector('[data-ai-fab-answer]'); }
    function thinkOf(root) { return root.querySelector('[data-ai-fab-think]'); }
    function thinkBodyOf(root) { return root.querySelector('[data-ai-fab-think-body]'); }

    function csrfToken() {
        var meta = document.querySelector('meta[name="csrf-token"]');
        if (meta && meta.content) { return meta.content; }
        return '';
    }

    function open(root) {
        var panel = panelOf(root);
        var btn = buttonOf(root);
        if (!panel) { return; }
        panel.hidden = false;
        if (btn) { btn.setAttribute('aria-expanded', 'true'); }
        var input = inputOf(root);
        if (input) { input.focus(); }
    }

    function close(root) {
        var panel = panelOf(root);
        var btn = buttonOf(root);
        if (!panel) { return; }
        panel.hidden = true;
        if (btn) {
            btn.setAttribute('aria-expanded', 'false');
            // 焦点归还到球上：不还的话关掉面板后键盘用户会从页首重新 Tab 一遍。
            btn.focus();
        }
    }

    // reset 把面板恢复到「可以提问」的状态（上一次的残留清掉）。
    function reset(root) {
        var answer = answerOf(root);
        if (answer) { answer.textContent = ''; }
        var think = thinkOf(root);
        if (think) { think.hidden = true; }
        var thinkBody = thinkBodyOf(root);
        if (thinkBody) { thinkBody.textContent = ''; }
        setState(root, '');
    }

    function setState(root, text) {
        var state = stateOf(root);
        if (!state) { return; }
        state.textContent = text || '';
        state.hidden = !text;
    }

    function setBusy(root, busy) {
        var send = sendOf(root);
        var stop = stopOf(root);
        var input = inputOf(root);
        if (send) { send.disabled = busy; }
        if (stop) { stop.hidden = !busy; }
        if (input) { input.disabled = busy; }
    }

    // appendText 往节点里追加文本（对话内容一律走 textContent，不拼 HTML ——
    // 模型输出是不可信内容，拼进 innerHTML 就等于把它的标记渲染出来）。
    function appendText(el, text) {
        if (!el || !text) { return; }
        el.appendChild(document.createTextNode(text));
        var panel = el.closest('.ai-fab-panel');
        if (panel) { panel.scrollTop = panel.scrollHeight; }
    }

    // handleChunk 处理一条 SSE 数据。
    function handleChunk(root, chunk) {
        var answer = answerOf(root);
        switch (chunk.kind) {
        case 'reasoning':
            var think = thinkOf(root);
            var thinkBody = thinkBodyOf(root);
            if (think) { think.hidden = false; }
            appendText(thinkBody, chunk.text);
            break;
        case 'text':
            setState(root, '');
            appendText(answer, chunk.text);
            break;
        case 'tool':
            // 工具进展是状态行，不是回答内容：写进回答区会与正文混在一起，
            // 而且收尾时要清掉（它描述的是过程，不是结论）。
            setState(root, chunk.text);
            break;
        case 'error':
            setState(root, '');
            if (answer) {
                var p = document.createElement('p');
                p.className = 'ai-fab-error';
                p.setAttribute('role', 'alert');
                p.textContent = chunk.text;
                answer.appendChild(p);
            }
            break;
        case 'done':
            setState(root, '');
            // 用服务端的完整正文**校准**逐字拼出来的那份：两者不一致说明
            // 增量累积漏了或多了（本身就是缺陷信号，但用户不该因此看到半截回答）。
            if (answer && typeof chunk.answer === 'string' && chunk.answer !== '') {
                answer.textContent = chunk.answer;
            }
            if (chunk.note && answer) {
                var note = document.createElement('p');
                note.className = 'ai-fab-note';
                note.textContent = chunk.note;
                answer.appendChild(note);
            }
            break;
        }
    }

    // readStream 消费 SSE 正文。分帧规则按协议：两条换行分隔一个事件，
    // 每个事件里可能有多个 data: 行（按顺序拼接）。
    function readStream(root, reader, done) {
        var buffer = '';
        var decoder = new TextDecoder('utf-8');

        function pump() {
            return reader.read().then(function (result) {
                if (result.done) { done(); return; }
                buffer += decoder.decode(result.value, { stream: true });
                var idx;
                while ((idx = buffer.indexOf('\n\n')) >= 0) {
                    var block = buffer.slice(0, idx);
                    buffer = buffer.slice(idx + 2);
                    var payload = '';
                    block.split('\n').forEach(function (line) {
                        line = line.replace(/\r$/, '');
                        if (line.indexOf('data:') === 0) {
                            payload += line.slice(5).replace(/^ /, '');
                        }
                    });
                    if (!payload) { continue; }
                    try {
                        handleChunk(root, JSON.parse(payload));
                    } catch (e) {
                        // 坏片跳过：与协议层同一条规矩，已经显示出来的部分不该被丢掉。
                    }
                }
                return pump();
            });
        }
        return pump();
    }

    function ask(root) {
        var input = inputOf(root);
        if (!input) { return; }
        var text = input.value.trim();
        if (!text) { return; }
        var draft = text;

        reset(root);
        setState(root, root.getAttribute('data-thinking-label') || '');
        setBusy(root, true);
        // 先清空再发：见文件头第 1 条。
        input.value = '';

        var controller = new AbortController();
        root.__fabAbort = controller;

        var params = new URLSearchParams();
        params.set('input', draft);
        params.set('ctxPath', location.pathname);
        params.set('ctxQuery', location.search);
        params.set('ctxTitle', document.title);

        function finish() {
            root.__fabAbort = null;
            setBusy(root, false);
            setState(root, '');
            // 焦点回到输入框：连着问两句是常见用法，让用户还得再点一次很烦。
            input.focus();
        }

        function fail(msg) {
            var answer = answerOf(root);
            if (answer) {
                var p = document.createElement('p');
                p.className = 'ai-fab-error';
                p.setAttribute('role', 'alert');
                p.textContent = msg;
                answer.appendChild(p);
            }
            // 失败时把草稿填回去：一次网络抖动不该让用户刚写的那段话没了。
            if (!input.value) { input.value = draft; }
        }

        global.fetch('/admin/ai/ask/stream', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/x-www-form-urlencoded',
                'X-CSRF-Token': csrfToken(),
            },
            body: params.toString(),
            signal: controller.signal,
        }).then(function (resp) {
            if (!resp.ok || !resp.body) {
                throw new Error('bad status');
            }
            return readStream(root, resp.body.getReader(), finish);
        }).catch(function (err) {
            // abort 是用户主动停止，不是失败 —— 不弹错误、也不把草稿填回去。
            if (err && err.name === 'AbortError') { finish(); return; }
            fail(root.getAttribute('data-error-label') || '请求失败');
            finish();
        });
    }

    global.WBUI.register(function (scope) {
        var roots = (scope || global.document).querySelectorAll('[data-ai-fab]');
        Array.prototype.forEach.call(roots, function (root) {
            if (root.getAttribute('data-ai-fab-ready') === '1') { return; }
            root.setAttribute('data-ai-fab-ready', '1');

            var btn = buttonOf(root);
            if (btn) {
                btn.addEventListener('click', function () {
                    var panel = panelOf(root);
                    if (panel && panel.hidden) { open(root); } else { close(root); }
                });
            }
            var closer = root.querySelector('[data-ai-fab-close]');
            if (closer) {
                closer.addEventListener('click', function () { close(root); });
            }

            var send = sendOf(root);
            if (send) {
                send.addEventListener('click', function (e) {
                    e.preventDefault();
                    ask(root);
                });
            }

            var stop = stopOf(root);
            if (stop) {
                stop.addEventListener('click', function (e) {
                    e.preventDefault();
                    if (root.__fabAbort) { root.__fabAbort.abort(); }
                });
            }

            var input = inputOf(root);
            if (input) {
                input.addEventListener('keydown', function (e) {
                    // Enter 发送、Shift+Enter 换行。中文输入法组字期间要放过 ——
                    // 否则用拼音打字时每按一次选词键都会把半截问题发出去。
                    if (e.key !== 'Enter' || e.shiftKey || e.isComposing || e.keyCode === 229) { return; }
                    e.preventDefault();
                    ask(root);
                });
            }

            // Esc 关面板：与后台的抽屉 / 弹窗同一套键盘约定。
            root.addEventListener('keydown', function (e) {
                if (e.key === 'Escape') { close(root); }
            });
        });
    });
})(window);
