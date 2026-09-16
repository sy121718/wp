# 06 · 插件体系规范 (Plugin System Spec)

> 状态：**设计定稿，分阶段落地中**。样式声明引擎（§6）已实现并合入主干
> （`internal/builder/style`，含 fuzz 回归资产）；其余部分按 §13 阶段计划推进。
> 本文档是插件体系的唯一权威设计来源，会话与实现冲突时以本文档为准，
> 本文与实际代码冲突时先改文档评审、再改代码。
>
> 插件作者的入口（能做什么 / 不能做什么、字段合法值、可安装示例、排错）见
> [06-E-plugin-authoring.md](./06-E-plugin-authoring.md)。

## 1. 定位与核心决策

### 1.1 产品定位

插件简易开发 + 可视化操作多样是 go_wp 的核心竞争力。目标：第三方（或本团队
其他成员）能以**一个 zip 数据包**为单位扩展建站能力——新组件、新区块预设、
新数据表、新内容源——经控制面上传，异步编译，原子生效，可卸载可回滚。

典型场景：营销插件。它带可视化组件（活动卡片、倒计时）、区块预设（营销
Hero）、自有数据表（活动/报名/优惠码）与内容源（campaigns 集合），一次安装
全部就位。

### 1.2 核心决策（一票定调）

**插件 = 编译期输入，不是运行时代码。**

Go 静态编译语言没有运行时加载 Go 代码的可靠机制（`plugin` 包依赖 cgo 与
同环境编译，官方不建议）。因此 go_wp 的插件承载「数据与声明」——Jet 模板、
样式声明 schema、表单 schema、迁移 SQL、静态资产——全部在构建期被消费进
不可变 Artifact；运行时零插件代码。收益：

- 插件改动永不触碰运行时（访问面仍只读静态直出，不变量 1 不破）；
- 生效与回滚复用发布管线的原子切换（symlink + rename，§10）；
- 确定性构建约束自动覆盖插件路径（同一插件版本 + 同一 Page Document
  产出相同字节，Artifact hash 寻址复用不受影响）。

### 1.3 非目标

- 不做任意 Go 逻辑的热加载（Yaegi 解释器 / Wasm 沙箱仅记录为 §4.3 演进方向）；
- 不做 WP 式 `add_action`/`add_filter` 全局钩子网（不可知顺序即不可维护）；
- 不做 wp_options 式序列化万能桶（所有数据要么结构化表、要么 manifest 声明）；
- 不引入任何 AGPL 代码（Grafana 借鉴概念不抄实现）。

## 2. 生态调研结论（决策依据）

| 项目 | 机制 | 借鉴点 |
|---|---|---|
| Hugo (~80k★, Apache-2.0) | 主题 = 编译期插件；模板按「项目 → 主题」逐级回退的虚拟联合文件系统 | **CompositeLoader 原型**（§7）：内置模板优先，插件按序回退，同名不覆盖 |
| Grafana (~68k★, AGPL) | panel/datasource/app 三类插件 + plugin.json + 签名 | **插件工程化**（§5.4）：manifest 分节、`signature` 字段预留、插件分类 |
| Traefik (~55k★, MIT) | Yaegi 解释器 / Wasm 双引擎运行时加载 Go 插件 | 「运行时逻辑」第三轨的可行性证据（§4.3，暂不落地） |
| GrapesJS (~24k★, MIT) | Block Manager：块 = 预组合 HTML/CSS 片段，插件注册 | **presets 块预设**（§5.2）：格式极简，验证过可撑大生态 |
| PocketBase (Go) | collection → 自动建表 + Go 迁移函数 + Go/JS 双轨扩展 | **双轨制先例**（§4）与「声明 → 建表」路径（§8） |
| Strapi / Directus (Node) | content-type schema.json / 建表 + 元数据表双轨 | **collections.json 声明式内容源**（§9）与「DDL + 注册」双轨（§8） |
| WordPress（反面教材） | 前缀表全局命名空间、options 序列化垃圾、运行时 include | 每条屎山在 §8.3 有对应解药 |

