# 02 · 领域模型

> 本文覆盖 go_wp 的核心领域概念：Project / Blueprint / Page / ContentTemplate / PresentationInstance / DocumentSnapshot，以及它们依赖的 AST（ThemeNode）、Binding/BuildContext 协议和 Component Registry。所有协议定义集中在此文档，便于一次性审视封闭联合类型。

## 1. Project、Blueprint 与 Page

### 1.1 Project

Project 是站点级工程边界：

```ts
interface Project {
  id: string
  name: string
  settings: ProjectSettings
}

interface ProjectSettings {
  schemaVersion: number
  colors: {
    primary: string
    secondary: string
    text: string
    background: string
  }
  typography: {
    bodyFontFamily: string
    headingFontFamily: string
    baseFontSize: string
  }
  layout: {
    contentMaxWidth: string
  }
}
```

Project Settings 是页面构建输入。它变化时只把实际引用这些值的 Page 标记为 stale，不在访问时解析。

### 1.2 Blueprint

Blueprint 是创建 Page Document 的版本化初始化输入，不是 Page 的父类，不是 CMS 展示模板，也不参与构建期或访问时渲染。它可以保存一棵完整的初始 AST，但其职责在 Page 创建成功时即结束：

Blueprint 本身具有草稿→发布的版本化流程：编辑操作修改 `blueprints.draft_document`，发布操作基于当前 `draft_document` 生成新不可变版本写入 `blueprint_versions`。`draft_version` 是草稿迭代计数器，每次发布生成的新版本号等于 `draft_version`。发布后 `draft_document` 可继续编辑，`blueprint_versions` 的历史版本不可变。

```ts
interface BlueprintDocument {
  schemaVersion: number
  id: string
  name: string
  kind: PageKind
  root: ContainerNode
}
```

冻结规则：

- Blueprint 每个版本不可变。
- Blueprint 只能进入 `CreatePage` 或显式升级命令，Publish Compiler 不接收 Blueprint 作为输入。
- 创建 Page 时复制完整 Blueprint AST，并为复制树递归生成新 Node ID；创建结果必须是一份不再依赖 Blueprint 的完整 Page Document。
- Page 记录 `source_blueprint_version_id` 仅用于审计，不用于查找结构、解析 Binding 或决定发布结果。
- Blueprint 后续修改不自动改变任何 Page，也不自动触发页面重建。
- “升级到新 Blueprint”不是继承关系，而是显式迁移：先生成差异预览，用户确认后用一个 Command 修改 Page Document。

### 1.3 Page 与 Page Document

Page 是发布聚合根：它把一个 `PageKind`、一个受约束的 CMS 内容目标、一份独立 Page Document 和一个规范 URL 组合成可单独发布的页面实例。CMS 实体本身不能直接引用 Blueprint 作为访问时模板。

```ts
type PageKind =
  | 'home'
  | 'page'
  | 'article'
  | 'product'
  | 'category'
  | 'tag'
  | 'archive'
  | 'search'
  | 'notFound'

type ContentTarget =
  | { type: 'none' }
  | { type: 'page'; id: string }
  | { type: 'article'; id: string }
  | { type: 'product'; id: string }
  | { type: 'category'; id: string }
  | { type: 'tag'; id: string }

type PageContentContract =
  | { kind: 'home'; contentTarget: { type: 'none' } }
  | { kind: 'page'; contentTarget: { type: 'page'; id: string } }
  | { kind: 'article'; contentTarget: { type: 'article'; id: string } }
  | { kind: 'product'; contentTarget: { type: 'product'; id: string } }
  | { kind: 'category'; contentTarget: { type: 'category'; id: string } }
  | { kind: 'tag'; contentTarget: { type: 'tag'; id: string } }
  | { kind: 'archive'; contentTarget: { type: 'none' } }
  | { kind: 'search'; contentTarget: { type: 'none' } }
  | { kind: 'notFound'; contentTarget: { type: 'none' } }

interface PageDocument<K extends PageKind = PageKind> {
  schemaVersion: number
  id: string
  projectId: string
  kind: K
  settings: PageSettings
  root: ContainerNode
}

interface PageSettings {
  title?: string
  description?: string
  noIndex?: boolean
}
```

`PageContentContract` 是封闭判别联合，不是可扩展字符串映射。Application Service、Validator 和数据库约束必须共同拒绝不匹配组合，例如 `kind: 'product'` 配 `contentTarget.type: 'article'`，以及任何需要目标却缺少 `id` 的记录。

约束：

