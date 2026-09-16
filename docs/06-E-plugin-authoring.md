# 06-E · 轻轨插件作者指南（能做什么 / 不能做什么）

> 面向插件作者。规范权威是 [06-plugin-system.md](./06-plugin-system.md)；本文是它的
> 作者视角投影：**每一条能力/限制都标了依据**，末尾给三个可直接安装的示例。
> 目标是「照做能跑通」，不是营销文案。

## 0. 三个可直接安装的示例

三档表达力，从「零 CSS 资产」到「带 L1 数据层」，覆盖轻轨插件的能力边界。

| 目录 | 档位 | 展示什么 |
|---|---|---|
| `examples/l0-style-only/` | 纯样式声明（L0） | 全部样式来自 `manifest.json` 的 `styles.rules`：绑定、变体、伪类三态、结构伪类、变量导出、断点、容器/主题查询 |
| `examples/l0-template-assets/` | 模板片段 + 静态资产（L0） | Jet 模板条件输出、`assets/*.css` 消费导出的 CSS 变量、区块预设引用内置组件 + 自身组件 |
| `examples/l1-marketing/` | 完整档（L1 + 预设 + 查询） | `migrations/001_init.sql` 建自己的 schema、容器查询三桶（size/theme/local）、多组件 |

安装（三档同形）：

```bash
cd examples/l0-style-only          # 或 l0-template-assets / l1-marketing
zip -r /tmp/styletile-1.0.0.zip manifest.json components   # 有 assets/ migrations/ 就一起带上
# 后台 → 插件管理 → 上传 zip → 启用
```

装完在工作台组件库里能看到新组件（类型 `plugin.<插件id>.<组件名>`），检查器里是 manifest
声明的控件；有 `presets` 的档位，区块库里会多出对应预设。三个示例的自动化验收在
`public/test/plugin/unit/example_tiers_test.go`（真实装 + 真实编译渲染，不是伪造）。

## 1. 能做什么

轻轨插件 = **一个 zip 数据包**，装的全是「数据与声明」，没有任何可执行代码。

| # | 能力 | 声明位置 | 边界与合法值 | 依据 |
|---|---|---|---|---|
| 1 | Jet 模板渲染 | `components/<file>.jet` | 与内置组件同一 CompositeLoader（`plugin/{id}/{file}` 命名空间）、同一受限全局函数集；Jet 默认转义 | `internal/templates/plugin_loader.go`、`internal/builder/pluginview.go` |
| 2 | 检查器控件声明 | `components[].props` | 7 种：`text` / `textarea` / `number` / `select`（必带非空 `options`）/ `color` / `media` / `unit`；键名 `[A-Za-z0-9_-]{1,60}`；单组件上限 40 个 | `internal/builder/plugincomp/plugincomp.go` `propKindWhitelist` |
| 3 | 样式声明 | `components[].styles.rules` | 8 类原语，见 §3；属性名 ~100 个白名单、值白名单、选择器白名单三重约束 | `internal/builder/style/` |
| 4 | CSS 变量导出 | `rules[].vars` | 把控件值写成 `--<name>` 挂在该规则的选择器上，供 `assets/*.css` 或其它声明用 `var()` 消费 | `internal/builder/style/compile.go` |
| 5 | 容器 / 主题查询 | `rules[].queries` | `kind=size`（尺寸查询，`@layer sky-auto`）/ `theme`（主题档位，`@layer sky-theme`）/ `local`（局部开关，`@layer sky-local`） | `internal/builder/core/css.go` |
| 6 | 静态资产 | `assets/*.css` | 按文件名序拼接，构建期注入产物 `@layer sky-plugin`；`@import` 被剥除、`</style` 被清洗 | `internal/module/plugin/service/plugin_runtime.go` |
| 7 | 区块预设 | `presets[]` | `document` 为 AST 片段数组，可引用内置 `core.*` 与本插件 `plugin.*`；顶级 ≤10 节点、深度 ≤8、ID 白名单唯一 | `internal/builder/plugincomp/plugincomp.go` |
| 8 | L1 自有数据表 | `migrations/` + `schemaVersion` | 安装时执行 `.sql`（文件名序）；执行器锁定 `search_path` 到 `plugin_<id>` 并拒绝危险模式（见 §2.7） | `internal/module/plugin/service/plugin_migrate.go` |
| 9 | 生命周期 | — | 上传 / 升级 / 启停 / 卸载；卸载 = `DROP SCHEMA plugin_<id> CASCADE` + 清注册行与存储目录 | `internal/module/plugin/service/plugin_service.go` |

