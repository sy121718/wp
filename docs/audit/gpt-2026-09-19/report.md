# go_wp 全项目审查与修复说明（gpt）

架构主线成立，仓库已远超基础框架阶段。最需要修的是变更如何可靠到达所有语言产物，而不是重命名目录或统一字段类型。本轮落地 12 组修复；其余问题逐项标明，未宣称全面生产就绪。

审查基线：`71b0576203e5317fa0b431758b74e82246b1b621`。独立工作树：`/home/sky/project/go/wp-gpt`，分支 `gpt`。主工作区未提交改动未带入、未覆盖。

## 证据边界

完整读取并扫描 4412 个 Git 跟踪文件、492212 文本行，其中 1778 个 Go 文件。这里的“全量”是逐文件读取与完整测试范围，不是宣称 49 万行都做过逐行人工证明。详细哈希见 [coverage.json](coverage.json)。核心发布、依赖、数据库、权限和 UI 链路另做人工追踪。

没有 DBX 工具，因此现场业务库的 schema 漂移、索引使用率、膨胀、实际连接角色和执行计划未验证。没有绕过项目规则使用 psql 查询业务库。真实 PostgreSQL 迁移测试和隔离 UI 夹具只证明对应测试条件，不代表生产容量。

浏览器写操作只作用于隔离测试数据库；现有 8080 服务仅登录和查看。工作台 CSS 修复的后验是禁用脚本的真实 DOM 回放，使用 gpt 分支 CSS；没有把它冒充 gpt 完整后端端到端验收。

## 先纠正项目描述

- CMS、Builder、ArtifactStore、发布队列与电商业务均已有实现。
- Jet 还承担控制面后台 SSR；禁止的是普通静态访客响应执行模板，不是禁止所有运行期 Go。
- 默认语言无前缀与非默认语言短码是当前 default_plain 配置，并非所有语言强制前缀。
- 静态页面响应与公开动态 API 必须区分；购物车/结算/访客账号存在写入，不能笼统说 analytics 是整个访问面的唯一写路径。
- 插件 manifest 配置驱动已经存在；需要的是能力分级、验证和隔离，不是从零把 Go 注册表改成 YAML。

## 推荐的唯一演进方向

业务写事务 + outbox → 幂等依赖扇出 → 带 project/lang/intent/lease 的 BuildJob → 冻结 PublicationPlan/BuildContext → 锁外编译不可变 Artifact → 校验输入版本 → 激活批次 + 回执收敛。

继续保留模块化单体。先补可靠变更传播、按语言状态、任务租约和发布计划，再优化查询与资源成本。不推荐此时拆微服务、重造通用 Repository、把所有 JSONB 拆表，或为了形式统一重做主键。

建议维持以下包归属：

| 目录 | 职责 | 禁止扩张到 |
|---|---|---|
| `internal/builder/core` | 编译协议、受限组件契约、构建上下文 | CMS 持久化与 HTTP |
| `internal/builder/source` | 共享零依赖数据形状 | GORM、业务实现 |
| `internal/builder/components/<type>` | props/schema、构建视图、CSS/增强声明 | 任意数据库和远端请求 |
| `internal/pipeline` | Artifact、发布激活、依赖编排协议 | 各业务表查询 |
| `internal/module/<domain>/contract` | 消费方需要的能力与不可变投影 | 大而全的服务实现镜像 |
| `internal/module/<domain>/service` | 用例、业务规则、跨仓储事务 | 拼接 GORM 查询 |
| `internal/module/<domain>/model` | 本聚合/本模块表访问与具名 Tx 方法 | 跨模块调用与业务状态决策 |
| `internal/routers` | composition root 接线 | 业务规则 |
| `internal/templates/static/js/ui` | 原始交互、焦点与键盘能力 | 库存/订单等领域决策 |
| `public/migrations` | 生产 DDL/seed 真源 | 让模型 AutoMigrate 再造一份 schema |

对于 PHP 背景贡献者，关键差别是：Go 内置能力编进二进制，更新实现不等于已发布 HTML 自动变化；声明式 manifest 可以运行时安装，但不能凭配置凭空增加未编译的任意 Go 代码。Build、Publish、Rebuild 必须明确区分。

## 1. 动态内容与可视化结构

数据和结构的主分离是正确的；薄弱处在依赖传播与发布一致性。

- CMS/product 保存业务真源；FieldBinding/CollectionSource 是受限读取协议，不是查询 DSL。
- ContentTemplate 是版本化结构来源，实例生成 DocumentSnapshot；document 模式允许单商品脱离正文模板。
- Blueprint 与复制式预设只初始化；globalref 才是持久复用引用。修改一个复制节点不会自动修改所有商品页，这是语义差别，界面必须说清。

复用能力已存在；本次修复嵌套块消费、实例失效、多语言反查和结构模板模式过滤。下一步优先统一 outbox→依赖→任务→激活闭环。

### FIX-05 · 独立文档模式把页眉/页脚结构模板更新也一并豁免

**P1 · 已修复 · 影响 4/5 · 成本 2/5 · 置信度：高**

document 模式应脱离自身正文模板，仍消费站点结构模板。原模式过滤过宽，使结构模板变化无法标记独立商品页。

整改：仅在依赖键等于实例绑定正文模板时豁免 document 模式；其他结构模板依赖仍触发 stale。

验收：同一独立实例：正文模板依赖不命中，外部结构模板命中；且命中其他语言的当前发布产物。

证据：
- [internal/module/presentation/model/presentation_mode_model.go:107](/home/sky/project/go/wp-gpt/internal/module/presentation/model/presentation_mode_model.go:107)：`func (m *Model) MarkStaleTemplateModeByDependency`
- [public/test/presentation/unit/presentation_dependency_locales_test.go:57](/home/sky/project/go/wp-gpt/public/test/presentation/unit/presentation_dependency_locales_test.go:57)：`脱离正文模板`

### FIX-06 · 嵌套全局块消费记录和自动发布实例的块失效接线不完整

**P1 · 已修复 · 影响 5/5 · 成本 2/5 · 置信度：高**

仅静态扫外层文档不足以识别嵌套块。块变更桥接原本只通知 Page，自动实例即使有依赖也不会被这条入口标记。

整改：globalref 展开时调用 UseBlock；块更新桥接显式注入窄 DependencyTarget，同时通知自动实例。只负责标 stale，保持现有块更新的发布策略。

验收：嵌套 outer→inner 两者都进入消费集合；装配回归验证两类发布来源都收到块键，错误不被桥接吞掉。删除保护并未因此自动完整，见 ARCH-02。

证据：
- [internal/builder/jetview.go:1275](/home/sky/project/go/wp-gpt/internal/builder/jetview.go:1275)：`ctx.UseBlock(blockID)`
- [internal/routers/block_page_bridge.go:43](/home/sky/project/go/wp-gpt/internal/routers/block_page_bridge.go:43)：`presentations.MarkStaleByDependency`
- [internal/builder/globalref_usage_test.go:16](/home/sky/project/go/wp-gpt/internal/builder/globalref_usage_test.go:16)：`func TestGlobalrefRecordsNestedCompileDependencies`

