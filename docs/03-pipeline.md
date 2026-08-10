# 03 · 发布流水线

> 本文覆盖从编辑到访问的完整控制面流水线：Editor Kernel → Preview Build → Publish Compiler → Artifact → PublicationStore → 生命周期（Draft/Build/Stage/Publish/Rollback/删除/GC）→ 依赖失效与构建队列。流水线相关的数据库表集中在文末。

## 1. Editor Kernel

```text
Editor Kernel
├── Document       # Page Document，唯一真相源
├── Command        # 唯一写入口
├── History        # 当前编辑会话 Undo/Redo
├── NodeIndex      # nodesById / parentById
├── Selection      # UI 状态，不写入 Document
└── Change Event   # 驱动 debounced Preview Build
```

MVP Command：

- `update_props`
- `update_style`
- `add_node`
- `delete_node`
- `move_node`
- `duplicate_node`
- `replace_global_component_version`

每个 Command 必须在写入前完成校验，准备完整 before/after 或子树快照，并只发出一次 DocumentChanged。DOM 和 iframe 只是投影，禁止从拖拽后 DOM 反向生成 Page Document。

## 2. Preview Build

```text
Page Document Draft
+
Preview Data / Real BuildContext
→ Go Preview Build
→ 同一 Migrate / Normalize / Validate / Lower / Style / Jet Pipeline
→ Preview HTML + data-node-id
→ iframe
```

- Mock Preview Data 是固定、可复现的 BuildContext fixture。
- Real Preview 使用当前 CMS revision，但只读且不创建 Artifact。
- Preview 响应携带 Document revision；Builder 丢弃落后于当前 revision 的响应。
- Preview 可以输出 source map 和 `data-node-id`，不能写 PublicationStore。
- Preview 与 Publish 唯一允许的差异是数据来源、编辑元数据和是否持久化 Artifact。

## 3. Publish Compiler

### 3.1 构建流程

```text
Page + expected draft_version
→ Load Draft Document
→ Migrate / Normalize / Validate
→ Resolve ContentTarget
→ Create immutable BuildContext
→ Resolve pinned Variant / Global Component
→ Component Lowering
→ Fragment Compiler
→ Style Compiler
→ Asset Collector
→ Dependency Collector
→ SEO / Security / Accessibility Passes
→ Jet Build-time Render
→ Validate final HTML/CSS/JS
→ Build Artifact + Manifest
→ Recheck draft_version and dependency revisions
→ Store immutable Artifact
→ Stage Artifact
```

Jet 是 Go Publish Compiler 的内部构建期 emitter/renderer。Jet 模板只存在于构建内存或诊断产物中，不进入公开 Artifact。最终 `index.html` 不保留 FieldBinding、Jet 表达式或数据库访问能力。

### 3.2 Fragment Tree

```ts
type TemplateFragment =
  | TextFragment
  | ElementFragment
  | BindingFragment
  | RichTextFragment
  | MediaFragment
  | EachFragment
  | NavigationFragment
  | RuntimeFragmentHostFragment
  | ClientEnhancementHostFragment
```

Fragment 类型保持封闭，并且每个 Fragment 必须携带来源 Node ID。禁止加入：

- 任意表达式或函数调用
- 任意循环变量
- 用户输入 Jet
- 用户输入 script 标签
- 用户输入 API URL

JetEmitter 是构建阶段唯一 HTML 字符串输出点，统一处理 HTML/属性 escape、AllowedTag、URL 协议、Scope 和富文本边界。

### 3.3 Source Map 与诊断

```ts
interface SourceMapEntry {
  nodeId: string
  generatedFile: string
  generatedStartLine: number
  generatedEndLine: number
}
```

Source Map 只用于 Preview 和构建诊断，不发布到公共目录。后端错误返回文件和行号，Builder 用本次构建 Source Map 定位 Node；不能信任客户端 Node ID 作为安全判断依据。

### 3.4 确定性构建守则

同一 Page Document + BuildContext + Registry + Compiler 必须产生字节级相同的 Artifact。以下是 Compiler Pipeline 中消除非确定性的硬性规则：

**禁止项**：

