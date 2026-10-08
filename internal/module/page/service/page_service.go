package pageservice

// 一批 page 入口的签名里没有工程参数：契约由 workbench 等消费方编译期依赖（改签名会连带动
// 一大片），pipeline.DependencyTarget 也只带 (kind,key)。它们的语义本来就是跨工程的
// （整站标记待重建、按主题/块标记、全站草稿扫描），而 pages 在迁移 215 里带 FORCE 策略，
// 作用域只能落到某一个具体工程。
//
// 处理办法：枚举工程表后**逐工程独立作用域**执行（每个工程各自一次 set_config + 事务），
// 再合并结果。这不是「退化为不限工程」—— 每个事务的 app.project_id 都取确定值，
// 换非超级角色后每条语句都真的受策略约束。
//
// 为什么不合并成一次查询：RLS 的作用域是**单值**会话变量，把多个工程的 id 并进一次
// 查询只能靠放宽谓词，那等于取消隔离。presentation 的 MarkStaleByDependency（第二批）
// 是同一形状的样板。
//
// 工程表为空或读不到时**显式失败**：静默返回空结果会把「读不到工程表」伪装成
// 「没有受影响的页面」—— 那正是这一步要消灭的 fail-silent。

// 两件事：历史快照收敛（保存时顺手做一次 + 定时兜底）与产物 GC 定时化。
// 保留期都写成常量并从声明处引用：此前「产物 GC 默认 dryRun 且没有定时任务、修订快照
// 完全不清理」的根因不是写不出清理，而是**没有任何地方承诺保留期**。

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/internal/module/artifact/contract"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/blueprint/contract"
	"go_wp/internal/module/media/contract"
	"go_wp/internal/module/navigation/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/plugin/contract"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/internal/retention"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

var _ pagecontract.PageService = (*Service)(nil)