### ARCH-01 · 商品主写路径只失效运行时片段缓存，静态产物没有完整失效闭环

**P1 · 待整改 · 影响 5/5 · 成本 4/5 · 置信度：高**

product_crud 的 Create/Update/Delete 调用 bumpFragmentCache；装配给商品注入的也是片段缓存回调。与静态依赖 Fanout 是两条链，不能用“片段刷新了”推导详情和集合静态页已更新。

整改：在商品聚合提交事务内写 outbox，事件含 project、entity、变更版本和依赖键；消费者同时处理 direct_content 与集合成员变化。价格、变体、分类、品牌、标签写入口进入同一失效策略。

验收：改标题、价格、上下架、分类及增删商品后，详情和列表全部语言最终更新；事务回滚无事件；消费者崩溃可重放且幂等。

证据：
- [internal/module/product/service/product_crud.go:283](/home/sky/project/go/wp-gpt/internal/module/product/service/product_crud.go:283)：`s.bumpFragmentCache(ctx, projectID)`
- [internal/routers/assembly_publish.go:141](/home/sky/project/go/wp-gpt/internal/routers/assembly_publish.go:141)：`SetFragmentCacheBumper`
- [internal/pipeline/dependency.go:126](/home/sky/project/go/wp-gpt/internal/pipeline/dependency.go:126)：`type DependencyTarget`

## 2. 组件、区块、页面、主题

应理解为依赖图，不能强行做四级继承树。

- Component 有 props 校验/元信息和 children；不等于支持任意命名 slots 或任意 JS 事件。行为通过受控增强/片段能力实现。
- Block 是可复用 AST；Page 是路由、草稿和编排；ContentTemplate 是内容页面结构；Theme 是视觉 token 与结构绑定。
- 固定页眉/页脚槽位与变化的正文已通过共同装配进入编译。主题不是访客请求时运行的模板引擎。

保留这些不同生命周期的概念，UI 明确“编辑本实例/编辑共享块/编辑模板”的影响范围；不要把主题重新做成万能业务插件。

### ARCH-02 · 全局块删除保护只覆盖主题和 Page

**P1 · 待整改 · 影响 5/5 · 成本 3/5 · 置信度：高**

BlockReferenceChecker 只查主题和 pages.CountBlockReference；本次修复的是变更失效，并未补齐自动实例、模板、其他块中引用的删除保护。实体仍可被其他结构消费时删除，下一次构建才暴露缺失。

整改：引用归属由 Page、Presentation、ContentTemplate、Block 各自提供只读契约，装配层合并；将“已发布产物引用”和“可编辑源码引用”分开显示。先禁止删除有有效源码引用的块，再提供替换引用操作。

验收：引用仅存在于嵌套块、未发布模板、独立实例时均拒绝删除；历史不可变 Artifact 的保留由 GC 策略决定，不能永久阻断源码删除。

证据：
- [internal/routers/block_page_bridge.go:52](/home/sky/project/go/wp-gpt/internal/routers/block_page_bridge.go:52)：`func BlockReferenceChecker`

### ARCH-03 · 组件升级 stale 判定包含历史产物，且自动实例缺少同等启动入口

**P1 · 待整改 · 影响 4/5 · 成本 3/5 · 置信度：高**

artifact.ListPageIDsByOtherRegistryVersion 扫全部 available 历史行，旧产物未 GC 时每次重启仍可能把已重建页面标 stale。启动只调用 Page 的 RegistryVersion 比对；自动实例虽然保存版本字段，不能据此认定有启动收敛。

整改：先由来源模块选出各语言当前 active/staged 产物，再经 artifact 契约比对版本；自动实例提供相同契约。两种来源共同输出精确影响面，历史回滚产物不参与当前 stale 判定。

验收：重建后保留旧历史再重启不误标；自动实例组件升级会标记；仅另一语言旧组件也必须命中。

证据：
- [internal/module/artifact/model/artifact_model.go:244](/home/sky/project/go/wp-gpt/internal/module/artifact/model/artifact_model.go:244)：`func (m *Model) ListPageIDsByOtherRegistryVersion`
- [internal/routers/assembly_publish.go:408](/home/sky/project/go/wp-gpt/internal/routers/assembly_publish.go:408)：`pageService.MarkStaleByRegistryVersion`
- [internal/module/presentation/service/presentation_persist.go:113](/home/sky/project/go/wp-gpt/internal/module/presentation/service/presentation_persist.go:113)：`RegistryVersion: builder.RegistryVersion()`

### ARCH-05 · 缺失结构模板/块的降级行为可能把配置错误发布成不完整页面

**P1 · 待整改 · 影响 4/5 · 成本 3/5 · 置信度：高**

BuildStructureSlots 的注释和装配说明明确存在模板缺失/非法时回退块的路径；这对预览友好，却会弱化发布阶段的失败可见性。不能只看 HTTP 构建成功判断结构完整。

整改：明确 CompileModePreview 与 CompileModePublish：预览返回带归因的占位和诊断，发布对显式绑定但不可用的结构依赖失败。无绑定才允许按显式设计回退。诊断进入 Manifest 和工作台。

验收：删除/禁用已绑定模板、跨工程绑定、循环块、缺插件时发布失败且原线上产物保持；预览可定位到节点或槽位。

证据：
- [internal/module/page/service/page_assemble.go:171](/home/sky/project/go/wp-gpt/internal/module/page/service/page_assemble.go:171)：`模板未绑定 / 不存在 / 文档非法时`
- [internal/pipeline/structure_template.go:138](/home/sky/project/go/wp-gpt/internal/pipeline/structure_template.go:138)：`func BuildStructureSlots`

## 3. 多语言与 SEO

构建期能力较完整，批次确定性和失败策略仍需收敛。

- 配置默认是 default_plain：默认语言不加前缀，其他语言使用短码；用户假定一律 /{lang}/{path} 与当前示例不一致。
- 已有 lang、canonical、OG/Twitter、JSON-LD、hreflang、sitemap、SEO 巡检/评分与内容翻译批量取词。
- 不同语言 URL 可独立缓存；个性化 Runtime Fragment 不能照搬静态 Artifact 的缓存策略。

使用统一发布计划冻结语言和 URL；将缺译、方向和 SEO 校验变成发布诊断。SEO 分数与外部搜索表现分开。

### FIX-04 · 依赖反查漏掉其他语言的已发布或暂存产物

**P1 · 已修复 · 影响 5/5 · 成本 2/5 · 置信度：高**

pages/presentation_instances 主行是最近一次产物镜像，无法代表全部语言。原 SQL 只认 active/staged 两个指针，其他语言独有的依赖会漏标。

整改：Page 查询同时纳入 page_publications 与 page_stagings；自动实例纳入 presentation_publications。同步修改只读反查、普通写入、事务写入三类入口。

验收：真实生产迁移库验证：镜像指向 B，A 仅存在于语言账本时仍命中；不再活跃的历史产物不得命中。

