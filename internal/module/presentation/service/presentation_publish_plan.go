package presentationservice

// presentation_publish_plan.go — 多语言发布的「锁内冻结 → 锁外编译 → 锁内提交」三段式（PERF-01）。
//
// 背景（审计 PERF-01）：编译内核（pipeline.Publisher.Build）早已是「锁内取快照 →
// 锁外编译落盘 → 锁内二次校验并提交」，但上层把**整个多语言发布**包在一把实例锁里：
// 逐语言 render、内容寻址落盘、版本分配、激活访问面、推进指针全在同一段临界区。
// 于是同一实例的两次发布完全串行 —— 一次发布里某种语言编译慢，另一条同实例的
// 写路径（依赖失效自动重建、改 URL、保存独立文档、快照回滚、产物回滚）只能干等，
// 而它们要等的其实只有最后那几步状态推进。
//
// 现在实例锁只覆盖两段（都在本文件）：
//
//	① freezePublish（锁内，短）：重读实例行 → 冻结本次发布的不可变输入
//	   （实例行快照、语言清单与路径规则、编译底稿）→ 记下实例状态指纹；
//	② commitPublish（锁内）：指纹校验 → 发布计划落库（版本分配 + 快照 + 产物行 + 依赖）
//	   → 逐语言「登记回执 → 激活访问面 → 登记路由 → 写语言账本」→ 推进实例指针。
//
// 中间最慢的一段 compileLangs（Jet 渲染 + 各语言产物落盘）**不持锁**。
//
// 锁保护的不变量（少一条就会出错；写在这里，免得下次有人把编译又挪回锁内）：
//
//	L1 版本分配单调：产物版本号是 MAX(version)+1（NextArtifactVersionTx）。
//	   没有实例级互斥时两次发布会算出同一个版本号，撞 uk_presentation_artifacts_instance_version_lang；
//	L2 active 指针是实例状态的单一真源：指针列（current_snapshot_id / staged_* / active_artifact_id
//	   / stale / published_at）只由整批成功后的收尾写入，且必须与语言账本（presentation_publications）
//	   指向同一批产物 —— 谁最后提交谁是真源，中间态不能被另一次提交截断；
//	L3 每语言单一 active 版本：某个语言的账本行与访问面符号链接必须同批次前进
//	   （publishOneLang 的「回执 → 激活 → 路由 → 账本」是一条串行链），
//	   否则出现「数据库声称该语言已发布，而线上文件不是账本记的那一份」；
//	L4 提交顺序 ≠ 开始顺序：编译期间别的发布会话可以先进锁提交。所以提交前必须校验
//	   「我冻结出来的实例状态仍然是当前状态」，否则基于旧输入编译出来的字节、以及
//	   快照里记的模板绑定/路径/渲染模式，会把别人的新状态写回去。
//	   校验失败 → 本次编译结果作废 → 重新冻结重试（见 publishAllLangs 的循环）。
//
// 谁在等这把锁：同一实体的全部写路径 —— 创建、重建（含依赖失效触发的自动重建）、
// 改 URL、保存独立文档 / 重新套用预设、快照回滚、产物指针回滚。改前它们整段排队，
// 改后只有 ① 与 ② 排队。
//
// 进程内锁的前提不变（见 presentation_service.go 的 lockInstance 注释）：跨进程互斥仍只由
// 构建队列的 SKIP LOCKED claim 提供，且只覆盖自动重建；本文件不改变这一点，
// 因此 ① ② 之外还多一道**数据库侧**的兜底 —— 提交前的指纹校验对「同一进程内」
// 与「别的进程刚提交过」两种情形一视同仁（后者读到的是库里已经推进过的状态）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// publishAttempts 一次发布会话的最大尝试次数。
//
// 冲突（编译期间别的批次先提交）时重新冻结再编译，最多这么多次。两次以上冲突意味着
// 这个实例正被高频写入（例如依赖扇出连续触发重建）：继续自旋会把它变成热循环，
// 交给调用方重试（自动重建保持 stale，下次触发再来）比在这里抢更快。
const publishAttempts = 3

// errPublishStateMoved 提交前发现实例状态已被别的批次推进：本次编译所依据的实例状态
// 已经过期，产出的字节与提交计划都必须作废，重新冻结再编译（见 publishAllLangs）。
var errPublishStateMoved = errors.New("实例状态已被并发的发布批次推进")