- Page 控制面记录的 `kind` 必须等于 `PageDocument.kind`，构建时不得自动改写或推断。
- `PageDocument.id` 与 Page 控制面记录一一对应。
- Blueprint 来源是 Page 控制面审计元数据，不进入 Page Document，也不参与渲染语义和 source hash。
- URL 只属于 Page 控制面记录，并通过 BuildContext 注入；Page Document 不保存第二套 canonical path。
- Page Document 不保存 CMS 内容快照；不可变内容快照只进入 Page Artifact 的 BuildInputManifest。
- 一个 Article 或 Product 若存在公开详情 URL，必须通过 PresentationInstance 自动发布，不单独创建手工 Page；发布流程分别是 `Article → PresentationInstance → DocumentSnapshot → Artifact` 与 `Product → PresentationInstance → DocumentSnapshot → Artifact`。
- 一个 CMS 内容默认最多绑定一个 canonical PresentationInstance。若需要多个展示页面，必须明确声明 canonical/noIndex，防止重复内容。
- Home 和 404 在一个 Project 内各最多一个 active Page。
- `archive` 只消费 Registry 定义的集合输入，不伪装成某个 CMS 实体详情页；`search` 只定义规范化 Search Shell，查询词不属于 Page identity；`notFound` 不绑定 CMS 实体。

### 1.4 Product Page Publishing（via PresentationInstance）

Product Detail 不再作为独立 Page 发布，而是通过 PresentationInstance 自动完成：

```text
Product Revision
+
ContentTemplate (product-detail, v5)
+
DocumentSnapshot (已解析 AST + 已固化数据)
+
Typed BuildContext
→ Publish Compiler
→ Immutable Product Page Artifact
→ PublicationStore.activate(product URL)
```

- 创建 Product 的公开页面时，系统自动创建 PresentationInstance，从 ContentTemplate 派生 DocumentSnapshot。无需手工创建 Page。
- 商品名称、描述、基础价格、主图和 SEO 内容在构建期固化到 DocumentSnapshot，并记录 Product 与 Media revision。
- Product revision 变化只使实际依赖该 Product 的 PresentationInstance stale；同一个 ContentTemplate 创建的其他 Instance 不因模板来源相同而自动重建。
- 库存、会话价格和购物车操作不阻塞静态主页面发布，分别通过受控 Runtime Fragment 或 Client Enhancement 增强。

### 1.5 Project Assets

Project Assets 是对 CMS Media 中构建资源的工程级引用，不复制二进制，也不在 Page Document 保存最终 URL：

- Page/Blueprint/Global Component 只保存稳定 `assetId`。
- BuildContext Resolver 把 `assetId` 解析为带 revision、尺寸和内容 hash 的 BuildMedia。
- 图片替换或变体变化通过 media dependency 使实际引用页面 stale。
- Compiler 只复制或引用本次 Artifact 所需的公开变体，禁止访问原始私有文件。
- 删除媒体前必须检查 Page、Blueprint、Global Component 和历史 Artifact 引用；历史 Artifact 使用已经冻结的内容寻址资源，不被新删除操作破坏。

## 2. ContentTemplate

ContentTemplate 是为内容实体类型（Product/Article/Category）设计的版本化页面结构定义。与 Page Blueprint 的关键区别：它**参与每次构建**以派生 DocumentSnapshot，不是一次性初始化工具。

```ts
interface ContentTemplate {
  id: string
  name: string
  entityType: 'product' | 'article' | 'category'
  root: ContainerNode                     // 完整组件 AST
  bindings: ContentBinding[]              // 映射到 CMS 实体字段
  versionId: string                       // 当前版本指针
}

interface ContentTemplateVersion {
  id: string
  templateId: string
  version: number
  document: ContentTemplate               // 该版本的不可变快照
  createdAt: timestamp
}

interface ContentBinding {
  fieldPath: string                       // 内容实体字段路径，如 "product.name"
  binding: FieldBinding                   // 映射到 Page Document 的 Binding
  valueType: string
}
```

### 2.1 与 Page Blueprint 的差异

| | Page Blueprint | ContentTemplate |
|---|---|---|
| 用途 | 初始化手工 Page Document | 生成自动内容页 DocumentSnapshot |
| 参与构建 | 否（仅初始化） | 是（每次构建派生） |
| Binding 目标 | 不限 | 限 Content Entity 字段 |
| 更新传播 | 不传播 | 版本变化 → 重建所有关联实例 |
| 管理位置 | Visual Builder | CMS Admin |

### 2.2 生命周期