// Service Page 草稿、修订与发布业务服务。
//
// 发布链路持有 pipeline 内核（内存态可由数据库重建）与相邻模块契约，
// 不直接依赖 GORM 之外的模块实现。
type Service struct {
	model     *pagemodel.Model
	project   projectcontract.ProjectService
	artifacts artifactcontract.ArtifactService
	// pageArtifacts 页面产物元数据的只读视图（artifact 契约的另一个窄接口）。
	// 与 artifacts 是**同一个对象**的两种能力：ArtifactService 管记录/GC/状态，
	// PageArtifactReader 管「按 id 反查 hash / 产物挂在哪张页面」这类 page 模块要的读。
	pageArtifacts artifactcontract.PageArtifactReader
	// buildQueue 构建队列端口（审计 DB-007）：超出单次上限的自动重建交给它。
	// 未注入 = 没有队列，超出部分保持 stale 并记告警（既有行为）。
	buildQueue pagecontract.BuildQueueEnqueuer
	routes     pubcontract.PublicationService
	blocks     blockcontract.BlockService
	// blueprints 蓝图契约（审计 VIS-010）：新建页面时把蓝图 AST 复制成初始文档。
	// 用完即弃 —— 页面创建之后与蓝图再无关系（改蓝图不传播、不参与构建期）。
	blueprints blueprintcontract.BlueprintService
	plugins    plugincontract.PluginService
	content    core.CollectionResolver
	// productDS 商品构建期数据源（issue #35）：页面里的商品列表组件直连它（受限接口），
	// 未注入时组件回退按名路由。可选依赖不进构造参数，与其它端口同模式。
	productDS productcontract.ProductDataSource
	// navigation 公开站点导航契约：core.nav 绑定菜单位置时构建期解析菜单项。
	navigation navigationcontract.NavigationService
	// media 媒体契约：构建期探测图片变体，输出响应式 srcset（访客零查询）。
	media mediacontract.MediaService
	// structureTemplates 结构模板解析端口（页眉 / 页脚绑定结构模板时构建期取文档）。
	//
	// 消费者侧最窄端口（pipeline.StructureTemplatePort）：本模块不需要 contenttemplate
	// 的 DTO / 版本表 / 类型校验，只要「一份文档」。未注入时模板槽位一律回退块绑定 ——
	// 存量站点行为与改造前逐字节一致（见 page_document.go 的装配注释）。
	structureTemplates pipeline.StructureTemplatePort
	// contentStore 内容译文读取端口（多语言 P5b）：为 nil 时用 pkg/i18n 默认存储
	// （sys_translation 表 + 默认数据库）。测试经 SetContentTranslationStore 注入
	// 隔离 schema 的存储，用于验证「块内文本进候选集合 + 每页每语言一次查库」。
	contentStore i18n.ContentStore

	publisher *pipeline.Publisher
	store     *pipeline.LocalStore
	// publication 访问面激活存储（active 目录符号链接）：页面删除时必须按路径
	// 解除激活，否则「DB 路由已清、符号链接还在」会让已删内容继续可访问。
	publication *pipeline.LocalPublicationStore
	// externalArtifactOwners 其它模块的产物 hash 清单（IDX-015 反向对账用）。
	// 自动发布实例的产物与本模块共用一个 artifacts 根：不注入就只能把它们误报成孤儿。
	// 用 setter 注入而不是构造参数 —— page 不能反向依赖 presentation（那是依赖成环）。
	externalArtifactOwners func(ctx context.Context) ([]string, error)
	// publishWindowFault 是发布链「访问面已切换、数据库尚未落定」窗口的故障注入点
	// （审计 AR2-002 的故障注入测试）。生产恒为 nil。
	//
	// 为什么需要它：这个窗口恰好是崩溃恢复协议唯一无法用静态断言覆盖的分支 ——
	// 真实崩溃会连进程一起终止，子进程方案又无法保证终止点落在两次调用之间。
	// 注入后主链按「状态不可判定」收敛：保留 pending 回执、把错误上抛，由启动恢复
	// 按符号链接的实际指向补齐或回滚。
	// i18nPeer 文案词条 / 内容译文变更时的其它发布来源端口（可空）：见 page_lang.go
	// 的 I18nStalePeer —— 商品详情页（自动发布实例）等来源同样在构建期取词注入字节，
	// 由 page 的 MarkStaleForI18n 统一扇出，避免每个 i18n 保存路径各写一次、漏一处就静默失效。
	i18nPeer I18nStalePeer
	//
	// 三个入口共用这一个字段（发布 / 改 URL / 回滚），命中点都在各自的 DB 事务**内部**
	// 且位于第一条写之后 —— 要证明的是「半截写随事务一起回滚」，而不是「还没开始写」。
	// 测试见 public/test/page/unit/page_publish_ledger_test.go 与
	// page_url_rollback_ledger_test.go（改 URL / 回滚的收敛与幂等）。
	publishWindowFault func() error

	// convergeWake 发布回执收敛的进程内快通道（容量 1）。
	//
	// 写路径在**事务提交之后**非阻塞地推一下，正常路径毫秒级收敛；
	// 通道满时丢弃信号，由定时器兜底（详见 page_publish_converge.go）。
	convergeWake chan struct{}
	// lastConvergeAt 最近一次收敛运行时刻（Unix 秒；0 = 本进程还没跑过）。
	//
	// 进程内观测值，供 PendingReceiptStatus 与健康检查判断「本实例的收敛还在跑」；
	// 多实例部署下每个实例各记各的，不做全局真源（这是进程健康信号，不是业务状态）。
	lastConvergeAt atomic.Int64

	// checkoutCountries 结算表单国家下拉的来源（装配期注入，构建期按本页语言取一份）。
	//
	// 是**函数**而不是一份切片：国家选项与语言有关（sys_area 的中文名 / 英文名两列），
	// 而编译是逐语言的 —— 注入一份冻结的切片会让英文站点拿到中文国名。
	// 未注入时结算表单若含国家字段会在构建期明确报错（见 core.checkoutForm）。
	checkoutCountries func(ctx context.Context, lang string) []core.CheckoutCountry
}