关键判断：**Go 后端 + 页面级可视化编辑 + 插件安装三者兼备的建站项目，
开源生态基本空白**（Hugo 无控制面、Grafana 非 Web 建站、Ponzu 已停更）。
go_wp 的定位在生态空位上，各取四家「局部最优」拼成完整参考系。

## 3. 三级能力分层

「扩展表」的前提判断：**不是所有插件都需要建表**。营销插件是复合体，
拆开各归其层，层与层可独立存在：

| 层 | 能力 | 表归属 | 营销场景举例 | 生效方式 |
|---|---|---|---|---|
| **L0 展示层** | 组件模板 / 样式 / 区块预设 | 无自有表（manifest + Page Document） | 倒计时、优惠卡、营销 Hero 区块 | 上传即编译即生效 |
| **L1 数据层** | 插件自有表 + 后台管理页 | 独立 PG schema（`plugin_<name>`） | 活动表、报名表、优惠码表 | 安装时建 schema + 跑迁移 |
| **L2 内容源层** | 数据进可视化绑定 | 只读注册（CollectionSource 白名单） | 「活动列表」组件渲染 campaigns | 注册后检查器可绑定 |

大多数可视化组件插件停在 L0；营销系统走到 L1+L2。

## 4. 双轨制

### 4.1 轻轨（zip 数据插件，热装）

纯数据包：模板 + schema + 迁移 + 内容源 + 资产（§5）。上传 → 校验入库 →
asynq 异步编译 → 原子切换生效（§10）。覆盖约 80% 营销需求（落地页、活动
展示、表单收集、内容列表）。

### 4.2 重轨（Go 模块插件，编译期）

强业务逻辑（支付回调、优惠码核销、并发扣库存）走编译期模块——即现有
`internal/module/` 规范（参照 `member` 模块骨架：contract/model/service/
dto/enums + 自装配）。重轨模块**必须遵守同样的纪律**：

- 自有表仍放独立 schema（`plugin_<name>` 前缀转换，模块 dao 一处封装）；
- 数据进可视化仍走 CollectionSource 注册（不得让 Page Document 直连表）；
- 后台入口仍经迁移 seed 权限点 + 菜单（030/031 模式）。

两轨共享同一个 schema 隔离与注册体系——营销系统可从轻轨起步（展示+收集），
逻辑重的部分长成重轨模块，数据与内容源无缝衔接。

### 4.3 第三轨（演进方向，暂不落地）

Traefik 的 Yaegi（Go 解释器）/ Wasm 沙箱证明「zip 内带受限逻辑」可行。
但性能（解释执行 1/2~1/20）、符号表暴露面、调试体验成本高，且 0-D
`runtimefragment`（HTMX Fragment）能覆盖大部分运行时逻辑需求。**留作
Fragment 落地后的再评估项，首发不做。**

## 5. 插件包规范（zip）

### 5.1 目录结构

```text
marketing-plugin.zip
├── manifest.json          # 唯一入口：插件元数据 + components[]（组件声明内联在此）+ presets[]
├── components/            # L0：只放 Jet 模板文件；声明（props/styles）在 manifest.components[]
│   ├── campaign_list.jet
│   └── coupon_card.jet
├── assets/                # 静态资产：*.css 按文件名序拼接，注入产物 @layer sky-plugin
├── migrations/            # L1：001_init.sql（CREATE SCHEMA plugin_<id> + 建表）…（版本化）
├── collections.json       # L2：内容源注册（§9）—— **未实现**（缺口见 §11A / §13）
├── admin/                 # L1：后台管理页 —— **未实现**（缺口见 §11A / §13）
└── fragments/             # 运行时片段 —— 0-D 落地前不对插件开放
```

> 组件是**内联在 `manifest.json` 的 `components[]`**，不是「每个组件一个子目录 manifest」——
> `components/` 目录只承载 `.jet` 模板文件（安装期校验「声明了模板但包里没有 → 拒绝」）。
> 手写插件的完整字段参考与三个可安装示例见 [06-E-plugin-authoring.md](./06-E-plugin-authoring.md)。

### 5.2 presets（区块预设）

对标 GrapesJS 的块注册格式，落成声明：

```json
"presets": [{
  "id": "marketing-hero",
  "label": "营销 Hero 区块",
  "category": "营销区块",
  "thumbnail": "assets/hero.svg",
  "document": { "type": "core.container", "children": [ /* AST 片段 */ ] }
}]
```