证据：
- [internal/module/page/model/page_dependency_lookup_model.go:70](/home/sky/project/go/wp-gpt/internal/module/page/model/page_dependency_lookup_model.go:70)：`page_publications`
- [internal/module/page/model/page_tx.go:120](/home/sky/project/go/wp-gpt/internal/module/page/model/page_tx.go:120)：`page_publications`
- [internal/module/presentation/model/presentation_model.go:645](/home/sky/project/go/wp-gpt/internal/module/presentation/model/presentation_model.go:645)：`OR d.artifact_id IN (SELECT artifact_id FROM presentation_publications`
- [internal/module/page/model/page_dependency_locales_test.go:15](/home/sky/project/go/wp-gpt/internal/module/page/model/page_dependency_locales_test.go:15)：`func TestDependencyLookupIncludesEachLocaleLedger`

### FIX-12 · 国际化 seed 数量账本漏记角色权限页 15 对词条

**P2 · 已修复 · 影响 2/5 · 成本 1/5 · 置信度：高**

全量迁移两轮结果稳定为 zh-CN 3769 / en-US 3654，旧断言仍为 3754 / 3639。差异来自已注册的 228 角色权限页 15 对词条。

整改：核对具体 seed 后更新精确计数，保留逐 key 的业务语义验证；删除难以维护的超长历史算术注释。

验收：两轮真实迁移/seed 幂等测试通过，不以放宽阈值或删除断言换绿。

证据：
- [public/migrations/register_core.go:296](/home/sky/project/go/wp-gpt/public/migrations/register_core.go:296)：`228-i18n-seed-role-permissions`
- [public/test/pkg/i18n/i18n_seed_functional_test.go:126](/home/sky/project/go/wp-gpt/public/test/pkg/i18n/i18n_seed_functional_test.go:126)：`角色权限页新增 15 对词条`

### I18N-01 · 手工 Page 的 hreflang 构建输入仍受当前线上状态影响

**P1 · 待整改 · 影响 4/5 · 成本 4/5 · 置信度：中高：源码证据，故障注入待补**

Page 的 RoutePublished 在构建中读取访问面现状；presentation 已有 TargetLangs 批次概念，Page 未同等冻结。先构建两种语言、逐个发布时，第二次一致性构建看到的在线语言集合可能变化。另一个风险是批次部分发布成功后互指到未激活语言。

整改：两类来源统一 PublicationPlan：冻结目标语言、URL、默认语言、canonical origin 和构建依赖版本；先全量产物准备并验证，再激活批次。失败时保留旧批次，或明确记录部分成功并触发互指重建。

验收：不同语言构建顺序、发布顺序与中途失败不改变同一计划的字节；每条 hreflang 目标在同一激活批次可达。当前结论为控制流风险，未执行整站故障注入。

证据：
- [internal/module/page/service/page_assemble.go:147](/home/sky/project/go/wp-gpt/internal/module/page/service/page_assemble.go:147)：`RoutePublished: publishScope`
- [internal/pipeline/site_lang.go:124](/home/sky/project/go/wp-gpt/internal/pipeline/site_lang.go:124)：`TargetLangs 本批次`

### I18N-02 · 语言失败回退与 RTL 缺少完整发布验收

**P2 · 待整改 · 影响 3/5 · 成本 3/5 · 置信度：中：RTL 未做完整语言环境实测**

EnabledLangs 读取失败会降级为默认语言；内容译文缺失已有统计日志，但发布是否允许回退缺少统一等级策略。已检查的构建输入有 Lang，未见与之对应的完整 dir/RTL 发布路径及视觉回归。

整改：预览允许可见告警回退；发布冻结语言表，清单读取失败直接失败。为字段声明 required/fallback 策略并把缺译统计写入 Manifest。引入 locale direction 元数据，输出 html dir，逐步替换布局物理方向属性为逻辑属性。

验收：至少覆盖默认语言、英文、一个 RTL 语言；混排货币/数字、导航、表格、抽屉和富文本有截图基线；禁止把日志里的缺译告警当作发布质量检查。

证据：
- [internal/pipeline/site_lang.go:35](/home/sky/project/go/wp-gpt/internal/pipeline/site_lang.go:35)：`func EnabledLangs`
- [internal/pipeline/content_miss.go:8](/home/sky/project/go/wp-gpt/internal/pipeline/content_miss.go:8)：`func LogContentTranslationMisses`
- [internal/builder/builder.go:60](/home/sky/project/go/wp-gpt/internal/builder/builder.go:60)：`Lang 本次编译的目标语言`

### SEO-01 · SEO 评分、技术可抓取性与实际搜索表现需要分开

**P2 · 待整改 · 影响 3/5 · 成本 2/5 · 置信度：高**

仓库已实现构建期 SEO head、sitemap、结构化数据和评分，不存在“全部发布后补救”的空白。但内容评分规则不是搜索引擎排名算法，也不能证明 canonical/hreflang 的线上目标实际正确。

整改：构建期做确定性合规校验：唯一 canonical、语种/路径一致、JSON-LD 可解析且与内容一致、标题和索引策略明确。发布后按激活清单抓取校验 sitemap/重定向/互指；运营表现单独对接站长平台数据，保留维度和采样时间。

验收：技术巡检输出 URL、规则、证据、ArtifactHash；运营报告按语言/页面/查询观察展示、点击和收录变化，不将单个内部 SEO 分数作为发布成功或排名保证。

证据：
- [internal/builder/seo_head.go:68](/home/sky/project/go/wp-gpt/internal/builder/seo_head.go:68)：`func BuildSEOHead`
- [internal/seo/sitemap.go:1](/home/sky/project/go/wp-gpt/internal/seo/sitemap.go:1)：`package seo`
- [internal/seo/scoring/scoring.go:5](/home/sky/project/go/wp-gpt/internal/seo/scoring/scoring.go:5)：`package scoring`

## 4. 性能、内存与静态交付

优先改有源码证据的查询/调度问题，暂不做无测量的对象池优化。

- Publisher 内核锁外编译，本地不可变目录和 symlink+rename 激活已经实现。
- 构建会保存必要快照，GetArtifact 在控制面读取完整文件；不能把它误认为每次访客请求都反序列化 Artifact。
- 插件启用集有指纹缓存；译文有批量查询；不能泛称全系统存在 N+1。标签页有明确逐标签查询。

预算控制并发和输出大小；分解构建阶段指标。快照复制是并发隔离成本，只有 pprof 证实热点后才优化 pooling/preallocation。

### FIX-03 · gzip 忽略 q=0，identity 与 304 缺少编码协商 Vary

**P1 · 已修复 · 影响 4/5 · 成本 1/5 · 置信度：高**

只判断 Accept-Encoding 包含 gzip，无法尊重客户端禁用；仅压缩时加 Vary 会使共享缓存混用不同编码变体。

整改：解析质量因子，显式 gzip 优先于通配符；GET/HEAD、identity/304 合并 Vary，保留原有 Origin 等值。

验收：q=0、通配符、非法质量值、identity、HEAD/304 场景通过；206 保持不压缩。

证据：
- [internal/middleware/builtin/gzip.go:149](/home/sky/project/go/wp-gpt/internal/middleware/builtin/gzip.go:149)：`func acceptsGzip`
- [internal/middleware/builtin/gzip_negotiation_test.go:12](/home/sky/project/go/wp-gpt/internal/middleware/builtin/gzip_negotiation_test.go:12)：`func Test`