> 插件组件与内置组件是**同一条编译管线**：同一模板集、同一 `CSSBuckets`、同一确定性约束。
> 差别只在「Go 能力面」，这正是下面一节的内容。

## 2. 不能做什么

### 2.1 不能携带新组件（最关键的一条）

**现象**：插件包里的组件不是「一份组件代码」，而是「模板 + 样式声明 + 控件声明」这三样数据；
内核组件（`core.*`）则是编译进二进制的 Go 包 —— 各组件包在 `init()` 里注册自己，
`internal/builder/builder.go` 用空导入把它们串起来（`internal/builder/builder.go`）。

**为什么不能反过来（插件带 Go 组件、运行时加载）** —— 两条硬理由，都是有意的取舍：

1. **确定性构建**（架构不变量 5）：产物必须由「Page Document + BuildContext + Registry + Compiler」
   唯一决定，同一输入恒同字节。运行时加载第三方 Go 代码意味着产物可能依赖「加载到了什么」，
   hash 寻址复用、stale 标记、回滚都会失去判定依据；Go 的 `plugin` 包还要求同环境同编译器构建，
   官方不建议用于生产。
2. **不执行第三方代码的安全边界**：访问面只读静态产物、访客请求不执行任何插件代码；
   控制面若在构建期执行插件编译产物，等于把任意代码执行权交给了 zip 分发链路
   （插件市场/第三方分发一旦开口，这条边界就没了）。

**这不是能力缺失，而是把「新组件」换成了另一种形态**：插件组件的表达力来自「声明」，
它同样能出现在组件库、同样能被检查器编辑、同样参与确定性编译、同样适配多端。

**想要新组件时的正确路径（按代价从低到高）**：

| 需求 | 走哪条路 | 说明 |
|---|---|---|
| 现有元素的重新组合 / 换皮 / 布局变体 | **轻轨插件**（本文） | 模板片段 + 样式声明就够，见 `examples/l0-template-assets/` |
| 需要数据进可视化 | 轻轨 + L2 内容源 | `collections.json` **尚未实现**（见 §2.8），当前拿不到集合数据 |
| 需要新检查器控件类型 / 新 CSS 原语 | **内核**（提 Issue / 贡献 PR） | 扩控件白名单或 style 引擎原语，插件与内置同时受益 |
| 需要 Go 逻辑、运行时行为、并发与资金 | **重轨模块**（`internal/module/`）或 0-D `runtimefragment` | 见 06-plugin-system.md §4.2 / §4.3；插件壳可提供展示与管理页 |
| 组件必须访问数据库 / 跨表 | 重轨模块 + 受限数据源接口 | 不变量 7：契约包声明受限接口，`builder/core` 持有契约，页面文档只存白名单绑定 |

### 2.2 其余限制（逐条给依据）