插入 = 往 Page Document 塞一段 AST。workbench 已有「区块预设：预组合的
全局 section，一键插入整个容器」概念，本设计只把注册入口开放给插件。

### 5.3 manifest.json 骨架

```json
{
  "id": "marketing",
  "name": "营销系统",
  "version": "1.2.0",
  "requires": { "core": ">=0.5" },
  "signature": null,
  "components": ["components/campaign_list", "components/coupon_card"],
  "presets": [ /* §5.2 */ ],
  "migrations": "migrations/",
  "collections": "collections.json",
  "admin": { "menu": [{ "title": "营销", "path": "/admin/plugin/marketing" }] }
}
```

### 5.4 安全分发（预留）

- `signature` 字段预留给插件市场公钥验签；未签名插件仅 dev 模式可装；
- 不引入任何 AGPL 代码；借鉴 Grafana 概念（plugin.json 分节/签名）不抄实现。

## 6. 样式声明引擎（**已落地**）

`internal/builder/style` 包：把「检查器 props 值 + manifest styles 声明」
确定性编译进 `core.CSSBuckets`（与内置组件同一管线、同一确定性约束）。

### 6.1 统一受控规则原语

一个 `Rule` 表达五类能力，解决声明式 CSS 的表达力鸿沟：

| 能力 | 字段 | 对标内置组件的既有模式 |
|---|---|---|
| 属性绑定 | `bindings: [{prop, from, prefix?, suffix?}]` | 检查器控件值 → CSS 声明 |
| 变量导出 | `vars: [{name, from}]` | 控件值 → CSS 变量 `--<name>`，供 `assets/*.css` 与同规则 `var()` 消费 |
| 变体 | `when: "style=raised"`（枚举等值） | button 的 solid/outline/ghost |
| 状态 | `pseudo` 枚举（含 `hover` / `hover-none` / `active` 专用桶与 `first-child` 等结构伪类） | button 的 Normal/Hover 双态、触屏等价形态与按压反馈 |
| 响应式 | `breakpoints: {tablet/mobile: [...]}` | 三端桶（core.Breakpoint*） |
| 容器 / 主题查询 | `queries: [{kind: size/theme/local, ...}]` | AddContainer（sky-auto）/ AddThemeQuery（sky-theme）/ AddStyleQuery（sky-local） |
| 子元素 | `target: ".badge"`（1~3 段类，后代语义） | 容器内子结构样式 |

### 6.2 三重安全约束（无任意 CSS 逃逸面）

1. **属性名白名单**（`style.IsSafeProp`）：约 90 个安全属性，排除
   behavior/binding/content 等注入载体；
2. **值统一走 `core.IsSafeCSSValue`**：全组件共用的唯一值白名单入口
   （字符集 + 封禁 url() 外联 + 长度上限），不重造；
3. **选择器受控拼装**：target/伪类/nodeID 三面白名单
   （`IsSafeTarget`/`IsSafePseudo`/`IsSafeNodeID`，与 core.ValidateNodeID
   同规则），杜绝选择器注入越权命中其他组件。

### 6.3 防御深度与确定性

- 两道校验：上传安装时 `Schema.Validate()`（坏 schema 拒绝安装）；
  构建期 `Compile()` 对绑定值（运行期用户输入）再过值白名单，不安全值
  返回错误（构建失败优于静默丢样式）；
- 确定性：声明用 slice 保序 + 复用 `CSSBuckets.Add`，产物与内置组件
  字节级同构；
- **伪类分派遵循多端硬规则**：`hover` 进 `AddHover`（规则包进 `@media (hover: hover)`，
  触屏整段不输出，防粘滞 hover）、`hover-none` 进 `AddHoverNone`（触屏等价形态）、
  `active` 进 `AddActive`（不包媒体查询 —— `:active` 在触屏同样触发，是移动端唯一可靠的
  按压反馈）；专用桶没有断点维度，与 `breakpoints` 互斥（校验期拒绝，不静默丢样式）；