// publishInputError 发布**输入**解析失败（模板 / 覆盖文档）。改前这一步在调用点、
// 发布之前完成，错误是**原样透出**的 —— 各调用方按自己的白名单把它翻成
// 「该去建模板 / 换模板 / 改文档」的具体指引（见 content / product 的 FacingMessages）。
//
// 收窄锁覆盖范围后这一步移进了发布会话的冻结段（必须与实例行同源），若不显式区分，
// 错误会被统一裹成 ErrBuildFailed，用户看到的就是「构建失败」这条笼统文案 ——
// 具体原因被吃掉是真实的功能退化（运营据此不知道下一步该做什么），所以保留一条
// 「输入错误原样透出」的通道：只有真正的构建失败才打 ErrBuildFailed。
type publishInputError struct{ err error }

func (e *publishInputError) Error() string { return e.err.Error() }
func (e *publishInputError) Unwrap() error { return e.err }

// publishFailedErr 组装发布失败的对外错误：输入解析失败原样透出（改前口径），
// 其余（编译 / 落库 / 激活 / 语言清单）一律打上 ErrBuildFailed。
func publishFailedErr(err error) error {
	var inputErr *publishInputError
	if errors.As(err, &inputErr) {
		return inputErr.err
	}
	return fmt.Errorf("%s: %w", presentationenums.ErrBuildFailed, err)
}

// publishIntent 一次多语言发布的**意图**：与实例当前行无关的那部分输入。
//
// 与实例行有关的部分（绑定模板、覆盖文档）不在这里固化，而是由 resolveDoc 按
// 「锁内重读出来的那一行」解析 —— 冲突重试时会重新调用，因此并发期间被换掉的
// 绑定不会被本次发布悄悄写回去。
type publishIntent struct {
	// resolveDoc 解析本次要编译的模板文档。每次尝试（首次 + 每次冲突重试）都会调用，
	// 调用点在实例锁内、实例行已重读之后。
	resolveDoc func(ctx context.Context, inst *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error)
	// logicalPath 本次发布的逻辑路径；空串 = 沿用实例当前 url_path。
	logicalPath string
	// mode 本次要落的渲染模式 / 独立文档变更（nil = 本次不改）。
	mode *instanceModePending
}

// publishFreeze 一次发布会话在锁内冻结出来的不可变输入。
//
// 「不可变」是就本次编译而言：编译阶段只读它，不再回库读语言清单或模板
// （唯一例外是编译期实体字段 —— 那本来就是构建期数据源，见 renderHTML）。
type publishFreeze struct {
	// langs 本次要上线的语言集合：既是逐语言编译的范围，也是产物里 hreflang
	// 互指的判定依据（SEO-026：必须是同一个切片，中间不再推导第二次）。
	langs []string
	// rule 站点语言 URL 规则（默认语言是否有前缀由它决定）。
	rule pipeline.LangURLRule
	// logicalPath 归一化后的逻辑路径（不含语言前缀）。
	logicalPath string
	// defaultLang 默认语言：决定哪一份产物成为实例的 active 指针。
	defaultLang string
	// tpl 本次编译的模板文档（已按实例当前行解析）。
	tpl *contenttemplatecontract.ResolvedTemplate
	// mode 本次要落的渲染模式 / 独立文档变更（由调用方的意图固定，不随重试变化）。
	mode *instanceModePending
	// fingerprint 冻结时实例行的状态指纹，提交前逐列比对（见 L4）。
	fingerprint instanceStateFingerprint
}

