package dashboardhttp

// editor_bridge.go — 编辑器桥接脚本（工作台 iframe 内注入）。
// 从 dashboard_handle.go 拆出：单一职责（编辑器桥接），与页面渲染/预览解耦。

import "strings"

// editorBridgeScript 在 iframe 内运行的编辑器桥接脚本（仅编辑器预览注入）。
// 职责：节点选择标记还原、点击选中上报、选中高亮、容器/元素下方
// 「+ 插入组件」浮标、拖放落点指示线样式。
const editorBridgeScript = `<script>
(function(){
  // 编译器把节点 ID 编入 wp-c-* CSS 类；编辑器桥接层将其还原为选择标记。
  document.querySelectorAll('[class]').forEach(function(el){
    el.classList.forEach(function(cls){
      if (cls.indexOf('wp-c-') !== 0) return;
      el.setAttribute('data-wp-id', cls.slice(5));
    });
  });
  document.querySelectorAll('[id]').forEach(function(el){
    if (!el.getAttribute('data-wp-id')) el.setAttribute('data-wp-id', el.id);
  });
  document.querySelectorAll('[data-wp-id]').forEach(function(el){ el.setAttribute('draggable', 'true'); });

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
    var target = ev.target.closest ? ev.target.closest('[data-wp-id]') : null;
    if(!target) return;
    ev.dataTransfer.effectAllowed = 'move';
    ev.dataTransfer.setData('application/x-wb-node', target.getAttribute('data-wp-id'));
    target.style.opacity = '0.4';
    setTimeout(function(){ target.style.opacity = ''; }, 0);
  }, true);
  document.addEventListener('dragover', function(ev){
    var target = ev.target.closest ? ev.target.closest('[data-wp-id]') : null;
    clearDropMarks();
    if(!target) return;
    ev.preventDefault();
    var rect = target.getBoundingClientRect();
    var offset = ev.clientY - rect.top;
    var inMiddle = offset > rect.height * .3 && offset < rect.height * .7;
    var placement = inMiddle ? 'inside' : (offset < rect.height / 2 ? 'before' : 'after');
    // 容器判定由父窗口按 AST 进行；桥接按「有子元素且中带」粗略显示内部虚线。
    target.classList.add(placement === 'inside' ? 'wb-drop-inside' : (placement === 'before' ? 'wb-drop-before' : 'wb-drop-after'));
    dropCtx = { targetID: target.getAttribute('data-wp-id'), placement: placement, inMiddle: inMiddle, hasChildren: target.children.length > 0 };
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
    '[data-wp-id]:hover{outline:1px solid rgba(37,99,235,.45);outline-offset:-1px;cursor:pointer;}',
    '[data-wp-id].wb-selected{outline:2px solid #2563eb;outline-offset:-2px;}',
    '.wb-bridge-insert{',
    '  position:absolute;z-index:99998;left:50%;transform:translateX(-50%);',
    '  padding:4px 12px;font-size:12px;line-height:1.6;white-space:nowrap;',
    '  color:#fff;background:#2563eb;border:none;border-radius:999px;cursor:pointer;',
    '  box-shadow:0 2px 10px rgba(37,99,235,.45);',
    '}',
    '.wb-bridge-insert:hover{background:#1d4ed8;}',
    '[data-wp-id].wb-drop-before{box-shadow:0 -3px 0 0 #2563eb;}',
    '[data-wp-id].wb-drop-after{box-shadow:0 3px 0 0 #2563eb;}',
    '[data-wp-id].wb-drop-inside{outline:2px dashed #2563eb;outline-offset:-2px;}'
  ].join('');
  document.head.appendChild(style);

  document.addEventListener('click', function(ev){
    var target = ev.target.closest('[data-wp-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    parent.postMessage({type:'wb-select', id: target.getAttribute('data-wp-id')}, location.origin);
  }, true);

  // 「+ 插入组件」浮标：父窗口在选中变化时发 wb-mark-selected，
  // 此处把浮标定位到选中元素底部中央；点击上报插入意图，
  // 由父窗口根据 AST 判断目标是容器(inside)还是普通元素(after)。
  var insertBtn = document.createElement('button');
  insertBtn.type = 'button';
  insertBtn.className = 'wb-bridge-insert';
  insertBtn.textContent = '+ 插入组件';
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
      var prev = document.querySelector('[data-wp-id].wb-selected');
      if (prev) prev.classList.remove('wb-selected');
      var el = ev.data.id ? document.querySelector('[data-wp-id="' + ev.data.id + '"]') : null;
      if (el) {
        el.classList.add('wb-selected');
        var rect = el.getBoundingClientRect();
        insertBtn.style.display = 'block';
        insertBtn.setAttribute('data-target-id', ev.data.id);
        insertBtn.style.top = (rect.bottom + window.scrollY + 4) + 'px';
      } else {
        insertBtn.style.display = 'none';
      }
    }
  });

  // ========== 画布直改三件套（对标 Figma/Elementor 就地编辑） ==========

  // 1) 双击就地编辑：文本类组件（heading/text/button/card 等）双击 →
  //    contenteditable 就地编辑 → 失焦/回车回写 AST（wb-edit-text 消息）。
  document.addEventListener('dblclick', function(ev){
    var target = ev.target.closest('[data-wp-id]');
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
          id: target.getAttribute('data-wp-id'),
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
    var target = ev.target.closest('[data-wp-id]');
    closeCtxMenu();
    if(!target) return; // 画布空白处不拦截（浏览器原生菜单）。
    ev.preventDefault(); ev.stopPropagation();
    var id = target.getAttribute('data-wp-id');
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
    item('✏️ 编辑文本', function(){ // 触发双击编辑。
      var el = document.querySelector('[data-wp-id="' + id + '"]');
      if (el) { var d = new MouseEvent('dblclick', {bubbles:true}); el.dispatchEvent(d); }
    });
    item('⧉ 复制', function(){ send({type:'wb-ctx', id:id, op:'copy'}); });
    item('✂ 剪切', function(){ send({type:'wb-ctx', id:id, op:'cut'}); });
    item('📋 粘贴到内部', function(){ send({type:'wb-ctx', id:id, op:'paste-inside'}); });
    separator();
    item('↑ 上移', function(){ send({type:'wb-ctx', id:id, op:'move-up'}); });
    item('↓ 下移', function(){ send({type:'wb-ctx', id:id, op:'move-down'}); });
    separator();
    // 动效快捷子项（效果基本库入口：常用 4 种入场 + 悬浮）。
    var anim = document.createElement('div'); anim.className='wb-ctx-group'; anim.textContent='✨ 入场动画';
    ctxMenu.appendChild(anim);
    ['fade-up','zoom-in','slide-up','blur-in'].forEach(function(eff){
      item('　' + eff, function(){ send({type:'wb-ctx', id:id, op:'entrance', value:eff}); });
    });
    item('🌀 悬浮上浮', function(){ send({type:'wb-ctx', id:id, op:'hover', value:'lift'}); });
    separator();
    item('🗑 删除', function(){ send({type:'wb-ctx', id:id, op:'delete'}); });
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
    quickBar.setAttribute('data-target-id', el.getAttribute('data-wp-id'));
  }
  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var el = ev.data.id ? document.querySelector('[data-wp-id="' + ev.data.id + '"]') : null;
      if (el) positionQuickBar(el); else quickBar.style.display = 'none';
    }
  });
  [['✏️','编辑',function(){ var el=document.querySelector('[data-wp-id="'+quickBar.getAttribute('data-target-id')+'"]'); if(el) el.dispatchEvent(new MouseEvent('dblclick',{bubbles:true})); }],
   ['⧉','复制',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'copy'}); }],
   ['🗑','删除',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'delete'}); }]
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
    '[data-wp-id].wb-editing{outline:2px solid #3d444f !important;cursor:text;}',
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
    var el = document.querySelector('[data-wp-id="' + ev.data.id + '"]');
    if (el && ev.data.html) {
      var tmp = document.createElement('div');
      tmp.innerHTML = ev.data.html;
      var fresh = tmp.firstElementChild;
      if (fresh) {
        fresh.setAttribute('data-wp-id', ev.data.id);
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

// injectEditorBridge 把编辑器桥接脚本追加到 </body> 前。
func injectEditorBridge(html string) string {
	idx := strings.LastIndex(html, "</body>")
	if idx < 0 {
		return html
	}
	return html[:idx] + editorBridgeScript + html[idx:]
}