- 不在引擎内表达的（有意取舍）：伪元素（`::before/::after`，`content` 是注入载体）、
  `@keyframes` 与复杂动画、`@font-face` / 外联资源、子代组合符（`>`）与任意选择器 ——
  前两类写进插件包 `assets/*.css` 静态资产（打包层 scope 处理），后两类是安全边界的代价。
  完整清单与「想要时走哪条路」见 [06-E-plugin-authoring.md](./06-E-plugin-authoring.md) §2。
- 已含 12 语义/注入用例 + fuzz（30s 720 万次执行零失败）；开发中 fuzz
  实际抓出两个真 bug（后代选择器缺空格、nodeID 未校验）并修复，失败
  样本入库 `testdata/fuzz` 作回归资产。

## 7. 模板加载：CompositeLoader（命名空间合并）

`templates` 包 Composite Loader，采用**命名空间合并**（终态设计，强于
Hugo overlay 回退）：内置模板与插件模板路径空间不相交——

```text
内置模板：  "{name}.jet"                      （如 heading.jet）
插件模板：  "plugin/{pluginID}/{file}.jet"    （如 plugin/marketing/campaign_card.jet）
```

路径前缀 `plugin/{pid}/` 路由到对应插件包的 `components/` 目录。插件永远
不可能覆盖内置模板——比「先到先得」更强的安全基线（从路径层面杜绝同名）。

- 实现 `jet.Loader` 接口（`Exists` + `Open`）；`embedLoader` 为内置来源，
  `compositeLoader` 按 `plugin/{pid}/` 前缀路由到启用插件的 fs.FS；
- 无启用插件：构建/预览走内置 embed 单例（`NewEmbeddedComponentSet`，
  hot path 缓存）；有插件：按任务组装 `NewCompositeSet`（内置 + 插件），
  确定性：同一插件版本集 → 同一模板内容；
- 组件 palette 注入：workbench 的 `wb-schemas`（ComponentSchemas 产物）
  合并插件组件 schema，组件库自动出现新组件（`registerPluginPalette`）。

## 8. 表扩展：独立 PG Schema + 迁移执行器

### 8.1 机制

- 每插件一个 PG schema：`plugin_marketing`（表名零冲突）；
- 插件 zip 携带版本化迁移 SQL（`migrations/001_init.sql` 内含
  `CREATE SCHEMA IF NOT EXISTS plugin_marketing` + 建表）；
- `plugin_registry` 表记账 `(plugin_id, schema_version)`；升级走增量迁移；
- 安装/升级执行器扩展现有迁移器（`public/migrations/` 版本化 SQL +
  register.go 注册 + 幂等模式），迁移源支持 io/fs（zip 解包后传入）；
- GORM 访问：`db.Table("plugin_marketing.campaigns")` 显式前缀，封装在
  插件 dao 一处（延续模块表隔离纪律：service 不碰跨模块表）。

### 8.2 卸载

`DROP SCHEMA plugin_marketing CASCADE` + registry 除名。一行干净卸载。

### 8.3 WP 屎山对照（解药表）

| WP 屎山 | go_wp 解药 |
|---|---|
| `wp_` 前缀全局命名空间冲突 | 独立 schema，表名零冲突 |
| `wp_options` 序列化垃圾场 | 结构化表或 manifest 声明，无万能桶 |
| 插件任意 dbDelta 建表无人管 | 版本化迁移 + registry 记账 |
| 卸载残留脏表 | DROP SCHEMA CASCADE |
| 插件互读对方表 | 跨插件只走 CollectionSource 契约 |

## 9. 内容源：CollectionSource 白名单

插件数据进可视化的**唯一通道**（严格延续架构不变量 4：Binding 不是
Query DSL）：

```json
{"collections": [{
  "name": "campaigns",
  "source": "plugin_marketing.campaigns",
  "fields": ["id", "title", "cover", "discount", "starts_at"],
  "filters": [{"key": "status", "enum": ["running", "ended"]}],
  "orderBy": ["starts_at", "id"]
}]}
```

- Page Document 中插件组件只存白名单绑定
  （`{"collection":"campaigns","filter":{"status":"running"}}`）；
- 构建期由插件注册的解析器展开为静态数据（对齐 0-A2 content 的
  ContentResolver 注入模式）；