- 禁止在构建过程中调用 `time.Now()` 或任何运行时时间函数；时间戳只保存在数据库，不进入 manifest 或 Artifact 内容。
- 禁止使用 `math/rand` 或任何随机数生成器。
- 禁止在构建过程中发起新的网络请求（BuildContext 创建后已冻结全部外部输入）。
- 禁止在产出 HTML/CSS/JS 字符串的环节直接 `range` Go map（map 迭代顺序被 Go runtime 故意随机化）。

**必须项**：

- 所有影响输出的集合遍历必须先转 slice + 显式排序再遍历。包括但不限于：CSS 合并顺序、Jet 模板 `{{ range }}` 遍历、Dependency 收集、Asset 收集。
- manifest 的 `dependencies` 数组按 `(kind, key)` 排序；`files` 对象由 Go `encoding/json` 自动按 key 排序（标准库保证），无需额外处理。
- AST 遍历使用深度优先 + children 数组有序遍历，单线程顺序执行；只在加载不可变外部数据（如 Global Component）时允许并发，收集结果后按 AST 顺序组装。
- 所有 ID 来自 Page Document 本身，构建过程中不生成任何新 ID。
- BuildMedia 的 URL 必须是稳定的相对路径（如 `/objects/{content-hash}.css`），不包含 presigned URL、CDN 域名或时间戳参数。

## 4. Artifact 与资源

### 4.1 Page Artifact

```text
artifacts/{project-id}/{page-id}/{artifact-hash}/
├── index.html
└── manifest.json

objects/{content-hash}/
├── core.{content-hash}.css
├── components.{content-hash}.css
├── page.{content-hash}.css
├── htmx.{content-hash}.js             # 页面使用 Runtime Fragment 时存在
└── enhancements.{content-hash}.js     # 页面使用 Client Enhancement 时存在
```

每个 Page Artifact 是完整、不可变、可独立激活的发布单元。“完整”表示 manifest 闭包包含其全部内容对象，不要求物理复制共享对象。资源规则：

- Artifact 目录保存 Page 入口与 manifest；CSS、JS、字体和公开媒体变体保存为独立内容对象，manifest 以 hash 引用。
- 所有公开文件名包含内容 hash；禁止 `common.css`、`page.css` 等可变别名。
- Component Resource 是逻辑贡献，不要求每个组件产生一个网络请求。
- Asset Collector 按页面实际组件与 capability 集合生成稳定 bundle；相同内容对象可跨 Article、Product 和聚合 Page 复用。
- Category、Archive、分页等聚合 Page 按页面组件集合生成 bundle；集合成员数量变化只改变页面 HTML/数据依赖，不为每个成员生成重复组件资源。
- Runtime Fragment 只增加 HTMX runtime 与片段声明；Client Enhancement 只收集实际引用的模块。两者不能让静态页面默认携带全量动态代码。
- HTML 只引用 manifest 声明的内容寻址资源。
- Artifact 不得引用未被 manifest 声明的本地文件或内容对象。
- 公共资源更新只写新对象，不覆盖旧对象，因此逐页发布不能污染其他 Page。

### 4.2 Artifact Manifest

```ts
interface ArtifactManifest {
  manifestSchemaVersion: number
  pageDocumentSchemaVersion: number
  compilerVersion: string
  registryVersion: string
  minimumCMSVersion: string
  sourceId: string                         // pageId 或 presentationInstanceId
  sourceType: 'page' | 'presentation'
  canonicalPath: string
  sourceHash: string
  buildInputHash: string
  dependencyKinds: ('direct_content' | 'content_collection' | 'menu' | 'media'
                   | 'content_template' | 'global_component' | 'site_setting' | 'runtime')[]
  requiredRuntimeCapabilities: RuntimeCapabilityRequirement[]
  variants: Record<string, VariantManifest> // variant name → variant manifest
  globalComponents: Record<string, {       // componentId → resolved version
    id: string
    versionId: string
  }>
  dependencies: Array<{
    kind: string                           // 与 BuildDependency.kind 一致
    key: string
    revision: string
  }>
  files: Record<string, string>            // path → contentHash（内容寻址）
}
```

manifest 不写构建时间，构建时间保存在数据库。`files` 是 `Record<path, contentHash>`，每个值是通过 `content_objects.content_hash` 可查的 SHA256 hash。Compiler 必须对数组和对象稳定排序，确保确定性 hash。

