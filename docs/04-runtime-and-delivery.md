# 04 · 运行时能力与交付

> 本文集中存放运行时层的动态能力协议（Runtime Fragment / Client Enhancement）、横切关注点（SEO / 安全 / 性能）、以及面向交付的内容（Phase 顺序、验收清单、迁移结论）。

## 1. 组件执行模型

### 1.1 三类执行能力

```ts
interface RuntimeFragmentProps {
  fragment: RuntimeFragmentType
  context?: 'currentProduct' | 'visitorSession' | 'searchQuery'
  trigger: 'load' | 'revealed' | 'submit'
  fallback: RuntimeFragmentFallback
}

type RuntimeFragmentType =
  | 'productAvailability'
  | 'productLivePrice'
  | 'cartSummary'
  | 'loginPanel'
  | 'searchResults'

type ClientEnhancementRef =
  | { type: 'disclosure' }
  | { type: 'carousel'; autoplay: boolean }
  | { type: 'copyText'; source: 'nodeText' }

type RuntimeCapabilityRequirement =
  | { kind: 'fragment'; type: RuntimeFragmentType; protocolVersion: number }
  | { kind: 'enhancement'; type: ClientEnhancementRef['type']; moduleHash: string }
```

`BuildComponent` 在 Publish Compiler 中解析为最终 HTML，包括：

- 标题、正文、摘要和 SEO 内容
- 商品名称、基础描述、基础价格和媒体
- 菜单
- 分类、归档、分页和其他有限集合

`RuntimeFragment` 是默认动态能力路径。Compiler 输出带可访问 fallback 的 HTMX host；HTMX 再向 Registry 固定的 Fragment Endpoint 请求 HTML，并用返回片段替换目标区域：

```html
<div
  hx-get="/_fragments/product-availability?ref=compiler-issued-ref"
  hx-trigger="load"
  hx-swap="outerHTML"
>
  <span>库存状态暂不可用</span>
</div>
```

`ClientEnhancement` 只渐进增强已经完整输出的 HTML，例如 disclosure、carousel 和复制文本。它不拥有 CMS 查询能力，也不作为调用任意 JSON API 的通用逃生口。

能力选择顺序：

1. 发布时可确定且影响 SEO/首屏语义的数据必须使用 Build Component。
2. 需要服务端状态、认证、查询或写操作时，优先使用 HTMX Runtime Fragment。
3. 只有纯浏览器交互或必须使用浏览器 API 时，才使用 Client Enhancement。
4. 不允许为了避免重建，把本可静态固化的正文、商品基础信息或有限集合改成动态片段。

### 1.2 Runtime Fragment 协议

- Fragment Type、props schema、路由、HTTP method、Go Handler 和响应模板全部来自 Registry。
- Page Document 只保存语义化 Fragment Type、上下文和触发方式，不保存 endpoint、header、脚本、模块 URL 或任意请求参数。
- Compiler 根据 Page、BuildContext 和 Registry 生成请求声明；`compiler-issued-ref` 只能引用允许公开的实体上下文，不能包含用户 token、凭据或私有字段。
- Handler 不读取 Page Document，不执行 Jet，不解释 Binding；它只实现该 Fragment capability 的固定运行时协议并返回 HTML 片段。
- Fragment 响应使用固定模板和统一 escape/sanitize 规则，不返回由客户端解释的 AST 或模板表达式。
- GET 只用于无副作用查询；购物车等写操作使用带 CSRF 防护的 POST，并在成功后返回新的 HTML 状态。
- Handler 明确匿名/认证策略、权限、限流、超时、缓存和错误响应；超时或失败时保留或返回可访问 fallback。
- 搜索关键字只能进入固定 Search Fragment 的受约束参数，必须限制长度、分页和排序枚举，不能进入通用 Query DSL。
- Fragment host 的 fallback 不能是空白区域，必须是语义化骨架或可访问占位内容（如"库存状态加载中…"），避免访客看到内容闪烁后跳变。骨架态属于静态 Artifact 的一部分，由 Compiler 在构建期固化，不依赖运行时请求。

### 1.3 Client Enhancement 协议

- Enhancement Type、配置 schema 和内容寻址 JS module 全部来自 Registry。
- Document 不能保存 JavaScript、动态 import URL、CSS selector 查询脚本或网络 endpoint。
- 模块只在声明的 host 元素作用域内工作，不扫描或接管整页 DOM。
- Enhancement 默认不得发起网络请求；需要服务端交互时改用 Runtime Fragment 或 Registry 明确组合的 HTMX 行为。
- props 必须安全序列化，不嵌入用户 ID、token 或个性化数据。
- JavaScript 加载失败或被禁用时，基础内容、链接、表单和可访问语义仍成立。

### 1.4 Capability 发布边界

- `requiredRuntimeCapabilities` 由实际 Runtime Fragment 和 Client Enhancement 自动推导，Document 不直接编辑该列表。
- 发布与回滚必须确认目标环境支持 manifest 中每个 capability 及其协议版本。
- Runtime Fragment 的服务部署可独立于静态 Artifact，但 capability 协议必须向后兼容；不兼容升级必须先部署新版本，再发布引用它的 Page。
- 标题、正文、商品基础描述、canonical 和其他关键 SEO 内容不得只存在于动态能力中。

