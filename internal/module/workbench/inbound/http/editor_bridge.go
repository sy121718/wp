package workbenchhttp

// editor_bridge.go — 编辑器桥接脚本（工作台 iframe 内注入）。
// 从 dashboard_handle.go 拆出：单一职责（编辑器桥接），与页面渲染/预览解耦。

import (
	"strings"

	workbenchenums "go_wp/internal/module/workbench/enums"
)

// editorBridgeScript 在 iframe 内运行的编辑器桥接脚本（仅编辑器预览注入）。
// 职责：节点选择标记还原、点击选中上报、选中高亮、容器/元素下方
// 「+ 插入组件」浮标、拖放落点指示线样式。
const editorBridgeScript = `<script>
(function(){
  // 编译器把节点 ID 编入 sky-c-* CSS 类；编辑器桥接层将其还原为选择标记。
  //
  // 前缀长度一律取 WB_SKY_PREFIX.length，不写死数字：这里曾是 slice(5)，
  // 而 'sky-c-' 是 6 个字符 —— 每个节点的 data-sky-id 都多出一个前导 "-"，
  // 于是画布发回父窗口的每一条消息（选中 / 直改文本 / 右键操作 / 拖放重排 /
  // 就地插入）带的都是 findNode 查不到的 id：双击能进入编辑态，失焦后
  // 回写被静默丢弃（文字弹回），点选也毫无反应。
  var WB_SKY_PREFIX = 'sky-c-';
  document.querySelectorAll('[class]').forEach(function(el){
    el.classList.forEach(function(cls){
      if (cls.indexOf(WB_SKY_PREFIX) !== 0) return;
      el.setAttribute('data-sky-id', cls.slice(WB_SKY_PREFIX.length));
    });
  });
  document.querySelectorAll('[id]').forEach(function(el){
    if (!el.getAttribute('data-sky-id')) el.setAttribute('data-sky-id', el.id);
  });
  document.querySelectorAll('[data-sky-id]').forEach(function(el){ el.setAttribute('draggable', 'true'); });

  // 结构槽位（页眉 / 页脚）在画布里是**只读边界**，不是可编辑节点。
  //
  // 它的 data-sky-id（__layout_header）在页面 AST 里并不存在 —— 槽位是编译期注入的，
  // 画布按 AST 查不到它，于是拖动 / 双击改文本 / 右键菜单对它全是「消息发出去没人认」
  // 的静默失败。这里把它单独标出来：不可拖，点击上报 wb-slot-select，
  // 由父窗口打开槽位面板（点进去编辑的是**全局块**，与 WP 的 header 模板同一范式）。
  document.querySelectorAll('[data-sky-slot], [data-sky-slot-frame]').forEach(function(el){
    el.setAttribute('draggable', 'false');
    el.classList.add('wb-slot');
  });

  // 画布内元素可直接拖动重排：与大纲树/组件库共用同一数据键。
  // 拖放落点在本桥接内计算（iframe 每次刷新必然重新注入，
  // 不依赖父窗口绑定时序），通过 wb-canvas-drop 消息交父窗口执行 AST 变更。
  var dropCtx = null;
  function clearDropMarks(){
    document.querySelectorAll('.wb-drop-before,.wb-drop-after,.wb-drop-inside').forEach(function(el){
      el.classList.remove('wb-drop-before','wb-drop-after','wb-drop-inside');
    });
  }
  document.addEventListener('dragstart', function(ev){
    // 槽位子树（页眉 / 页脚）不属于本页 AST：拖它只会得到一次无人响应 moveNode。
    if (ev.target.closest && ev.target.closest('[data-sky-slot]')) return;
    var target = ev.target.closest ? ev.target.closest('[data-sky-id]') : null;
    if(!target) return;
    ev.dataTransfer.effectAllowed = 'move';
    ev.dataTransfer.setData('application/x-wb-node', target.getAttribute('data-sky-id'));
    target.style.opacity = '0.4';
    setTimeout(function(){ target.style.opacity = ''; }, 0);
  }, true);
  document.addEventListener('dragover', function(ev){
    // 槽位子树不接收落点：往里插组件等于往「站点结构里那份块」插，而这里改的是本页文档。
    if (ev.target.closest && ev.target.closest('[data-sky-slot]')) { clearDropMarks(); dropCtx = null; return; }
    var target = ev.target.closest ? ev.target.closest('[data-sky-id]') : null;
    clearDropMarks();
    if(!target) return;
    ev.preventDefault();
    var rect = target.getBoundingClientRect();
    var offset = ev.clientY - rect.top;
    var inMiddle = offset > rect.height * .3 && offset < rect.height * .7;
    var placement = inMiddle ? 'inside' : (offset < rect.height / 2 ? 'before' : 'after');
    // 容器判定由父窗口按 AST 进行；桥接按「有子元素且中带」粗略显示内部虚线。
    target.classList.add(placement === 'inside' ? 'wb-drop-inside' : (placement === 'before' ? 'wb-drop-before' : 'wb-drop-after'));
    dropCtx = { targetID: target.getAttribute('data-sky-id'), placement: placement, inMiddle: inMiddle, hasChildren: target.children.length > 0 };
  });
  document.addEventListener('dragleave', function(ev){
    if (!ev.relatedTarget) { clearDropMarks(); dropCtx = null; }
  });
  document.addEventListener('drop', function(ev){
    ev.preventDefault();
    clearDropMarks();
    var componentType = ev.dataTransfer.getData('application/x-wb-component');
    if (componentType) {
      // 组件库拖入：DataTransfer 归父窗口所有，交父窗口 bindCanvasDrop 处理。
      return;
    }
    var nodeID = ev.dataTransfer.getData('application/x-wb-node');
    if (!nodeID) return;
    var ctx = dropCtx || {};
    dropCtx = null;
    parent.postMessage({
      type: 'wb-canvas-drop',
      nodeID: nodeID,
      targetID: ctx.targetID || '',
      placement: ctx.placement || 'after',
      inMiddle: !!ctx.inMiddle,
      hasChildren: !!ctx.hasChildren
    }, location.origin);
  });

  var style = document.createElement('style');
  style.textContent = [
    '[data-sky-id]:hover{outline:1px solid rgba(37,99,235,.45);outline-offset:-1px;cursor:pointer;}',
    '[data-sky-id].wb-selected{outline:2px solid #2563eb;outline-offset:-2px;}',
    '.wb-bridge-insert{',
    '  position:absolute;z-index:99998;left:50%;transform:translateX(-50%);',
    '  padding:4px 12px;font-size:12px;line-height:1.6;white-space:nowrap;',
    '  color:#fff;background:#2563eb;border:none;border-radius:999px;cursor:pointer;',
    '  box-shadow:0 2px 10px rgba(37,99,235,.45);',
    '}',
    '.wb-bridge-insert:hover{background:#1d4ed8;}',
    '[data-sky-id].wb-drop-before{box-shadow:0 -3px 0 0 #2563eb;}',
    '[data-sky-id].wb-drop-after{box-shadow:0 3px 0 0 #2563eb;}',
    '[data-sky-id].wb-drop-inside{outline:2px dashed #2563eb;outline-offset:-2px;}',
    // 结构槽位：紫色虚线边界，与普通组件的蓝色区分开 —— 它不是本页的节点。
    '[data-sky-slot].wb-slot{outline:1px dashed rgba(124,58,237,.5);outline-offset:-1px;}',
    '[data-sky-slot].wb-slot:hover{outline:2px dashed #7c3aed;}',
    '[data-sky-slot].wb-selected{outline:2px solid #7c3aed;outline-offset:-2px;}'
  ].join('');
  document.head.appendChild(style);

  document.addEventListener('click', function(ev){
    // 槽位优先：点页眉 / 页脚（含它们内部的内容）走的不是「选中本页节点」，
    // 而是「这段 DOM 属于站点结构」——父窗口据此打开槽位面板。
    var slotEl = ev.target.closest ? ev.target.closest('[data-sky-slot]') : null;
    if (slotEl) {
      ev.preventDefault(); ev.stopPropagation();
      parent.postMessage({
        type: 'wb-slot-select',
        slot: slotEl.getAttribute('data-sky-slot') || '',
        ref: slotEl.getAttribute('data-sky-slot-ref') || slotEl.getAttribute('data-sky-ref') || '',
        refKind: slotEl.getAttribute('data-sky-slot-ref-kind') || '',
        id: slotEl.getAttribute('data-sky-id') || ''
      }, location.origin);
      return;
    }
    var target = ev.target.closest('[data-sky-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    parent.postMessage({type:'wb-select', id: target.getAttribute('data-sky-id')}, location.origin);
  }, true);

  // 「+ 插入组件」浮标：父窗口在选中变化时发 wb-mark-selected，
  // 此处把浮标定位到选中元素底部中央；点击上报插入意图，
  // 由父窗口根据 AST 判断目标是容器(inside)还是普通元素(after)。
  var insertBtn = document.createElement('button');
  insertBtn.type = 'button';
  insertBtn.className = 'wb-bridge-insert';
  insertBtn.textContent = '+ {{bridge.insert}}';
  insertBtn.style.display = 'none';
  document.body.appendChild(insertBtn);
  insertBtn.addEventListener('click', function(ev){
    ev.preventDefault(); ev.stopPropagation();
    var id = insertBtn.getAttribute('data-target-id') || '';
    if (id) parent.postMessage({type:'wb-insert-here', id: id}, location.origin);
  });

  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var prev = document.querySelector('[data-sky-id].wb-selected');
      if (prev) prev.classList.remove('wb-selected');
      var el = ev.data.id ? document.querySelector('[data-sky-id="' + ev.data.id + '"]') : null;
      if (el) {
        el.classList.add('wb-selected');
        var rect = el.getBoundingClientRect();
        // 槽位不挂「+ 插入组件」：它内部的内容属于全局块，不归本页文档。
        if (el.hasAttribute('data-sky-slot')) {
          insertBtn.style.display = 'none';
        } else {
          insertBtn.style.display = 'block';
          insertBtn.setAttribute('data-target-id', ev.data.id);
          insertBtn.style.top = (rect.bottom + window.scrollY + 4) + 'px';
        }
      } else {
        insertBtn.style.display = 'none';
      }
    }
  });

  // ========== 画布直改三件套（对标 Figma/Elementor 就地编辑） ==========

  // 1) 双击就地编辑：文本类组件（heading/text/button/card 等）双击 →
  //    contenteditable 就地编辑 → 失焦/回车回写 AST（wb-edit-text 消息）。
  document.addEventListener('dblclick', function(ev){
    // 槽位（页眉 / 页脚）里的文本不能就地改：改的必须是**全局块**，
    // 否则页内副本会与站点结构那份分叉（就是「两个页眉」那条老路）。
    if (ev.target.closest && ev.target.closest('[data-sky-slot]')) return;
    var target = ev.target.closest('[data-sky-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    // 已在编辑中不重复进入。
    if (target.isContentEditable) return;
    target.setAttribute('contenteditable', 'plaintext-only');
    target.focus();
    // 全选文本（就地替换习惯）。
    var range = document.createRange();
    range.selectNodeContents(target);
    var sel = window.getSelection();
    sel.removeAllRanges(); sel.addRange(range);
    target.classList.add('wb-editing');
    function finish(save){
      target.removeAttribute('contenteditable');
      target.classList.remove('wb-editing');
      target.removeEventListener('blur', onBlur);
      target.removeEventListener('keydown', onKey);
      if (save) {
        parent.postMessage({
          type: 'wb-edit-text',
          id: target.getAttribute('data-sky-id'),
          text: target.textContent.trim()
        }, location.origin);
      } else {
        // 取消：下次画布刷新自动还原（不主动刷新，等下次交互）。
      }
    }
    function onBlur(){ finish(true); }
    function onKey(e){
      if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); target.blur(); }
      if (e.key === 'Escape') { e.preventDefault(); finish(false); }
    }
    target.addEventListener('blur', onBlur);
    target.addEventListener('keydown', onKey);
  });

  // 2) 画布右键菜单：编辑/复制/粘贴到内部/删除/上移/下移/隐藏 + 动效快捷项。
  var ctxMenu = null;
  function closeCtxMenu(){ if (ctxMenu) { ctxMenu.remove(); ctxMenu = null; } }
  document.addEventListener('contextmenu', function(ev){
    var slotHit = ev.target.closest ? ev.target.closest('[data-sky-slot]') : null;
    closeCtxMenu();
    if (slotHit) {
      // 槽位没有「复制 / 删除 / 上下移」这类本页操作：它只有「去改那个块」。
      ev.preventDefault(); ev.stopPropagation();
      parent.postMessage({
        type: 'wb-slot-select',
        slot: slotHit.getAttribute('data-sky-slot') || '',
        ref: slotHit.getAttribute('data-sky-slot-ref') || slotHit.getAttribute('data-sky-ref') || '',
        refKind: slotHit.getAttribute('data-sky-slot-ref-kind') || '',
        id: slotHit.getAttribute('data-sky-id') || ''
      }, location.origin);
      return;
    }
    var target = ev.target.closest('[data-sky-id]');
    if(!target) return; // 画布空白处不拦截（浏览器原生菜单）。
    ev.preventDefault(); ev.stopPropagation();
    var id = target.getAttribute('data-sky-id');
    parent.postMessage({type:'wb-select', id: id}, location.origin);
    ctxMenu = document.createElement('div');
    ctxMenu.className = 'wb-ctx-menu';
    function item(label, action){
      var b = document.createElement('button');
      b.type = 'button'; b.textContent = label;
      b.addEventListener('click', function(e){ e.stopPropagation(); closeCtxMenu(); action(); });
      ctxMenu.appendChild(b);
    }
    function separator(){ var s = document.createElement('div'); s.className='wb-ctx-sep'; ctxMenu.appendChild(s); }
    function send(msg){ parent.postMessage(msg, location.origin); }
    item('✏️ {{bridge.editText}}', function(){ // 触发双击编辑。
      var el = document.querySelector('[data-sky-id="' + id + '"]');
      if (el) { var d = new MouseEvent('dblclick', {bubbles:true}); el.dispatchEvent(d); }
    });
    item('⧉ {{bridge.copy}}', function(){ send({type:'wb-ctx', id:id, op:'copy'}); });
    item('✂ {{bridge.cut}}', function(){ send({type:'wb-ctx', id:id, op:'cut'}); });
    item('📋 {{bridge.pasteInside}}', function(){ send({type:'wb-ctx', id:id, op:'paste-inside'}); });
    separator();
    item('↑ {{bridge.moveUp}}', function(){ send({type:'wb-ctx', id:id, op:'move-up'}); });
    item('↓ {{bridge.moveDown}}', function(){ send({type:'wb-ctx', id:id, op:'move-down'}); });
    separator();
    // 动效快捷子项（效果基本库入口：常用 4 种入场 + 悬浮）。
    var anim = document.createElement('div'); anim.className='wb-ctx-group'; anim.textContent='✨ {{bridge.entranceGroup}}';
    ctxMenu.appendChild(anim);
    ['fade-up','zoom-in','slide-up','blur-in'].forEach(function(eff){
      item('　' + eff, function(){ send({type:'wb-ctx', id:id, op:'entrance', value:eff}); });
    });
    item('🌀 {{bridge.hoverLift}}', function(){ send({type:'wb-ctx', id:id, op:'hover', value:'lift'}); });
    separator();
    item('🗑 {{bridge.delete}}', function(){ send({type:'wb-ctx', id:id, op:'delete'}); });
    document.body.appendChild(ctxMenu);
    // 定位（不越界）。
    var x = Math.min(ev.pageX, window.innerWidth - 180);
    var y = Math.min(ev.pageY, window.innerHeight - 320);
    ctxMenu.style.left = x + 'px'; ctxMenu.style.top = y + 'px';
  });
  document.addEventListener('click', function(ev){
    if (ctxMenu && !ctxMenu.contains(ev.target)) closeCtxMenu();
  }, true);

  // 3) 选中悬浮快捷条（Elementor 式小工具条：编辑/复制/删除）。
  var quickBar = document.createElement('div');
  quickBar.className = 'wb-quick-bar';
  quickBar.style.display = 'none';
  document.body.appendChild(quickBar);
  function positionQuickBar(el){
    var rect = el.getBoundingClientRect();
    quickBar.style.display = 'flex';
    quickBar.style.left = rect.left + 'px';
    quickBar.style.top = (rect.top - 30 + window.scrollY) + 'px';
    quickBar.setAttribute('data-target-id', el.getAttribute('data-sky-id'));
  }
  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var el = ev.data.id ? document.querySelector('[data-sky-id="' + ev.data.id + '"]') : null;
      // 槽位的快捷条没有意义（没有「复制本页副本 / 删除」这类操作），不显示。
      if (el && !el.hasAttribute('data-sky-slot')) positionQuickBar(el); else quickBar.style.display = 'none';
    }
  });
  [['✏️','{{bridge.editText}}',function(){ var el=document.querySelector('[data-sky-id="'+quickBar.getAttribute('data-target-id')+'"]'); if(el) el.dispatchEvent(new MouseEvent('dblclick',{bubbles:true})); }],
   ['⧉','{{bridge.copy}}',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'copy'}); }],
   ['🗑','{{bridge.delete}}',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'delete'}); }]
  ].forEach(function(t){
    var b = document.createElement('button');
    b.type='button'; b.textContent=t[0]; b.title=t[1];
    b.addEventListener('click', function(e){ e.stopPropagation(); t[2](); });
    quickBar.appendChild(b);
  });
  function send2(msg){ parent.postMessage(msg, location.origin); }

  // 直改样式（右键菜单/快捷条/编辑态）。
  var directStyle = document.createElement('style');
  directStyle.textContent = [
    '[data-sky-id].wb-editing{outline:2px solid #3d444f !important;cursor:text;}',
    '[contenteditable]{outline-offset:-2px;}',
    '.wb-ctx-menu{position:absolute;z-index:99999;min-width:160px;background:#fff;',
    '  border:1px solid #e5e7eb;border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.14);',
    '  padding:4px;font-size:13px;color:#1a1d21;}',
    '.wb-ctx-menu button{display:block;width:100%;text-align:left;padding:6px 10px;',
    '  border:none;background:none;cursor:pointer;border-radius:6px;font-size:13px;color:inherit;}',
    '.wb-ctx-menu button:hover{background:#eceef1;}',
    '.wb-ctx-sep{height:1px;background:#e5e7eb;margin:4px 0;}',
    '.wb-ctx-group{padding:6px 10px 2px;font-size:11px;color:#6b7280;font-weight:600;}',
    '.wb-quick-bar{position:absolute;z-index:99998;display:none;gap:2px;',
    '  background:#1a1d21;border-radius:6px;padding:3px;box-shadow:0 4px 12px rgba(0,0,0,.25);}',
    '.wb-quick-bar button{border:none;background:none;cursor:pointer;font-size:13px;',
    '  padding:4px 8px;border-radius:4px;color:#fff;}',
    '.wb-quick-bar button:hover{background:rgba(255,255,255,.15);}'
  ].join('');
  document.head.appendChild(directStyle);

  // 局部刷新（父窗口 wb-patch）：只替换目标节点的 DOM 与整页样式，不重载 iframe。
  window.addEventListener('message', function (ev) {
    if (ev.origin !== location.origin || !ev.data || ev.data.type !== 'wb-patch') return;
    var el = document.querySelector('[data-sky-id="' + ev.data.id + '"]');
    if (el && ev.data.html) {
      var tmp = document.createElement('div');
      tmp.innerHTML = ev.data.html;
      var fresh = tmp.firstElementChild;
      if (fresh) {
        fresh.setAttribute('data-sky-id', ev.data.id);
        fresh.setAttribute('draggable', 'true');
        if (el.classList.contains('wb-selected')) fresh.classList.add('wb-selected');
        el.replaceWith(fresh);
      }
    }
    if (typeof ev.data.css === 'string') {
      var st = document.getElementById('wb-live-css');
      if (!st) { st = document.createElement('style'); st.id = 'wb-live-css'; document.head.appendChild(st); }
      st.textContent = ev.data.css;
    }
  });

  // 拖放落点指示：父窗口 bindCanvasDrop 在 dragover 时给目标加类，
  // 这里只负责样式；drop/dragleave 时父窗口负责移除。
})();
</script>`