- 字段/过滤/排序全部白名单枚举，无任意 SQL、无任意 endpoint。

## 10. 生效流水线（复用发布管线）

```text
上传插件 zip
  → 校验（manifest/schema/styles 结构，坏包拒绝）
  → 入库（模板/资产进插件存储；L1 跑迁移；registry 记账）
  → asynq 入队：重建受影响页面（stale 传播编排）
  → Build Worker：CompositeLoader 拉模板 + style 引擎编译 → 新 Artifact（hash 寻址）
  → Activate()：临时 symlink + 原子 rename 切换访问面指针
  → 前台请求即命中新版本；旧版本保留可回滚
```

与现有 page 发布流水线完全同构，只是编译输入多了一维「插件集」。
运行时动态能力（登录态、表单提交）走 0-D `runtimefragment`，不在本管线。

## 11A. 内置组件与插件组件的能力边界（REG-004）

> 结论先行：**渲染同管线、样式同白名单、模板同约束**；差距只在「Go 能力面」——
> 插件是编译期数据输入，凡需要 Go 逻辑、运行时行为或超出声明白名单的能力，
> 插件组件一律没有。

### 11A.1 为什么轻轨插件不能携带新组件（VIS-015）

**机制事实**：内核组件（`core.*`）是编译进二进制的 Go 包 —— 各组件包在自己的 `init()`
里注册，`internal/builder/builder.go` 用空导入把它们串起来。插件包的「组件」不是代码，
而是**三份声明**（Jet 模板 + props 控件 schema + styles 样式规则），由内核的
`plugincomp` / `style` / CompositeLoader 消费成与内置组件同形的编译输入。

所以「插件带新组件」在 go_wp 里的真实含义是**换一种形态表达组件**，而不是不能扩展组件；
但若要求的是「插件里写 Go 代码，安装后在构建期/运行期执行它」，那就是另一回事 —— 两条硬理由否掉它：

1. **确定性构建（架构不变量 5）**：产物必须由「Page Document + BuildContext + Registry + Compiler」
   唯一决定，同一输入恒同字节。运行时加载第三方 Go 代码意味着产物取决于「当时加载到了什么」，
   hash 寻址复用、stale 标记、发布回滚都失去判定依据；Go 官方的 `plugin` 包还要求同环境同编译器
   构建（cgo + 精确版本匹配），官方不建议用于生产。
2. **「不执行第三方代码」的安全边界**：访问面只读静态产物（不变量 1），控制面若在构建期执行
   插件携带的代码，等于把任意代码执行权交给了 zip 的分发链路。插件生态一旦对上第三方开放，
   这道边界就再也没有第二个落点。

这是**有意的取舍**（06 §1.2 一票定调），不是待偿还的技术债；把它讲清楚，是为了让插件作者知道
能力天花板在哪里、以及撞到天花板时该往哪走。

### 11A.2 想要新组件时的正确路径

| 需求 | 路径 | 代价 / 现状 |
|---|---|---|
| 既有元素的重新组合、换皮、布局变体 | **轻轨插件**（模板 + 样式声明 + presets） | 最低，上传即用；三个示例覆盖三档表达力（见 §11A.3） |
| 需要数据进可视化绑定 | 轻轨 + L2 内容源 | `collections.json` 未实现（§13 P3，硬依赖 0-A2） |
| 需要新的检查器控件类型 / 新的 CSS 原语 | **内核**（Issue / PR）：扩 `propKindWhitelist` 或 style 引擎原语 | 一次投入，内置组件与所有插件同时受益 |
| 需要 Go 逻辑、运行时行为、并发或资金操作 | **重轨模块**（`internal/module/`，§4.2）或 0-D `runtimefragment` | 编译期接入、走模块纪律；插件壳可提供展示与管理页 |
| 组件必须访问数据库 / 跨表 | 重轨模块 + 受限数据源接口（不变量 7） | 契约包声明只读接口，`builder/core` 持有契约 |

判定速查：**「这些像素用声明能描出来吗」→ 能就是轻轨插件；描不出来但只是差一种 CSS 原语 → 提内核需求；
差的是执行逻辑或数据读写 → 重轨模块。**

### 11A.3 能力清单的权威版与示例