## 2. SEO、安全与性能

### 2.1 SEO

- Artifact 必须包含完整 title、description、canonical、Open Graph 和可索引正文。
- Heading 层级问题生成 warning；缺少 canonical 或关键正文生成 error。
- Category、Archive 和分页输出稳定 canonical/prev/next 策略。
- Runtime Fragment 和 Client Enhancement 不承载唯一的标题、正文或产品基础描述。

### 2.2 安全

- HTML 标签来自固定 AllowedTag。
- 普通文本默认 escape。
- 富文本只来自已净化字段。
- URL 协议使用白名单。
- Artifact 校验路径穿越、文件 hash、manifest 引用和 CSP 要求。
- Page path 与 Artifact key 完全分离。
- Publish Compiler 不读取 BuildContext 之外的数据库或网络。
- Builder 管理员权限不等于任意代码执行权限。

### 2.3 性能

- 访客页面请求不查询 CMS 数据库、不执行 Jet、不解释 AST。
- HTML、CSS、JS 可由 CDN 长期缓存；hash 资源使用 immutable cache。
- HTML 激活采用 URL 级原子切换。
- 页面 bundle 按实际组件集合生成，避免每个组件一个请求，也避免每页携带全部组件代码。
- Build fan-out 有上限、队列与 backpressure，禁止同步内容保存阻塞全站重建。

### 2.4 运行时路由表

| 路由 | 语义 |
|---|---|
| `GET /livez` | 存活探针：进程存活 |
| `GET /readyz` | 就绪探针：组件级就绪（数据库 / Redis / Casbin 等） |
| `/static` | 后台静态资源（`internal/templates/static`） |
| `/storage` | 媒体上传存储，静态直出（`public/storage`） |
| `/site` | 激活产物静态面（`http.Dir` 只读，零查库零模板） |

## 3. Phase 交付顺序

### 3.1 Phase 0-A1：Page 手工页面静态发布主链

只做手工 Page 路径，不引入 PresentationInstance / ContentTemplate / DocumentSnapshot，用最小代价验证「Document → Compiler → Artifact → PublicationStore → 静态访问」核心链路。

实现：

- PageDocument / ThemeNode / Binding Registry
- Migrate / Normalize / Validate
- BuildContext fixture
- Fragment / Style / Asset / Diagnostic Pipeline
- Jet Build-time Render
- ArtifactStore local provider
- PublicationStore local provider
- Page Draft / Stage / Publish / Rollback
- 静态 HTTP 访问

0-A1 已实现可视化工作台外壳（`/workbench` 画布、拖拽、检查器 schema、块编辑、预览编译）；依赖 fan-out、Runtime Fragment、Client Enhancement、PresentationInstance、ContentTemplate 仍属规划，E2E 验收清单保持规划态。

验收：手写 Page Document 可以确定性生成 Artifact，发布到一个 URL，修改后再次发布，并只回滚该 Page。同一 Document + BuildContext 两次构建产生相同 hash。

### 3.2 Phase 0-A2：内容自动发布（PresentationInstance）

在 0-A1 核心链路稳定后，引入内容实体自动发布路径。

实现：

- PresentationInstance 基础结构（实体创建、DocumentSnapshot 派生）
- ContentTemplate 版本管理（创建、版本不可变、Binding 约束）
- DocumentSnapshot 从 ContentTemplate + CMS 实体派生
- PresentationInstance 独立 Draft / Stage / Publish / Rollback

不实现依赖 fan-out（ContentTemplate 版本变化触发重建留到 0-C）、Runtime Fragment。

验收：ContentTemplate 能派生 DocumentSnapshot，PresentationInstance 能独立发布到 CMS 实体对应的 URL。Product/Article 创建后自动产生公开页面。

### 3.3 Phase 0-B：Builder 与 Blueprint

实现：

- Blueprint version 与快照实例化
- ContentTemplate 版本管理（创建、版本不可变、Binding 约束）
- Global Component version、固定引用与显式升级
- Global Component Update Policy（immutable / auto-update / pinned）
- Registry Manifest
- Editor Kernel
- Command / Undo / Redo
- iframe Preview Build
- Alpine / Sortable / Inspector
- 草稿乐观锁

### 3.4 Phase 0-C：构建图与资源治理

实现：

- page_dependencies（含 DependencyKind 与 runtime 过滤）
- stale 标记与 Build Queue
- ContentTemplate fan-out（版本变化→重建所有实例）
- PresentationInstance stale 标记与自动重建
- Global Component auto-update fan-out
- Category / Archive / Pagination fan-out
- 内容寻址资源复用
- URL 修改与 Redirect Artifact
- Tombstone、保留期和安全 GC
- Publication 故障恢复任务

### 3.5 Phase 0-D：受控动态能力

在静态发布主链稳定后按优先级加入：