- 创建 ContentTemplate 时需要指定 entityType 和初始 AST，系统自动生成 version 1。
- 每个版本不可变；修改产生新版本，旧版本保留用于历史 Artifact 重建。
- 版本变化触发 dependencyKind `content_template` 的 fan-out 重建：查询所有引用该模板的 PresentationInstance，逐个派生新 DocumentSnapshot 并重建。
- 同一 Project 内 Product 和 Article 各至少有一个 ContentTemplate。

### 2.3 Binding 约束

ContentTemplate 的 Binding 只能引用 Content Entity 的固定字段：

```text
product:  .name, .description, .price, .images, .seoTitle, .seoDescription
article:  .title, .body, .excerpt, .featuredImage, .seoTitle, .seoDescription
category: .name, .description, .image
```

Compiler 拒绝 entityType 与 Binding 不匹配的组合，例如 Product 模板中出现 `article.title`。

## 3. PresentationInstance 与 DocumentSnapshot

PresentationInstance 是内容实体（Product/Article/Category）的自动发布页面实例，与手工 Page 共享 Publish Compiler、ArtifactStore 和 PublicationStore，但管理路径不同——创建、编辑和删除由 CMS 实体驱动，不通过 Visual Builder。

```ts
interface PresentationInstance {
  id: string
  entityType: 'product' | 'article' | 'category'
  entityId: string                        // CMS 实体 ID
  urlPath: string                         // 规范化公开 URL
  status: 'draft' | 'active' | 'archived'
  currentSnapshotId: string | null        // 当前激活的 DocumentSnapshot
  artifactId: string | null               // 当前激活的 Artifact
  createdAt: timestamp
  updatedAt: timestamp
}

interface DocumentSnapshot {
  id: string
  presentationInstanceId: string
  sourceTemplateVersionId: string         // 来源 ContentTemplate 版本
  sourceEntityRevisionId: string          // 来源 CMS 实体 revision
  document: PageDocument                  // 完整 AST，Bindings 已解析为字面量
  createdAt: timestamp
}
```

### 3.1 生命周期

```text
CREATE:
  CMS Entity Created (Product#100)
  ↓
  Create PresentationInstance { entityType, entityId, urlPath }
  ↓
  Resolve ContentTemplate("product-detail", v5) + Product#100(rev 1)
  → DocumentSnapshot v1 (AST with resolved data from Bindings)
  → Build → Artifact → PublicationStore.activate(urlPath)

DATA UPDATE:
  Product#100.revision: 1 → 2 (price changed)
  ↓
  Mark PresentationInstance stale
  ↓
  Re-resolve ContentTemplate(v5) + Product#100(rev 2)
  → DocumentSnapshot v2 (same AST structure, new data values)
  → Build → Artifact → PublicationStore.activate(urlPath)

TEMPLATE UPDATE:
  ContentTemplate "product-detail": v5 → v6 (rearranged layout)
  ↓
  ContentTemplateVersion record (v6, immutable)
  ↓
  Dependency lookup: entityType='product', dependencyKind='content_template'
  ↓
  For each PresentationInstance: re-resolve v6 + current entity data
  → New DocumentSnapshot → Build → Publish
```

### 3.2 Page 与 PresentationInstance 对比

| | Page | PresentationInstance |
|---|---|---|
| 创建方式 | Visual Builder 手工创建 | CMS 实体自动创建 |
| 文档来源 | 独立 PageDocument，可编辑 | ContentTemplate 自动派生 |
| AST 存储 | 完整 PageDocument | DocumentSnapshot（含已解析数据） |
| 生命周期 | 独立于 CMS 内容 | 跟随 CMS 实体（同生同灭） |
| 管理界面 | Visual Builder | CMS Admin（内容编辑区） |
| 适用场景 | Home / About / 营销落地页 | Product / Article / Category 详情页 |
| URL 共享 | 与 PresentationInstance 互斥 | 与 Page 互斥 |

### 3.3 约束

- PresentationInstance 的 URL 从 CMS 实体自动推导（如 `/products/{slug}`），不可通过 Builder 手工修改。
- DocumentSnapshot 保存完整已解析 AST，确保 Artifact 的独立可重现性：即使 ContentTemplate 被删除，已有 DocumentSnapshot 仍可编译为 Artifact。
- 一个 CMS 实体默认最多一个 active PresentationInstance。若需要多个展示 URL（如同时存在 Product Detail 和 Product Microsite），必须额外创建手工 Page 并声明 canonical/noIndex。
- PresentationInstance 没有独立 Draft 状态——实体编辑即产生新版本 Draft，构建后直接进入 Stage/Publish 流程。

