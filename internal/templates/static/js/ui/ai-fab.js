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
    function attachOf(root) { return root.querySelector('[data-ai-fab-attach]'); }
    function pickOf(root) { return root.querySelector('[data-ai-fab-pick]'); }
    function fileOf(root) { return root.querySelector('[data-ai-fab-file]'); }

    // fabImageFallbackLabel 与 ai_fab_image.go 里的同名常量对应：没有文件名时
    // 界面上画什么。两边都写死「图片」而不是空串 —— 空串会让标识行变成
    // 「📎 」这样一句没头没尾的话，看起来像渲染坏了。
    var fabImageFallbackLabel = '图片';

    // imagesOf 当前待发送的图片（{label, uri}），存在 root 上而不是闭包里：
    // 一个页面上可能有两个悬浮球宿主（概览页那个常驻输入框 + 右下角球），
    // 闭包变量会让它们互相串图。
    function imagesOf(root) { return root.__fabImages || []; }

    function clearImages(root) {
        root.__fabImages = [];
        renderAttach(root);
    }

    // addImageFiles 把选中的 / 粘贴的文件读成 data URI 并挂到待发送列表上。
    //
    // 为什么在前端读 base64 而不是上传到媒体库再传地址：上游模型在另一台机器上，
    // 站内相对地址它取不到；而让服务端去「按地址取图」等于凭空开一个 SSRF 面
    //（用户可控的 URL 能指到内网）。图片只给模型看，一次性的像素没必要落库。
    function addImageFiles(root, files) {
        if (!files || !files.length) { return; }
        var list = imagesOf(root).slice();
        var pending = files.length;
        Array.prototype.forEach.call(files, function (file) {
            if (!file || file.type.indexOf('image/') !== 0) {
                pending -= 1;
                return;
            }
            var reader = new FileReader();
            reader.onload = function () {
                list.push({ label: file.name || fabImageFallbackLabel, uri: String(reader.result || '') });
                pending -= 1;
                if (pending <= 0) {
                    root.__fabImages = list;
                    renderAttach(root);
                }
            };
            // 读失败不进列表：宁可少一张也不要塞一个空 uri 进去（那会被服务端
            // 整批拒掉，用户看到的是「图片格式不认识」而图明明是他刚选的）。
            reader.onerror = function () {
                pending -= 1;
                if (pending <= 0) {
                    root.__fabImages = list;
                    renderAttach(root);
                }
            };
            reader.readAsDataURL(file);
        });
    }

    // renderAttach 画待发送图片的缩略图条。没有图时整块 hidden（不占高度）。
    function renderAttach(root) {
        var box = attachOf(root);
        if (!box) { return; }
        var list = imagesOf(root);
        box.textContent = '';
        if (!list.length) {
            box.hidden = true;
            return;
        }
        list.forEach(function (item, idx) {
            var chip = document.createElement('span');
            chip.className = 'ai-fab-chip';

            var thumb = document.createElement('img');
            thumb.className = 'ai-fab-chip-thumb';
            thumb.src = item.uri;
            thumb.alt = item.label;
            chip.appendChild(thumb);

            var name = document.createElement('span');
            name.className = 'ai-fab-chip-name';
            name.textContent = item.label;
            chip.appendChild(name);

            var del = document.createElement('button');
            del.type = 'button';
            del.className = 'ai-fab-chip-del';
            del.setAttribute('aria-label', root.getAttribute('data-image-remove-label') || '');
            del.title = del.getAttribute('aria-label');
            del.textContent = '✕';
            del.addEventListener('click', function () {
                var next = imagesOf(root).slice();
                next.splice(idx, 1);
                root.__fabImages = next;
                renderAttach(root);
            });
            chip.appendChild(del);

            box.appendChild(chip);
        });
        box.hidden = false;
    }

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

    // turnsOf 历史轮次的容器（有它才是「一段对话」而不是「一次问答」）。
    function turnsOf(root) { return root.querySelector('[data-ai-fab-turns]'); }

    // beginTurn 开一轮：把用户的提问插进去，返回这一轮要写正文的三个节点。
    //
    // 发问前**不清空**面板：清空之后界面上永远只剩最新那一问一答，而服务端那边
    // 会话一直是连着的（会话键固定、历史逐轮进请求），模型记得上一轮、界面却看不出来
    // —— 「它像个搜索引擎」这个印象就是这么来的。
    //
    // 结构化写法（turns 容器）与旧写法（面板里单个 [data-ai-fab-answer]）都支持：
    // htmx 那条非流式路径渲染的是 partials/ai_fab_result.html，它没有 turns 容器。
    function beginTurn(root, userText, imageLabels) {
        var turns = turnsOf(root);
        if (!turns) {
            var plain = { answer: answerOf(root), think: thinkOf(root), thinkBody: thinkBodyOf(root) };
            if (plain.answer) { plain.answer.textContent = ''; }
            if (plain.think) { plain.think.hidden = true; }
            if (plain.thinkBody) { plain.thinkBody.textContent = ''; }
            setState(root, '');
            return plain;
        }

        var turn = document.createElement('div');
        turn.className = 'ai-turn';

        var q = document.createElement('div');
        q.className = 'ai-turn-user';
        q.textContent = userText;
        turn.appendChild(q);

        // 图片标识：这一轮带了哪几张。**只画标识不画像素** —— 历史回填时像素
        // 早已不在（图片是前端一次性读的，没落库），而用户要看的是「我当时发了图」，
        // 不是那张图本身。本轮刚发出去的图同理：发完就把缩略图条清掉了，
        // 留一行标识才不会让提问看起来什么附件都没带。
        if (imageLabels && imageLabels.length) {
            turn.appendChild(imageRow(imageLabels));
        }

        // 思考过程每轮一个折叠块：几轮之后只看最后一轮的思考等于丢掉「它为什么
        // 选这个工具」——那部分恰恰在前面几轮里。
        var think = document.createElement('details');
        think.className = 'ai-fab-think';
        think.hidden = true;
        var summary = document.createElement('summary');
        summary.textContent = root.getAttribute('data-think-label') || '';
        think.appendChild(summary);
        var thinkBody = document.createElement('div');
        thinkBody.className = 'ai-fab-think-body';
        think.appendChild(thinkBody);
        turn.appendChild(think);

        var answer = document.createElement('div');
        answer.className = 'ai-turn-answer';
        turn.appendChild(answer);

        turns.appendChild(turn);
        setState(root, '');
        return { answer: answer, think: think, thinkBody: thinkBody };
    }

    // appendTurn 往面板里补一轮完整对话（用于历史回填：历史是「一条提问、
    // 一条回答」两条记录，界面上要合回一轮）。
    function appendTurn(root, userText, answerText, reasoning, imageLabels) {
        var cur = beginTurn(root, userText, imageLabels);
        if (reasoning) {
            if (cur.think) { cur.think.hidden = false; }
            appendText(cur.thinkBody, reasoning);
        }
        if (answerText) { appendText(cur.answer, answerText); }
        return cur;
    }

    // loadHistory 把这条会话已有的对话铺回面板。
    //
    // 服务端那边会话一直是连着的（会话键固定、历史逐轮进请求），只有界面每次
    // 从空白开始 —— 于是「它记得上一轮」这件事用户看不见，只觉得是个搜索框。
    // 历史为空时什么都不做（首访不留痕迹）。
    function loadHistory(root) {
        var turns = turnsOf(root);
        if (!turns || root.getAttribute('data-ai-fab-history') === 'off') { return; }
        var url = root.getAttribute('data-ai-fab-history-url') || '/admin/ai/fab/history';
        global.fetch(url, { headers: { 'Accept': 'application/json' } })
            .then(function (resp) { return resp.ok ? resp.json() : null; })
            .then(function (body) {
                var items = body && body.data && body.data.items;
                if (!items || !items.length) { return; }
                // 一条提问后面可能没有回答（上一轮中断了）：那就只画提问，
                // 不要凭空造一个空回答块。
                var pending = null;
                items.forEach(function (it) {
                    if (it.role === 'user') {
                        pending = it.text;
                        return;
                    }
                    if (pending === null) { pending = ''; }
                    appendTurn(root, pending, it.text, it.reasoning || '', it.imageLabels || null);
                    pending = null;
                });
                if (pending !== null) { beginTurn(root, pending, null); }
                var scroller = root.querySelector('[data-ai-fab-body]');
                if (scroller) { scroller.scrollTop = scroller.scrollHeight; }
            })
            .catch(function () {
                // 历史读不到不是错误：面板照常可用，用户照样能提问。
            });
    }

    // imageRow 一轮提问下的图片标识行。
    function imageRow(labels) {
        var row = document.createElement('div');
        row.className = 'ai-turn-images';
        labels.forEach(function (label) {
            var chip = document.createElement('span');
            chip.className = 'ai-fab-chip ai-fab-chip-plain';

            var icon = document.createElement('span');
            icon.className = 'ai-fab-chip-icon';
            icon.setAttribute('aria-hidden', 'true');
            icon.textContent = '🖼';
            chip.appendChild(icon);

            var name = document.createElement('span');
            name.className = 'ai-fab-chip-name';
            name.textContent = label;
            chip.appendChild(name);

            row.appendChild(chip);
        });
        return row;
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
        // 滚到最新：两个宿主（悬浮球的 .ai-fab-panel、概览页的 .dash-ai .ai-fab-body）
        // 的可滚容器不同，按最近的祖先找 —— 不滚的话长回答一超出就得手动往下拖。
        var scroller = el.closest('[data-ai-fab-body], .ai-fab-panel');
        if (scroller) { scroller.scrollTop = scroller.scrollHeight; }
    }

    // handleChunk 处理一条 SSE 数据（cur 是 beginTurn 返回的当前轮节点）。
    function handleChunk(root, chunk, cur) {
        var answer = cur.answer;
        switch (chunk.kind) {
        case 'reasoning':
            if (cur.think) { cur.think.hidden = false; }
            appendText(cur.thinkBody, chunk.text);
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
    function readStream(root, reader, done, cur) {
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
                        handleChunk(root, JSON.parse(payload), cur);
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

        // 图片在**发出去之前**先取快照，且必须取在 beginTurn 之前：
        // 气泡上的标识行由 beginTurn 画，而 finish() 里会 clearImages 把列表清掉。
        // 取晚了（或忘了提升）的表现是——图发得出去、模型也答对了，只有气泡下
        // 少一行标识，看起来像「这条消息本来就没带图」。
        var pendingImages = imagesOf(root);
        var imageLabels = pendingImages.map(function (it) { return it.label; });

        var cur = beginTurn(root, draft, imageLabels);
        // 带 data-ai-fab-dock 的实例（概览页那只会常驻的提问框）：一开始提问就把它
        // 沉到页面底部（.is-asking），回答长在输入框上方。不沉的话它留在页面中段，
        // 一边出回答一边把下面的卡片往下挤。
        if (root.hasAttribute('data-ai-fab-dock')) { root.classList.add('is-asking'); }
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
        if (pendingImages.length) {
            // form 里传数组只能是 JSON 字符串（URLSearchParams 对数组会摊成重复键，
            // 而服务端读的是单个字段）。
            params.set('images', JSON.stringify(pendingImages.map(function (it) { return it.uri; })));
            params.set('imageLabels', JSON.stringify(imageLabels));
        }

        function finish() {
            root.__fabAbort = null;
            setBusy(root, false);
            setState(root, '');
            // 发出去就把待发送列表清掉：不清的话下一句提问会把同一批图再发一遍，
            // 而用户以为自己只附了一次。图本身的标识已经画在那一轮的气泡上了。
            clearImages(root);
            // 焦点回到输入框：连着问两句是常见用法，让用户还得再点一次很烦。
            input.focus();
        }

        function fail(msg) {
            var answer = cur.answer;
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
            return readStream(root, resp.body.getReader(), finish, cur);
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

            // 铺历史：会话在服务端一直是连着的，界面每次却从空白开始 ——
            // 把已有的对话放回来，用户才知道它记得上一轮（见 loadHistory）。
            loadHistory(root);

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

            var pick = pickOf(root);
            var file = fileOf(root);
            if (pick && file) {
                pick.addEventListener('click', function (e) {
                    e.preventDefault();
                    file.click();
                });
                file.addEventListener('change', function () {
                    addImageFiles(root, file.files);
                    // 清空 value：不然连选同一个文件两次时第二次不触发 change
                    //（值没变），表现是「再选一次没反应」。
                    file.value = '';
                });
            }

            var input = inputOf(root);
            if (input) {
                // 粘贴图片：与选文件等价的一条路，且是用户最自然的动作。
                // 剪贴板里同时有文字与图片时**不拦默认行为** —— 文字照常进输入框，
                // 两种内容一起收到才是用户预期的结果。
                input.addEventListener('paste', function (e) {
                    var dt = e.clipboardData;
                    if (!dt || !dt.files || !dt.files.length) { return; }
                    var picked = [];
                    Array.prototype.forEach.call(dt.files, function (f) {
                        if (f && f.type && f.type.indexOf('image/') === 0) { picked.push(f); }
                    });
                    if (picked.length) { addImageFiles(root, picked); }
                });
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