### 4.3 ArtifactLocator 与 Store

```ts
interface ArtifactLocator {
  provider: 'local' | 's3' | 'oss'
  key: string
}

interface ArtifactStore {
  put(artifact: PageArtifact): Promise<ArtifactLocator>
  get(locator: ArtifactLocator): Promise<PageArtifact>
  verify(locator: ArtifactLocator, hash: string): Promise<void>
  deleteArtifact(locator: ArtifactLocator, expectedHash: string): Promise<void>
  deleteObject(locator: ArtifactLocator, expectedHash: string): Promise<void>
}
```

Locator 不保存 endpoint、bucket credentials 或临时签名 URL。Artifact 一旦登记到 Page Artifact 后不可修改。删除接口只接受持久化 Locator 与期望 hash，必须幂等，并且只能由完成数据库引用检查和 GC claim 的生命周期服务调用；`ArtifactStore` 自身不判断业务引用。

### 4.4 Redirect Artifact

URL 修改（§6.5）和删除流程中需要把旧 URL 重定向到新 URL。Redirect Artifact 是一种极简 Artifact，不经过 Publish Compiler，不产生 AST/CSS/JS，只有重定向指令。

```ts
interface RedirectDirective {
  targetPath: string                 // 目标 URL path，必须以 / 开头
  statusCode: 301 | 302              // 301 = 永久（URL 修改默认），302 = 临时（删除后过渡期）
}
```

存储与激活：

- Redirect Artifact 在 ArtifactStore 中存储为单个 `redirect.json` 文件（而非 `index.html` + CSS + JS 闭包）：

  ```text
  artifacts/{project-id}/redirects/{redirect-hash}/
  └── redirect.json        // 序列化的 RedirectDirective
  ```

- `page_routes` 中 `route_kind = 'redirect'`，`artifact_id` 指向该 Redirect Artifact。
- PublicationStore 的 `activate(path, locator)` 对 redirect 类型做同样的 symlink 激活；`inspect(path)` 返回 redirect 状态。
- Static Server 检测到 symlink 目标目录下存在 `redirect.json` 时，直接返回 `statusCode` + `Location: {targetPath}` header，不提供文件下载。
- 对象存储/CDN 环境下，redirect 通过等价的 CDN redirect rule 或边缘函数实现。

Redirect Artifact 同样不可变。修改重定向目标需要生成新的 Redirect Artifact 并重新 activate。

## 5. PublicationStore 与 URL

```ts
interface PublicationStore {
  activate(path: string, locator: ArtifactLocator): Promise<PublicationReceipt>
  deactivate(path: string): Promise<PublicationReceipt>
  inspect(path: string): Promise<PublicationState>
}
```

PublicationStore 负责访问面的 URL 激活，不负责 Artifact 构建：

- 本地 provider 使用符号链接切换策略：

  ```text
  public/
    active/
      about/ → ../../artifacts/{hash}/     # symlink
      products/phone/ → ../../artifacts/{hash}/
    artifacts/
      {hash}/
        index.html
        style.{hash}.css
        ...
  ```

  `activate(path, locator)` = 原子替换 symlink（`rename(2)` 或 `symlink(2)` 覆盖）。`deactivate(path)` = 删除 symlink。`inspect(path)` = `readlink` 确认目标。

- 对象存储/CDN 使用等价原子性的别名或版本路由指针，不支持 symlink 的平台使用临时目录 + 原子 rename 交换。

- `activate` 成功后，新请求必须只看到新 Artifact；旧请求可以完成读取旧文件。

- 不允许逐文件覆盖公开目录，因为请求可能观察到 HTML 与 CSS/JS 混合版本。

- Receipt 必须可用于幂等重试和故障恢复。

- `activate` 不查询数据库；URL → 文件系统的映射只由 PublicationStore 的文件状态决定，不依赖 `pages.active_artifact_id` 或 `page_routes.artifact_id` 等数据库指针。

### 5.1 URL 规范化