下面两张表是速查版；**逐条合法值、报错文案、排查路径**的权威版在
[06-E-plugin-authoring.md](./06-E-plugin-authoring.md)（含三个可直接安装的示例）：

| 示例 | 档位 | 展示的表达力 |
|---|---|---|
| `examples/l0-style-only/` | 纯样式声明 | 绑定 / 变体 / 伪类三态 / 结构伪类 / 变量导出 / 断点 / 容器与主题查询 |
| `examples/l0-template-assets/` | 模板片段 + 静态资产 | Jet 模板条件输出 + `assets/*.css` 消费导出变量 + presets |
| `examples/l1-marketing/` | 完整档 | L1 迁移（自有 schema）+ 容器查询三桶 + presets + 多组件 |
| `examples/l0-demo/` | 最小样例 | 与 `go run ./cmd/plugin init <id>` 脚手架产物逐字节一致 |

三档示例的自动化验收（真实安装 + 真实编译渲染）见 `public/test/plugin/unit/example_tiers_test.go`。

### 插件组件能用的能力（全部已实现）

| 能力 | 说明 |
|---|---|
| Jet 模板渲染 | 与内置组件同一 CompositeLoader（§7）与受限全局函数集，Jet 默认转义；`{{ .V.字段 }}` 走检查器 props |
| 样式声明（§6） | 与内置组件同一 style 引擎：属性绑定 / 变量导出 / 变体 / 伪类（含 `hover`、`hover-none`、`active` 三个专用桶与结构伪类）/ 响应式断点 / 容器与主题查询 / 子元素 target，编译进 `core.CSSBuckets`，同一确定性约束 |
| props 控件白名单 | 7 种：text / textarea / number / select / color / media / unit（`plugincomp.propKindWhitelist`） |
| 区块预设 presets | 与内置组件同库注册，预设 AST 可引用 `core.*` 内置组件 |
| L1 迁移 | 安装/升级时执行 `CREATE SCHEMA plugin_<id>` + 版本化 SQL，级联卸载 |
| 静态资产 | assets/ 打包进产物 |
| 生命周期 | 上传 / 版本 / 启停 / 卸载（plugin registry 记账），与内置组件同为编译期输入 |

### 插件组件没有的能力（与内置组件的差距）

| 差距 | 原因 |
|---|---|
| 任意 Go 逻辑 / 运行时代码 | §1.2 一票定调：插件 = 编译期输入；运行时行为等 0-D runtimefragment 能力对插件开放（当前未开放） |
| 新增检查器控件类型 | 控件类型白名单封闭（越权类型在 manifest 校验期拒绝），扩展需先扩检查器原语 |
| 任意 CSS / 外联资源 | §6.2 三重约束对插件与内置同样生效：属性名白名单（布局/盒模型/视觉/排版/变换约 100 项）、值白名单封 `url()` 外联、选择器 1~3 段类白名单 |
| 伪元素、`@keyframes`、`@font-face` | 不在声明引擎内表达（`content` 是字符串注入载体）；写进 `assets/*.css` 静态资产 |
| 子代组合符（`>`）、属性选择器、`nth-child(n)` | 放宽选择器组合面只增加越权命中风险；后代选择器 + `first-child` / `last-child` / `only-child` 已覆盖绝大多数需求 |
| 覆盖内置模板/组件 | CompositeLoader 命名空间隔离（plugin/{pid}/），路径层面不存在覆盖 |
| 直连数据库 / 跨表查询 | 只能经 §8 迁移建自有 schema 表；Page Document 数据只走 CollectionSource 白名单（§9） |
| 后台菜单/管理页 | manifest `admin` 字段未实现（**能力缺口**，依赖后台菜单注册链路对插件开放） |
| L2 内容源 | `collections.json` 未实现（**能力缺口**，硬依赖 0-A2 落地节奏，见 §13 P3） |

> 两个能力缺口（admin 页 / collections）在 `examples/l0-demo/README.md` 与本文件的
> §11A.2 同步标注；L1/L2 声明式片段在缺口闭合前不得在示例或脚手架中伪造。
> 已实现的 L0 三档示例不在此列 —— 它们的每一行声明都由 `example_tiers_test.go` 真实验收。