// NewService 创建 Page 服务；同时初始化本地产物根（GO_WP_ARTIFACT_ROOT 可覆盖，
// 默认与访问面一致，经 pipeline.DefaultArtifactRoot/ActiveRoot 单源取值）。
// 构建注入装配感知编译器：页眉/页脚块内联（方案 C）+ 插件组件（docs/06）
// + 集合内容解析（docs/06 §9）。
func NewService(model *pagemodel.Model, artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService, project projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	content core.CollectionResolver, navigation navigationcontract.NavigationService,
	media mediacontract.MediaService) *Service {
	store := &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()}
	publication := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}
	s := &Service{
		model:       model,
		artifacts:   artifacts,
		routes:      routes,
		project:     project,
		blocks:      blocks,
		plugins:     plugins,
		content:     content,
		navigation:  navigation,
		media:       media,
		store:       store,
		publication: publication,
		// 容量 1：同一时刻只留一个待处理信号，多次写入合并成一次收敛。
		convergeWake: make(chan struct{}, 1),
	}
	// 页面产物只读视图从同一个 artifacts 实例派生：它们本来就是同一个对象的两种能力
	// （ArtifactService ∋ 记录/GC/状态，PageArtifactReader ∋ 按 id 反查 hash / 产物挂在哪张
	// 页面）。派生而不是再要一个构造参数，是为了不让二十多个建服务的地方各漏接一次 ——
	// 漏接的表现是「构建期依赖归属校验失败」，而它与调用方要验的东西毫无关系。
	if r, ok := artifacts.(artifactcontract.PageArtifactReader); ok {
		s.pageArtifacts = r
	}
	// 依赖提供者：把文案词条资源版本号写进 Manifest.dependencies
	// （DependencyKind=i18n，改文案触发重建，docs/06-D §10.4）。
	s.publisher = pipeline.NewPublisher(store, publication, pipeline.WithDependencies(s.buildDependencies))
	s.publisher.SetCompile(s.assembleCompile)
	return s
}

// SetStructureTemplatePort 注入结构模板解析端口（装配期调用，与其它可选依赖同模式）。
//
// 未注入（或某套模板解析失败）时结构槽位回退块绑定：漏接的表现是「主题里配了
// 页眉结构模板，站点上却还是旧块」—— 不会报错，故装配层按必需端口断言（见 assembly_publish.go）。
// SetPageArtifacts 注入页面产物元数据只读视图（装配期调用一次，与 artifacts 同源）。
//
// 未注入时依赖归属校验与孤儿对账会以「契约缺失」显式失败，而不是退回直读 page_artifacts
// —— 那张表属 artifact 模块，读它的列名等于把列名变成跨模块接口。
func (s *Service) SetPageArtifacts(reader artifactcontract.PageArtifactReader) {
	s.pageArtifacts = reader
}

func (s *Service) SetStructureTemplatePort(port pipeline.StructureTemplatePort) {
	s.structureTemplates = port
}

// SetContentTranslationStore 注入内容译文读取端口（测试用；生产走 pkg/i18n 默认存储）。
func (s *Service) SetContentTranslationStore(store i18n.ContentStore) {
	s.contentStore = store
}