- Runtime Fragment Registry 与版本化 capability
- HTMX Runtime Fragment Endpoint
- 库存、搜索、购物车数量和登录状态 Fragment
- Client Enhancement Registry
- 内容寻址 Enhancement bundle
- Global Component Update Policy 运行时联动（auto-update → rebuild queue）
- CSP、认证/CSRF、超时、fallback 和无 JavaScript 验收

## 4. 验收清单

### 4.1 协议与编辑器

1. Page Document 能完成序列化、Migration、Normalize 和 Validate。
2. Node ID 在整份 Document 内唯一，duplicate/undo/redo 保持固定映射。
3. Blueprint 实例化生成独立 AST，来源版本只保存在 Page 审计字段；后续修改 Blueprint 不改变 Page。
4. ContentTemplate 版本管理正确：新版本不可变，旧版本保留；版本变化触发 entityType 关联的 PresentationInstance fan-out。
5. PresentationInstance 从 ContentTemplate 派生 DocumentSnapshot，Bindings 正确解析为实体字段字面量。
6. GlobalComponentNode 固定引用不可变版本，不允许局部 override；auto-update 策略下新版本触发依赖页重建，pinned 覆盖生效。
7. Binding Scope、valueType、renderer 和 ContentTarget 组合校验正确。
8. Command 是 Document 唯一写入口，Replay 后 Document 与 NodeIndex 一致。
9. Preview 与 Publish 使用相同 Registry、Lowering、Style 和 Jet pipeline。
10. BuildContext 只能携带类型化 Field、Collection、Menu 和 Media；金额采用 minorUnits，未知 Binding/valueType 组合被拒绝。

### 4.2 构建与 Artifact

1. 同一规范化 Document、BuildContext 和 Compiler 得到完全相同文件和 hash。
2. 最终 HTML 不包含 Jet 表达式、FieldBinding 或编辑器属性。
3. Source Map 能把 HTML/CSS 诊断定位到 Node ID。
4. 内容寻址资源不会被后续 Page 发布覆盖。
5. Manifest 的文件 hash、依赖 revision 和 capability 可完整校验。
6. BuildContext 固定后，Compiler 不再读取数据库或网络。

### 4.3 发布状态机

1. URL 规范化、冲突、保留路径和路径穿越被拒绝。
2. Artifact 写入或激活失败时，旧 URL 继续提供旧版本。
3. 数据库提交失败后的 Receipt 恢复能前滚或回退到一致状态。
4. 并发旧 Draft 或旧 Dependency Revision 的构建不能 stage/publish。
5. 回滚 Page A 不改变 Page B 的 URL、Artifact 或资源。
6. canonicalPath 与当前 active_path 不同的历史 Artifact 不能直接回滚。
7. URL 修改先激活新地址，再处理旧地址，任一步失败至少保留旧地址可用。
8. 删除内容后 URL 最终取消激活，历史 Artifact 仍可恢复。
9. GC 不删除任何被 Page、Publication、Redirect 或 Job 引用的对象。

### 4.4 依赖与动态能力

1. Article 修改只标记 active 依赖中真实读取它的 Page 或 PresentationInstance stale。
2. Article 新增、删除或排序变化能通过 contentSet revision 使尚未直接依赖该实体的列表页失效。
3. staged Artifact 的依赖不能覆盖 active Artifact 的失效索引。
4. Menu 修改能 fan-out 到所有实际引用该位置的 Page 和 PresentationInstance。
5. Blueprint 修改不触发已有 Page 重建。ContentTemplate 版本变化触发关联 PresentationInstance 重建。
6. 运行时依赖（如 product.stock）不触发页面重建，仅影响 Fragment 响应。
7. DependencyKind 正确区分 build dependency 与 runtime dependency；Build Queue 只消费非 runtime 依赖。
8. Superseded Build Job 不能覆盖新 Artifact。
9. Runtime Fragment props 不允许注入 endpoint、script、用户凭据或危险序列化内容。
10. Fragment Endpoint 强制执行 method、认证、权限、CSRF、限流和超时协议。
11. Fragment 失败时 fallback 可访问，Client Enhancement 禁用时基础 HTML 与操作仍成立。
12. SEO 关键内容不依赖 Runtime Fragment 或 Client Enhancement。
13. 搜索 Fragment 的查询、分页和排序参数受到固定 schema 与资源上限约束。

## 5. 从 v3.7 的迁移结论

v4.1 是产品和协议的破坏性替换，不对 v3.7 Theme Document 做自动兼容承诺。迁移实现前必须单独编写离线转换器，并以输出等价性为验收目标：

- 原 Home Template 可转换为一个 Home Blueprint 和 Home Page。
- 原 Page/Post Template 只能转换为 Blueprint，不能自动为所有 CMS 内容创建并发布 Page。
- 原 Theme Version Artifact 是动态 Jet 模板，不能登记为 v4 Page Artifact。
- 原 Component、Style、Variant、Fragment 和 Binding 定义可作为 Registry 迁移输入。
- 原 Theme Version 历史保留只读，不与 Page Artifact 历史混写。

在 v4.0 Phase 0-A 完成前，不实现自动迁移，也不保留访问时 Theme Runtime 作为双轨兜底，避免两套发布语义长期并存。