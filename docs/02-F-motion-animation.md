# 02-F 动效系统选型评估（Motion & Animation Library）

> 状态：选型评估完成，未落地。落地顺序见 §8。
> 数据核对：2026-09（星数经 context7 API 印证；GSAP 体积经 jsdelivr gsap@3.13.0 实测）。
> context7 回查：本文所有候选库的 library id 已录入根目录 context7.json，见 §9。

---

## 1. 背景与结论摘要

建站工具的组件（internal/builder/components/，现 30 个）交互层刻意纯 CSS 优先，观感偏"简单"。
本文评估融入动画库的可行性、候选库全量对比与按需加载机制，覆盖四类目标效果：

1. 滚动驱动时间线（timeline scrub）
2. 产品固定 + 轨迹运动（sticky + motion path）
3. 固定容器翻页（pinned horizontal deck）
4. 卡片堆叠 3D 展示（card stack / 序列帧 360°）

**结论**：

- 架构有三条现成通道，融入不破坏任何核心不变量（见 §3）；
- CSS 路线可做到「编译期单动画裁剪」（用一个动画只编一个动画，见 §6）；
- GSAP 路线最小颗粒度是「core + 用到的插件」，每页 79~134KB min（gzip 27~44KB），
  永远不是完整库（~250KB），且只出现在用到对应组件的页面（见 §6）；
- 推荐组合：自建 CSS keyframes 精简集 + CSS scroll-driven animations 为主，
  GSAP（core + ScrollTrigger + MotionPath + Observer）vendor 兜底，多数页面 0 动画 JS（见 §8）。

---

## 2. 架构约束（动效必须守住的 4 条）

| # | 约束 | 来源 | 动效侧做法 |
|---|---|---|---|
| 1 | **确定性构建**：同输入同字节 | 核心不变量 5 | 动画库 vendor 本地锁版本（Trix 先例 /static/vendor/trix/），禁 CDN 动态版本链接 |
| 2 | **无 JS 降级**：纯 CSS 优先 | tabs radio hack / accordion details 哲学 | HTML 初始态必须可见；JS 动画只做增强，禁止初始 opacity:0 等 JS 恢复（AOS 经典坑） |
| 3 | **样式引擎安全模型**：三重白名单 | internal/builder/style/whitelist.go | 动画只作为组件 props 枚举字段；检查器不放开任意 animation-* 字符串。已知缺口：safeProps 缺 filter/z-index/backdrop-filter、伪元素 ::before/::after（含 content 文字——由组件 compileCSS 从 props 编译期转义生成）、值模型不含 calc() 表达式——内置组件不受限（compileCSS 全权生成），插件路径按需小扩（§4.6） |
| 4 | **画布 scope**：workbench 不滚动 | 构建器体验 | 画布内 scrub 动画永远停在 0%——画布渲染静态终态，"预览动画"走真产物 iframe |
| 5 | **prefers-reduced-motion**：无障碍基线 | WCAG 动效约定 | 常驻/无限循环动画在 reduce 时停用显示静态帧；交互与滚动动画可保留但去位移。常驻动画另有页面级性能预算：无限 keyframes 实例 ≤3/页（合成层内存），构建期 lint 校验 |

补充约束：

- **按需打包**：GSAP 等大库禁止走 enhance.js 内联通道（全页面背负），必须走页面级 JS 依赖闭包（§6.3）；
- **SEO**：scrub/pin 用 sticky + transform 实现，不影响布局流（CLS 安全）；外部 JS 按需加载保 LCP。

---

## 3. 三条融入通道（现成管线，不新增架构）

| 通道 | 载体 | 适用 | 现状 |
|---|---|---|---|
| A. 静态 CSS 动画资产 | CSSBuckets 编译 + keyframes | 入场/显隐/hover 微交互 | 机制现成：样式引擎白名单已注明「keyframes 走静态 CSS 资产」（docs/06-plugin-system.md:191） |
| B. enhance.js 模块 | 零依赖 IIFE + data-* 按需初始化 | 滚动显隐、视差、打字机等轻交互 | 已存在（internal/builder/enhance.js，覆盖轮播/计数器/手风琴），需演进为构建期裁剪拼接 |
| C. 页面级 JS 依赖闭包 | Artifact assets/ 内容寻址 + 组件 js_deps | GSAP 等引擎库 | 需新建（§6.3），机制顺延自媒体内容闭包 |

---

## 4. 候选库全量分析

> 星数为 context7 API / GitHub 2026-09 核对值；体积未注明者为官方 min 产物量级。

### 4.1 CSS 动画库（星高、价值有限——当词典抄，不整库引）