// newContentTranslator 构造本次编译的内容译文取词器（一次批量查询 + 内存索引）。
//
// 注入端口优先（测试），否则用 pkg/i18n 默认存储（sys_translation）。
// 取词语义与缓存行为完全由 pkg/i18n 决定，本层不做二次缓存（docs/06-D §7.7）。
//
// projectID 为本次编译所属工程（审计 I18N-009）：译文按工程隔离，未覆盖时回落全局行。
func (s *Service) newContentTranslator(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator {
	if s != nil && s.contentStore != nil {
		return i18n.NewContentTranslatorScoped(ctx, projectID, s.contentStore, lang, hashes)
	}
	return i18n.NewContentTranslatorScoped(ctx, projectID, nil, lang, hashes)
}

// getExistingPage 按 id 定位未删除页面（逐工程独立作用域探测，DB-009 第四批），
// 统一映射未找到错误。
//
// 为什么不能直查：pages 带 FORCE 策略，GetByID(ctx, id, 空工程) 在换非超级角色后一律
// 返回 ErrRecordNotFound —— 于是构建 / 发布 / 回滚 / 改 URL 全部报「页面不存在」。
// 调用方（后台 API）手上只有 pageId，归属只能由本层逐工程探测确定；拿到页面之后，
// page.ProjectID 就是后续每一步写入的作用域来源。
func (s *Service) getExistingPage(ctx context.Context, id string) (page *pagemodel.PageEntity, err error) {
	page, err = s.locatePageInProjects(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}

// SetProductDataSource 注入商品构建期数据源（issue #35，装配期调用）。
func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }

// SetCheckoutCountries 注入结算表单国家下拉的来源（core.checkoutForm 的构建期输入）。
//
// 传函数而不是切片：国家清单与语言有关，而编译是逐语言的（见字段注释）。
// 未注入的后果可见：页面里有结算表单 + 国家字段时构建失败（不是静默出一个空国家下拉），
// 因此装配层按必需端口断言。
func (s *Service) SetCheckoutCountries(fn func(ctx context.Context, lang string) []core.CheckoutCountry) {
	s.checkoutCountries = fn
}

// SetBlueprints 注入蓝图契约（装配期调用，审计 VIS-010）。
//
// 未注入时「从蓝图建页」会明确报错而不是静默建空页：空页在后台看起来像新建成功，
// 要等编辑者打开画布才发现什么都没有。
func (s *Service) SetBlueprints(bp blueprintcontract.BlueprintService) { s.blueprints = bp }

// SetPublishWindowFault 注入「访问面已切换、数据库尚未写入」窗口的故障（见字段注释；测试用）。
func (s *Service) SetPublishWindowFault(fn func() error) {
	if s == nil {
		return
	}
	s.publishWindowFault = fn
}

// fanoutProjectIDs 返回逐工程扇出要用的工程清单。
//
// 数量级很小（站点工程），逐个设一次作用域比在数据层引入 BYPASSRLS 连接便宜得多。
func (s *Service) fanoutProjectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	// 契约未注入就是装配漏接，直接失败。
	//
	// 这里原来回退到 `model.ListAllProjectIDs`（直接读 projects 表）：它能工作，但
	// **工程清单的所有权在 project 模块**，page 的 model 层只该碰本模块的表。更糟的是
	// 回退让漏接表现为「一切正常」，而 warn 日志没人看 —— 于是同一份「列出全部工程」
	// 的 SQL 在 page / order / block / navigation 里各存一份，四份将来会各自漂移
	// （比如某个模块开始按 create_time 排序、另一个按 id）。漏接时整站标记 / 全站扫描 /
	// 依赖扇出会整体失效，那本来就该是一次响亮的失败。
	if s.project == nil {
		return nil, ErrProjectRequired
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		// 一个工程都没有：不是「没有受影响页面」，而是没有可作用域的工程。
		return nil, ErrProjectRequired
	}
	return ids, nil
}

// locatePageInProjects 按页面 id 定位页面：逐工程独立作用域按 id 取，命中即返回。
//
// 页面 id 是主键（跨工程不会重复），所以逐工程探测的结果是确定的；反过来，
// 「不设作用域按 id 直查」在换非超级角色后是静默 ErrRecordNotFound ——
// 那种形态会让「页面明明在，却报不存在」。全部未命中返回 gorm.ErrRecordNotFound。
func (s *Service) locatePageInProjects(ctx context.Context, id string) (*pagemodel.PageEntity, error) {
	ids, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	var lastErr error = gorm.ErrRecordNotFound
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		page, gerr := s.model.GetByID(ctx, id, projectID)
		if gerr == nil {
			return page, nil
		}
		lastErr = gerr
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, lastErr
}