### ARCH-04 · Page 超过 20 个的异步重建与同步路径语义不同

**P1 · 待整改 · 影响 5/5 · 成本 3/5 · 置信度：高**

同步 RebuildStale 遍历启用语言，并重新发布此前已发布的语言；溢出部分入队后，executor 仅 Build(ID)，没有语言、没有重新发布。同一批第 21 个之后可能停在默认语言暂存态。

整改：定义显式任务意图（手工构建/依赖重建），引入一个单页重建编排方法供同步与 worker 共用；语言集合、旧发布范围和输入版本固定进任务上下文。不要用空 hash 永久充当隐藏操作码。

验收：构造 21 页×两语言，其中一页草稿未上线；前 20 与最后一页结果一致，未上线的语言仍不自动上线，失败准确反映到任务。

证据：
- [internal/module/page/service/page_dependency.go:39](/home/sky/project/go/wp-gpt/internal/module/page/service/page_dependency.go:39)：`const maxAutoRebuildPages = 20`
- [internal/module/page/service/page_dependency.go:171](/home/sky/project/go/wp-gpt/internal/module/page/service/page_dependency.go:171)：`EnqueuePageBuild(ctx, id, page.DraftVersion, "")`
- [internal/routers/assembly_publish.go:287](/home/sky/project/go/wp-gpt/internal/routers/assembly_publish.go:287)：`&pagedto.BuildReq{ID: job.SourceID}`

### PERF-01 · 内核锁外编译成立，但上层实例锁覆盖整个多语言发布

**P2 · 待整改 · 影响 3/5 · 成本 4/5 · 置信度：高**

Publisher.compileArtifact 明确锁外编译；presentation.rebuildInstance 仍持来源锁执行 publishAllLangs。这能保护单进程正确性，却意味着热点实例的整个多语言构建串行；也不能提供跨进程互斥。

整改：先完成来源租约与版本校验，再缩短锁：锁内冻结不可变 BuildContext/草稿版本，锁外编译和落盘，锁内比较版本并激活。为构建并发设置按预计内存的预算，不盲目提高固定 worker 数。

验收：改草稿与重建并发时旧构建不能覆盖新稿；记录等待锁、编译、落盘、激活的独立耗时及 peak heap。本次未做 pprof/RSS 压测，不能给出虚构吞吐提升。

证据：
- [internal/pipeline/publisher.go:625](/home/sky/project/go/wp-gpt/internal/pipeline/publisher.go:625)：`func (p *Publisher) compileArtifact`
- [internal/module/presentation/service/presentation_persist.go:71](/home/sky/project/go/wp-gpt/internal/module/presentation/service/presentation_persist.go:71)：`func (s *Service) rebuildInstance`

### PERF-02 · 标签后台存在明确的 N+1 读取

**P2 · 待整改 · 影响 3/5 · 成本 2/5 · 置信度：高**

标签页先 listTags，再对每个标签 GetTag 取命中商品。注释把“标签是个位数”当边界，但协议没有使这个假设成为约束；商品命中数据也会增加页面体积。

整改：标签列表只返回分页元数据和数量；展开时 HTMX 按需取一页商品，或由本模块仓储一次批量查询后在 service 组装。避免在 handler 开 goroutine 并发 N 次查询来掩盖 N+1。

验收：1、100、1000 标签的首屏查询数保持常数级；命中商品分页，空工程不拉取其他工程详情。

证据：
- [internal/module/product/inbound/http/product_tag_page.go:63](/home/sky/project/go/wp-gpt/internal/module/product/inbound/http/product_tag_page.go:63)：`每个标签再取一次详情`

## 5. 数据库设计

已有相当多 PostgreSQL 能力，真正优先项是状态机和工程一致性。

- 迁移已有 JSONB GIN、pg_trgm、部分索引、分区与 RLS。不是一套只按 MySQL 思路写出来的空库。
- TEXT/unbounded varchar 本身不是值得全库整改的证据；短字段长度应由业务约束决定，而不是假设改型自然更快。
- 外部 UUID 与内部 bigint 并存是有意设计；不应为形式统一批量改主键。

本次只在隔离测试库验证迁移和行为，未给生产执行 DDL；具体建议见数据库方案表。

### FIX-07 · 后台分类、品牌、标签操作丢失工程参数

**P1 · 已修复 · 影响 5/5 · 成本 2/5 · 置信度：高**

handler 读取了 projectId，却没有放入部分 Update/Delete DTO；单工程回退掩盖了问题，第二个工程存在后保存直接报参数无效。标签详情同样丢失所选工程。

整改：分类/品牌/标签更新、单删、批删、商品分类品牌标签设置及标签详情显式传递工程。授权链仍使用原有 Casbin/数据域，不把传参当作授权。

验收：两工程真实 service + handler 回归通过；浏览器在两工程夹具中完成品牌 SEO、分类 SEO、标签名称保存并读回。

证据：
- [internal/module/product/inbound/http/product_taxonomy_page.go:202](/home/sky/project/go/wp-gpt/internal/module/product/inbound/http/product_taxonomy_page.go:202)：`func (h *productPageHandle) ProductBrandsUpdate`
- [internal/module/product/inbound/http/product_tag_page.go:67](/home/sky/project/go/wp-gpt/internal/module/product/inbound/http/product_tag_page.go:67)：`GetTagReq{ProjectID: selected`
- [public/test/product/feature/taxonomy_project_scope_test.go:16](/home/sky/project/go/wp-gpt/public/test/product/feature/taxonomy_project_scope_test.go:16)：`func TestTaxonomyFormsCarryProjectScope`

### DB-01 · 队列回收可撞 pending 唯一键，claim 不是来源互斥，完成写入无租约归属

**P1 · 待整改 · 影响 5/5 · 成本 4/5 · 置信度：高**

真实 PG 复现：同输入第一条 running 后可再入队 pending；ReclaimStale 把旧任务改 pending，触发 uq_build_jobs_pending 的 23505，整条回收语句失败。SKIP LOCKED 只独占 job 行，不保证同一来源只跑一个任务；完成更新只按 id，旧 worker 可能覆盖重新认领后的结果。

整改：为 job 增加 lease_token/lease_expires_time/attempt 与显式 project_id、lang、intent。claim 原子产生 token；完成条件包含 running+token。用来源租约行或事务级调度锁串行同来源，把重复陈旧任务合并/标 superseded，不能整批无条件退回 pending。

验收：双 worker、超时回收、同输入二次入队、旧 worker 延迟完成、取消和 retry 均有竞争测试；回收一条坏任务不阻断其他任务。

证据：
- [internal/module/build/model/build_model.go:86](/home/sky/project/go/wp-gpt/internal/module/build/model/build_model.go:86)：`const claimSQL`
- [internal/module/build/model/build_model.go:110](/home/sky/project/go/wp-gpt/internal/module/build/model/build_model.go:110)：`func (m *Model) MarkSucceeded`
- [internal/module/build/model/build_model.go:136](/home/sky/project/go/wp-gpt/internal/module/build/model/build_model.go:136)：`const reclaimStaleSQL`
- [public/migrations/171_build_jobs_queue.sql:15](/home/sky/project/go/wp-gpt/public/migrations/171_build_jobs_queue.sql:15)：`uq_build_jobs_pending`