| 不能做的事 | 为什么 | 想要时的正确路径 |
|---|---|---|
| 运行任意 Go 逻辑 / 运行时代码 | 插件是编译期输入（06 §1.2 一票定调） | 0-D `runtimefragment`（**当前未对插件开放**）；需要写库的能力只能是重轨模块 |
| 新增检查器控件类型 | 控件类型白名单封闭（`propKindWhitelist`），越权类型在安装期拒绝 | 提 Issue 扩白名单，或改用 7 种原语的组合 |
| 任意 CSS 逃逸 | 属性名白名单、值白名单（封 `url()` 外联）、选择器 1~3 段类白名单 | 伪元素 / `@keyframes` / 字体等结构细节写进 `assets/*.css`（它不受 style 引擎限制，但仍被清洗 `@import` 与 `</style`） |
| 子代组合符 `>`、属性选择器、`nth-child(n)` | 放宽选择器组合面只增加越权命中风险，后代选择器已覆盖绝大多数需求 | 后代选择器（`.a .b`）+ `first-child` / `last-child` / `only-child` 三个结构伪类 |
| 覆盖内置组件或内置模板 | CompositeLoader 命名空间隔离（`plugin/{id}/`），路径层面不存在同名 | 用 `presets` 组合内置组件成新预设 |
| 直连数据库 / 跨表查询 | 页面文档只允许白名单绑定（不变量 4） | L2 内容源（未实现）或重轨模块 |
| 后台管理页与插件菜单 | `manifest.admin` **未实现**（能力缺口） | 等管理页注册链路对插件开放；当前把配置做成组件控件 |
| L2 内容源（`collections.json`） | **未实现**，硬依赖 0-A2 content 落地节奏（06 §13 P3） | 组件暂时只能渲染控件值与预设结构，不能渲染集合列表 |
| 携带可执行文件 / 外部资源 | zip 扩展名白名单只有模板/样式/资产/迁移/文档；`url()` 外联与 `@import` 一律封禁 | 外链由平台侧统一管理 |
| 未签名插件分发 | `manifest.signature` 仅预留字段，验证链路未实现 | 当前只在小范围可信分发 |

### 2.3 安装期会拒绝什么（失败时看到什么）

| 报错（前缀） | 触发条件 |
|---|---|
| `ErrInstallParse: 缺少 manifest.json` | zip 根目录没有 manifest.json |
| `ErrInstallParse: 插件 id "X" 非法` | id 不是「小写字母开头 + 字母数字下划线连字符」 |
| `ErrInstallParse: 插件版本 "X" 非法（期望 x.y.z）` | 版本不是 `x.y.z`（可带 `-rc1` 后缀） |
| `ErrInstallParse: 组件 X 的模板 Y 不在包内` | manifest 声明的模板文件没打进 zip |
| `ErrInstallParse: 组件 X: props "k" 的控件类型 "z" 不在白名单` | 控件类型不在 7 种内 |
| `ErrInstallParse: 组件 X: props "k" 为 select 但缺枚举选项` | `select` 没给 `options` |
| `ErrInstallParse: 组件 X: 样式声明非法: 规则 N ...` | 属性名/值/选择器不在白名单，`N` 是 `styles.rules` 的下标（从 0 起） |
| `ErrInstallParse: 规则 N: pseudo "hover" 走专用样式桶，不能同时声明 breakpoints` | 悬浮/按压规则没有断点维度 |
| `ErrUnsafePackage: 路径穿越拒绝 / 扩展名 "x" 不在白名单 / 文件过大` | zip 内有 `../`、绝对路径、未知扩展名或超限文件 |
| `ErrMigrationFailed: 迁移语句被安全策略拒绝（命中 ...）` | L1 SQL 出现 `DROP SCHEMA` / `CREATE ROLE` / `GRANT` / 访问 `public.` / 文件与系统目录等 |

## 3. 样式声明引擎参考

`styles` 段是一组 `rules`。每条规则 = 「在哪个选择器上」（`target` + `pseudo`）+
「什么条件下」（`when`）+「输出什么」（`decls` / `vars` / `bindings` / `queries` / `breakpoints`）。

```json
"styles": {"rules": [
  {"decls": [["display","flex"],["gap","12px"],["border-left-style","solid"],["border-left-width","4px"]],
   "vars": [{"name":"note-accent","from":"accent"}],
   "bindings": [{"prop":"padding","from":"padY"}],
   "breakpoints": {"mobile": [["padding","12px"]]}},
  {"when": "tone=success", "decls": [["border-left-color","#16a34a"]]},
  {"target": ".note__title", "pseudo": "first-child", "decls": [["margin-top","0"]]},
  {"pseudo": "hover", "decls": [["box-shadow","0 10px 28px rgba(0,0,0,0.16)"]]},
  {"pseudo": "hover-none", "decls": [["border-left-width","8px"]]},
  {"pseudo": "active", "decls": [["transform","scale(0.99)"]]},
  {"decls": [], "queries": [{"kind":"size","condition":"(width >= 480px)","decls":[["flex-direction","row"]]},
                            {"kind":"theme","container":"sky-theme","prop":"--sky-density","value":"compact","decls":[["padding","8px"]]}]}
]}
```