## 4. ThemeNode：有限判别联合 AST

```ts
interface NodeBase {
  id: string
  variant?: ComponentVariantRef
  style?: ResponsiveStyle
  animation?: Animation
  enhancement?: ClientEnhancementRef
}

interface ContainerNode extends NodeBase {
  type: 'container'
  props: ContainerProps
  children: ThemeNode[]
}

interface TextNode extends NodeBase {
  type: 'text'
  props: TextProps
}

interface HeadingNode extends NodeBase {
  type: 'heading'
  props: HeadingProps
}

interface ImageNode extends NodeBase {
  type: 'image'
  props: ImageProps
}

interface ButtonNode extends NodeBase {
  type: 'button'
  props: ButtonProps
}

interface FieldNode extends NodeBase {
  type: 'field'
  props: FieldProps
}

interface CollectionNode extends NodeBase {
  type: 'collection'
  props: CollectionProps
  children: ThemeNode[]
}

interface NavigationNode extends NodeBase {
  type: 'navigation'
  props: NavigationProps
}

interface MediaNode extends NodeBase {
  type: 'media'
  props: MediaProps
}

interface RuntimeFragmentNode extends NodeBase {
  type: 'runtimeFragment'
  props: RuntimeFragmentProps
}

interface GlobalComponentNode {
  id: string
  type: 'globalComponent'
  props: {
    ref: GlobalComponentRef
  }
}

type ThemeNode =
  | ContainerNode
  | TextNode
  | HeadingNode
  | ImageNode
  | ButtonNode
  | FieldNode
  | CollectionNode
  | NavigationNode
  | MediaNode
  | RuntimeFragmentNode
  | GlobalComponentNode
```

只有 Container 和 Collection 允许 children。Collection 不允许嵌套 Collection；RuntimeFragment 与 GlobalComponent 是叶节点引用，GlobalComponent 在构建期展开为固定版本子树。Client Enhancement 只能附着到已经能输出完整、可访问 HTML 的节点，不能用空容器替代基础内容。

`NodeBase` 中引用的 `ComponentVariantRef` 定义：

```ts
interface ComponentVariantRef {
  name: string                 // Registry 中该组件定义的 variant 名称，如 "primary", "compact"
}
```

- Variant 是 Registry 在 `ComponentDefinition` 中为每个组件类型声明的预设样式集（包含 props 默认值覆盖 + style 片段）。不同组件类型有不同的 variant 枚举，由 Registry 封闭定义。
- Compiler 在构建期把 variant 解析为 props merge + style override，生成最终 HTML/CSS，不进入运行时。
- Variant 不是任意 CSS class 或自由字符串。Validator 校验 `variant.name` 必须存在于 Registry 中该节点 `type` 对应的 `ComponentDefinition.variants` 列表中；Registry 未声明 variant 的组件，该字段必须为 `undefined`。
- Variant 与节点自身的 `style` 字段独立：variant 提供组件级预设，`style` 提供节点级覆盖，两者在 Style Compiler 中按「Registry 默认 → variant 预设 → 节点 style」优先级合并。

### 4.1 Node ID 生命周期

- Node ID 使用 UUID，并在整个 Page Document 内全局唯一。
- 新建节点时分配新 ID；复制子树必须为所有节点生成新 ID。
- 删除后不主动复用 ID；移动、属性修改和样式修改不改变 ID。
- Undo 恢复原 ID；Redo 复用首次执行时生成的 ID 映射。
- Normalize 不修改合法 ID；Validator 拒绝空 ID、非法 UUID 和重复 ID。
- Migration 若补 ID，迁移后的 Document 必须持久化，后续加载不得再次生成。

### 4.2 Global Component

Global Component 是 Project 内显式复用的不可变组件子树：

```ts
interface GlobalComponentRef {
  id: string
  version: number
}

interface GlobalComponentDocument {
  schemaVersion: number
  id: string
  name: string
  root: ThemeNode
}
```

- Page 固定引用 `id + version`，Compiler 必须校验 Global Component 与 Page 属于同一个 Project。
- GlobalComponentNode 只允许 `id` 和 `props.ref`；Validator 拒绝其 `variant`、`style`、`animation` 和 children，避免引用点形成隐式 override。
- 新版本不会自动替换 Page 中的旧引用，也不会隐式改变线上页面。
- 升级必须显式选择目标版本并重新构建该 Page。
- 发布 Artifact 不依赖 Global Component Store；构建时已展开并记录版本。
- 禁止根据节点相似度自动抽取 Global Component。