### DB-02 · 大 JSONB 的必要历史快照与重复字段混在一起

**P2 · 待整改 · 影响 3/5 · 成本 3/5 · 置信度：高**

Page 草稿、修订、source_document、DocumentSnapshot 是不同生命周期的恢复边界，不能只因内容相似删除。另一方面 presentation 的 recordArtifactTx 把同一 manifestJSON 同时写入 BuildInputManifest 和 Manifest，是可确认的同字节冗余。ListByPage 还读取完整产物实体。

整改：保留不可变恢复快照；先定义 input manifest 与 output manifest 的不同语义，如果当前确实同义就合并为一个真源。列表用轻量 projection，详情才取源码。历史 JSONB 不默认铺 GIN；使用保留期和引用闭包 GC 控制量。

验收：回滚/缺文件重建功能保持；列表行不带源码与双份 manifest；以 pg_column_size、TOAST 大小、网络字节和查询计划证明收益。未获得业务库统计，节省比例待测。

证据：
- [internal/module/presentation/service/presentation_persist.go:106](/home/sky/project/go/wp-gpt/internal/module/presentation/service/presentation_persist.go:106)：`BuildInputManifest: manifestJSON`
- [internal/module/artifact/model/artifact_model.go:254](/home/sky/project/go/wp-gpt/internal/module/artifact/model/artifact_model.go:254)：`func (m *Model) ListByPage`

### DB-03 · JSONB 关系与工程边界不能全部依赖服务层自觉

**P2 · 待评估 · 影响 3/5 · 成本 4/5 · 置信度：高：DDL 形状；业务库数据质量未验证**

商品 category_ids/tag_ids 使用 JSONB 和 GIN，品牌等标量引用有 FK；不能声称全库缺 FK。但 JSON 数组成员无法由这些标量 FK 保证存在性，单列对象 FK 本身也不保证 parent/child 的 project_id 相等。

整改：先按领域规定关系基数。经常做反向查找、删除限制和跨工程校验的分类/标签关联优先评估规范化关系表；标量跨工程关系评估 (project_id,id) 复合唯一与复合 FK。Document/manifest 继续 JSONB，不统一改成关系表。

验收：新增约束前盘点孤儿与跨工程历史数据，禁止自动删数据；比较真实写负担与查询收益后实施。迁移方案未执行。

证据：
- [public/migrations/081_product_tables.sql:95](/home/sky/project/go/wp-gpt/public/migrations/081_product_tables.sql:95)：`category_ids`
- [public/migrations/081_product_tables.sql:99](/home/sky/project/go/wp-gpt/public/migrations/081_product_tables.sql:99)：`brand_id`

### DB-04 · RLS 是否生效取决于实际连接角色，文档状态相互矛盾

**P1 · 待现场核验 · 影响 5/5 · 成本 3/5 · 置信度：现场未知；源码/文档冲突已证实**

AGENTS 称只有 locale 接了 scope，但审计记录与源码已有多批 RLS 接线。项目说明记录应用使用超级角色；本次缺 DBX 工具，未验证当前业务连接角色，因此不能把这一历史记录当作现场实测。

整改：经 DBX 明确 session_user/current_user、rolsuper/rolbypassrls、owner、策略与分区状态；以非超级测试角色跑缺 scope/跨工程/任务消费者回归，再切运行角色。迁移角色与业务角色分开，启动校验业务角色能力。

验收：每张工程表：无 scope 不可见、正确工程可读写、错误工程被拒；新分区、新模块和后台任务纳入覆盖。不可仅凭表上 FORCE 标记宣布隔离生效。

证据：
- [public/migrations/215_project_isolation_rls.sql:14](/home/sky/project/go/wp-gpt/public/migrations/215_project_isolation_rls.sql:14)：`ROW LEVEL SECURITY`
- [docs/audit/README.md:51](/home/sky/project/go/wp-gpt/docs/audit/README.md:51)：`RLS 接线主体`

## 6. Go 包组织、插件与安全

模块化单体方向合适；不建议此时拆微服务或再套一层通用仓储。

- contract/dto/enums/inbound/model/outbound/service 分工可以保持，按复杂度裁剪。model 是本模块表访问单元，跨聚合事务由 service 编排。
- 插件脚手架已经生成 manifest.json、Jet、样式与迁移；L0/L2 声明式插件不需要修改核心 Go 注册文件。内置 Go 能力新增仍需编译，这是信任边界。
- 许可、SECURITY.md、英文贡献入口与示例已经存在；主要问题是规则状态漂移和插件 L1 安全/升级协议。

采用 JSON 单一声明格式，避免再加 YAML 制造双协议；原生 Go 扩展留给受信核心集成。第三方 SQL 先建立真实数据库隔离。

### FIX-01 · Webhook 校验与实际拨号存在 DNS 重绑定窗口

**P1 · 已修复 · 影响 5/5 · 成本 2/5 · 置信度：高**

原实现校验域名解析结果后，默认 Transport 再解析一次；校验时公网、连接时内网可以绕过前置检查。

整改：将全部地址检查放入 DialContext，并直接拨已检查的 IP；保留 URL hostname 的 Host/TLS 语义。解析与连接受 5 秒预算约束，任一受限地址使整组拒绝。

验收：公网→环回的两阶段 DNS 回归在建立连接前拒绝；原签名和重定向协议测试仍经过本地服务器。此修复不替代出站网络策略。

证据：
- [internal/module/webhook/service/ssrf.go:52](/home/sky/project/go/wp-gpt/internal/module/webhook/service/ssrf.go:52)：`func dialWebhookContext`
- [internal/module/webhook/service/deliver.go:40](/home/sky/project/go/wp-gpt/internal/module/webhook/service/deliver.go:40)：`DialContext:`
- [internal/module/webhook/service/dial_ssrf_test.go:12](/home/sky/project/go/wp-gpt/internal/module/webhook/service/dial_ssrf_test.go:12)：`func TestWebhookTransportRejectsDNSRebinding`

### FIX-11 · 测试依赖本机 config.yaml，部分后台测试实际上跳过

**P1 · 已修复 · 影响 4/5 · 成本 2/5 · 置信度：高**

隔离工作树全量基线暴露 18 个配置缺失失败；8 个后台用例因同一缺配置条件跳过。照搬开发配置又会把测试指向开发业务库。

整改：无组件测试读取版本库 example 的临时 YAML；真实组件测试显式生成独立迁移库/seed 配置与测试 Redis DB 15。依赖就绪后初始化错误必须 Fatal。

验收：普通与 race 全量各 169 测试包通过；此前 8 个后台用例实际执行。剩余 7 个条件用例逐项列明，不能把 skip 计为通过。