### 3.1 字段合法值

| 字段 | 类型 | 合法值 / 边界 |
|---|---|---|
| `target` | string | 空（组件根）或 1~3 段类选择器：`.badge`、`.head .title`（**后代语义**，不是同元素双类） |
| `pseudo` | string | `hover` / `hover-none` / `focus` / `active` / `focus-visible` / `focus-within` / `disabled` / `first-child` / `last-child` / `only-child` |
| `when` | string | `key=value`，两侧均为 `[A-Za-z0-9_-]`；`key` 必须是 manifest 声明过的 props 键，`value` 枚举等值匹配 |
| `decls` | `[["属性","值"], ...]` | 属性名白名单（布局/盒模型/视觉/排版/变换/过渡约 100 项）；值经统一白名单（长度 ≤500、封 `url()//外联`、禁引号/分号/花括号） |
| `vars` | `[{"name","from"}]` | `name`：`[a-z][a-z0-9-]{0,40}`（**不含前导 `--`**）；`from`：props 键。控件值为空则不导出 |
| `bindings` | `[{"prop","from","prefix?","suffix?"}]` | `prop` 属性白名单；`from` props 键；前后缀用于 `translateY(` + 值 + `)` 这类拼接 |
| `queries` | `[{"kind","condition?","container?","prop?","value?","decls"}]` | `kind=size` 需 `condition`（如 `(width >= 480px)`，只允许单条比较式）；`kind=theme`/`local` 需 `container`（如 `sky-theme`）+ `prop`（`--` 开头的自定义属性）+ `value`；`decls` 不能为空 |
| `breakpoints` | `{"tablet":[...], "mobile":[...]}` | 仅 `tablet` / `mobile`；**与 `hover` / `hover-none` / `active` 互斥** |

### 3.2 三条容易忽略的语义

1. **`hover` / `hover-none` / `active` 是专用桶，不是普通伪类**：`hover` 会被包进
   `@media (hover: hover)`（触屏整段不输出，避免「点一下卡住悬浮态」），`hover-none` 输出
   `@media (hover: none)` 的触屏等价形态，`active` 不包媒体查询（`:active` 在触屏同样触发）。
   **有悬浮形态就必须给触屏等价形态**，否则手机端这个功能等于不存在（详见 `AGENTS.md` 多端硬规则）。
2. **`vars` 是静态 CSS 拿到检查器值的唯一途径**：`assets/*.css` 是全局静态文件，
   不知道任何组件的 props；变量导出把它变成可继承的 CSS 变量（声明在组件根，子树内可继承、不外溢）。
3. **容器查询与断点是两套正交的响应式**：`breakpoints` 按**视口**分三端（内核固定的 1024/767 断点），
   `queries.kind=size` 按**组件所在容器**的宽度适配；后者在未声明 `container-type` 的容器内不匹配，
   自然降级为默认样式，零副作用。

## 4. manifest 字段参考

| 字段 | 必填 | 合法值 |
|---|---|---|
| `id` | ✅ | `^[a-z][a-z0-9_-]{1,60}$`；同时决定组件类型前缀 `plugin.<id>.` 与 L1 schema 名 `plugin_<id>` |
| `name` | ✅ | 显示名，≤60 字 |
| `version` | ✅ | `x.y.z`（可带 `-rc1`）；升级时旧版本目录被清理 |
| `components` | ✅ | 1~50 个；每个含 `name`（`^[a-z][a-z0-9_]{0,60}$`）/ `label`（≤40 字）/ `template`（`^[a-z0-9_-]{1,60}.jet$`，必须存在于包内 `components/`）/ `props` / `styles` |
| `presets` | ✖ | ≤100 个；`id` 白名单唯一、`label` ≤40 字、`category` 允许中英文、`document` 为 AST 数组 |
| `migrations` | ✖ | 目录名 `^[a-z0-9_-]{1,60}$`；目录内含 `*.sql` 且不能为空 |
| `schemaVersion` | ✖ | 整数；`0` = 无自有表。与已登记版本相同 → 幂等跳过，不同 → DROP 旧 schema 后跑全量 |
| `requires` | ✖ | 目前只记录 `core` 版本要求，**不做强制校验** |
| `signature` | ✖ | 预留字段，验签链路未实现 |