// 本包 sentinel error（审计项「page 错误码靠中文文案 strings.Contains 匹配」）。
//
// 修复前：service 各处 errors.New(pageenums.ErrXxx) 生成普通字符串错误，
// handler 用 strings.Contains(err.Error(), 文案) 分类映射 HTTP 状态码——
// 文案改动/拼接前缀即失效，属于脆弱的字符串耦合。
// 修复后：service 统一返回下方包级 sentinel（Error() 文案与 pageenums 一致，
// 前端响应文案不变），handler 通过 errors.Is 精确分类。
//
// pipeline 内核错误（internal/pipeline/publisher.go 已定义 ErrPageNotFound /
// ErrVersionConflict / ErrNoStagedArtifact / ErrRollbackPathMismatch 等 sentinel）
// 由 mapPublishError 归一到本包 sentinel（见 page_publish.go）；
// pipeline 侧尚未 sentinel 化的字符串错误暂以 default 分支原样透传，
// 待 pipeline 后续 sentinel 化后统一 %w 收敛。
var (
	// ErrInvalidParam 请求本身不合法（nil 请求、空/空白 ID 等），与资源存在性无关。
	ErrInvalidParam    = errors.New(pageenums.ErrInvalidParam)
	ErrPageNotFound    = errors.New(pageenums.ErrPageNotFound)
	ErrProjectNotFound = errors.New(pageenums.ErrProjectNotFound)
	// ErrProjectRequired 跨工程扇出入口无法确定工程作用域（DB-009 第三批）。
	// 只用于「工程表读不到 / 一个工程都没有」这类真实异常：正常多工程部署下这些入口
	// 会逐工程设作用域执行，不会走到这里。
	ErrProjectRequired      = errors.New(pageenums.ErrProjectRequired)
	ErrInvalidKind          = errors.New(pageenums.ErrInvalidKind)
	ErrInvalidDocument      = errors.New(pageenums.ErrInvalidDocument)
	ErrInvalidPath          = errors.New(pageenums.ErrInvalidPath)
	ErrDraftVersionConflict = errors.New(pageenums.ErrDraftVersionConflict)
	ErrPathOccupied         = errors.New(pageenums.ErrPathOccupied)
	ErrNoStagedArtifact     = errors.New(pageenums.ErrNoStagedArtifact)
	ErrRollbackTargetMiss   = errors.New(pageenums.ErrRollbackTargetMiss)
	ErrRebuildRequired      = errors.New(pageenums.ErrRebuildRequired)

	// 重定向管理（审计 SEO-025）。ErrRedirectUnavailable 覆盖「装配期未注入路由契约」
	// 这一种明确异常：此时新增/删除重定向只会产生「线上生效但账上没有」的半成品，
	// 宁可显式失败也不静默跳过。
	ErrRedirectUnavailable = errors.New(pageenums.ErrRedirectUnavailable)
	ErrRedirectNotFound    = errors.New(pageenums.ErrRedirectNotFound)
	ErrRedirectOccupied    = errors.New(pageenums.ErrRedirectOccupied)
	ErrRedirectTargetMiss  = errors.New(pageenums.ErrRedirectTargetMiss)
	ErrRedirectLoop        = errors.New(pageenums.ErrRedirectLoop)

	// 定时上下线（PIPE-7）。到点执行失败**不走这些 sentinel**：那条路径的失败要落进
	// page_schedules.last_error（业务 key），而不是抛给某个请求的调用方。
	ErrScheduleNotFound      = errors.New(pageenums.ErrScheduleNotFound)
	ErrScheduleInPast        = errors.New(pageenums.ErrScheduleInPast)
	ErrScheduleActionInvalid = errors.New(pageenums.ErrScheduleActionInvalid)
	ErrScheduleRunning       = errors.New(pageenums.ErrScheduleRunning)
	ErrScheduleOccupied      = errors.New(pageenums.ErrScheduleOccupied)
)

const (
	// pageRevisionKeep 每页保留的历史快照条数。
	pageRevisionKeep = 20
	// pageRevisionRetainDays 历史快照保留期：超出条数**且**早于该窗口才清理。
	pageRevisionRetainDays = 90
	// artifactRetentionDays 产物保留窗口：早于它且不再被任何指针引用的产物可回收。
	artifactRetentionDays = 30
	// pageRetentionInterval 保留期任务的运行间隔。
	pageRetentionInterval = 24 * time.Hour
	// pageRetentionBatch 单批删除行数：批次存在的意义是不制造长事务与锁表。
	pageRetentionBatch = 500
)

// pruneRevisions 保存草稿后收敛该页的历史快照。
//
// 放在保存路径上是刻意的：定时任务一天只跑一次，高频编辑的页面在这之间照样能堆出
// 成百上千份完整文档快照。失败只记日志 —— 清理不该让一次保存失败。
// projectID 必填（DB-009 第四批）：修订表没有 project_id 列、不受策略约束，
// 归属经 pages 判断 —— 缺它这条清理会删到别的工程页面的历史版本。
func (s *Service) pruneRevisions(ctx context.Context, projectID, pageID string) {
	if s == nil || s.model == nil {
		return
	}
	n, err := s.model.PruneRevisions(ctx, projectID, pageID, pageRevisionKeep)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "收敛页面历史快照失败")
		return
	}
	if n > 0 {
		logger.Scene("page").With("pageId", pageID).With("deleted", n).Info("已收敛页面历史快照")
	}
}