证据：
- [public/test/support/test_bootstrap.go:34](/home/sky/project/go/wp-gpt/public/test/support/test_bootstrap.go:34)：`初始化外部组件的测试必须显式提供隔离配置`
- [public/test/support/component_config.go:15](/home/sky/project/go/wp-gpt/public/test/support/component_config.go:15)：`func NewComponentTestConfig`
- [public/test/admin/feature/admin_shell_i18n_test.go:30](/home/sky/project/go/wp-gpt/public/test/admin/feature/admin_shell_i18n_test.go:30)：`NewComponentTestConfig`

### SEC-01 · 插件 SQL 正则过滤不能提供 schema 权限隔离

**P0（开放不可信插件前） · 待整改 · 影响 5/5 · 成本 4/5 · 置信度：高**

无数据库执行的校验探针确认：SET search_path TO public 与 SELECT 1 FROM "public".sys_admin LIMIT 0 都被接受。迁移器使用应用事务执行任意 SQL；search_path 和 public. 文本黑名单无法限制权限。影响取决于 plugin:install 授予谁和实际连接权限。

整改：普通生态插件默认仅提供声明式组件/资产。需要 L1 的插件使用独立、非超级、无 BYPASSRLS、非主表 owner 的登录连接，仅授权自己的 schema。不能在超级 session 上简单 SET ROLE 后声称沙箱成立。插件安装能力与一般后台配置权限拆开，保留来源和版本校验。

验收：恶意限定名、修改 search_path、动态 SQL/函数、其他插件 schema、角色/文件访问在数据库权限层被拒；测试必须使用部署同形角色。当前两条探针作为已知未解决问题保留在 evidence.json。

证据：
- [internal/module/plugin/service/plugin_migrate.go:50](/home/sky/project/go/wp-gpt/internal/module/plugin/service/plugin_migrate.go:50)：`func (s *Service) migrateSchema`
- [internal/module/plugin/service/plugin_migrate.go:133](/home/sky/project/go/wp-gpt/internal/module/plugin/service/plugin_migrate.go:133)：`func validatePluginStatement`

### OSS-01 · L1 插件升级按 DROP SCHEMA 重装，不适合作为开源生产升级协议

**P1（生产插件前） · 待整改 · 影响 4/5 · 成本 4/5 · 置信度：高**

migrateSchema 对 schemaVersion 变化先 DROP SCHEMA CASCADE 再执行全部 SQL。开发阶段按仓库约定可以重构，但该行为会删除插件业务数据，不能不加区分地推广给第三方使用者。

整改：正式生态增加 migration ledger（plugin_id/version/checksum/applied_time），升级只跑新迁移且检查已应用校验和；卸载保留数据与清除数据分成明确操作。默认脚手架使用不需要 SQL 的展示插件，数据插件另有专门文档。

验收：升级保留业务行；失败事务回滚；已应用迁移被改写时报错；卸载恢复策略有演练。当前分支未执行插件升级或删除。

证据：
- [internal/module/plugin/service/plugin_migrate.go:45](/home/sky/project/go/wp-gpt/internal/module/plugin/service/plugin_migrate.go:45)：`DROP SCHEMA CASCADE`
- [internal/module/plugin/scaffold/scaffold.go:32](/home/sky/project/go/wp-gpt/internal/module/plugin/scaffold/scaffold.go:32)：`"migrations/001_init.sql"`

### OSS-02 · 目录分层总体合理，规则和术语的实际状态比目录更需要收敛

**P2 · 待整改 · 影响 3/5 · 成本 2/5 · 置信度：高**

用户背景称核心尚未实现、Jet 仅构建期、插件须硬编码，与现有代码不符；AGENTS 还称 build 无独立目录，但 internal/module/build 已存在。contract 限定 import 方向，不自动保证 DTO 不可变、接口足够窄或事务所有权明确。

整改：保留模块化单体；用一页当前架构和扩展教程替代互相重复的状态叙事。contract 按消费方能力拆小，在 composition root 接线。builder/core 保留编译协议，source 放零依赖形状；不因追求七层而给简单模块造空包。

验收：新贡献者按一条路径完成“字段→受限数据源→组件→绑定→多语言构建→失效→回归”；架构测试检查依赖方向，ADR 明确例外及退出条件。

证据：
- [README.md:11](/home/sky/project/go/wp-gpt/README.md:11)：`下面这些是`
- [internal/module/build/model/build_model.go:1](/home/sky/project/go/wp-gpt/internal/module/build/model/build_model.go:1)：`Package buildmodel`
- [internal/builder/core/component.go:39](/home/sky/project/go/wp-gpt/internal/builder/core/component.go:39)：`type Component interface`

## 7. UI 基座、交互与布局

一套原始控件、两个消费端成立；一套全量后台样式打进访客页不成立。

- 共享 token/控件/减少动效基础已经存在，builder 有按需资源注入。
- HTMX 负责服务交互；焦点、抽屉、选择器、富文本等需要控件层 JS，不能把“零自定义 JS”当作忽略可访问性的理由。
- 实际检查仪表盘、工作台响应式与 SEO 设置，以及隔离品牌/分类/标签创建编辑和键盘操作。

已修共用抽屉、移动底栏及严格 CSS 门禁；完整控件矩阵与 RTL/读屏覆盖仍是后续质量工作。

### FIX-02 · Jet 执行中失败会产生 200 和半截页面

**P1 · 已修复 · 影响 4/5 · 成本 1/5 · 置信度：高**

直接向 ResponseWriter 渲染，模板中途报错时响应已提交。用户看到缺失的 HTML，监控却把请求算作成功。

整改：先渲染到请求内 buffer；成功后写响应；失败记录模板名与内部错误，向用户返回受控中文 500。

验收：执行中报错、模板加载报错、成功渲染分别验证状态和响应内容。新增 buffer 增加单次后台响应大小级别的内存，占用应通过列表分页限制。

证据：
- [internal/templates/jet_render.go:75](/home/sky/project/go/wp-gpt/internal/templates/jet_render.go:75)：`func (i *jetInstance) Render`
- [internal/templates/jet_render_atomic_test.go:13](/home/sky/project/go/wp-gpt/internal/templates/jet_render_atomic_test.go:13)：`func Test`

### FIX-08 · 抽屉打开后仍向辅助技术声明隐藏

**P1 · 已修复 · 影响 4/5 · 成本 1/5 · 置信度：高**

浏览器复现：抽屉可见但 aria-hidden=true，基于可访问名称的按钮定位失败；背景可交互且关闭后焦点无统一归属。

整改：共用 Drawer 维护 aria-hidden、dialog/labelledby、背景 inert、可见焦点列表、Tab 循环及关闭后返回焦点。

验收：修复后可按按钮名称提交；Esc 后焦点回原品牌按钮，背景 inert 移除；375px 抽屉无页面横向溢出。未声称完成全站 WCAG 审计。

证据：
- [internal/templates/static/js/ui/drawer.js:27](/home/sky/project/go/wp-gpt/internal/templates/static/js/ui/drawer.js:27)：`function focusableElements`
- [internal/templates/static/js/ui/drawer.js:51](/home/sky/project/go/wp-gpt/internal/templates/static/js/ui/drawer.js:51)：`aria-hidden`