### 4.3 Global Component Update Policy

Global Component 的版本更新策略由组件级策略和页面级覆盖共同决定：

```ts
interface GlobalComponentUpdatePolicy {
  componentId: string
  defaultUpdateMode: 'immutable' | 'auto-update'
}

// Page / PresentationInstance 级版本覆盖
type ComponentPin = {
  componentId: string
  pinnedVersionId: string
}
```

三种行为：

**immutable**（默认）：新版本发布时，已引用旧版本的 Page 和 PresentationInstance 不受影响。新页面创建时使用最新版本。适用于 Promo Banner、营销组件等需固定展示内容的场景。

```text
GlobalHeader@v1 发布 → v2 发布
Page-A (immutable): 保持 v1，不自动重建
```

**auto-update**：新版本发布时，系统查询 Dependency Graph 中所有引用该组件的 Page 和 PresentationInstance，自动标记 stale 并触发 rebuild。适用于 Header、Footer、Analytics Snippet 等需要全站统一更新的组件。

```text
GlobalFooter@v5 发布
→ dependency lookup (global_component, componentId)
→ 所有引用 Page 标记 stale → rebuild → publish
```

**pinned**（页面级覆盖）：用户可在特定 Page 上锁定到某个版本。即使该组件的策略为 auto-update，此页面不跟随更新。

```text
GlobalHeader (auto-update, latest v8)
Page-B (pin: GlobalHeader@v3): 即使全局 auto-update，仍保持 v3
```

对于 PresentationInstance，pinned 在 ContentTemplate 层面设置（指定组件版本），不逐个实例操作。ContentTemplate 的 auto-update 行为遵循组件策略：组件 `immutable` 则不更新，组件 `auto-update` 则在每次构建时解析最新版本。

## 5. Binding 与 BuildContext

### 5.1 Binding 不是 Query DSL

```ts
type FieldBinding =
  | 'site.name'
  | 'site.description'
  | 'page.title'
  | 'page.content'
  | 'article.title'
  | 'article.content'
  | 'article.excerpt'
  | 'article.author'
  | 'article.publishedAt'
  | 'product.name'
  | 'product.description'
  | 'product.basePrice'
  | 'category.name'
  | 'item.title'
  | 'item.excerpt'
  | 'item.url'

type CollectionSource =
  | 'recentArticles'
  | 'currentCategoryArticles'
  | 'featuredProducts'

type MenuLocation = 'primary' | 'footer'

type MediaBinding =
  | 'site.logo'
  | 'page.featuredImage'
  | 'article.featuredImage'
  | 'product.primaryImage'
  | 'item.image'
```

Binding Registry 固定描述 scope、valueType、renderer、依赖类型和空值策略。Document 不能保存 SQL、过滤表达式、排序表达式、任意函数名或 Provider endpoint。

```ts
type BindingScope =
  | 'site'
  | 'page'
  | 'article'
  | 'product'
  | 'category'
  | 'item'

type FieldRenderer =
  | { type: 'text' }
  | { type: 'heading'; level: 1 | 2 | 3 | 4 | 5 | 6 }
  | { type: 'date'; format: 'date' | 'dateTime' | 'yearMonthDay' }
  | { type: 'money'; currencyDisplay: 'symbol' | 'code' }
  | { type: 'link'; labelSource: 'item.title' }
  | { type: 'richText' }

interface FieldProps {
  source: FieldBinding
  renderer: FieldRenderer
}
```

`source` 是数据绑定，`renderer` 是展示语义。Inspector 将两者分区展示，但不增加只有一层字段的 `presentation` 包装对象。

### 5.2 BuildContext

BuildContext 是 Publish Compiler 根据 Page、ContentTarget 和 Registry 生成的一次性、只读、可序列化构建输入：