// editorBridgeTexts 桥接脚本里的可见文案：占位符名 → 词条 key + 中文兜底。
//
// 脚本以 `{{bridge.<name>}}` 书写、注入时替换，而不是把中文直接写在脚本里 ——
// 这些串会由 iframe 内的 JS 输出（浮标文字、右键菜单项、快捷条 title），
// 硬编码中文在英文画布上就是漏译。占位符形态还让「脚本里的占位符集合 =
// 本表的名字集合」成为可机器校验的判据（见 editor_bridge_test.go），
// 漏登记一个的表现是按钮上原样显示花括号，而这类缺陷不会让任何断言变红。
var editorBridgeTexts = []struct{ Name, Key, Fallback string }{
	{"bridge.insert", workbenchenums.BridgeInsert, "插入组件"},
	{"bridge.editText", workbenchenums.BridgeEditText, "编辑文本"},
	{"bridge.copy", workbenchenums.BridgeCopy, "复制"},
	{"bridge.cut", workbenchenums.BridgeCut, "剪切"},
	{"bridge.pasteInside", workbenchenums.BridgePasteInside, "粘贴到内部"},
	{"bridge.moveUp", workbenchenums.BridgeMoveUp, "上移"},
	{"bridge.moveDown", workbenchenums.BridgeMoveDown, "下移"},
	{"bridge.delete", workbenchenums.BridgeDelete, "删除"},
	{"bridge.entranceGroup", workbenchenums.BridgeEntranceGroup, "入场动画"},
	{"bridge.hoverLift", workbenchenums.BridgeHoverLift, "悬浮上浮"},
}