- URL 只保存 path，不保存 scheme、host 或 query。
- 必须以 `/` 开头并规范化尾斜杠策略。
- 拒绝 `..`、重复分隔符、编码后的路径穿越、控制字符和保留路径。
- `page_routes` 保证同一 Project 的 reserved、active 和 redirect path 全局唯一，不能只检查 `pages.draft_path`。
- `/admin/`、`/api/`、`/_fragments/`、`/assets/`、`/objects/` 等系统路径不可被 Page 占用。
- 文件系统 key 必须由安全编码器从 Page ID/Artifact hash 生成，不能直接拼接用户 URL。

## 6. Draft、Build、Stage、Publish 与 Rollback

```text
Draft（可变 Page Document）
    │ Build
    ▼
Page Artifact（不可变）
    │ Stage
    ▼
staged_artifact_id
    │ Publish
    ▼
PublicationStore.activate(URL)
    │
    ▼
active_artifact_id
```

### 6.1 Draft

- 每个 Page 只有一个当前 Draft。
- `draft_version` 是乐观锁。
- 保存 Draft 不改变 staged/active Artifact。
- 异步保存和 Preview 响应不能覆盖更新 revision。

### 6.2 Build 与 Stage

构建请求固定携带：

```text
page_id
draft_version
expected draft_path
```

Build Worker 在写 Artifact 前后都检查：

- Draft version 未变化。
- ContentTarget 未变化。
- BuildContext 中所有 dependency revision 未变化。
- URL 仍属于该 Page 且未产生冲突。

任一变化都把本次构建标记为 superseded，不得 stage 或 publish。

### 6.3 Publish

页面发布是单 Page 原子操作，不影响其他 URL：

1. 锁定目标 Page。
2. 校验 staged Artifact、hash、manifest、URL 和依赖 revision。
3. 调用 `PublicationStore.activate(path, locator)`。
4. 成功后在数据库事务中更新 `active_path`、`active_artifact_id`、route 投影和 `published_at`。
5. 写追加式 Publication Event。

PublicationStore 已成功但数据库提交失败时，恢复任务必须根据 Receipt 和 `inspect(path)` 完成前滚或回退。不能假设跨数据库与文件系统存在分布式事务。

### 6.4 Rollback

- 回滚选择同一 Page 的历史 Artifact。
- Artifact Manifest 的 `canonicalPath` 必须等于当前 `active_path`；URL 不同的历史 Artifact 不能直接回滚，必须先按 URL 修改流程基于该历史源码重建。
- 重新校验 Artifact、hash 和 Runtime Capability。
- 原子激活目标 Artifact，然后更新 `active_artifact_id`。
- 不修改 Draft、不修改 staged 指针、不影响其他 Page。
- CMS 内容已经变化不阻止回滚，因为 Artifact 是发布时内容快照；界面必须明确显示快照时间和依赖 revision。

### 6.5 URL 修改

URL 修改不是普通 props 更新：

1. 将新 URL 写入 `draft_path`，创建或确认该 Page 拥有对应 reserved route。
2. 基于新 URL 构建 Artifact。
3. 原子激活新 URL。
4. 在数据库事务中更新 `active_path`、`active_artifact_id` 和新 route 状态。
5. 根据显式策略将旧 active route 取消或改为指向 Redirect Artifact。

禁止先删除旧 URL 再构建新 URL。旧 URL 与新 URL 的 PublicationStore 操作不具备跨路径原子性；故障恢复必须保证至少旧 URL 仍可用，并通过 Receipt 幂等完成剩余步骤。

## 7. 删除、取消发布与 GC

删除内容不是立即递归删除 Artifact 目录：

```text
删除 CMS Content
→ 标记关联 Page 为 stale/deletion_pending
→ 取消对应 URL 激活
→ Page tombstone
→ 保留历史 Artifact
→ 保留期结束且无引用
→ GC
```

冻结规则：