```ts
interface BuildContext {
  projectId: string
  pageId: string
  canonicalPath: string
  pageKind: PageKind
  scopes: BindingScope[]              // 编译期校验用：Validator 拒绝不在 scopes 中的 Binding
  fields: Partial<Record<FieldBinding, BuildValue>>
  collections: Partial<Record<CollectionSource, BuildCollectionItem[]>>
  menus: Partial<Record<MenuLocation, BuildMenu>>
  media: Partial<Record<MediaBinding, BuildMedia>>
  dependencies: BuildDependency[]
}

type BuildValue =
  | { type: 'text'; value: string }
  | { type: 'richText'; sanitizedHTML: string }
  | { type: 'date'; iso8601: string }
  | { type: 'url'; value: string }
  | { type: 'money'; minorUnits: number; currency: string }

interface BuildCollectionItem {
  fields: Partial<Record<FieldBinding, BuildValue>>
  media: Partial<Record<MediaBinding, BuildMedia>>
}

interface BuildMenu {
  items: BuildMenuItem[]
}

interface BuildMenuItem {
  label: string
  url: string
  children: BuildMenuItem[]
}

interface BuildMedia {
  url: string
  alt: string
  width: number
  height: number
}

interface BuildDependency {
  kind:
    | 'direct_content'         // 实体字段变化，如 product.price
    | 'content_collection'     // 集合成员变化，如 category.products
    | 'menu'                   // 菜单项变化
    | 'media'                  // 媒体 revision 变化
    | 'content_template'       // ContentTemplate 版本变化
    | 'global_component'       // 全局组件版本变化
    | 'site_setting'           // 站点/项目设置变化（合并原 siteSettings + projectSettings）
  key: string
  revision: string
}
```

约束：

- Resolver 只读取 Registry 允许的数据，不能由 Document 决定任意查询；禁止退回 `Record<string, unknown>` 后按路径临时推断类型。
- `BuildValue.type` 必须与 Binding Registry 的 valueType 匹配；金额使用最小货币单位整数和 ISO currency，禁止浮点金额。
- BuildContext 创建后不可变；构建过程中不得再次读取数据库或网络。
- `dependencies` 是实际读取集合，不是按 PageKind 猜测的粗粒度依赖。
- Collection 除记录当前结果实体外，还必须记录稳定的 `contentSet` revision，例如 `article:recent` 或 `category:{id}:articles`；新增、删除或重排成员时提升集合 revision，使列表页能够被失效。
- 同一规范化 Page Document、BuildContext、Registry 和 Compiler 必须产生相同 Artifact 字节。
- 富文本必须由 CMS Core 在入库时净化，Compiler 只通过专用 RichText Fragment 输出。

## 6. Component Registry 与 Compiler IR

Registry 是 Go 代码内置、版本固定的编译协议。Builder 使用同版本 Registry Manifest 展示组件和 Inspector。

```ts
type ComponentExecution =
  // 纯构建期静态 HTML 输出
  | { strategy: 'static' }
  // HTMX fragment，访问时由白名单 Go Handler 返回 HTML 片段
  | { strategy: 'fragment'; endpoint: string }
  // 静态 HTML + 客户端渐进增强
  | { strategy: 'static'; enhancement: string }
  // Fragment + 客户端渐进增强
  | { strategy: 'fragment'; endpoint: string; enhancement: string }

interface ComponentDefinition<N extends ThemeNode> {
  type: N['type']
  execution: ComponentExecution
  create(): N
  migrate(node: unknown): N
  normalize(node: N): N
  validate(node: N, ctx: ValidationContext): ValidationResult
  lower(node: N, ctx: LowerContext): ComponentIR
}

interface ComponentIR {
  fragment: TemplateFragment
  styleInputs: StyleCompileInput[]
  assetRequirements: AssetRequirement[]
  dependencies: DependencyRequirement[]
  runtimeRequirements: RuntimeCapabilityRequirement[]
}
```

执行分类是封闭协议：

- `{ strategy: 'static' }` 在 Publish Compiler 中完全求值，输出最终静态 HTML/CSS。
- `{ strategy: 'static'; enhancement: 'carousel' }` 输出静态 HTML + 内容寻址 JS bundle；JavaScript 被禁用时 HTML 和基础操作仍成立。
- `{ strategy: 'fragment'; endpoint: '/_fragments/product-stock' }` 在 Artifact 中输出可访问 fallback 和由 Registry 生成的 HTMX 请求声明。
- `{ strategy: 'fragment'; endpoint: '...'; enhancement: 'quantitySelector' }` 同时具有动态片段和客户端增强的复合组件（hybrid）。

Component 只回答“这个节点表达什么”。以下职责必须属于 Compiler Pipeline：

- 树遍历和 Scope 栈
- Variant 解析
- CSS 合并
- 资源去重与排序
- Dependency 收集
- SEO / Security / Accessibility 诊断
- Jet 和 Preview 输出
- Artifact 组装

## 7. 领域层持久化投影

本节存放与领域模型直接相关的数据库表。流水线相关的表（Artifact、Dependency、Build Job、Publication Receipt 等）见 `03-pipeline.md`。