> `components[]` 是**内联在 manifest.json 里**的（不是每个组件一个子目录 manifest）——
> `components/` 目录只放 `.jet` 模板文件。

## 5. 三个示例逐档讲解

### 5.1 `l0-style-only`（纯样式声明）

一个组件、零 CSS 资产、零迁移、零预设；组件的每个像素都由 `styles.rules` 生成。
装完看到：组件库多出「提示条（纯样式声明）」，检查器 6 个控件（标题/正文/语义/强调色/
纵向内边距/悬浮位移）；切「语义」为「成功」时左边框与底色变绿（`when`），把「强调色」
改成橙色时标题颜色跟着变（`vars` + `var()`）。

### 5.2 `l0-template-assets`（模板片段 + 静态资产）

两个组件（卡片 + 角标）与一个预设。卡片模板用 `{{ if }}` 做条件输出：副标题传空串时整块不渲染；
卡片的 `accent` 控件导出为 `--nc-accent`，`assets/card.css` 用 `var(--nc-accent, #2563eb)` 消费它；
预设「三卡特性区」引用内置 `core.container` / `core.heading` 与本插件组件。
装完看到：组件库两个组件、区块库多出「三卡特性区」，插入后是一整块可编辑内容区。

### 5.3 `l1-marketing`（完整档）

两个组件 + 预设 + L1 迁移。安装时建 `plugin_campaignkit` schema 与 `campaigns` 表；
Hero 组件同时声明了三类容器查询（`size` / `theme` / `local`），券卡组件演示结构伪类与三个状态桶。
装完看到：组件库两个组件、区块库「营销 Hero 区块」；插件列表里该插件 `schema_version = 1`；
卸载后 `plugin_campaignkit` schema 连同表一起消失（`DROP SCHEMA ... CASCADE`）。

## 6. 排错速查

| 现象 | 先查这里 |
|---|---|
| 上传报错 | §2.3 的错误对照表，报错前缀指明阶段（解析 / 安全 / 迁移） |
| 装上了但组件库没有 | 插件是否「启用」；启停只在**下一次构建**生效 |
| 页面用了插件组件但构建失败「不可用（未安装或未启用）」 | 页面构建时该插件未启用；参照 §2.3 的表检查启停状态 |
| 「props 含未声明的键」 | 页面或预设里的 props 键没在 manifest 声明；`decodePluginProps` 只接受声明过的键 |
| 样式不生效 | `target` 是**后代**选择器（`.a .b` 而非 `.a.b`）；变量名在 `vars` 里不带 `--`，消费时写 `var(--name)` |
| 悬浮样式在手机上没效果 | 预期行为：`hover` 只输出给真悬浮设备；用 `hover-none` 给触屏等价形态 |
| 变量没导出 | 对应控件值当前为空（空值不导出），消费端用 `var(--name, 兜底值)` |
| 静态 CSS 里的 `@import` 不见了 | 平台主动剥除（外联通道）；改用平台统一管理的资源 |

## 7. 相关文档

- [06-plugin-system.md](./06-plugin-system.md) — 插件体系规范（权威）：三级能力分层、双轨制、安全边界、实施阶段
- [06-A-plugin-ecosystem-roadmap.md](./06-A-plugin-ecosystem-roadmap.md) — 轨道判定与首批插件规划
- [06-B-dual-track-adr.md](./06-B-dual-track-adr.md) — 双轨制 ADR
- `examples/l0-demo/` — 与 `go run ./cmd/plugin init <id>` 脚手架产物逐字节一致的最小示例