### FIX-09 · 375px 工作台底栏溢出到 426px

**P2 · 已修复 · 影响 3/5 · 成本 1/5 · 置信度：高**

实际编辑器 375px 视口 scrollWidth=426，底栏操作不换行，把聚焦模式按钮推到屏幕外。

整改：底栏 flex-wrap:wrap，保持功能按钮可见。

验收：原页面 DOM 回放加载 gpt CSS 后，375px 视口 scrollWidth=375。回放禁用脚本，不等于整套 gpt 工作台后端端到端测试。

证据：
- [internal/templates/static/css/workbench.css:111](/home/sky/project/go/wp-gpt/internal/templates/static/css/workbench.css:111)：`.wb-bottombar`

### FIX-10 · 本地 CSS 默认告警掩盖了 CI 严格模式失败

**P2 · 已修复 · 影响 3/5 · 成本 1/5 · 置信度：高**

默认脚本 warn 返回成功，CI 实际配置 SKY_CSS_GUARD=error。基线有 7 处违规：裸 hover 和过滤控件固定最小宽度。

整改：后台主题与富文本工具栏的 hover 放入能力媒体查询，保留 focus/点击路径；移除冗余 hover 状态；最小宽度不超过容器。

验收：严格模式扫描 44 个组件、147 个样式文件，未豁免违规为 0；8 个原有明确豁免未扩大。

证据：
- [.github/workflows/go-test.yml:38](/home/sky/project/go/wp-gpt/.github/workflows/go-test.yml:38)：`SKY_CSS_GUARD=error`
- [internal/templates/static/css/theme.css:899](/home/sky/project/go/wp-gpt/internal/templates/static/css/theme.css:899)：`.help:focus-within`
- [internal/templates/static/css/rich-editor.css:109](/home/sky/project/go/wp-gpt/internal/templates/static/css/rich-editor.css:109)：`@media (hover: hover)`

### UI-01 · 共享控件基座成立，但后台壳、站点主题与组件交互应保持独立

**P2 · 持续改进 · 影响 3/5 · 成本 2/5 · 置信度：高**

已有 UI token、按需资产、reduced-motion 和共用控件；本次抽屉问题说明共用层的错误会同时扩散。工作台工具栏仍有仅符号名称的按钮，RTL、读屏与触屏覆盖并不完整。

整改：固定四层：设计 token → 无业务原始控件/行为 → 后台和站点各自组合 → 模板/主题视觉参数。基础动画属于 token+控件，区块只编排，主题只覆写参数；HTMX 管服务交互，控件 JS 管焦点、选择、键盘状态。

验收：同一控件在后台与发布页跑相同键盘/触屏/减少动效用例；无 hx-* 页面不注入 htmx；不把整个后台 CSS/JS 打进所有静态页。

证据：
- [internal/templates/static/css/ui.css:248](/home/sky/project/go/wp-gpt/internal/templates/static/css/ui.css:248)：`prefers-reduced-motion`
- [internal/builder/ui_script.go:1](/home/sky/project/go/wp-gpt/internal/builder/ui_script.go:1)：`package builder`
- [internal/templates/static/js/ui/drawer.js:72](/home/sky/project/go/wp-gpt/internal/templates/static/js/ui/drawer.js:72)：`function closeDrawer`

## 数据库落地方案（本次不执行 DDL）

| 对象 | 方案 | 验收与边界 |
|---|---|---|
| `build_jobs`：任务状态与租约 | 增加 project_id/lang/intent/lease_token/lease_expires_time/attempt；完成写入守卫 token；回收合并重复 pending。 | 最高优先；真实 23505 已复现。不要只把唯一索引删除。 |
| `page_dependencies / presentation_dependencies`：依赖查找 | 保留 (dependency_kind,dependency_key) 索引；当前语言账本已纳入反查。对高基数数据评估覆盖 artifact/source id 的索引，避免先建一堆重叠索引。 | 使用 DBX EXPLAIN(ANALYZE,BUFFERS) 与索引体积证明；本次无业务库执行计划。 |
| `page_publications / page_stagings / presentation_publications`：语言真源 | 以 (source_id,lang) 账本为真实当前状态；主行 active/staged 明确降为摘要镜像，业务查询不得只看镜像。 | 唯一键与引用闭包必须保留；本次修改消费查询，未改表结构。 |
| `presentation_artifacts`：重复 manifest | 确认两字段语义后合并同字节 Manifest/BuildInputManifest，列表用 projection；SourceHash/InputHash 保留明确职责。 | 单独迁移，先统计 JSONB/TOAST 占用；不可连恢复快照一起删。 |
| `page_artifacts / page_revisions / document_snapshots`：必要不可变快照 | 保留重建与审计来源；对历史数据执行有引用闭包保障的保留期清理，区分元数据、源码快照与物理 payload 生命周期。 | GC dry-run、回滚、缺文件恢复和活跃链接巡检共同验收。 |
| `products / product_categories / product_tags`：关系与工程一致性 | 分类标签关系是否拆 junction 表按反查与删除约束需求决定；标量关系评估项目复合 FK。 | 不盲目把所有 JSONB 规范化；迁移前清点孤儿和跨工程关系。 |
| `products / orders 列表`：查询与索引 | 沿现有 pg_trgm/部分索引做访问路径审查；按 project+状态+排序的实际查询评估复合索引与游标分页。 | 生产采样 pg_stat_statements/pg_stat_user_indexes；idx_scan=0 在短观察窗口内不足以删索引。 |
| `带 project_id 的表与分区`：RLS/角色 | 先以非超级角色验证 scope 完整性，再切业务连接角色；独立迁移角色；分区维护继承策略由显式代码保证。 | 现场角色未知；不会在本轮改配置造成静默空查询。 |
| `插件 schema`：权限与升级 | 单独登录连接和 schema owner；拒绝公共业务 schema 授权；迁移台账保留 checksum 和应用时间，停止生产升级 DROP 重装。 | 正则只是输入诊断，数据库权限才是防线。 |
| `时间与主键`：保持既有合理约定 | 新表 timestamptz+time.Time，对外 DTO JSONTime 到秒；外部 UUID 与内部 identity 按边界选择。 | 不开展无收益的大规模 TEXT→varchar(n)、UUID→bigint、生命周期时间列统一改名。 |
| `搜索与日志表`：选择性使用 PG 能力 | trgm 已覆盖部分模糊搜索；长文检索再按语言词典评估全文检索投影。追加流水是否 BRIN 按时间相关性和扫描范围测量。 | 未从现有 SQL 确认完整全文检索/BRIN 体系，不宣称已经使用，也不为功能清单硬加。 |

索引实施顺序：先经 DBX 拿目标 SQL 和参数分布，再看现有索引、行数/选择率与 EXPLAIN；在隔离数据集验证后安排迁移。索引名字、列数或 TEXT 类型都不能单独证明性能问题。所有耗时、节省空间比例和生产容量在拿到真实数据前保持“待测”。

## 整改顺序

影响和成本采用 1~5 级；风险门槛优先于分数。同级按 `影响 × (6−成本)` 排序。下表只列尚未闭环事项。