// editorBridgeScriptFor 按当前语言产出桥接脚本：占位符换成译文后返回完整 <script>。
func editorBridgeScriptFor(tr func(key, fallback string) string) string {
	pairs := make([]string, 0, len(editorBridgeTexts)*2)
	for _, it := range editorBridgeTexts {
		pairs = append(pairs, "{{"+it.Name+"}}", jsSingleQuoted(tr(it.Key, it.Fallback)))
	}
	return strings.NewReplacer(pairs...).Replace(editorBridgeScript)
}

// jsSingleQuoted 把译文转义成能放进单引号 JS 字符串字面量的形态。
//
// 词条是运营可改的数据，不是编译期常量：一个撇号（如 "Don't"）就会当场把脚本打断，
// 而断掉的后果是**画布里的桥接整体失效**（选中、拖放、右键全部无反应）——
// 报错在 iframe 控制台，父窗口看起来只是「点了没反应」。
func jsSingleQuoted(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\r", `\r`,
		"\n", `\n`,
		"</", `<\/`,
	).Replace(s)
}

// injectEditorBridge 把编辑器桥接脚本（按请求语言取词后）追加到 </body> 前。
func injectEditorBridge(html string, tr func(key, fallback string) string) string {
	idx := strings.LastIndex(html, "</body>")
	if idx < 0 {
		return html
	}
	return html[:idx] + editorBridgeScriptFor(tr) + html[idx:]
}