## 11. 安全边界汇总

| 面 | 防线 |
|---|---|
| CSS 注入 | §6.2 三重约束（属性白名单/值白名单/选择器受控） |
| 模板注入 | 插件 Set 使用与内置一致的受限全局函数集（injectGlobals 三函数），禁为插件扩展 safe 类过滤器；Jet 默认转义 |
| schema 结构攻击 | 解析器 fuzz + manifest 尺寸/嵌套限制 |
| SQL | 迁移 SQL 仅安装/升级时由执行器跑（管理面权限）；CollectionSource 白名单展开，无用户输入拼接 |
| 选择器越权 | target 白名单（类选择器 1~3 段），不可命中其他组件/页面元素 |
| 分发 | manifest `signature` 预留公钥验签；未签名仅 dev 可装 |
| 许可证 | 不引入 AGPL 代码 |

## 12. 与架构不变量的衔接

| 不变量 | 插件体系的遵守方式 |
|---|---|
| 1. 控制面/访问面分离 | 插件只在构建期被消费；访客请求零插件代码零查库 |
| 2. 两条发布路径共享管线 | 插件编译进同一 Publish Compiler → ArtifactStore → PublicationStore |
| 4. Binding 不是 Query DSL | 插件数据只经 CollectionSource 白名单（§9） |
| 5. 确定性构建 | 同插件版本 + 同文档 = 相同字节；style 引擎与 CompositeLoader 均复用确定性管线 |
| 模块表隔离 | 插件 schema 隔离 + dao 一处封装，跨插件只走契约 |

## 13. 实施阶段计划

| 阶段 | 内容 | 状态 |
|---|---|---|
| **P0（已完成）** | 样式声明引擎 `internal/builder/style`（§6） | ✅ 已合入主干 |
| **P1（已完成）** | `plugin` 模块（上传/版本/启停/卸载 + registry + zip 安全解析 + manifest 校验）；CompositeLoader 命名空间合并（§7）；plugin.* 节点编译分发（builder 内核契约）；组件注册进 workbench palette；编译同源注入（dashboard 预览 + page 构建）；后台插件管理页 | ✅ 已合入主干 |
| **P2（已完成）** | L1 数据层迁移执行器（插件 schema 创建/版本管理/级联卸载 + registry 记账，真实 PG 测试）；presets 区块预设注册进组件库（§5.2，对标 GrapesJS Block Manager） | ✅ 已合入主干 |
| **P2.5（已完成）** | 轻轨插件表达力收口（VIS-015）：style 引擎补齐变量导出 / 容器与主题查询 / 触屏三态分派 / 结构伪类 / 单项边框属性；三档可安装示例（`examples/l0-style-only`、`l0-template-assets`、`l1-marketing`）；作者指南 06-E | ✅ 已合入主干 |
| **P3** | L2 CollectionSource（对齐 0-A2 content 落地节奏，**硬依赖 0-A2 未落地，缓**）；脚手架 `plugin init`（对标 strapi generate） | 脚手架 ✅ 已合入；CollectionSource 待做 |
| **演进** | 第三轨（Yaegi/Wasm 受限逻辑）；插件市场与签名分发 | 评估项 |

## 14. 术语表

| 术语 | 定义 |
|---|---|
| 插件（Plugin） | 一个 zip 数据包，承载编译期输入：模板/schema/迁移/资产，无 Go 代码 |
| 轻轨 / 重轨 | zip 数据插件（热装）/ Go 模块插件（编译期），共享 schema 与注册体系 |
| L0 / L1 / L2 | 展示层（无表）/ 数据层（自有 schema）/ 内容源层（白名单注册） |
| styles 声明 | manifest 中的样式规则集，由 style 引擎编译进 CSSBuckets |
| CompositeLoader | 命名空间合并的模板加载器：内置 `{name}.jet` + 插件 `plugin/{pid}/{file}.jet`，路径层面杜绝覆盖 |
| CollectionSource | 插件数据进可视化的唯一白名单通道 |
| presets | 区块预设：插件声明的预组合 AST 片段，一键插入组件库 |
| plugin_registry | 插件记账表：plugin_id、版本、schema 版本、启停状态 |