```sql
CREATE TABLE projects (
    id          uuid PRIMARY KEY,
    name        text NOT NULL,
    settings    jsonb NOT NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

CREATE TABLE blueprints (
    id              uuid PRIMARY KEY,
    project_id      uuid NOT NULL REFERENCES projects(id),
    name            text NOT NULL,
    kind            text NOT NULL,
    draft_document  jsonb NOT NULL,
    draft_version   bigint NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

CREATE TABLE blueprint_versions (
    id              uuid PRIMARY KEY,
    blueprint_id    uuid NOT NULL REFERENCES blueprints(id),
    version         bigint NOT NULL,
    document        jsonb NOT NULL,
    source_hash     text NOT NULL,
    created_by      uuid NOT NULL,
    created_at      timestamptz NOT NULL,
    UNIQUE (blueprint_id, version)
);

CREATE TABLE global_components (
    id              uuid PRIMARY KEY,
    project_id      uuid NOT NULL REFERENCES projects(id),
    name            text NOT NULL,
    draft_document  jsonb NOT NULL,
    draft_version   bigint NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

CREATE TABLE global_component_versions (
    id                   uuid PRIMARY KEY,
    global_component_id  uuid NOT NULL REFERENCES global_components(id),
    version              bigint NOT NULL,
    document             jsonb NOT NULL,
    source_hash          text NOT NULL,
    created_by           uuid NOT NULL,
    created_at           timestamptz NOT NULL,
    UNIQUE (global_component_id, version),
    UNIQUE (id, global_component_id)
);

CREATE TABLE pages (
    id                    uuid PRIMARY KEY,
    project_id            uuid NOT NULL REFERENCES projects(id),
    kind                  text NOT NULL,
    content_target_type         text NOT NULL,
    content_target_id           uuid NULL,
    source_blueprint_version_id uuid NULL REFERENCES blueprint_versions(id),
    draft_path                  text NOT NULL,
    active_path           text NULL,
    draft_document        jsonb NOT NULL,
    draft_version         bigint NOT NULL DEFAULT 1,
    staged_artifact_id    uuid NULL,
    active_artifact_id    uuid NULL,
    stale                 boolean NOT NULL DEFAULT true,
    deleted_at            timestamptz NULL,
    published_at          timestamptz NULL,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    UNIQUE (id, project_id),
    CONSTRAINT pages_content_contract_check CHECK (
        (kind IN ('home', 'archive', 'search', 'notFound')
            AND content_target_type = 'none'
            AND content_target_id IS NULL)
        OR (kind = 'page'
            AND content_target_type = 'page'
            AND content_target_id IS NOT NULL)
        OR (kind = 'article'
            AND content_target_type = 'article'
            AND content_target_id IS NOT NULL)
        OR (kind = 'product'
            AND content_target_type = 'product'
            AND content_target_id IS NOT NULL)
        OR (kind = 'category'
            AND content_target_type = 'category'
            AND content_target_id IS NOT NULL)
        OR (kind = 'tag'
            AND content_target_type = 'tag'
            AND content_target_id IS NOT NULL)
    )
);

CREATE TABLE page_routes (
    project_id   uuid NOT NULL REFERENCES projects(id),
    path         text NOT NULL,
    page_id      uuid NULL REFERENCES pages(id),
    presentation_id uuid NULL REFERENCES presentation_instances(id),
    route_kind   text NOT NULL CHECK (route_kind IN ('reserved', 'active', 'redirect')),
    artifact_id  uuid NULL,                    -- active 或 redirect 的 Artifact；由 PublicationStore.activate 原子更新
    updated_at   timestamptz NOT NULL,
    PRIMARY KEY (project_id, path),
    CHECK (
        (page_id IS NOT NULL AND presentation_id IS NULL)
        OR (page_id IS NULL AND presentation_id IS NOT NULL)
    )
);

CREATE TABLE content_templates (
    id                  uuid PRIMARY KEY,
    project_id          uuid NOT NULL REFERENCES projects(id),
    name                text NOT NULL,
    entity_type         text NOT NULL CHECK (entity_type IN ('product', 'article', 'category')),
    draft_document      jsonb NOT NULL,             -- 当前可编辑草稿
    draft_version       bigint NOT NULL DEFAULT 1,
    current_version_id  uuid NULL,                  -- 已发布的最新 blueprint_version id
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL
);

CREATE TABLE content_template_versions (
    id                   uuid PRIMARY KEY,
    template_id          uuid NOT NULL REFERENCES content_templates(id),
    version              bigint NOT NULL,
    document             jsonb NOT NULL,
    source_hash          text NOT NULL,
    created_by           uuid NOT NULL,
    created_at           timestamptz NOT NULL,
    UNIQUE (template_id, version),
    UNIQUE (id, template_id)
);

CREATE TABLE presentation_instances (
    id                    uuid PRIMARY KEY,
    project_id            uuid NOT NULL REFERENCES projects(id),
    entity_type           text NOT NULL CHECK (entity_type IN ('product', 'article', 'category')),
    entity_id             uuid NOT NULL,
    url_path              text NOT NULL,
    template_id           uuid NOT NULL REFERENCES content_templates(id),
    current_snapshot_id   uuid NULL,
    staged_snapshot_id    uuid NULL,              -- staging 中的 DocumentSnapshot
    staged_artifact_id    uuid NULL,              -- staging 中的 Artifact
    active_artifact_id    uuid NULL,
    stale                 boolean NOT NULL DEFAULT true,
    deleted_at            timestamptz NULL,
    published_at          timestamptz NULL,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    UNIQUE (entity_type, entity_id),
    UNIQUE (project_id, url_path)
);

CREATE TABLE document_snapshots (
    id                            uuid PRIMARY KEY,
    presentation_instance_id      uuid NOT NULL REFERENCES presentation_instances(id),
    source_template_version_id    uuid NOT NULL REFERENCES content_template_versions(id),
    source_entity_revision_id     uuid NOT NULL,
    document                      jsonb NOT NULL,
    created_at                    timestamptz NOT NULL,
    UNIQUE (id, presentation_instance_id)
);

ALTER TABLE presentation_instances
    ADD CONSTRAINT presentation_instances_snapshot_fk
        FOREIGN KEY (current_snapshot_id, id)
        REFERENCES document_snapshots(id, presentation_instance_id);

-- 全局组件更新策略
CREATE TABLE global_component_policies (
    component_id        uuid PRIMARY KEY REFERENCES global_components(id),
    default_update_mode text NOT NULL CHECK (default_update_mode IN ('immutable', 'auto-update'))
);

-- 页面级组件版本锁定
CREATE TABLE page_component_pins (
    page_id       uuid NOT NULL REFERENCES pages(id),
    component_id  uuid NOT NULL REFERENCES global_components(id),
    pinned_version_id uuid NOT NULL REFERENCES global_component_versions(id),
    PRIMARY KEY (page_id, component_id)
);

-- ContentTemplate 级组件版本锁定
CREATE TABLE content_template_component_pins (
    template_id       uuid NOT NULL REFERENCES content_templates(id),
    component_id      uuid NOT NULL REFERENCES global_components(id),
    pinned_version_id uuid NOT NULL REFERENCES global_component_versions(id),
    PRIMARY KEY (template_id, component_id)
);
```