// freezePublish 锁内：重读实例行、解析语言清单与编译底稿，并把**库里的行**写回 inst。
//
// 为什么必须在锁内重读：调用方手里的 inst 是进锁之前读的（改 URL / 保存独立文档等路径
// 还要更早），拿它当冻结值会让指纹校验永远对不上（自己把自己判成冲突），也无法据此
// 判断「等锁期间实例被删了 / 被换绑定了」。写回 inst 是刻意的：后续的落库比较
// （persistMultiLangArtifacts 里的模板 / 路径 / 模式判定）与调用方的响应组装
// （toResp）看的都应该是库里那一行。
func (s *Service) freezePublish(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent) (frozen *publishFreeze, timing phaseTiming, err error) {
	if inst == nil || strings.TrimSpace(inst.ID) == "" {
		return nil, timing, errors.New(presentationenums.ErrNotFound)
	}
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	waitStart := time.Now()
	lock.Lock()
	timing.lockWait = time.Since(waitStart)
	workStart := time.Now()
	// defer 顺序（LIFO）：先结算本段工作耗时，再放锁 —— 否则「等锁」会把放锁后的
	// 调度延迟也算进这一段。
	defer lock.Unlock()
	defer func() { timing.work = time.Since(workStart) }()

	fresh, gerr := s.m.GetInstance(ctx, inst.ProjectID, inst.ID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, timing, errors.New(presentationenums.ErrNotFound)
		}
		return nil, timing, gerr
	}
	*inst = *fresh

	// 语言清单按**发布口径**取（审计 I18N-02）：它就是「本次发布要上线哪些语言」的全部
	// 内容，紧接着整批结案并推进实例指针。按可见回退取列表时，清单读不到会退化成
	// 「只发布默认语言 + 指针前进 + 批次标记收敛」—— 其余语言的线上产物静默停在旧字节，
	// 且再没有任何东西会去发现它。所以这里读不到就整批失败（实例标回未收敛，下次触发重来）。
	langs, lerr := s.publishLangsOf(ctx, inst.ProjectID)
	if lerr != nil {
		return nil, timing, fmt.Errorf("站点语言清单不可读，多语言整批发布中止: %w", lerr)
	}
	tpl, terr := intent.resolveDoc(ctx, inst)
	if terr != nil {
		// 输入解析失败单独标记：调用方据此原样透出（口径见 publishInputError）。
		return nil, timing, &publishInputError{err: terr}
	}
	logicalPath := pipeline.LogicalPathOf(ctx, s.project, inst.ProjectID, intent.logicalPath)
	if logicalPath == "" {
		logicalPath = s.instanceLogicalPath(ctx, inst)
	}
	return &publishFreeze{
		langs:       langs,
		rule:        pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID),
		logicalPath: logicalPath,
		defaultLang: pipeline.DefaultLocale(ctx, s.project, inst.ProjectID),
		tpl:         tpl,
		mode:        intent.mode,
		fingerprint: fingerprintOfInstance(fresh),
	}, timing, nil
}

// compileLangs 锁外：逐语言编译 + 产物落盘（Jet 渲染 + 内容寻址写盘，全链最慢的一段）。
//
// 刻意不做批内并行：多语言编译的内存峰值随并发数线性增长（每个语言一份完整渲染产物），
// 审计 PERF-01 明确要求「为构建并发设置按预计内存的预算，不盲目提高固定 worker 数」。
// 这里让「不同发布会话之间」并行（锁已让出），同一批语言仍然串行，峰值与改前一致。
func (s *Service) compileLangs(ctx context.Context, inst *presentationmodel.InstanceEntity,
	frozen *publishFreeze) (results []langBuildResult, work time.Duration, err error) {
	start := time.Now()
	defer func() { work = time.Since(start) }()
	results = make([]langBuildResult, 0, len(frozen.langs))
	for _, lang := range frozen.langs {
		accessPath, perr := pipeline.SitePath(frozen.rule, lang, frozen.logicalPath)
		if perr != nil {
			return nil, work, perr
		}
		// langs 原样透传（SEO-026）：就是本冻结随后逐语言结案的那一份。
		built, berr := s.buildArtifact(ctx, inst.EntityType, inst.EntityID, accessPath,
			inst.ProjectID, lang, frozen.langs, frozen.tpl)
		if berr != nil {
			return nil, work, berr
		}
		results = append(results, langBuildResult{lang: lang, accessPath: accessPath, built: built})
	}
	return results, work, nil
}

// commitPublish 锁内：指纹校验 → 发布计划落库 → 逐语言结案 → 推进指针。
//
// 只有走到最后一步（finalizeMultiLangBatch）实例指针才前进；失败分支与改前一致
// （标记待重建 + 保留 pending 回执），语义没有任何放宽。返回 errPublishStateMoved
// 表示校验没过、**没有写入任何东西**，调用方应重新冻结重试。
func (s *Service) commitPublish(ctx context.Context, inst *presentationmodel.InstanceEntity,
	frozen *publishFreeze, results []langBuildResult) (primaryArtifactID string, timing phaseTiming, err error) {
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	waitStart := time.Now()
	lock.Lock()
	timing.lockWait = time.Since(waitStart)
	workStart := time.Now()
	// defer 顺序（LIFO）：先结算本段工作耗时，再放锁（同 freezePublish）。
	defer lock.Unlock()
	defer func() { timing.work = time.Since(workStart) }()

	// L4：先确认实例状态还是我们冻结时的样子。放在任何写入之前 —— 校验失败必须
	// 是「零副作用」的，否则重试会建立在一半已落库的状态上。
	current, gerr := s.m.GetInstance(ctx, inst.ProjectID, inst.ID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return "", timing, errors.New(presentationenums.ErrNotFound)
		}
		return "", timing, gerr
	}
	if !frozen.fingerprint.equal(fingerprintOfInstance(current)) {
		return "", timing, errPublishStateMoved
	}

	// 本次发布前各语言的活跃产物（回执里的 from）：恢复与审计据此比对「从哪个产物切到
	// 哪一个」。必须在登记账本产物之前读取 —— 之后读到的是本次新写的行。
	previousArtifacts := s.publishedArtifactsByLang(ctx, inst.ID)

	now := time.Now().UTC()
	primaryArtifactID, snapID, perr := s.persistMultiLangArtifacts(ctx, inst, frozen.tpl, results,
		now, frozen.logicalPath, frozen.defaultLang, frozen.mode)
	if perr != nil {
		return "", timing, perr
	}

	pinged := make([]string, 0, len(results))
	for i := range results {
		b := &results[i]
		if pubErr := s.publishOneLang(ctx, inst, b, previousArtifacts[b.lang], now); pubErr != nil {
			s.markBatchUnconverged(ctx, inst, pubErr)
			return "", timing, pubErr
		}
		pinged = append(pinged, b.accessPath)
	}

	// 收尾：指针只在全部语言都结案后前进（失败分支不会走到这里）。
	if ferr := s.finalizeMultiLangBatch(ctx, inst, snapID, primaryArtifactID, now); ferr != nil {
		s.markBatchUnconverged(ctx, inst, ferr)
		return "", timing, ferr
	}
	s.notifyIndexNow(ctx, inst, pinged...)
	return primaryArtifactID, timing, nil
}

