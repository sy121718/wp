# 06 · 插件体系规范 (Plugin System Spec)

> 状态：**设计定稿，分阶段落地中**。样式声明引擎（§6）已实现并合入主干
> （`internal/builder/style`，含 fuzz 回归资产）；其余部分按 §13 阶段计划推进。
> 本文档是插件体系的唯一权威设计来源，会话与实现冲突时以本文档为准，
> 本文与实际代码冲突时先改文档评审、再改代码。

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
├── manifest.json          # 唯一入口声明：版本/L1迁移清单/L2内容源/组件/presets/后台
├── migrations/            # L1：001_init.sql（CREATE SCHEMA + 建表）…（版本化）
├── collections.json       # L2：内容源注册（§9）
├── components/            # L0：可视化组件
│   ├── campaign_list/
│   │   ├── manifest.json  # props schema（ct 语义）+ styles 声明（§6）
│   │   └── list.jet       # Jet 模板
│   └── coupon_card/
├── presets/               # L0：区块预设（对标 GrapesJS Block Manager）
├── admin/                 # L1：后台管理页（Jet 模板 + HTMX，菜单经 manifest 注册）
├── fragments/             # 运行时片段（表单提交/剩余名额，0-D 落地后启用）
└── assets/                # 静态资产（css/js/图片，打包进产物）
```

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
| 变体 | `when: "style=raised"`（枚举等值） | button 的 solid/outline/ghost |
| 状态 | `pseudo: "hover"`（枚举） | button 的 Normal/Hover 双态 |
| 响应式 | `breakpoints: {tablet/mobile: [...]}` | 三端桶（core.Breakpoint*） |
| 子元素 | `target: ".badge"`（1~3 段类） | 容器内子结构样式 |

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
- 复杂动画/特殊结构不在引擎内表达：走插件包 `assets/*.css` 静态资产
  （打包层 scope 处理）；
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
| **P3** | L2 CollectionSource（对齐 0-A2 content 落地节奏，**硬依赖 0-A2 未落地，缓**）；脚手架 `plugin init`（对标 strapi generate） | 待做 |
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
