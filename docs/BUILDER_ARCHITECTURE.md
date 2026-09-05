# Builder 架构方案对比

> **已归档（2025-09）**：本文为工作台选型纪要，保留作为历史记录。所选方案已落地——iframe 隔离 + Jet 预览/发布一致渲染已实现：可视化工作台（`/workbench`）已实装，构建期组件渲染走 Jet（`internal/templates/components/*.jet` + `builder/jetview.go`，预览即发布）。文中「20 个组件」为当时估算，实际组件数为 **18 个**（`internal/builder/components/` 目录：accordion/button/container/counter/divider/gallery/globalref/heading/image/infobox/list/marquee/slider/socialbuttons/spacer/tabs/text/video）。以下方案对比与优化讨论不再代表当前实现状态。

## 当前方案：iframe 隔离

### 为什么选择 iframe

go_wp Builder 使用 **iframe 隔离方案**，这是经过深思熟虑的架构决策：

#### 1. 符合静态发布架构

```
编辑时：Builder 编辑器（Alpine.js）→ 修改 Document JSON
预览时：iframe 加载独立 HTML → 渲染组件
发布时：Jet 模板引擎 → 生成静态 HTML（与预览一致）
```

- **预览即发布**：iframe 中看到的效果就是最终静态页面效果
- **样式一致性**：预览和发布使用相同的 CSS，无样式差异

#### 2. 完全样式隔离

```
外层（Builder UI）：
  - Alpine.js 响应式
  - 左侧组件库样式
  - 右侧 Inspector 面板样式

iframe 内（预览区）：
  - 18 个组件的独立样式
  - 用户自定义 CSS（未来）
  - 第三方库样式（未来）
```

- 无 CSS 污染：预览区和编辑器 CSS 完全独立
- 真实渲染：预览区看到的就是访客看到的

#### 3. 安全沙箱

```javascript
// 未来支持自定义代码时
{
  type: 'container',
  props: {
    customCSS: '.my-class { color: red; }',  // 用户 CSS
    customJS: 'console.log("hello");'        // 用户 JS
  }
}
```

- iframe 隔离确保用户代码不会破坏编辑器
- 恶意代码无法访问编辑器 DOM

#### 4. 成熟方案

**主流产品都使用 iframe**：
- Elementor（WordPress）
- Webflow
- Framer
- Wix Editor X

---

## 其他方案对比

### 方案 1：同页面编辑

**实现**：
```html
<div class="editor">
  <div class="toolbar"></div>
  <div class="canvas" contenteditable>
    <!-- 直接编辑 -->
  </div>
</div>
```

**优点**：
- 实现简单
- 拖放原生支持

**缺点**：
- ❌ **致命缺陷**：编辑器 CSS 和预览 CSS 混在一起
- ❌ **预览不准确**：编辑状态 ≠ 发布状态
- ❌ **不安全**：用户 CSS 可能破坏编辑器

**结论**：不适合 go_wp，因为需要「所见即所得」

---

### 方案 2：Shadow DOM

**实现**：
```javascript
const shadow = canvas.attachShadow({ mode: 'open' });
shadow.innerHTML = `
  <style>/* 隔离样式 */</style>
  <div><!-- 预览 --></div>
`;
```

**优点**：
- 样式隔离
- 同页面，无 postMessage

**缺点**：
- ❌ **兼容性差**：某些库不支持 Shadow DOM
- ❌ **全局样式无法继承**：字体、CSS 变量等需手动注入
- ❌ **调试困难**：开发者工具显示不直观
- ❌ **事件冒泡限制**：拖放、键盘事件可能有问题

**结论**：理论上可行，但工程复杂度高，收益不明显

---

### 方案 3：Canvas 渲染

**代表**：Figma、Canva

**优点**：
- 性能极高
- 完全可控

**缺点**：
- ❌ **无 HTML**：需要自己实现所有渲染
- ❌ **无法复用 CSS**：go_wp 的 18 个组件都用 CSS，全部重写成本巨大
- ❌ **文本编辑困难**：需要自己实现光标、选区
- ❌ **无障碍性差**：屏幕阅读器无法识别

**结论**：不适合 Web Builder，适合设计工具

---

### 方案 4：服务端预览

**实现**：
```javascript
// 每次修改都请求服务端渲染
fetch('/preview', { body: JSON.stringify(doc) })
  .then(html => iframe.srcdoc = html);
```

**优点**：
- 预览 = 发布（使用 Jet 渲染）

**缺点**：
- ❌ **延迟高**：每次改动都需要网络请求
- ❌ **服务器压力**：用户频繁修改 = 频繁渲染
- ❌ **离线无法用**：无网络时无法预览

**结论**：可以作为「精确预览」的辅助功能，但不能作为主预览方案

---

## iframe 方案的优化

### 1. 优化跨窗口通信

**问题**：`postMessage('*')` 可能被浏览器扩展干扰

**解决**：
```javascript
// 使用明确的 origin
const targetOrigin = window.location.ancestorOrigins?.[0] || window.location.origin;
parent.postMessage(msg, targetOrigin);
```

### 2. 优化拖放体验

**当前实现**：
```javascript
// iframe 内监听拖放事件，通过 postMessage 通知外层
iframe.contentWindow.addEventListener('dragover', (e) => {
  parent.postMessage({ type: 'dragover', x: e.clientX, y: e.clientY }, origin);
});
```

**进一步优化**：使用 `pointer-events: none` 让拖放穿透 iframe

### 3. 按需加载

```javascript
// 只有预览时才加载 iframe
if (mode === 'preview') {
  iframe.srcdoc = BuilderRenderer.render(document);
}
```

---

## 结论

**iframe 是 go_wp Builder 的最佳选择**，因为：

1. ✅ 符合静态发布架构（预览 = 发布）
2. ✅ 完全样式隔离（无 CSS 污染）
3. ✅ 安全沙箱（支持自定义代码）
4. ✅ 成熟方案（行业标准）
5. ✅ 未来扩展性强（多设备预览、A/B 测试等）

**当前遇到的 `postMessage` 错误**：
- 不是 iframe 方案的问题
- 是浏览器扩展（翻译、AI 助手）的干扰
- 已通过明确 origin 优化

**不建议更换方案**，应该优化现有 iframe 实现。
