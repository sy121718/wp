# 02-F · 前端控件基座（已落地）

> 状态：**已实现**（2026-09）。三个提交：资产归位 → 基座落地 → 前台注入。

## 1. 这一层解决什么

后台主题设置页的「下拉框点中部不展开」，暴露出一件事：同一个交互能力，项目里有**三处各写一份**。

| 位置 | 形态 | 谁在用 | 覆盖范围 |
|---|---|---|---|
| \`static/js/enhance.js\` | 普通脚本，构建期**按需内联** | 前台产物（访客） | 8 个**组件级**增强：计数器/轮播/图集/倒计时/灯箱/卡片环/堆叠/全屏分页 |
| \`static/js/workbench/core.js\` | **ES module** | 只有工作台 | \`wbDropdown\` / \`wbColorPicker\` 等编辑器原语 |
| ~~\`select-enhance.js\`~~ | 普通脚本 | ~~只有后台~~ | 已并入基座，删除 |

**根因**：没有"原始控件"这一层。①是组件级能力（轮播要滚、灯箱要开），②只有工作台能加载，
两者都不回答"一个 \`<select>\` 在任意页面该长什么样、怎么交互"——于是同一个下拉被写了三遍。

## 2. 三层各司其职（现状）

\`\`\`
③ 组件增强   static/js/enhance.js          轮播/灯箱/卡片环…      按 data-* 特征，构建期按需内联
② 编辑器控件 static/js/workbench/          检查器取色器/下拉       仅工作台（ES module）
① 原始控件   static/js/ui/                 select /（后续 input…）后台与前台**共用一份**
       外观    static/css/ui.css           .wbs-* 走 --sky-* 变量  主题可覆写
\`\`\`

### 一并归位的还有 CSS 效果层

\`internal/builder/core/effects.go\` 是**组件库外观**那一半：13 类效果词汇表
（surface / border / background / focus / text / motion / image / button / table / loader /
shadow / shape / viewport），组件只声明词汇、编译期出 CSS，颜色全部走 \`--sky-*\`。

它与 \`ui/\` 的关系：**effects.go 管外观与效果，ui/ 管基础控件行为**，两者都跟主题走。
新增控件时外观写 \`ui.css\`、行为写 \`ui/<control>.js\`。

## 3. 一份实现、两个投递口

\`\`\`
static/js/ui/*.js
   ├── 运行时：/static（gin.Dir）→ 后台页面 <script src>（顺序：_util → 控件 → index）
   └── 构建期：templates.StaticJS 读 embed → 装配层注入 builder → 按 data-ui-* 挑控件内联进产物
\`\`\`

**为什么构建期走 embed 而不是读磁盘**：页面编译发生在运行时，而生产部署可能只带二进制、
磁盘上没有源码树。embed 保证「构建不依赖文件路径」（与组件模板同一条原则）。

**为什么用普通脚本而不是 ES module**：两个消费方都吃不了 \`import\`——后台是 \`<script src>\`，
前台是构建期拼进 \`<script>\` 正文。②的模块形态只在工作台成立，不能作为通用基座。

**为什么 builder 要注入而不是自己 embed**：\`builder\` 不依赖 \`internal/templates\`（后者含 gin 依赖），
所以走 \`WithUISources\` / \`WithEnhanceSource\`（与 \`WithComponentSet\` 同一条路）。

## 4. 新增一个控件的标准动作

1. **特征**：组件模板在需要它的元素上打 \`data-ui-<控件>\`；
2. **登记**：\`internal/builder/ui_script.go\` 的 \`uiBlocks\` 加一行（文件名 + 特征）。
   **漏登记 = 用到它的页面静默失去该控件增强**，所以这一步不能忘；
3. **行为**：写 \`static/js/ui/<control>.js\`，用 \`WBUI.register(function (scope) { … })\` 登记，
   内部用 \`WBUI.each\` / \`WBUI.$$\`，**每个元素第一件事是 \`WBUI.markOnce(el, '<Name>')\`**；
4. **外观**：写进 \`ui.css\`，颜色走 \`--sky-*\`（回退 \`--c-*\` 兼容后台）；
5. **测试**：\`ui_script_test.go\` 补拼装断言（命中/未命中/源码缺失），
   必要时加产物实测（CDP 真实事件）。

### 两条硬规矩

- **渐进增强**：原生元素保留、表单提交不变、无 JS 时功能不丢。控件只是让交互更可控，
  不是替代原生语义；
- **\`markOnce\` 必须打**：htmx 局部替换后会重扫，没标记就会在同一元素上叠出第二套菜单
  （表现为点一次开两个、或关不掉）。

## 5. 本次踩到的坑（都留了测试）

| 坑 | 现象 | 现在怎么防 |
|---|---|---|
| 拼装时多套 \`<script>\` | 产物里明明有基座代码，运行时 \`WBUI\` 是 undefined——外层模板已套 \`<script>\`，再套一次成了嵌套，整段语法错误 | \`uiScriptFor\` 只返回正文；测试断言禁止出现 script 标签 |
| \`aria-selected\` 被当违规 | 走查页断言"tabs 不该输出 aria-selected"误伤控件基座（它合法使用该属性） | 判据改用 ARIA tab 专属词（\`role="tab"\` / \`role="tablist"\`） |
| 增强源码包级变量 | 旧实现里测试不注入也有值，改成注入后 golden 立刻漂移 | \`document.html\` golden 与改动前**逐字节一致**，证明搬家没改产物 |
| air 不监听 js/css | 改磁盘文件后台立刻生效、产物却是旧版 | \`.air.toml\` 的 \`include_ext\` 加上 js/css |

## 6. 暂不做

- **工作台 \`wbDropdown\` / \`wbColorPicker\` 收敛**：涉及工作台模块加载与既有交互，
  等 ① 有第二三个控件之后再迁，避免一次性大改；迁移时注意两者 API 不同
  （工作台那套面向检查器字段，基座这套面向页面上的原生控件）；
- **\`effects.go\` 里的 ◻️ 项**（噪点/极光/逐字入场/模糊过渡/悬停展开/断点变量化）：
  属于效果层，与本次无关，按需立项。

## 7. 关于"能不能不用 JS"

常被问到的三个替代方案，结论写在前面：**都不能整体替代**。

- **Jet（构建期）**：它算的是"此刻页面该长什么样"，产物即固化。而增强要的是访客的当下
  （滚到哪、拖多远、现在几点）——不是能力问题，是构建期根本拿不到那个变量；
- **HTMX**：它解决"去服务器拿一段 HTML 换进来"，不碰客户端瞬时状态。而且前台是纯静态
  产物、访问面不查库不执行模板，用 HTMX 做倒计时等于每秒打后端，架构上不成立。
  HTMX 的正确位置是后台（已用）与前台白名单 Runtime Fragment（库存/购物车那类）；
- **CSS**：能替一部分——\`scroll-snap\` 做全屏分页/部分轮播很干净，\`@property\` 能做数字滚动。
  但代价通常是**DOM 复杂化 + 可访问性退步**（纯 CSS 交互几乎都要 radio+label 或 \`:target\`
  这类 hack，cardstack 与 tabs 当初就因此丢了键盘可达）。判据是**DOM 语义不退化**，
  而不是"能省则省"。

**倒计时（需要真实当前时间）与拖拽（需要指针事件）永远得留 JS。**