// PurgeRetention 执行一次保留期清理：历史快照（分批删）+ 产物 GC（真删）。
//
// 产物 GC 的顺序是「先确认不在保护集合（已激活/已暂存）→ 删 DB 行 → 删磁盘」，
// 磁盘删除失败只记日志：孤儿文件由反向对账（IDX-015）暴露，不需要在这里回滚 DB。
func (s *Service) PurgeRetention(ctx context.Context) (deletedRevisions int64, err error) {
	if s == nil || s.model == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	revisions := retention.Task{
		Name: "page_revisions", Table: "page_revisions", TimeColumn: "create_time",
		Retain: pageRevisionRetainDays * 24 * time.Hour, BatchSize: pageRetentionBatch,
		Note: "每页保留最近若干版本；只有既超出条数、又早于保留期的才删（回退需要近期版本）",
		Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
			// 逐工程（DB-009 第四批）：修订表不受策略约束，「全库清理」必须由调用方
			// 逐工程展开，否则这条 DELETE 会跨工程删历史快照，而且一句日志都不报。
			projects, perr := s.fanoutProjectIDs(ctx)
			if perr != nil {
				return 0, perr
			}
			var total int64
			for _, pid := range projects {
				if ctx.Err() != nil {
					break
				}
				n, derr := s.model.DeleteStaleRevisions(ctx, pid, pageRevisionKeep, cutoff, limit)
				if derr != nil {
					return total, derr
				}
				total += n
			}
			return total, nil
		},
	}
	// 定时上下线的排定记录（PIPE-7）：只清**终态**且到点时刻超保留期的行。
	// 保留期常量取自 retention 包（与 internal/retention/catalog.go 的声明同一个数字）——
	// 两处各写一份的下场是「声明说留 90 天、实际清了 30 天」且没人会去比。
	schedules := retention.Task{
		Name: "page_schedules", Table: "page_schedules", TimeColumn: "scheduled_at",
		Retain: retention.RetainPageScheduleDays * 24 * time.Hour, BatchSize: pageRetentionBatch,
		Note: "只清终态（done/failed/canceled）且到点时刻早于保留期的行；待执行 / 执行中一律保留",
		Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
			return s.model.DeleteFinishedSchedules(ctx, cutoff, limit)
		},
	}
	outcomes := retention.RunAll(ctx, []retention.Task{revisions, schedules}, now)
	total, failed := retention.Summary(outcomes)
	if len(failed) > 0 {
		logger.Scene("page").With("failed", failed).Warn("保留期任务部分失败")
	}
	if total > 0 {
		logger.Scene("page").With("deleted", total).Info("已清理超期页面历史快照")
	}

	// 产物 GC：显式传 dryRun=false 才会真删（安全默认仍在接口侧保留）。
	notDryRun := false
	res, err := s.GarbageCollectArtifacts(ctx, &pagedto.GCArtifactsReq{
		RetentionDays: artifactRetentionDays, DryRun: &notDryRun,
	})
	if err != nil {
		logger.Scene("page").Error(err, "定时回收产物失败")
		// 产物回收失败不算整个保留期任务失败：历史快照那一半已经做完了。
		return total, err
	}
	if res != nil && (res.Deleted > 0 || res.Failed > 0) {
		logger.Scene("page").
			With("scanned", res.Scanned).With("deleted", res.Deleted).With("failed", res.Failed).
			Info("已回收超期产物")
	}
	return total, nil
}

// StartPageRetentionScheduler 启动每日保留期清理（IDX-004 / IDX-005）。
//
// 进程内 goroutine + ticker，与 analytics / order 的既有调度同形（先跑一次再等间隔）：
// 单实例部署够用；多实例部署下重复执行是安全的（删除按时间分界幂等）。
func StartPageRetentionScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, _ = svc.PurgeRetention(ctx)
		}
		run()
		ticker := time.NewTicker(pageRetentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