| 库 | 星数 | context7 id | 许可 | 定位 | 融入判断 |
|---|---|---|---|---|---|
| [animate.css](https://github.com/animate-css/animate.css) | 82,050 | /animate-css/animate.css | MIT（以仓库为准） | keyframes 大全集（~100 个入场/强调/退场动画） | **不整库引**（全量 100KB+）。价值 = keyframes 词典：挑 10~20 个抄进自建精简集（§6.1） |
| [hover.css](https://github.com/IanLunn/Hover) | ~29,000 | 无条目 | MIT（以仓库为准） | hover 微交互集（2D/3D/边框/背景过渡） | 同上，摘进组件 hover 变体 |
| [magic](https://github.com/miniMAC/magic) | ~9,000 | 无条目 | MIT（以仓库为准） | 酷炫入场（perspective/rotate 系） | 参考价值，可选摘抄 |

**CSS 库共同结论**：本质都是静态 keyframes 文本集合。项目走"组件 props 枚举 → 编译期按值裁剪"（§6.1）后，
自建精简集比引库更干净（体积、命名前缀、确定性三者全占优）。

### 4.2 JS 动画引擎（真正干活的角色）

| 库 | 星数 | context7 id | 许可 | 定位 | 融入判断 |
|---|---|---|---|---|---|
| [GSAP](https://github.com/gsap/gsap)（+ScrollTrigger/MotionPath/Observer） | 21,097（行业事实标准，awwwards 获奖站标配） | /greensock/gsap；文档聚合 /websites/gsap_v3 | 官方免费许可（3.13 起全部插件免费含商用） | 滚动驱动编排之王：pin/scrub/motionpath/拖拽全覆盖 | ✅ **首选兜底**。框架无关、模块独立可按页组装（§6.3）。实测体积见 §7 |
| [Motion](https://motion.dev)（原 Framer Motion / Motion One 合流，vanilla 版） | ~33,500 | /websites/motion_dev | MIT | 现代 JS 引擎，mini 版 ~5KB，spring/FLIP 强 | ✅ 轻量备选：只做入场/微交互且不想引 GSAP 时用 |
| [anime.js](https://github.com/juliangarnier/anime) v4 | 59,283 | /juliangarnier/anime；文档聚合 /websites/animejs | MIT | 轻量引擎，API 优雅 | ⚠️ 可用但编排能力/生态/文档不及 GSAP，没必要双引擎并存 |
| [mo.js](https://github.com/mojs/mojs) | 18,626 | /mojs/mojs | MIT | 图形化 motion graphics（burst 爆炸等） | ❌ 维护放缓、场景偏装饰特效，建站组件用不上 |
| [kute.js](https://github.com/thednp/kute.js) | ~4,000 | 无条目 | MIT | 轻量引擎 | ❌ 生态弱于 GSAP/Motion，无引入理由 |
| Framer Motion（React 版） | ~30,000+ | （vanilla 版即 Motion，见上） | MIT | React 专用 | ❌ 项目无 React 运行时，排除（其 vanilla 形态已并入 Motion 行） |

### 4.3 滚动与辅助库

| 库 | 星数 | context7 id | 许可 | 定位 | 融入判断 |
|---|---|---|---|---|---|
| [Lenis](https://github.com/darkroomengineering/lenis) | 10,259 | /darkroomengineering/lenis | MIT | 平滑滚动（惯性滚轮），与 ScrollTrigger 黄金搭档 | ✅ 可选配件。高端站"丝滑感"一半来自它；注意平滑滚动可能干扰锚点跳转与无障碍，需提供关闭开关 |
| [AOS](https://github.com/michalsnik/aos) | 27,604 | /michalsnik/aos | MIT | 滚动入场（data-aos 属性驱动） | ❌ 不必引：功能被 enhance.js + IntersectionObserver 完全覆盖；且其初始态隐藏模式违反约束 2-2 |
| [ScrollReveal](https://github.com/jlmakes/scrollreveal) | 22,504 | /jlmakes/scrollreveal | **GPL-3.0 双许可（商用需购买）** | 滚动入场 | ❌ **许可风险：CMS 商用产品禁碰** |
| [Splitting.js](https://github.com/shshaw/Splitting.js) | ~6,000 | 无条目 | MIT | 文字逐字/逐行/网格拆分（输出 data-* 供动画编排） | ✅ 实用小件：配 GSAP/scroll-driven 做标题逐字入场 |
| [barba.js](https://github.com/barbajs/barba) | 12,292 | /barbajs/barba | MIT | 多页站页面转场（拦截跳转做过渡） | ❌ 判定收敛（2026-09）：原生 View Transitions API 已支持 cross-document 转场（§4.7），零库替代 barba 全部价值，且无接管导航/与 HTMX 共存的顾虑——不再观望，直接等原生全绿后采用 |
| [tsParticles](https://github.com/tsparticles/tsparticles) | 8,288 | /tsparticles/tsparticles | MIT | 粒子背景（hero 区特效） | ⏸ 观望：按需组件化可行（走通道 C），但属"可选装饰"，等真实需求 |

### 4.4 零库路线（优先级最高，先做）

| 路线 | 说明 | 颗粒度 | 支持度 |
|---|---|---|---|
| **CSS scroll-driven animations**（浏览器原生标准） | animation-timeline: view()/scroll()，动画跑合成器线程（不掉帧）、0 JS、@supports 门控自动降级为静态终态 | **单效果 = 几行 CSS**（真正"用一个截一个"） | Chrome/Edge 115+（2023-07）稳定；Safari 26 已支持；Firefox 渐进——@supports 门控后版本差异不再是问题 |
| **自研 enhance.js 模块** | 现有零依赖 IIFE 模式扩展：initReveal（IntersectionObserver + data-anim）等 | 单模块 ~1-2KB，构建期按组件清单拼接 | 全浏览器 |
| **纯 CSS / 薄 JS 社区效果** | transition 时序编排 + `--i` 派生 + `@property` 鼠标跟随 tilt（§4.6），零库零 keyframes | **单效果 = 十几行 CSS 或 ~25 行 JS** | 全浏览器 |

### 4.5 目标效果 → 候选映射（四效果决策表）

| 目标效果 | 首选路线 | 兜底路线 | 页面携带 |
|---|---|---|---|
| 滚动时间线 | CSS scroll-driven（view() 驱动竖线生长 + 节点点亮） | GSAP ScrollTrigger scrub（节奏编排更细时） | 0 JS 或 core+ST |
| 产品固定轨迹 | sticky + offset-path + scroll-timeline | GSAP MotionPathPlugin（路径跟手/复杂路径） | 0 JS 或 core+ST+MP |
| 固定容器翻页 | sticky 外容器 + scroll() 映射 translateX（§6.2 骨架） | GSAP pin + scrub + anticipatePin（边界处理最稳） | 0 JS 或 core+ST |
| 卡片堆叠 3D 展示 | CSS 3D（perspective+rotateY）+ 交错 delay，点击/hover 驱动 | GSAP Observer/Draggable（拖拽跟手、惯性）；真·产品 360° 序列帧 scrub 必须 JS（canvas 换帧） | 0 JS 或 core+Observer |

新组件规划（配置化 props，建站用户不写代码）：

```text
timeline    nodes[{title,desc,icon}] + lineGrow(bool) + scrub(枚举: none|css|js)
pindeck     pages[children] + direction(h|v) + scrub + 页切换节奏
cardstack   cards[children] + interaction(hover-fan|scroll-stack|drag-360) + scaleBase/scaleStep + spacing
            —— 已落地为 core.cardstack（2026-09）：trigger(hover|scroll) × shape(fan|line) 两轴，
            scroll 模式用 sticky + CSS scroll-driven（零 JS，见 §4.6）；drag-360 待做
productcard image + badge + title/desc + feats[] + price{old,new} + cta{label,icon} + rating + stock
            + hoverStyle(lift|circle-flip|tilt) + badgeStyle(pill|ribbon) —— 纯 CSS hover 编排零 JS，电商核心组件（对齐 docs/06-A 商品重轨）
showcase    image + presentation(cube-rotate|float) —— 常驻 3D 旋转展示台（产品盒/徽章），受约束 5 性能预算
motionpath  path(预设轨迹枚举: arc|scurve|zigzag + 自定义 SVG path) + target(child) + scrub
```

### 4.6 零依赖社区效果：纯 CSS 与薄 JS（2026-09 补充）

参考案例：掘金「Playing Card Hover Effects」（juejin.cn/post/7297665681016684598，作者「掘一」，仅作技术参考）扑克牌扇形展开；本项目对照样本 public/a/test.html（GSAP 滚动堆叠，§4.5 cardstack scroll-stack 模式）。

机制拆解：

- 9 张卡各带 `style="--i: -4..4"` 序号变量；
- `filter: hue-rotate(calc(var(--i) * 50deg))` 每卡异色；
- `.container:hover .card` → `transform: rotate(calc(var(--i)*5deg)) translate(calc(var(--i)*120px), -50px)` 扇形展开；
- `:active` 点击聚焦（换背景 + z-index + 微位移）；全程 `transition: .5s`。
- 零 JS、零 keyframes、零依赖，初始态完全可见（天然满足约束 2）。

对项目的意义：**--i 索引模式是所有「N 个子元素按序派生样式」效果的通用机制**（扇形展开、阶梯延迟
delay: calc(var(--i)*0.1s)、交错入场），且编译期可确定性生成——Jet 循环输出 --i，CSS 一份通用，
同输入同字节自动成立。

**落地方式（已实现，2026-09）**：内置组件 **core.cardstack**（`internal/builder/components/cardstack/`）。
上文设想的「Jet 输出 --i、CSS 一份通用」**未采用** —— 实际由 compileCSS 用 `:nth-child(N)` 逐卡生成
色相/旋转/平移声明。原因：产物是静态 CSS，样式引擎有 safeProps 白名单，`calc()` 表达式生成只保留在
内置组件编译期（Go 侧），不让 `calc(var(--i)*…)` 进入词汇。代价是 CSS 随卡片数线性增长
（默认 9 张 ≈ 9 条基础规则 + 9 个 hover 块），且同页多实例各自成块 —— 取舍与尺寸约束详见该组件包注释。

组件把「按序号派生几何」收敛成**两条正交轴**，覆盖 §4.5 规划里的前两种模式：

```text
trigger: hover  → 悬停展开（纯 CSS）              shape: fan              弧线扇形（rotate 在前，带角度）
trigger: scroll → 滚动堆叠（sticky + scroll-driven）     line + horizontal   横排一行（无任何角度）
                                                        line + vertical     竖排一列（无任何角度）
```

两轴正交：`trigger` 只改布局与驱动方式，`shape`/`direction` 只改变换写法与收敛式。

**line 是「不带角度的纯排开」**：卡片不做任何 rotate，只用 `translate` 沿一个轴排列 ——
横排时所有卡的 top 完全相同、left 严格等距（实测 9 张卡间距恰为 spreadDistance=120px）；
竖排时 left 完全相同、top 等距，收敛式改按视口高度 `50vh − gutter − 卡高/2` 算，
轨道另按 `卡高 + 2×最大步距×位移` 预留纵向空间。这与 fan 的差别不只是"位移方向"：
fan 的位移落在旋转后的坐标系里（外接框会变大、卡片高度参差），line 的外接框就是卡本身。

卡片内容同样是两态 —— 拖入子节点即内容卡（每张卡一个子节点），删空则退回 1~N 数字占位卡
（先调几何再填内容）。
若插件路径也要此类效果，safeProps 需小扩（filter 以 hue-rotate 枚举值收口、z-index 直接收），
不引入任意 calc 表达式——表达式生成保留在内置组件编译期。

实现要点（照抄社区 demo 会踩的坑）：

1. **取色**一律走 `var(--wp-c-primary, …)` / `var(--wp-c-surface, …)` 主题变量（与 badge/quote/progress
   同一约定）；
2. **数字颜色**同时写在**不包 `@media (hover: hover)`** 的 `:active` 规则里（触屏没有 hover，
   只写悬停态 = 移动端永远看不到数字）；
3. **纵向**：悬停模式的容器高度用 `calc(卡高 + 2×上抬量)` 预留展开空间，否则展开时卡片越出容器顶部；
4. **展开位移**写成 `calc(offset × clamp(0px, 允许值, spreadDistance))`，允许值按形态算，
   都等于「视口相应半轴 − 安全留白 − 该形态下卡片占据的半尺寸」，再除以最大步距：
   ```text
   fan          (50vw − gutter − L·sinθ − (W/2)cosθ − (H/2)sinθ) / (cosθ · offsetMax)
   line 横排     (50vw − gutter − W/2) / offsetMax
   line 竖排     (50vh − gutter − H/2) / offsetMax
   ```
   `50%` 在 `translate` 的 X 方向即卡 border box 半宽（所以卡宽写成 px/rem/% 都不影响）；
   卡高拿不到百分比，由容器以 `--wp-cardstack-h` 变量传入。上界仍是用户参数 `spreadDistance`：
   **视口够宽时参数 100% 生效，装不下才收敛**，不静默改写参数。反之把它设成比允许值大的数
   （如 200），就是「自动铺满视口」。

   **收敛基准为什么取视口（vw）而不是容器（cqw）**：cqw 需要容器声明 `container-type: inline-size`，
   它等于 `contain: layout` —— 而 `contain: layout` 会让容器成为 fixed 后代的包含块，
   「点击放大到屏幕中央」的 fixed 层就再也走不出容器。两者不可兼得，取视口：
   横向溢出的实际危害本来就是撑出视口（横向滚动条）。实测（1440 视口）视口 1440/1000/700 三档下
   展开跨度均比视口窄 16px，全程无横向滚动。

5. **点击放大**：卡片是 `<label>`，内部藏一个同组 radio —— 纯 CSS 零 JS，同组互斥天然保证
   「一次只放大一张」。放大态用 `:has(> .wp-cardstack-toggle:checked)` 命中，`inset:0 + margin:auto`
   居中（不用 transform，免得和展开位移抢属性）；遮罩是容器末尾的 label，点它即选中关闭 radio。
   radio 无法「再点一次取消」，所以另配一个右上角关闭按钮（`<label for>` 指向同一个关闭 radio），
   且关闭 radio **不能共用 toggle 类名**，否则「有 toggle 被选中」在关闭后依然为真、遮罩收不回去。
6. **内容集合（自动出卡）**：卡片数量由 CMS 内容条数决定时，不要在页面里手工复刻卡片 ——
   组件声明 `collectionSource`（`content:article` 等）+ 一组字段映射（图片/标题/正文/附注/链接），
   构建期解析集合、按条数展开卡片。三层职责：

   ```text
   core.CollectionResolver        内容 → 字段列表（构建期静态填入）
   core.CollectionSchemaProvider  集合源 → 字段白名单（可选能力，组件侧按能力探测使用）
   content 模块                   实现两者；HTTP 侧 /api/content/collections 供工作台渲染字段下拉
   ```

   **白名单是唯一的**：组件在构建期用 `CollectionSchemas` 校验字段映射（写错直接报错并列出可用字段，
   不静默渲染空白），并按白名单裁剪集合项（不变量 4：模板只能渲染声明字段）；工作台用同一个接口
   渲染字段**下拉**而不是输入框 —— 手填字段名会绕过白名单，所以界面层不给这个口子。
   解析器未实现元数据契约时退回「按数据实际字段判断」，不阻断构建。

   集合模式的卡片样式与子节点内容卡一致（走卡片级背景/内边距/圆角），卡内元素由组件渲染：
   `img → 标题 → 正文 → 附注 → 链接`，字段留空则该元素不渲染。子节点内容卡与集合卡互斥 ——
   声明了集合源就等于「卡片交给内容」，子节点不再参与卡片渲染。

7. **滚动堆叠**：基础规则就是 `position: sticky + top + translate: 0 -50%` 的纯层叠，跟手收敛叠在同一条
   规则的 `animation` 上，靠 `animation-timeline: view()` 驱动。**不需要 `@supports` 包裹** ——
   老浏览器把 `animation-timeline`/`animation-range` 当未知属性丢弃，动画按 0s 播完并由
   `fill-mode: both` 停在终态，视觉正好等于静态缩放。每张卡用独立关键帧（`wp-cs-<节点id>-<序号>`），
   因为各卡终态缩放不同，共用一份关键帧做不到。

**扩展案例：产品卡双形态（productcard 组件素材，2026-09 补充）**

- 形态 A「电商标准卡」（UltraBook 社区 demo）：hover 上浮 + 图 scale(1.05) + 按钮扫光（::before 从 -100% 到 100%）
  + 图标微旋；内容全真实语义（价格/划线价/库存/评分/标签），零 JS。SEO 加分：组件可顺带输出
  Product JSON-LD，接入 02-E SEO 评分引擎。
- 形态 B「圆形翻转卡」（公众号「前端Hardy」demo）：圆形封面 hover 淡出 → 内容卡展开（190→290px）
  → 产品图弹出 → 文字延迟淡入；核心技巧 **transition-delay 进出时序交换**（退出 delay 归 0、进入
  delay 0.3~0.6s，先退后进）——纯 CSS 编排复杂时序的关键手段，与 --i 派生并列为零依赖层两大机制。

评估：两形态均零 JS/零 keyframes，能力落在现有白名单（transform/transition/transition-delay/box-shadow/
visibility/opacity/width/height/border-radius），仅触发 §2 已记录的两处缺口（伪元素扫光、filter: drop-shadow）。
落地：productcard 以形态 A 为默认、形态 B 为 hoverStyle 变体；badge/rating/button/image 复用现有组件资产
（组合容器，非重造）。画布显示静态卡（hover 不触发 = 无 JS 形态）。

**模式三：鼠标跟随 3D tilt（@property + ~25 行薄 JS，零库，2026-09 补充）**

- 机制：`@property --x/--y` 注册为 <number> 使自定义属性**可过渡**——hover 时 transition-duration: 0s 跟手、
  离开时 400ms 缓动归零；JS 仅 ~25 行（mousemove 偏移 → 写入 --x/--y，零库零框架）。
- CSS 侧：rotateX/rotateY 由变量派生（perspective + preserve-3d + will-change）；进阶光泽层用
  原生三角函数 `calc(atan2(var(--x), var(--y)))` + conic-gradient mask + mix-blend-mode: plus-lighter
  （二期增强，MVP 先只做倾斜）。
- 融入：card/productcard 的 hoverStyle: tilt 变体（strength/glare/磁滞时长 props）；JS 走 enhance.js
  通道 initTilt 模块（data-tilt 属性驱动，rAF 节流，仅 hover 卡监听）。**不为此引 GSAP**——Observer
  的价值在拖拽/惯性/手势，tilt 用不上；再次验证"零库优先、GSAP 只兜底 scrub/pin/序列帧"。
- 支持度：@property（Chrome 85+/Safari 16.4+/Firefox 128+）、atan2()（2023 起全绿）；降级 =
  离开时跳变而非缓动，功能仍在（渐进增强零成本）。画布/NoScript = 静态卡。

**模式四：常驻循环动画（idle keyframes，2026-09 补充）——第三种触发类别**

前三种模式均为交互触发（hover/active）或滚动驱动；本模式是页面加载即播的无限循环动画。
触发方式分类学至此补全：交互触发 / 滚动驱动 / 常驻循环。

- 3D 立方体展示台（公众号「前端Hardy」demo .obj）：六面 rotateX/rotateY + translateZ 拼装 +
  rotate3d 4s linear infinite + updown 漂浮 + ::after blur(20px) 光晕投影。落点：showcase 组件
  presentation(cube-rotate|float)，产品 3D 旋转展示。
- 丝带角标（同来源 .card_box）：::before content 斜切丝带文字（rotate -45deg）+ box-shadow 偏移
  画折角（零元素装饰图形）。落点：productcard badgeStyle: ribbon 变体；角标文字来自 props，
  编译期转义进 content，不触碰样式引擎 content 禁区。
- 玻璃拟态光斑卡（同来源 .container）：backdrop-filter: blur(20px) + 双伪元素光斑无限漂移
  （5s linear infinite，delay 错峰 3s）+ hover 位移放大联动。落点：card 装饰变体 glow-orbs；
  触发 §2 新记录的 backdrop-filter 缺口。

约束（并入 §2 约束 5）：常驻动画必须适配 prefers-reduced-motion（reduce 停用显静态帧）；
页面级性能预算：无限 keyframes 实例 ≤3/页，构建期 lint 校验。常驻动画内容可见性不受影响
（SEO 安全），但多实例常驻合成层会累积 GPU 内存，移动端尤须节制。

### 4.7 CSS 平台能力观察与性能配套（2026-09 补充，非动画库）

来源：掘金《2026年了，CSS 终于能写 if 了》（juejin.cn/post/7681797570142781483，作者 来碗旺仔）、
《两行CSS让页面提升了近7倍渲染性能》（juejin.cn/post/7168629736838463525，作者 前端南玖）。

**观察清单（暂不采用，跟踪兼容性）**

- `if()` 条件函数（Chrome 137+，2025-05）：style()/media()/supports() 三种条件，运行时零 JS 切样式。
  Firefox/Safari 无时间表，C 端产物禁用；fallback 模式 = 双声明（旧行为在前，if() 在后）。
- `@function` 自定义函数：编译期能力，Go compileCSS 已覆盖，无需等待。
- `@scope`：与 CSSBuckets 作用域隔离诉求相关，非必需，观察。

**重点跟踪：@starting-style（动效直接相关）**

解决「元素首次渲染时过渡动画」——P1 入场动画（entrance）的下一代标准方案：起始态声明 + transition，
无需 keyframes。**与 HTMX Runtime Fragment 是绝配**：Fragment 动态插入 DOM 的内容用 @starting-style
做入场过渡，零 JS，贴合「动态片段 + 静态站」架构。支持度成熟后纳入 P1 演进（替换/补充 initReveal）。

**性能配套（可进生产）：content-visibility: auto + contain-intrinsic-size**

跳过离屏内容渲染（1000 项列表 261ms→37ms，约 7 倍）。兼容性已全绿（Chrome 85+/Safari 18+/Firefox 125+）。
两个要点：

1. **必须配 contain-intrinsic-size**（估算占位尺寸），否则离屏元素高度为 0 导致滚动条抖动/CLS；
2. 落地为集合型组件的**编译期默认行为**：gallery/list/table/productgrid 等容器由 compileCSS 自动附加
   content-visibility: auto + contain-intrinsic-size（组件可按典型卡片高度给估算值），保护 SEO 评分引擎
   的渲染性能与 CLS 指标；单卡交互组件不加（避免 hover 目标离屏失效的边缘情况）。

**平台能力补充（来源：juejin.cn/post/7608102583496720393，前端小小栈 2026-02）**

- **Speculation Rules API**：声明式预渲染（script type=speculationrules），静态多页站跳转趋近 0ms——
  与「静态发布引擎」架构绝配：builder 编译期注入产物 head，SiteSettings 站点级开关控制，零运行时、
  确定性构建无损。非动效，但属同类「零运行时平台能力」。
- **View Transitions API（cross-document）**：多页跳转原生转场动画（@view-transition {navigation: auto}），
  §4.3 barba.js 判定据此收敛为「等原生全绿、零库采用」。组件级用法（同文档 SPA 式切换 startViewTransition）
  留给 HTMX 片段替换场景观察。
- **Popover / Dialog API**：零 JS 浮层/模态（Top Layer、::backdrop、Esc/焦点原生管理），兼容性 2024 起全绿；
  与 §10 #4 的 lightbox/dialog 组件机会合并落点。

---

## 5. 推荐组合（最终答案）

```text
必配（分阶段）：自建 CSS keyframes 精简集 + CSS scroll-driven animations（@supports 门控）
第二阶段 vendor：/static/vendor/gsap/{gsap,ScrollTrigger,MotionPathPlugin,Observer}.min.js  ← 锁 3.13.x
可选 vendor：Lenis（平滑滚动）、Splitting（文字拆分）
CSS 词典来源：animate.css / hover.css 摘抄，不整库引
不引：AOS（自研覆盖）、ScrollReveal（GPL）、Framer Motion（无 React）、mo.js / kute.js（无场景）
```

---

## 6. 按需加载机制（三档颗粒度）★ 后续实现对照节

### 6.1 CSS 单动画级裁剪（编译期，100% 满足"用一个截一个"）

keyframes 是普通文本块，编译期确切知道页面每个组件的 props 值：

```go
// 组件 compileCSS 内（与 button variant 系统同款思路）
var animCSS = map[string]string{
    "fade-up": "@keyframes wp-fade-up{from{opacity:0;transform:translateY(24px)}to{opacity:1;transform:none}}",
    "zoom-in": "@keyframes wp-zoom-in{from{opacity:0;transform:scale(.92)}to{opacity:1;transform:none}}",
}

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
    if p.Entrance != "" && p.Entrance != "none" {
        b.Append("anim", animCSS[p.Entrance]) // ← 只写选中的这一个 keyframes
        b.Append("anim", fmt.Sprintf("[data-wp-%s]{animation:wp-%s .8s ease-out both}", id, p.Entrance))
    }
}
```

纪律：**keyframes 不放公共层**，跟组件 CSS 一样由 props 枚举驱动按页编译；同一 keyframes 多组件复用靠 bucket 内容去重。
产物示例：页面 B（仅 fade-up）的 <style> 只含该 keyframes 的 ~5 行，无任何动画 JS。

### 6.2 零 JS 路线示例（pindeck 纯 CSS 骨架）

```css
.wp-pindeck { position: sticky; top: 0; height: 100vh; overflow: hidden; }
.wp-pindeck-track {
  display: flex; height: 100%;
  animation: wp-deck-move linear both;
  animation-timeline: scroll(nearest block); /* 滚动进度 → 横移 */
}
@keyframes wp-deck-move { to { transform: translateX(calc(-100% + 100vw)); } }
@supports not (animation-timeline: scroll()) { /* 降级：普通横向滚动容器 */ }
```

### 6.3 GSAP 插件级依赖闭包（构建期，页面级颗粒度）

GSAP 出厂即拆好的独立文件（jsdelivr gsap@3.13.0 实测）：

| 模块 | min | gzip | 能力 |
|---|---|---|---|
| gsap.min.js（core 引擎，**不是全家桶**） | 70 KB | ~24 KB | 底座（tween/timeline/ticker/插件注册） |
| ScrollTrigger.min.js | 43 KB | ~13 KB | pin/scrub/时间线 |
| MotionPathPlugin.min.js | 21 KB | ~7 KB | 轨迹 |
| Observer.min.js | 9 KB | ~3 KB | 拖拽/手势 |

组合下限：pinned/时间线 = core+ST（113KB min，gzip ~37KB）；轨迹 = +MP（134KB min，gzip ~44KB）；
堆叠跟手 = core+Observer（79KB min，gzip ~27KB）。core 是整体引擎，无法再抠"半个发动机"——
插件级就是 GSAP 体系内的最小颗粒度；更细的诉求全部由 §4.4 零库路线承接。

实现改动点（全部是现成管线顺延，不动架构不变量）：

1. **组件声明依赖**：core/component.go:39 的 Component 接口（现 Type()+Validate()）照 TranslatableProvider
   同款可选接口模式加 JSDepsProvider，返回如 ["gsap.core","gsap.ScrollTrigger"]；
2. **构建期收集**：builder.go:435 Compile() 遍历节点树写 CSSBuckets（:455）的同一趟，收集组件类型集合
   → 映射 deps 去重 → CompiledPage 加 Scripts []AssetRef 字段（现仅 EnhanceScript string，:534/:556）；
3. **Artifact 闭包写入**：chunk 走内容寻址资源通道（同媒体机制），HASH 命名防缓存失效，多页共享同 URL；
4. **引用顺序**：页面 <script defer> 按依赖排序（core 先于插件，defer 保序足够）。

### 6.4 enhance.js 演进

现状：全量内联（builder.go:534），运行时按 data-* 静默跳过 → 演进为**构建期按组件清单裁剪拼接**：
init 函数模块化（initReveal/initPinDeck/initCardStack...），Compile 时只拼该页用到的模块。GSAP 永不走此通道。

---

## 7. 画布与产物行为规范

| 场景 | 行为 |
|---|---|
| workbench 画布 | scrub/pin 全部渲染静态终态（剥 data-anim 或画布重置类）；属性面板"预览动画"按钮弹真产物 iframe |
| 发布产物（支持 scroll-timeline） | CSS 动画按 scrub 配置播放 |
| 发布产物（不支持 / NoScript） | @supports 门控 → 静态终态可见；GSAP 页面降级为静态内容 |
| SEO 爬虫 | 初始态可见、内容在 DOM，无隐藏内容惩罚风险 |

---

## 8. 落地路线（分阶段）

| 阶段 | 内容 | 产出/验证 |
|---|---|---|
| P1 | §6.1 机制：入场动画 props 枚举（entrance）+ animCSS 精简集 + enhance.js initReveal | 组件观感升级，零新依赖；确定性/降级测试 |
| P2 | pindeck + timeline 组件（纯 CSS scroll-driven + @supports 降级），画布静态终态规范 | 验证"CSS 动画资产 + 门控降级"管线，交付 80% 观感 |
| P3 | cardstack（CSS 3D 为主） | 无 GSAP 的堆叠展示 |
| P4 | JS 依赖闭包（§6.3 三改动）+ GSAP vendor（core/ST/MP/Observer）+ motionpath 组件 + 序列帧 360° | "跟手/编排/序列帧"最后 20%；按需加载生效（未用页面 0 字节） |
| P5（可选） | Lenis 平滑滚动（带关闭开关）、Splitting 文字拆分、barba.js 评估 | 锦上添花项 |

---

## 9. context7 回查表（已同步至根目录 context7.json）

| 库 | context7 id | 备注 |
|---|---|---|
| GSAP | /greensock/gsap | 仓库源码文档（7.6k tokens）；聚合版 /websites/gsap_v3 文档最全（284k tokens，2026-09-06 更新） |
| Motion | /websites/motion_dev | 官网聚合 trust 9.7；官方 repo 无 owner/repo 条目 |
| anime.js | /juliangarnier/anime | 聚合版 /websites/animejs |
| Lenis | /darkroomengineering/lenis | |
| AOS | /michalsnik/aos | |
| ScrollReveal | /jlmakes/scrollreveal | 仅作排查用（已排除引入） |
| barba.js | /barbajs/barba | 聚合版 /websites/barba_js |
| tsParticles | /tsparticles/tsparticles | |
| mo.js | /mojs/mojs | |
| animate.css | /animate-css/animate.css | |
| hover.css | 无条目 | context7 搜索两轮未命中 |
| magic | 无条目 | 同上 |
| Splitting.js | 无条目 | 同上 |
| kute.js | 无条目 | 同上 |
| CSS scroll-driven animations | 无（浏览器原生标准） | 文档：MDN "CSS scroll-driven animations" |

context7 查询模板：

```bash
KEY=$(cat ~/.dsh/secrets/context7-api-key)
# 文档拉取（topic 可选）
curl -s "https://context7.com/api/v1/libraries/greensock/gsap?topic=ScrollTrigger&tokens=5000&type=txt" -H "Authorization: Bearer $KEY"
```

---

## 10. 素材库索引（Source Library）

所有收集素材的统一索引。价值分级：高（进 P 阶段）/ 中（变体或随做）/ 低（仅归档模式）/ 词典（随手查）/ 不适用（记录判定）。

| # | 素材 | 来源 | 类型 | 价值 | 状态 / 落点 |
|---|---|---|---|---|---|
| 1 | GSAP 卡片滚动堆叠 demo | public/a/test.html | demo | 高 | cardstack scroll-stack 实现参考（§4.5） |
| 2 | 产品卡·电商标准卡（UltraBook） | 用户提供 demo | demo | 高 | productcard 默认形态（§4.6 扩展案例） |
| 3 | content-visibility 性能 | juejin.cn/post/7168629736838463525（前端南玖 2022-11） | 文章 | 高 | §4.7 性能配套，集合组件编译期默认行为 |
| 4 | 冷门 HTML 标签（details/dialog/datalist） | juejin.cn/post/7576468602304495654（ErpanOmer 2025-11） | 文章 | 高 | 印证 accordion=details 现有决策（含页内搜索增益）；新组件机会：**lightbox/dialog 弹窗**（gallery 大图查看，Top Layer 零 JS）、**form datalist 变体** |
| 5 | img vs picture | juejin.cn/post/7577298871005036578（程序员大华 2025-11） | 文章 | 高 | **image 组件优化清单**：srcset/sizes 响应式输出 + picture WebP 格式降级（媒体库 nativewebp 能力现成）+ width/height 防 CLS（联动 SEO 评分） |
| 6 | 鼠标跟随 3D tilt | 用户提供 demo | demo | 中 | hoverStyle: tilt 变体（§4.6 模式三） |
| 7 | 产品卡·圆形翻转卡 | 公众号「前端Hardy」 | demo | 中 | productcard hoverStyle: circle-flip（§4.6 扩展案例 B） |
| 8 | 丝带角标 | 公众号「前端Hardy」 | demo | 中 | productcard badgeStyle: ribbon（§4.6 模式四） |
| 9 | 扑克牌扇形展开 | juejin.cn/post/7297665681016684598（掘一 2023-12） | demo | 范式高/效果低 | --i 索引模式归档（§4.6①）；效果本体不进 P 阶段 |
| 10 | 3D 立方体旋转 / 玻璃光斑卡 | 公众号「前端Hardy」×2 | demo | 低 | §4.6 模式④归档；showcase/glow-orbs 不进 P 阶段（装饰性 + 性能负担） |
| 11 | CSS if()/@starting-style 等 | juejin.cn/post/7681797570142781483（来碗旺仔 2026-09） | 文章 | 分裂：@starting-style 高 / if() 低 | §4.7；@starting-style 重点跟踪（HTMX Fragment 入场过渡） |
| 12 | 灵活运用 CSS 开发技巧（100+ 条） | juejin.cn/post/6844903926110617613（JowayYoung 2019-08） | 词典 | 词典 | 组件开发随手查；2019 年产物，优先用新标准（容器查询/aspect-ratio/:has()）替代旧技巧 |
| 13 | UnoCSS 范式 | juejin.cn/post/7512392168783659071（ErpanOmer 2025-06） | 文章 | 不适用 | Node/Vite 工具链与 Go-only 确定性构建不搭；思想印证：CSSBuckets 即编译期按需生成的 Go 版实现 |
| 14 | Blob/Base64 客户端二进制 | juejin.cn/post/7523065182429904915（小飞悟 2025-07） | 文章 | 不适用 | 客户端 JS 知识与 Go 服务端媒体管线正交；Trix 附件/上传预览场景媒体库已覆盖 |
| 15 | WAI-ARIA Menu/Menubar Pattern | juejin.cn/post/7675216417545371674（anOnion 2026-08） | 规格参照 | 中高 | nav 子菜单键盘交互规格（方向键/Esc/aria-haspopup/aria-expanded），补 dropdown_contract 的 ARIA 语义依据；同系列还有 Dialog/Combobox 等，写 lightbox/dialog 组件规格时必引 |
| 16 | WAI-ARIA Listbox Pattern | juejin.cn/post/7671219839775997962（anOnion 2026-08） | 规格参照 | 中高 | form 组件「优先原生 select」的外部权威背书；Listbox 边界（option 内禁交互元素→Grid/Combobox）可作未来组件校验器的 a11y 规则素材 |
| 17 | 9 个原生 API 替代 JS 库（Popover/Dialog/Speculation Rules/View Transitions/Wasm…） | juejin.cn/post/7608102583496720393（前端小小栈 2026-02） | 文章 | 高 | **Speculation Rules**：builder 编译期注入静态产物，整站跳转 0ms（SiteSettings 开关，§4.7）；**View Transitions**：barba 判定据此收敛为原生零库（§4.3 已更新）；Popover/Dialog 并入 #4 落点 |
| 18 | @property 渐变色过渡 | juejin.cn/post/7591697558377873450（JIE_ 2026-01） | 文章 | 中 | §4.6 模式三机制的延伸：注册 <color> 自定义属性让 linear-gradient 可 transition；落点 = button/productcard 渐变变体 hover 平滑变色。注：文中「实验性」说法已过时，@property 2026 全绿 |
| 19 | CSS 长虹玻璃效果（SVG feDisplacementMap） | juejin.cn/post/7399273700117168178（苏武难飞 2024-08） | 词典 | 词典→低 | SVG 滤镜位移形变，效果高级但 GPU 开销大 + 需贴图资产组合；gallery/image 艺术滤镜候选，有真实需求再取 |
| 20 | position:sticky 失效三坑（overflow 祖先/flex 主轴/缺 top） | juejin.cn/post/7507073213865689138（ErpanOmer 2025-05） | 规格参照 | **高** | 整个动效方案地基是 sticky（pindeck/timeline/motionpath/scroll-stack）。直接翻译为**构建期校验规则**：① sticky 组件祖先链 overflow 检测警告；② compileCSS 强制带 top 边界；③ 包含块=活动半径 → pindeck 外层 wrapper 撑高度实现依据。防一类"用户说效果坏了"工单 |
| 21 | HTML-in-Canvas 提案（layoutsubtree/drawElementImage/paint） | juejin.cn/post/7640829725629972489（Web情报局 2026-05） | 观察 | 观察项 | WICG 实验提案（需 flags+Canary），Canvas 内渲染交互式 HTML/着色器联动；远期跟踪，与建站组件当前无交集 |
| 22 | 文字雨动画（随机字符 + 无限下落） | juejin.cn/post/7270648629378367528（掘一 2023-08） | demo | 低/不适用 | 首条「机制也不可复用」：核心观感依赖非标准 -webkit-box-reflect（作者自警勿上生产）；无限生成 DOM 雨滴违反约束 5；随机字符对屏幕阅读器是噪声。同作者 #9 收录（--i 范式），本篇不收——判定标准是机制可复用性而非效果酷度 |
| 23 | 纯 CSS Soul 星球（金球法则球面分布） | juejin.cn/post/7440840299759747081（苏武难飞 2024-11） | demo | 中 | **--i 模式的高阶形态**：CSS 原生三角函数（acos/sin/cos/sqrt）+ 金球法则（Fibonacci 球面螺旋）把 N 个元素均匀映射到球面，编译期可确定性生成（Jet 循环输出 --index）。机制复用面：弧形文字/环形菜单/球面标签云。落点：§4.6 机制档案（CSS 数学能力线，与 #9/#18 同族） |
| 24 | 纯 CSS 视差星空宇宙 | juejin.cn/post/7497869631567167529（滚石_stars 2025-04） | demo | 低 | **性能反例**：demo 内 4+ 个无限动画（超约束 5 的 ≤3 预算）+ fixed+preserve-3d+多层 blur 移动端陷阱 + 依赖自建滚动容器（#20 滚动上下文坑同源）。仅记录**视差分层公式**（translateZ(-Npx)+scale 补偿）备用：hero 视差若做，公式取此、驱动改页面级 scroll-driven、动画砍至 1-2 个 |
| 25 | 51 个 CSS 动画效果合集（持续更新） | juejin.cn/post/7172535582206197797（水冗水孚 2022-12，附 GitHub 仓库） | 词典 | **高（词典首选）** | 自建 animCSS 精简集的直接素材矿：打字机/骨架屏/卡片翻转/倒计时翻页/按钮波纹/探照灯等全是组件会用的成品小块，对着抄进 §6.1 的 map。比 animate.css 贴场景；2022 年产物，抄时按新标准校 |
| 26 | H5 适配方案（amfe-flexible/rem/vw） | juejin.cn/post/7458970090932551689（destinying 2025-01） | 文章 | 不适用 | rem 动态方案需 JS+Node 构建链，与 Go-only 管线不搭；组件走样式引擎既有响应式断点（tablet/mobile）路线 |
| 27 | 纯 CSS 3D 立方体 + inline-block 间距坑 | juejin.cn/post/7659563288543952902（ReBound 2026-07） | 文章 | 低（坑有规范价值） | 立方体效果 #10 已归档；收获一条**Jet 模板编码规范：组件子元素布局一律 flex 不用 inline-block**（换行空白→意外间隙，解法表里 flex 为最优），写进组件模板规范 |
| 28 | CSS :has() 选择器大全 | juejin.cn/post/7349360925185802251（xingba 2024-03） | 规格参照 | 中高 | 2023 底全绿（Chrome 105+/Safari 15.4+/Firefox 121+）。落点：① rating 组件纯 CSS 交互评分升级（radio+:has 反转）；② form 校验状态上浮卡片容器（input:invalid 联动）；③ 内置组件 compileCSS 可直用，插件路径受控选择器白名单（targetRe）是否收 :has() 留评估 |
| 29 | CSS 文本/图片无限滚动动画（无缝 marquee） | juejin.cn/post/7306442463765544971（掘一 2023-11） | demo | 中高 | 双份内容 div + keyframes translateX(-100%) 无缝循环 + --t 速度变量，零 JS。**marquee/logo 墙组件机制参考**（标签滚带、图片横滚、客户 logo 墙全是建站高频）；实现要点：副本 aria-hidden、两端渐变蒙层；可升级 scroll-driven 版 |
| 30 | 现代图片优化指南·容错与可访问性 | juejin.cn/post/7208571916155895864（Coco 2023-03，系列终章） | 规格参照 | 中高 | **image 组件 a11y 规格**：alt 七类填充规范（信息性必有 alt/装饰性空 alt/功能性描述行为）+ onerror 容错兜底 + 装饰图 role=presentation。系列共五篇（picture 使用/响应式/缩放防偏移/懒加载解码/本篇）＝image 组件优化清单全集，与 #5 互补。校验器规则：有意义的 img 必须有 alt |
| 31 | HTML 语义化标签（告别 div 流水账） | juejin.cn/post/7642713822159650850（2026） | 规格参照 | 中高 | **SEO 评分引擎语义检查项 + 组件模板语义映射规范**：nav→<nav>、heading→h1-h6、text→p、container→section、正文→article/main（一页一个 main）。与 #4/#15/#16 同属「HTML 正确性」规格线；Trix 正文白名单同样保持语义标签 |
| 32 | Grid auto-fit/auto-fit 响应式网格（2 行替代媒体查询） | juejin.cn/post/7547728016069656627（前端Hardy 2025-09） | 规格参照 | **高** | **集合组件默认布局方案**：repeat(auto-fit, minmax(min, 1fr)) 一行替代断点堆，检查器只需「最小卡片宽度」滑杆即自适应；auto-fill(保留空轨道/固定网格) vs auto-fit(折叠空轨/内容拉伸) 的选择暴露为 layout props。印证 #26 判定：响应式不需要 rem/JS。grid-template-columns 已在样式引擎白名单；repeat/minmax 值由内置组件 compileCSS 直出 |
| 33 | 为什么不建议使用 css margin（gap 优先主张） | juejin.cn/post/7478967140378460194（阿古达木 2025-03） | 规格参照 | 低-中 | 采纳其一半：**compileCSS 生成规范——组件内部间距优先 gap（flex/grid gap），不用 margin**（避免塌陷与间距归属混乱）；不采纳教条禁用——margin 在文本流（段落间距）仍是正解。与 #27（flex 模板规范）同族 |
| 34 | 2025 CSS 新特性盘点（block 居中/subgrid 等） | juejin.cn/post/7450434330672234530（2025） | 词典 | 词典→中 | align-content 用于 block 垂直居中（免 flex，Chrome 123+）记备忘；**subgrid 有真实落点**：productcard/cardstack 多卡片「标题行对齐/价格行底对齐」的嵌套网格对齐难题正解（Chrome 117+/Safari 16+/Firefox 71+ 全绿），组件布局进阶参考 |
| 35 | 产品卡·主题/图片切换（checkbox hack 状态机） | juejin.cn/post/7610681694149869583（前端Hardy 2025） | demo | 中 | 隐藏 checkbox + label + :checked~ 兄弟选择器，零 JS 切换商品图/主题变体（与 tabs radio hack 同族状态机）。落点：productcard 新交互能力 **variant-switch**（商品色卡/主题切换）；副本可访问性同 #29 注意 label 关联 |
| 36 | 前端Hardy 产品卡系列同质批次（3 篇）+「产品卡片」检索页其余 | juejin.cn/post/7514009321324724224 、/7460692515177644043 、/7460169168677732415（抽查确认） | demo 批次 | 低 | hover 透明度/尺寸/位置变体，机制已被 #2/#7 覆盖；检索页其余（Anki 笔记、Notion 写作法、Android 3D 翻页、UI 设计手法篇）非代码或非 Web，不收录。**批次结论：产品卡 demo 赛道同质化严重，#2+#35 已覆盖其全部机制，此赛道关闭** |

收录规则：demo 只收「机制可复用或直接对应组件候选」的；文章按「兼容性 + 与 Go 编译管线适配度」判定；词典类只收藏不逐条采纳。后续新素材按表追加行。