> **注**：原稿中 `content_templates` 末尾有重复的 `updated_at` 行和多余 `);`，搬运时已修正为单一表定义。

### 7.1 跨表一致性约束

- `pages_content_contract_check` 在持久化层封闭 `PageKind + ContentTarget` 组合，Application Service 和 Document Validator 仍必须在写入前返回可理解的业务错误。
- `source_blueprint_version_id` 以及 Page Document 中的 Global Component 引用还必须在业务事务/Validator 中校验属于当前 Project，禁止跨 Project 引用。
- PresentationInstance 使用 `entity_type + entity_id` 保证每个 CMS 实体最多一个自动页面实例。
- `page_routes` 是跨 draft、active 和 redirect 的唯一 URL 占用表：`route_kind` 只能是 `reserved | active | redirect`；`artifact_id` 是当前 active/redirect 的 Artifact 缓存，由 PublicationStore.activate 原子更新，不作为访问面的唯一权威来源（权威来源是 PublicationStore 的文件系统状态）。
- Page 或 PresentationInstance 修改 URL 时先为新 path 创建 reserved route，避免与其他实体的线上或草稿地址冲突。路径由 `page_id` 或 `presentation_id` 独占，不可混挂。
- Tombstone 后是否释放 route 必须由显式恢复/释放流程决定，禁止静默把旧 URL 分配给新 Page 或 PresentationInstance。
- 复合外键（如 `pages_staged_artifact_fk`、`presentation_instances_snapshot_fk`）保证 staged/active Artifact、route、dependency 和 publication event 都属于同一个 Page 或 PresentationInstance。流水线相关外键见 `03-pipeline.md` 的 `page_artifacts` 和 `presentation_artifacts` 定义。