- CMS 内容删除与 URL 取消发布必须通过同一业务流程协调，但不伪造跨系统原子事务。
- 取消发布失败时 Page 保持 deletion_pending，并自动重试；不能报告删除完成却继续公开旧页面。
- Tombstone Page 不再接受普通编辑或发布，但允许恢复。
- 数据库保存 Page Artifact 的不可变源码快照、BuildInputManifest、manifest、hash、Locator 和生命周期状态；HTML/CSS/JS、字体与媒体变体等实际字节只保存在 ArtifactStore。
- Tombstone 不删除 `page_artifacts` 元数据或 ArtifactStore payload；保留期内恢复仍可校验并重新激活历史 Artifact。
- Page、历史保留策略、Publication、Redirect 或 Build Job 仍引用的 Artifact 不得进入 GC claim。
- GC 先在数据库事务中锁定候选并把 `payload_state` 从 `available` 改为 `gc_pending`；发布、回滚和恢复只能选择 `available` Artifact，因此 claim 后不能新增激活引用。
- GC claim 后必须再次检查 active/staged route、Publication 恢复任务和 Build Job 引用，再调用幂等删除接口；检查失败则释放 claim。
- Artifact payload 删除成功后保留 `page_artifacts` 审计记录，将其标记为 `deleted`；历史界面必须明确该版本已不可直接恢复。
- 内容对象按 `page_artifact_objects` 引用闭包独立回收。只有所有 Artifact 引用都释放且超过保留期时，才能删除共享对象。
- 外部删除成功但数据库确认失败时，恢复任务通过 Store inspect/verify 收敛状态；不能假设数据库与对象存储具有分布式事务。
- 默认保留期和强制清理属于部署策略，不写进 Page Document。

## 8. 依赖失效与构建队列

### 8.1 Revision 机制

Revision 是依赖源的变化追踪标识。每当一个依赖源的语义内容发生变化时，revision 更新。Build Worker 通过比较"构建时记录的 revision"和"当前 revision"来判断是否需要重建——相等即未变化，不等即已变化。

每种 DependencyKind 的 revision 来源和格式：

| DependencyKind | revision 来源 | 格式 | dependency_key 示例 |
|---|---|---|---|
| `direct_content` | CMS 实体的 revision 字段（单调递增整数） | 整数字符串，如 `"3"` | `product:100` |
| `content_collection` | 集合的 contentSet revision（成员增删或排序变化时递增） | 整数字符串，如 `"7"` | `collection:recentArticles` |
| `menu` | 菜单修订号（增删改菜单项时递增） | 整数字符串，如 `"2"` | `menu:footer` |
| `media` | 媒体文件的内容 hash | SHA256 hex，如 `"a1b2c3..."` | `media:{assetId}` |
| `content_template` | `content_template_versions.version` | 整数字符串，如 `"5"` | `content_template:{templateId}` |
| `global_component` | `global_component_versions.version` | 整数字符串，如 `"8"` | `global_component:{componentId}` |
| `site_setting` | Project settings 修订号（每次保存递增） | 整数字符串，如 `"1"` | `site_setting:{projectId}` |
| `runtime` | null（不触发重建） | null | `runtime:product-stock:{entityId}` |

约束：

- CMS 实体表（products / articles / categories / tags / menus）必须各自包含 `revision integer NOT NULL DEFAULT 1` 字段，每次保存操作在数据库事务中递增。
- contentSet revision 由 CMS Core 在集合成员变更时计算并维护，不是某个表的单一字段。实现方式可以是独立计数器或 `max(member.revision)`，但必须是稳定的、可重现的。
- 所有 revision 值统一序列化为字符串存入 `BuildDependency.revision` 和数据库 `revision` 列。比较规则是字符串相等性，不做数值大小比较。
- revision 只用于变化检测，不用于排序或版本回溯。历史版本通过 Artifact Manifest 中的 revision 记录追溯。

### 8.2 依赖记录

成功构建后，把实际 Build Dependency 写入对应 Artifact 的依赖表（`page_dependencies` 或 `presentation_dependencies`）。Artifact Manifest 是历史事实，依赖表是按 Artifact 可重建的查询投影。失效查询根据 source 类型连接 `pages.active_artifact_id → page_dependencies.artifact_id` 或 `presentation_instances.active_artifact_id → presentation_dependencies.artifact_id`；发布或回滚成功后，active 指针自然切换当前依赖集合，不允许 staged build 提前替换线上依赖。