// instanceStateFingerprint 实例行上「一次发布提交会覆盖」的那几列的取值快照。
//
// 只覆盖提交会覆盖的列，**刻意不含 update_time**：
//   - 依赖失效的 MarkStale 只把 stale 置 true 并刷新 update_time。若实例本来就是 stale，
//     那么这次标识与本次提交要落的 stale=false 并不冲突（提交本来就要清 stale），
//     把它判成冲突会让「依赖扇出与用户重建同时发生」这种日常场景白重试一轮；
//   - 反过来，任何一次**成功**提交都会换掉 current_snapshot_id（新快照 uuid）与指针列，
//     任何一次**失败**提交会把 stale 置 true —— 全都落在下面这些列里，一次也跑不掉。
//
// override_document 取 jsonb 读回来的字节：PostgreSQL 的 jsonb 是规范化存储
// （键序、空白都归一并固定），同一个值两次读出来的字节必然相同，可以直接比较。
type instanceStateFingerprint struct {
	templateID        string
	urlPath           string
	renderMode        string
	overrideDocument  string
	currentSnapshotID string
	stagedSnapshotID  string
	stagedArtifactID  string
	activeArtifactID  string
	stale             bool
	publishedAt       time.Time
}

// fingerprintOfInstance 取实例行的状态指纹（列集合见类型注释）。
func fingerprintOfInstance(e *presentationmodel.InstanceEntity) instanceStateFingerprint {
	if e == nil {
		return instanceStateFingerprint{}
	}
	f := instanceStateFingerprint{
		templateID:        e.TemplateID,
		urlPath:           e.URLPath,
		renderMode:        presentationmodel.NormalizeRenderMode(e.RenderMode),
		overrideDocument:  string(e.OverrideDocument),
		currentSnapshotID: derefString(e.CurrentSnapshotID),
		stagedSnapshotID:  derefString(e.StagedSnapshotID),
		stagedArtifactID:  derefString(e.StagedArtifactID),
		activeArtifactID:  derefString(e.ActiveArtifactID),
		stale:             e.Stale,
	}
	if e.PublishedAt != nil {
		f.publishedAt = e.PublishedAt.UTC()
	}
	return f
}

// equal 两份指纹是否表示同一个实例状态。
func (f instanceStateFingerprint) equal(o instanceStateFingerprint) bool {
	return f.templateID == o.templateID &&
		f.urlPath == o.urlPath &&
		f.renderMode == o.renderMode &&
		f.overrideDocument == o.overrideDocument &&
		f.currentSnapshotID == o.currentSnapshotID &&
		f.stagedSnapshotID == o.stagedSnapshotID &&
		f.stagedArtifactID == o.stagedArtifactID &&
		f.activeArtifactID == o.activeArtifactID &&
		f.stale == o.stale &&
		f.publishedAt.Equal(o.publishedAt)
}