| 顺序 | 问题 | 优先级 | 分数 |
|---|---|---|
| 1 | SEC-01：插件 SQL 正则过滤不能提供 schema 权限隔离 | P0（开放不可信插件前） | 10 |
| 2 | ARCH-02：全局块删除保护只覆盖主题和 Page | P1 | 15 |
| 3 | ARCH-04：Page 超过 20 个的异步重建与同步路径语义不同 | P1 | 15 |
| 4 | DB-04：RLS 是否生效取决于实际连接角色，文档状态相互矛盾 | P1 | 15 |
| 5 | ARCH-03：组件升级 stale 判定包含历史产物，且自动实例缺少同等启动入口 | P1 | 12 |
| 6 | ARCH-05：缺失结构模板/块的降级行为可能把配置错误发布成不完整页面 | P1 | 12 |
| 7 | ARCH-01：商品主写路径只失效运行时片段缓存，静态产物没有完整失效闭环 | P1 | 10 |
| 8 | DB-01：队列回收可撞 pending 唯一键，claim 不是来源互斥，完成写入无租约归属 | P1 | 10 |
| 9 | I18N-01：手工 Page 的 hreflang 构建输入仍受当前线上状态影响 | P1 | 8 |
| 10 | OSS-01：L1 插件升级按 DROP SCHEMA 重装，不适合作为开源生产升级协议 | P1（生产插件前） | 8 |
| 11 | SEO-01：SEO 评分、技术可抓取性与实际搜索表现需要分开 | P2 | 12 |
| 12 | PERF-02：标签后台存在明确的 N+1 读取 | P2 | 12 |
| 13 | OSS-02：目录分层总体合理，规则和术语的实际状态比目录更需要收敛 | P2 | 12 |
| 14 | UI-01：共享控件基座成立，但后台壳、站点主题与组件交互应保持独立 | P2 | 12 |
| 15 | I18N-02：语言失败回退与 RTL 缺少完整发布验收 | P2 | 9 |
| 16 | DB-02：大 JSONB 的必要历史快照与重复字段混在一起 | P2 | 9 |
| 17 | PERF-01：内核锁外编译成立，但上层实例锁覆盖整个多语言发布 | P2 | 6 |
| 18 | DB-03：JSONB 关系与工程边界不能全部依赖服务层自觉 | P2 | 6 |

建议按三个里程碑交付：

1. **发布正确性与安全**：商品 outbox、同来源任务租约、超量重建语义、多语言发布计划、块删除保护；在开放不可信插件前完成独立数据库权限隔离。
2. **成本与可观测性**：N+1、轻量列表 projection、当前产物 registry 判定、构建阶段指标、任务年龄与发布回执积压。
3. **生态与体验**：无损插件迁移、扩展教程、RTL、控件跨消费端验收和辅助技术检查。

## 验证结果与保留问题

完整普通与竞态测试各为 **169 个测试包通过、4925 个测试节点通过、0 失败、7 个条件用例跳过**。测试节点包含子用例；另有 132 个包无测试文件。全量基线则有 19 个失败节点和 15 个条件跳过。新增代码回归与 UI 浏览器回归的详细记录见 [validation.json](validation.json)。

`go vet`、生产包构建、工作台/Node 契约检查、内部错误泄露、service 数据访问边界、i18n、严格多端 CSS 门禁均通过。CSS 原有 8 个明确豁免未增加。

剩余 7 个条件跳过分别是：两个按需浏览器常驻夹具、两个需要完整业务配置的路由审计、一个外部邮件发送、两个本机 config.yaml 语言配置检查。后台 UI 夹具另行显式启动并完成浏览器验收。权限点现场对账脚本没有执行，不能列为通过。

两个已知未修复问题的临时探针预期失败：插件 SQL 过滤接受越界语句；队列回收触发 23505。探针退出后移除，结果见 [evidence.json](evidence.json)。主套件为绿不等于这些问题不存在。

## UI 验证矩阵

| 环境/页面 | 已验证 | 结果/限制 |
|---|---|---|
| 现有服务仪表盘 | 375px 页面宽度 | 无整页横向溢出；未修改业务内容 |
| 现有服务工作台 | 1440/768/375、断点按钮、页面 SEO 设置 | 375px 底栏 426px 溢出已复现；未点击发布/存草稿 |
| 工作台静态 DOM 回放 + gpt CSS | 375px 底栏 | 修复后 scrollWidth=375；脚本禁用，仅布局证据 |
| 隔离品牌页 | 新建、SEO 修改、抽屉、Esc、手机布局 | 创建/读回成功；多工程修改由失败修复为成功；返回原打开按钮 |
| 隔离分类页 | 层级展示、父级选择器可见、SEO 修改 | 两工程保存成功；没有穷举所有父子循环和权限组合 |
| 隔离标签页 | 手工标签编辑、命中区、键盘、桌面布局 | 保存名称可读回；1440px 无溢出，焦点保留在抽屉 |

![移动抽屉](/home/sky/project/go/wp-gpt/docs/audit/gpt-2026-09-19/screenshots/brand-drawer-mobile.png)

![工作台修复前](/home/sky/project/go/wp-gpt/docs/audit/gpt-2026-09-19/screenshots/workbench-mobile-before.png)

![工作台 CSS 修复后回放](/home/sky/project/go/wp-gpt/docs/audit/gpt-2026-09-19/screenshots/workbench-mobile-after-layout.png)

## 开源友好度

适合以开发预览版开放源码；不宜承诺不可信数据插件隔离或生产无损插件升级。

- 强项：已有许可证、安全说明、英文入口、插件示例、真实迁移测试和架构门禁。
- 强项：后台与静态发布统一 Go 技术栈，受限 DSL 与声明式组件降低学习面。
- 短板：术语与状态散布在多份长文，历史完成标记不能替代当前控制流证据。
- 短板：跨模块大 contract 与隐式装配回调需要一张扩展路径图。
- 短板：普通测试可因配置缺失跳过重要断言；CI 必须统计 skip 原因。
- 短板：原生 Go 插件、声明式展示插件、可执行 SQL 插件的信任模型需要直白区分。

不建议用主观“8.5 分”替代发行条件。验收标准应是：干净环境能启动、真实迁移测试不静默跳过、插件能力边界明确、一个最小例子能覆盖数据变更到两语言静态页更新、失败可以定位与恢复。

## 变更与回滚

只在 gpt 工作树修改并本地构建；未合并、未推送、未部署，未对业务库执行 DDL。回滚按本分支代码提交整体 revert；数据库结构未变。

本次修复不会替你修改现有已发布 Artifact。合并并部署后，应先抽样构建、预览与发布，再按当前语言账本收敛待重建来源；组件升级 stale 的现有缺口仍按 ARCH-03 处理，不能以此报告替代发布验收。

机器可读主报告：[report.json](report.json)。验证：[validation.json](validation.json)。逐文件覆盖：[coverage.json](coverage.json)。未解决问题复现：[evidence.json](evidence.json)。

代码提交：`751b65d7cd344b03789b902586e1137ec15d3adb`。最终复核：主工作区仍在 `main`，已跟踪未提交补丁与任务开始时逐字节一致。