```ts
// 完整的 DependencyKind 枚举（对应 SQL CHECK 约束）
type DependencyKind =
  | 'direct_content'         // 构建：实体字段变化（如 product.price）
  | 'content_collection'     // 构建：集合成员变化（如 category.products）
  | 'menu'                   // 构建：菜单项变化
  | 'media'                  // 构建：媒体 revision 变化
  | 'content_template'       // 构建：ContentTemplate 版本变化
  | 'global_component'       // 构建：全局组件版本变化
  | 'site_setting'           // 构建：站点/项目设置变化
  | 'runtime'                // 非构建：仅影响运行时 Fragment 响应（如 product.stock）

// 概念型依赖记录（SQL 中按 source 类型分属 page_dependencies / presentation_dependencies 两张表）
interface DependencyRecord {
  artifactId: string                    // page_artifacts.id 或 presentation_artifacts.id
  dependencyKind: DependencyKind
  targetKey: string                     // 如 "product:100", "menu:footer", "media:{hash}"
  revision: string | null               // build dep: 实体 revision；runtime dep: null
}
```

Build Queue 只消费 `dependencyKind != 'runtime'` 的依赖。Runtime 依赖仅用于 Manifest 声明和 Fragment 缓存键，不触发页面重建。

```text
CMS Entity Revision Changed (non-runtime)
→ lookup page_dependencies + presentation_dependencies WHERE dependencyKind != 'runtime'
→ mark affected Page / PresentationInstance stale
→ enqueue BuildRequest(source_id, expected revisions)
```

典型 fan-out：

- Article 修改（direct_content）：文章 PresentationInstance，以及 active 依赖中直接包含它的 category/archive/page collection。
- Article 新增、删除或排序字段变化（content_collection）：提升对应 `contentSet` revision，使 recent/category/archive 集合页失效，即使旧构建结果中还没有该实体。
- Product 修改（direct_content）：商品 PresentationInstance，以及 active 依赖中直接包含它的 featured/category 聚合 Page。
- Product 新增、删除、上下架或集合排序变化（content_collection）：提升对应 Product `contentSet` revision，使聚合 Page 失效；共享 ContentTemplate 来源不构成依赖。
- Product 库存变化（runtime）：不触发重建，仅使 Fragment Endpoint 缓存失效。
- ContentTemplate 版本变化（content_template）：所有关联 entityType 的 PresentationInstance。
- Menu 修改：实际引用该 MenuLocation 的所有 Page 和 PresentationInstance。
- Project Settings 修改：所有 Page 和 PresentationInstance（MVP 阶段保守 fan-out，待 AST 引用索引落地后再优化为精准失效）。
- Media 修改：引用该 media revision 的 Page 或 PresentationInstance。
- Blueprint 修改：不影响已有 Page。
- Global Component 版本变化（global_component + updatePolicy='auto-update'）：引用该组件的 Page 和 PresentationInstance。

#### stale 状态机

```
依赖变化 → stale = true
  → enqueue BuildRequest
  → Worker 启动：比较当前 draft_version（Page）或实体 revision（PresentationInstance）与请求时值
    ├── 一致 → 构建 → 成功写入 Artifact → stale = false, staged_artifact_id 更新
    └── 不一致 → 标记为 superseded → stale = true（保持），等待新请求
  → publish → active_artifact_id 更新
```

- `stale = true` 时构建请求可以入队，但 Worker 启动后必须校验版本一致性；不一致则退出，`stale` 保持 true。
- 构建成功仅 stage 时 `stale = false`，但 `active_artifact_id` 不变。
- 内容变化后若 `stale` 已经是 true，不重复入队（由依赖表 last_checked 判断）。
- 自动重建默认只产生 staged Artifact；是否自动 publish 由内容类型的显式发布策略决定。

### 8.3 队列规则

- Build Job 以 `(source_type, source_id, draft_version, build_input_hash)` 幂等。
- 同一 source（Page 或 PresentationInstance）同时只允许一个有效发布构建；新 revision 可以使旧任务 superseded（更新 `build_jobs.status = 'superseded'`）。
- Worker 启动时比较当前 `draft_version`（Page）或实体 revision（PresentationInstance）与 `build_jobs.draft_version`；不一致则标记 superseded 退出。
- Worker 必须设置超时、显式失败和有限重试。
- 语法/校验错误不重试；临时存储或队列错误可以指数退避重试。
- 失败不改变 staged/active Artifact。
- 自动重建默认只产生 staged Artifact；是否自动 publish 必须由内容类型的显式发布策略决定。

## 9. 流水线层持久化投影

本节存放与构建产物、URL 激活、依赖追踪、队列和故障恢复相关的数据库表。领域模型相关表（projects / pages / presentation_instances 等）见 `02-domain.md`。