// ---- 分阶段耗时观测（PERF-01 验收的另一半）----
//
// 审计 PERF-01 的验收写的是「记录等待锁、编译、落盘、激活的独立耗时及 peak heap」：
// 锁收窄之后，没有这层观测就**没人能回答「锁等待到底还占多少、编译占多少」**，
// 下次再要收窄覆盖范围只能凭猜。所以每次发布会话（成功或失败都算）发一条结构化 Info：
// 取锁等待 / 冻结 / 编译（含产物落盘）/ 提交（含激活与指针推进）各段毫秒 + 冲突重试次数。
//
// 为什么按「会话」而不是按语言：把语言维度摊开会让日志行数随站点语言数翻倍，而排障要看的是
//「这一次发布的时间花在哪一段」；语言数作为字段带上，需要时据此判断规模。
//
// peak heap 刻意不在这里测：每次发布读一次 runtime.ReadMemStats 会 stop-the-world 采样、
// 引入 GC 噪声，而且单次发布的峰值基本反映不出「并发预算」这件事（它由同时进行的会话数决定）。
// 需要内存画像请走报告里给出的可复现方法（-memprofile / gctrace），本包不做埋点。

// phaseTiming 一段的耗时：lockWait 是取实例锁的等待，work 是拿到锁之后实际干活的耗时。
//
// 两者分开记：这正是这次要回答的问题 —— 收窄之后锁等待还剩多少。
type phaseTiming struct {
	lockWait time.Duration
	work     time.Duration
}

// publishSessionMetrics 一次发布会话的分阶段耗时。
//
// 冲突重试会把各段**累加**（attempts 记录试了几次），因此各段之和与 total 对得上，
// 不会因为只记最后一次而漏掉被丢弃的那次编译（那正是「一份语言慢」的代价所在）。
type publishSessionMetrics struct {
	startedAt      time.Time
	attempts       int
	langs          int
	lockWaitFreeze time.Duration
	freeze         time.Duration
	compile        time.Duration
	lockWaitCommit time.Duration
	commit         time.Duration
}

// retried 是否发生过冲突重试（attempts > 1 即有一次以上的编译结果被丢弃）。
func (m publishSessionMetrics) retried() bool { return m.attempts > 1 }

// total 会话总时长（含所有重试尝试）。
func (m publishSessionMetrics) total() time.Duration { return time.Since(m.startedAt) }

// 发布会话日志的 outcome 取值。
const (
	publishOutcomeOK     = "ok"
	publishOutcomeFailed = "failed"
)

// publishOutcomeOf 一次发布会话的结局：失败路径同样要把已耗时发出来（带 outcome=failed）。
func publishOutcomeOf(cause error) string {
	if cause != nil {
		return publishOutcomeFailed
	}
	return publishOutcomeOK
}

// publishSessionFields 分阶段耗时日志的字段集合（写入端与用例读同一份）。
//
// 抽成纯函数是为了让「观测契约」可断言：用例据此钉住字段确实会被写出去（少了哪个字段即红），
// 而又不必去断言具体的毫秒数 —— 那是负载相关的量，断言它只会做出一个随机红的用例。
func publishSessionFields(instanceID string, m publishSessionMetrics, outcome string) []any {
	return []any{
		"instanceId", instanceID,
		"langs", m.langs,
		"attempts", m.attempts,
		"retried", m.retried(),
		"lockWaitFreezeMs", m.lockWaitFreeze.Milliseconds(),
		"freezeMs", m.freeze.Milliseconds(),
		"compileMs", m.compile.Milliseconds(),
		"lockWaitCommitMs", m.lockWaitCommit.Milliseconds(),
		"commitMs", m.commit.Milliseconds(),
		"totalMs", m.total().Milliseconds(),
		"outcome", outcome,
	}
}

// logPublishSession 发一条发布会话的分阶段耗时日志。
//
// 级别固定 Info：这是常态运维信号（每次发布一条），不是告警 —— 失败时也走 Info 并把原因
// 放进 error 字段，失败告警由调用方各自的错误日志负责，同一件事不记两遍。
// 调用点在 publishAllLangs 的 defer 里，因此**失败路径也会带着已耗时发出来**。
func (s *Service) logPublishSession(inst *presentationmodel.InstanceEntity, m publishSessionMetrics, cause error) {
	if s == nil {
		return
	}
	instanceID := ""
	if inst != nil {
		instanceID = inst.ID
	}
	fields := publishSessionFields(instanceID, m, publishOutcomeOf(cause))
	entry := logger.Scene("publication")
	for i := 0; i+1 < len(fields); i += 2 {
		key, _ := fields[i].(string)
		entry = entry.With(key, fields[i+1])
	}
	if cause != nil {
		entry = entry.With("error", cause.Error())
	}
	entry.Info("发布会话分阶段耗时")
}

// derefString 可空字符串列取值（NULL 与空串在指纹里等价：两列都不接受空串业务值）。
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