```sql
CREATE TABLE page_artifacts (
    id                           uuid PRIMARY KEY,
    page_id                      uuid NOT NULL REFERENCES pages(id),
    version                      bigint NOT NULL,
    source_document              jsonb NOT NULL,
    page_document_schema_version integer NOT NULL,
    source_hash                  text NOT NULL,
    build_input_manifest         jsonb NOT NULL,
    build_input_hash             text NOT NULL,
    artifact_provider            text NOT NULL,
    artifact_key                 text NOT NULL,
    artifact_hash                text NOT NULL,
    compiler_version             text NOT NULL,
    registry_version             text NOT NULL,
    manifest                     jsonb NOT NULL,
    payload_state                text NOT NULL DEFAULT 'available',
    payload_deleted_at           timestamptz NULL,
    note                         text NOT NULL DEFAULT '',
    created_by                   uuid NOT NULL,
    created_at                   timestamptz NOT NULL,
    UNIQUE (page_id, version),
    UNIQUE (id, page_id),
    CHECK (payload_state IN ('available', 'gc_pending', 'deleted'))
);

CREATE TABLE content_objects (
    content_hash   text PRIMARY KEY,
    provider       text NOT NULL,
    object_key     text NOT NULL,
    byte_size      bigint NOT NULL CHECK (byte_size >= 0),
    created_at     timestamptz NOT NULL,
    deleted_at     timestamptz NULL,
    UNIQUE (provider, object_key)
);

CREATE TABLE page_artifact_objects (
    artifact_id   uuid NOT NULL REFERENCES page_artifacts(id),
    content_hash  text NOT NULL REFERENCES content_objects(content_hash),
    PRIMARY KEY (artifact_id, content_hash)
);

-- pages 表的 staged/active Artifact 复合外键
ALTER TABLE pages
    ADD CONSTRAINT pages_staged_artifact_fk
        FOREIGN KEY (staged_artifact_id, id)
        REFERENCES page_artifacts(id, page_id),
    ADD CONSTRAINT pages_active_artifact_fk
        FOREIGN KEY (active_artifact_id, id)
        REFERENCES page_artifacts(id, page_id);

CREATE TABLE page_dependencies (
    page_id          uuid NOT NULL,               -- pages.id
    artifact_id      uuid NOT NULL,               -- page_artifacts.id
    dependency_kind  text NOT NULL CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime'
    )),
    dependency_key   text NOT NULL,               -- 如 "product:100", "menu:footer"
    revision         text NULL,                   -- build dep: 实体 revision；runtime: null
    last_checked     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (artifact_id, dependency_kind, dependency_key),
    FOREIGN KEY (artifact_id, page_id)
        REFERENCES page_artifacts(id, page_id)
);

CREATE TABLE presentation_artifacts (
    id                           uuid PRIMARY KEY,
    presentation_instance_id     uuid NOT NULL REFERENCES presentation_instances(id),
    snapshot_id                  uuid NOT NULL,
    version                      bigint NOT NULL,
    source_hash                  text NOT NULL,
    build_input_manifest         jsonb NOT NULL,
    build_input_hash             text NOT NULL,
    artifact_provider            text NOT NULL,
    artifact_key                 text NOT NULL,
    artifact_hash                text NOT NULL,
    compiler_version             text NOT NULL,
    registry_version             text NOT NULL,
    manifest                     jsonb NOT NULL,
    payload_state                text NOT NULL DEFAULT 'available',
    payload_deleted_at           timestamptz NULL,
    note                         text NOT NULL DEFAULT '',
    created_by                   uuid NOT NULL,
    created_at                   timestamptz NOT NULL,
    UNIQUE (presentation_instance_id, version),
    UNIQUE (id, presentation_instance_id),
    FOREIGN KEY (snapshot_id, presentation_instance_id)
        REFERENCES document_snapshots(id, presentation_instance_id),
    CHECK (payload_state IN ('available', 'gc_pending', 'deleted'))
);

CREATE TABLE presentation_artifact_objects (
    artifact_id   uuid NOT NULL REFERENCES presentation_artifacts(id),
    content_hash  text NOT NULL REFERENCES content_objects(content_hash),
    PRIMARY KEY (artifact_id, content_hash)
);

-- presentation_instances 表的 staged/active Artifact 复合外键
ALTER TABLE presentation_instances
    ADD CONSTRAINT presentation_instances_staged_artifact_fk
        FOREIGN KEY (staged_artifact_id, id)
        REFERENCES presentation_artifacts(id, presentation_instance_id),
    ADD CONSTRAINT presentation_instances_active_artifact_fk
        FOREIGN KEY (active_artifact_id, id)
        REFERENCES presentation_artifacts(id, presentation_instance_id);

CREATE TABLE presentation_dependencies (
    presentation_id  uuid NOT NULL,               -- presentation_instances.id
    artifact_id      uuid NOT NULL,               -- presentation_artifacts.id
    dependency_kind  text NOT NULL CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime'
    )),
    dependency_key   text NOT NULL,
    revision         text NULL,
    last_checked     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (artifact_id, dependency_kind, dependency_key),
    FOREIGN KEY (artifact_id, presentation_id)
        REFERENCES presentation_artifacts(id, presentation_instance_id)
);

CREATE TABLE publication_events (
    id                   uuid PRIMARY KEY,
    source_type          text NOT NULL CHECK (source_type IN ('page', 'presentation')),
    page_id              uuid NULL REFERENCES pages(id),
    presentation_id      uuid NULL REFERENCES presentation_instances(id),
    action               text NOT NULL,
    path                 text NOT NULL,
    from_artifact_id     uuid NULL,
    to_artifact_id       uuid NULL,
    receipt              jsonb NOT NULL,
    created_by           uuid NOT NULL,
    created_at           timestamptz NOT NULL,
    CHECK (
        (source_type = 'page' AND page_id IS NOT NULL AND presentation_id IS NULL)
        OR (source_type = 'presentation' AND page_id IS NULL AND presentation_id IS NOT NULL)
    )
);

-- 构建队列
CREATE TABLE build_jobs (
    id                uuid PRIMARY KEY,
    source_type       text NOT NULL CHECK (source_type IN ('page', 'presentation')),
    source_id         uuid NOT NULL,
    draft_version     bigint NOT NULL,
    build_input_hash  text NOT NULL,
    status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'superseded', 'failed', 'succeeded')),
    artifact_id       uuid NULL,
    error_message     text NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    started_at        timestamptz NULL,
    completed_at      timestamptz NULL
);

-- 发布回执，用于故障恢复
CREATE TABLE publication_receipts (
    id                uuid PRIMARY KEY,
    source_type       text NOT NULL CHECK (source_type IN ('page', 'presentation')),
    source_id         uuid NOT NULL,
    action            text NOT NULL,
    path              text NOT NULL,
    from_artifact_id  uuid NULL,
    to_artifact_id    uuid NULL,
    receipt_state     text NOT NULL CHECK (receipt_state IN ('pending', 'committed', 'rolled_back')),
    receipt_data      jsonb NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz NULL
);
```

### 9.1 表语义说明

- `page_artifacts` 保存审计与重建所需的 JSON、hash 和 Locator，不保存 Artifact 文件字节。`presentation_artifacts` 对应为 PresentationInstance 保存相同闭包。
- `content_objects` 是共享内容对象的 Locator 投影，`page_artifact_objects` 和 `presentation_artifact_objects` 是 manifest 闭包的引用投影；Artifact payload 完成 GC 后可删除对应引用投影，但不可改写历史 manifest。
- `payload_state` 是存储生命周期状态，不改变 Artifact 的源码和构建输入不可变性。
- `page_dependencies` 和 `presentation_dependencies` 按 Artifact 保存历史依赖；只有 active Artifact 对应的依赖用于线上失效，staged 构建不得覆盖 active 依赖。`dependency_kind` 区分构建依赖与运行时依赖（`runtime` 不影响重建队列）。
- `build_jobs` 记录构建队列状态。
- `publication_receipts` 记录发布操作的幂等回执，用于 `PublicationStore` 故障恢复：构建成功后写入 `receipt_state='pending'`，原子激活 URL 后更新为 `'committed'`，回滚后更新为 `'rolled_back'`。恢复任务扫描 pending 状态的 receipt，根据 Store inspect 结果决定前滚或回退。