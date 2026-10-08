package presentationservice

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

// 与 page 侧（page_publish_converge.go）同形、同表、同状态机，区别只有两处：
//   - 领取口径按 ReceiptSourcePresentation + presentation 自己的动作词表
//     （只认 switch_active；update_url / rollback 是手工 Page 的动作）；
//   - 重放例程是本模块既有的 recoverOneReceipt（活跃指针与语言账本在另外两张表上，
//     拿 page 的补齐逻辑会写错地方）。
//
// 问题：多语言发布的「已切换访问面、数据库没跟上」窗口里失败，回执留在 pending，
// 而此前**只有进程启动时**跑一次恢复（RecoverPendingPublications）——
// 长时间不重启就一直 pending，线上与库长期不一致，而且没有任何可见性。
//
// 三个驱动源共用同一段重放实现：
//
//	启动首跑  RecoverPendingPublications —— 不限批，把上次进程留下的残留一次收干净
//	定时兜底  ConvergePendingReceipts   —— 时间驱动，分批领取（多实例安全）
//	写路径    NotifyPendingReceipt      —— 事务落定后的进程内快通道，正常路径毫秒级

// 问题（AR2-004）：多语言发布的「逐语言激活」是访问面上不可逆的一步，而路由登记
// （page_routes）此前只是发布之后的 best-effort 副作用 —— 登记失败只写一条 Warn，
// publishAllLangs 照常返回成功。于是 URL 文件已可访问、路由账本却缺行：后续占用
// 预检 / 回滚 / 删除 / GC 依据路由表得出错误结论，留下无法管理的线上路径。
//
// 做法（与 page 侧 page_publish_ledger.go 同一张表、同一套状态机）：
//  1. 每语言在**切换访问面之前**登记 pending 回执（记 path / lang / from→to 产物）；
//  2. 激活 → 路由登记 → 结案写入都成功，才把回执标 committed；
//  3. 路由登记失败：回执保留 pending（访问面已切换，不能标 rolled_back 谎称没发生），
//     错误向上返回，不再静默成功；
//  4. 启动恢复逐条判定：访问面确实指向本次产物 → 幂等补齐路由登记后结案；
//     指向别处或路径已过期 → 标 rolled_back（不碰文件与数据库）。
//
// 两个恢复器各扫各的 source_type：page 侧写 pages 的活跃指针，presentation 侧的
// 活跃指针与语言账本在另外两张表上，用同一套落点会写错地方。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/presentation/dto"
	"go_wp/internal/module/presentation/enums"
	"go_wp/internal/module/presentation/model"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
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

// Rebuild 实体数据更新后重建：重解析模板+实体 → 重新编译发布。
func (s *Service) Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityID == "" {
		return nil, errors.New(presentationenums.ErrInvalidParam)
	}
	// 工程作用域（DB-009 第二批）：反查实例在本工程内进行。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 反查实例（entity_id 匹配）。
	inst, err := s.findByEntityID(ctx, projectID, req.EntityID)
	if err != nil {
		return nil, errors.New(presentationenums.ErrNotFound)
	}
	// req.TemplateID 非空 = 切换绑定并重新发布（验收 4：切换模板重新发布后产物随之变化）。
	// 换底稿 = 放弃自定义（docs/04-C）：切换路径不应用实例文档，产物按新模板编译，
	// 模式与文档列在同一次发布的事务里一并清除（presentation_i18n.go 的
	// ClearInstanceModeTx，与快照/产物/指针对齐）；非切换路径则按渲染模式取底稿
	//（document 用商品自己的文档；binding 照常解析，实体数据更新不丢自定义）。
	//
	// 模板解析**不在这里做**（PERF-01）：它跟实例行是同一份输入，必须一起在发布会话的
	// 锁内冻结阶段解析 —— 提前解析出来的绑定会在编译期间被换掉，而本次发布随后把
	// 旧绑定写回去（含 template_id 列）。
	switching := strings.TrimSpace(req.TemplateID) != ""
	return s.rebuildInstance(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			tpl, rerr := s.resolveBoundTemplate(ctx, fresh, req.TemplateID)
			if rerr != nil {
				return nil, rerr
			}
			// 非切换：按渲染模式取底稿（document 模式用该商品文档，模板照常解析数据）。
			if !switching {
				tpl = instanceDocumentFor(fresh, tpl)
			}
			return tpl, nil
		},
	})
}

// PreviewInstance 发布前预览（issue #14 验收 3）：按指定（或默认）模板渲染实体。
//
// 全程只读：不写快照/产物行/指针、不落盘产物、不激活 URL —— 预览看到的就是
// 发布时会产出的字节（与 buildArtifact 共用 renderHTML），但线上与库表状态不变。
// ListArtifactHashes 列出本模块认领的全部产物 hash（IDX-015 反向对账的属主清单）。
//
// 只读且不含内容：反查「磁盘上这个目录是不是我们产出的」只需要这一个答案。
// 与 page 模块的同名方法同义 —— 两个模块共用一个 artifacts 根，
// 漏接任何一方都会让对账把对方的产物误报成孤儿。
func (s *Service) ListArtifactHashes(ctx context.Context) (hashes []string, err error) {
	return s.m.ListArtifactHashes(ctx)
}

// rebuildInstance 实例重建主链：编译发布 → 新快照 → 新产物行 → 指针切换 → 依赖落库。
//
// 实例级互斥由 publishAllLangs 的分段锁提供（PERF-01）：编译与产物落盘在锁外，
// 只有冻结与「版本分配 / 落库 / 激活 / 指针推进」两段在锁内。
func (s *Service) rebuildInstance(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent) (res *presentationdto.InstanceResp, err error) {
	if _, err = s.publishAllLangs(ctx, inst, intent); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

// builtArtifact 一次构建的产物信息（编译结果 + 存储定位 + Manifest 依赖）。
type builtArtifact struct {
	Hash     string
	Loc      pipeline.Locator
	Manifest pipeline.Manifest
}

// recordArtifactTx 事务内写产物行（同 hash 幂等复用，避免重建时产物行膨胀）。
func (s *Service) recordArtifactTx(ctx context.Context, tx *gorm.DB, inst *presentationmodel.InstanceEntity,
	snapID string, built builtArtifact, lang string, version int64, now time.Time) (artifactID string, err error) {
	if existing, gerr := s.m.GetArtifactByHashTx(tx, inst.ID, built.Hash); gerr == nil {
		return existing.ID, nil
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return "", gerr
	}
	// manifestJSON 是 pipeline.Manifest —— **输出清单**（canonicalPath / files /
	// dependencies / diagnostics），就是 NewArtifact 写进产物目录 manifest.json 并参与
	// 产物 hash 的那一份字节。
	//
	// 只写 manifest 一列（审计 DB-02，迁移 305）：这里此前把同一个 manifestJSON 同时写进
	// BuildInputManifest 与 Manifest，两列同字节、且前者全仓零读取者。「输入清单」这种东西
	// 在代码里并不存在（pipeline.BuildInput 是内存结构，从不序列化落库；输入侧事实由
	// 快照文档 + source_hash / build_input_hash 承载），所以两列同义 → 合并为 manifest。
	manifestJSON, err := json.Marshal(built.Manifest)
	if err != nil {
		return "", err
	}
	e := &presentationmodel.ArtifactEntity{
		ID: uuid.NewString(), PresentationInstanceID: inst.ID, SnapshotID: snapID,
		Version: version, Lang: strings.TrimSpace(lang), SourceHash: built.Manifest.SourceHash,
		BuildInputHash:   built.Manifest.BuildInputHash,
		ArtifactProvider: "local", ArtifactKey: built.Loc.Key, ArtifactHash: built.Hash,
		CompilerVersion: built.Manifest.CompilerVersion,
		// 真实注册表版本（组件模板 + Props 结构 + 二进制 revision 的指纹）。
		// 原来与 CompilerVersion 写同一个常量 "internal-builder"，等于空转：
		// 自动发布实例的产物也就无法参与「组件更新后识别待重建页面」的比对，
		// 与手工 Page 路径行为不一致（见 builder.RegistryVersion）。
		RegistryVersion: builder.RegistryVersion(),
		Manifest:        manifestJSON, PayloadState: "available", Note: "",
		CreatedBy: systemCreator, CreatedAt: now,
	}
	if err = s.m.CreateArtifactTx(tx, e); err != nil {
		return "", err
	}
	return e.ID, nil
}

// persistDependenciesTx 事务内把本次产物的依赖集合写入 presentation_dependencies。
//
// 依赖记录是失效追踪的投影而非构建输入；但它必须与快照/产物行/指针同生共死，
// 否则会出现「指针已切到新产物、依赖记录仍是旧集合」的漂移，导致后续
// fan-out 按错误的依赖反查（漏标 stale 或误标）。
func (s *Service) persistDependenciesTx(tx *gorm.DB, instanceID, artifactID string, deps []pipeline.Dependency, now time.Time) error {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(artifactID) == "" {
		return nil
	}
	rows := make([]presentationmodel.DependencyEntity, 0, len(deps))
	seen := map[[2]string]bool{}
	for _, d := range deps {
		kind, key := strings.TrimSpace(d.Kind), strings.TrimSpace(d.Key)
		if kind == "" || key == "" {
			continue
		}
		if seen[[2]string{kind, key}] {
			continue
		}
		seen[[2]string{kind, key}] = true
		row := presentationmodel.DependencyEntity{
			PresentationID: instanceID, ArtifactID: artifactID,
			DependencyKind: kind, DependencyKey: key, LastChecked: now,
		}
		if rev := strings.TrimSpace(d.Revision); rev != "" {
			r := rev
			row.Revision = &r
		}
		rows = append(rows, row)
	}
	return s.m.ReplaceDependenciesTx(tx, artifactID, rows)
}

const (
	// pendingReceiptConvergeBatch 单批领取上限（条）。
	//
	// 与 page 侧同一条理由：不制造长事务，也不让一次收敛把启动链拖住。
	// 一条重放要读文件系统符号链接 + 若干次 DB 往返，20 条把单批耗时压在秒级以内。
	pendingReceiptConvergeBatch = 20
	// pendingReceiptConvergeMaxBatches 单次收敛的批次上限（20 × 50 = 1000 条/次）。
	//
	// 正常待办是个位数，这个上限只用来兜住「积压了成千上万条」的异常场面：
	// 超出的部分交给下一轮，而不是让一个 goroutine 长时间占着数据库连接跑下去。
	pendingReceiptConvergeMaxBatches = 50
	// pendingReceiptStartupMaxBatches 启动首跑的批次上限；<= 0 表示不限批。
	pendingReceiptStartupMaxBatches = 0
	// pendingReceiptConvergeInterval 定时兜底的间隔。
	//
	// 取值与 page 侧一致：正常路径由写路径的快通道以毫秒级驱动，定时器只兜底
	// 快通道信号丢失（容量 1 的通道在收敛进行中只能留住一次信号）与
	// **别的实例**留下的残留（本进程收不到它的写信号）。空转成本被迁移 267 的
	// 部分索引压到「一次零行的索引扫描」，与表的历史规模无关。
	pendingReceiptConvergeInterval = time.Minute
	// pendingReceiptConvergeTimeout 定时 / 快通道单次收敛的超时。
	pendingReceiptConvergeTimeout = 2 * time.Minute
	// pendingReceiptStartupTimeout 启动首跑的超时（与改动前的启动恢复一致，5 分钟）。
	pendingReceiptStartupTimeout = 5 * time.Minute
)

// presentationReceiptActions 本模块认得的访问面切换回执动作名（与 publication 的登记端
// 同一份词汇表）。
//
// 为什么必须显式在册：publication_receipts 是**共用表** —— 手工页面（page）的发布 /
// 改 URL / 回滚回执各有自己的补齐例程，路由变更回执（activate / redirect）由各自事务内的
// 结案与补偿处理。拿「多语言发布」的判据去补齐一条 page 回执，会把手工页面的活跃指针
// 写到错误的表上。
//
// 这份清单同时是**领取条件**（ClaimPendingReceipts 的 action IN）与**判定条件**
// （isPresentationReceiptAction 复核）——两处各写一套筛选条件，迟早出现
// 「领得到却判不出来」的空转。
var presentationReceiptActions = []string{
	// 与登记端同一个字面量：presentation 的发布只登记 switch_active
	//（beginPublishReceipt 不传 action，由 publication 侧默认成它）。
	presentationReceiptAction,
}

// isPresentationReceiptAction 判断某个动作是否在本模块的收敛范围内（不在册一律跳过，
// 而不是当成多语言发布形态硬套）。
func isPresentationReceiptAction(action string) bool {
	for _, known := range presentationReceiptActions {
		if known == action {
			return true
		}
	}
	return false
}

// pendingReceiptConvergeResult 一次收敛的结果（供日志与返回值使用）。
type pendingReceiptConvergeResult struct {
	claimed    int // 本轮领取到的回执条数
	completed  int // 补齐数据库状态后结案
	rolledBack int // 判定为未生效、结案为已回滚
	failed     int // 仍失败，保留 pending
	batches    int // 实际领取的批次数
}

// converged 已离开 pending 的条数（补完成 + 标回滚）。
//
// 「仍失败」不算收敛：它还在 pending，下一轮继续重放。把失败的 pending 直接标死，
// 等于拿「看起来收敛了」换掉「状态未知」，比不收敛更糟。
func (r pendingReceiptConvergeResult) converged() int { return r.completed + r.rolledBack }

// ConvergePendingReceipts 收敛未结案的多语言发布回执（定时器与写路径快通道的入口）。
//
// 与启动恢复共用同一段重放实现（convergePendingReceipts → recoverOneReceipt）：
// 两种入口各写一套判定迟早分叉，而分叉的表现是「定时收敛说已收敛、重启恢复又改一遍」。
//
// 幂等：重放的每一步都是 upsert / 内容寻址 / 按归属重复激活，结案又只改 pending 行，
// 因此重复收敛（多实例、快通道与定时器同时触发）不会写出新状态。
//
// 返回的 error 汇总「领取失败」与「单条仍失败」；已收敛的条数照常返回 ——
// 部分失败不该把整轮的结果丢掉（调用方据此记日志，失败的行保留 pending 等下轮）。
func (s *Service) ConvergePendingReceipts(ctx context.Context) (converged int, err error) {
	res, claimErr, itemErrs := s.convergePendingReceipts(ctx, pendingReceiptConvergeMaxBatches)
	return res.converged(), errors.Join(append([]error{claimErr}, itemErrs...)...)
}

// RecoverPendingPublications 启动 / 运维全量恢复：把未结案的多语言发布回执一次收干净。
//
// 与定时收敛共用同一段实现（convergePendingReceipts），区别只在批次上限：
// 这里不限批（由调用方给的超时封顶），定时 / 快通道按批（不制造长事务）。
// 返回值保持原有形状（补完成 / 标回滚），供既有调用方与用例断言。
//
// 只把「领取失败」回传给调用方；单条重放失败按「保留 pending、下一轮再试」处理，
// 不让一条收不了的旧回执把整个启动恢复判成失败。
func (s *Service) RecoverPendingPublications(ctx context.Context) (recovered, rolledBack int, err error) {
	res, claimErr, _ := s.convergePendingReceipts(ctx, pendingReceiptStartupMaxBatches)
	return res.completed, res.rolledBack, claimErr
}

// convergePendingReceipts 收敛主循环：分批领取 → 逐条重放既有恢复例程 → 批次收敛。
//
// maxBatches <= 0 表示不限批（启动首跑；由 ctx 超时封顶）。
// 三个返回值：结果统计、领取错误（数据库层，整轮中止）、单条重放错误（保留 pending）。
func (s *Service) convergePendingReceipts(ctx context.Context, maxBatches int) (res pendingReceiptConvergeResult, claimErr error, itemErrs []error) {
	// 没有契约、没有访问面存储时不做任何事（这台实例没有回执设施，不是「没有待办」）。
	if s == nil || s.routes == nil || s.publication == nil {
		return res, nil, nil
	}
	start := time.Now()
	// 本轮是「恢复驱动」：期间的批次重跑会重新走主链写路径，那条路径失败时会推快通道
	// 信号 —— 推回去就是自激热循环。标记住整轮，NotifyPendingReceipt 据此静默丢弃。
	s.converging.Add(1)
	defer s.converging.Add(-1)
	// 回执所属实例：单个语言的切换是否生效由回执证明，批次是否收敛要看语言账本与实例指针
	// （见 convergeInstanceBatch）。跨批次累积，最后统一收敛。
	touched := make(map[string]string)
	for batch := 0; maxBatches <= 0 || batch < maxBatches; batch++ {
		if cerr := ctx.Err(); cerr != nil {
			itemErrs = append(itemErrs, cerr)
			break
		}
		items, cerr := s.routes.ClaimPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
			SourceType: pubcontract.ReceiptSourcePresentation,
			Actions:    presentationReceiptActions,
			Limit:      pendingReceiptConvergeBatch,
		})
		if cerr != nil {
			claimErr = cerr
			logger.Scene("publication").Error(cerr, "领取未结案的多语言发布回执失败（本轮收敛中止）")
			break
		}
		if len(items) == 0 {
			break
		}
		res.claimed += len(items)
		res.batches++
		handled, failed := 0, 0
		for _, item := range items {
			// 领取条件已经筛过一遍，这里是第二道（防止领取口径将来被放宽后静默扩大重放范围：
			// 拿多语言发布的判据去「补齐」一条手工页面回执，会写错活跃指针所在的表）。
			if item.SourceType != pubcontract.ReceiptSourcePresentation || !isPresentationReceiptAction(item.Action) {
				continue
			}
			handled++
			done, rerr := s.recoverOneReceipt(ctx, item)
			if rerr != nil {
				failed++
				logger.Scene("publication").With("receiptId", item.ID).With("action", item.Action).
					With("path", item.Path).Error(rerr, "多语言发布回执收敛失败（保留 pending，下一轮重试）")
				itemErrs = append(itemErrs, fmt.Errorf("回执 %s(%s): %w", item.ID, item.Action, rerr))
				continue
			}
			touched[item.SourceID] = item.ProjectID
			if done {
				res.completed++
			} else {
				res.rolledBack++
			}
		}
		res.failed += failed
		// 本批出现仍失败的行就收手：那些行还在 pending（且多半正卡在最旧的一批上），
		// 继续领下一批只会把它们再重放一遍 —— 交给下一轮，别在这里空转。
		if failed > 0 {
			break
		}
		// 本批没有一行属于本收敛例程（领取口径与判定口径不一致）：再领还是同一批，收手。
		if handled == 0 {
			break
		}
	}
	// 批次收敛：回执只覆盖「已经切过访问面」的语言，激活之前就失败的语言根本没有
	// 可补齐的切换 —— 只有重跑整批才能把它带上线（幂等，见 convergeInstanceBatch）。
	for instanceID, projectID := range touched {
		if ctx.Err() != nil {
			break
		}
		s.convergeInstanceBatch(ctx, projectID, instanceID)
	}
	s.lastConvergeAt.Store(time.Now().Unix())
	s.logPendingReceiptConverge(res, time.Since(start))
	return res, claimErr, itemErrs
}

// logPendingReceiptConverge 记一条结构化收敛日志（收敛条数 / 失败条数 / 耗时）。
//
// 空转（一条都没领到）走 Debug：定时器每分钟一次，用 Info 记「什么都没发生」只会把
// 真正有用的那几条淹掉。有失败一律 Warn —— 失败的 pending 是「线上与库还不一致」，
// 必须在日志里看得见。
func (s *Service) logPendingReceiptConverge(res pendingReceiptConvergeResult, cost time.Duration) {
	entry := logger.Scene("publication").
		With("sourceType", pubcontract.ReceiptSourcePresentation).
		With("claimed", res.claimed).
		With("converged", res.converged()).
		With("completed", res.completed).
		With("rolledBack", res.rolledBack).
		With("failed", res.failed).
		With("batches", res.batches).
		With("costMs", cost.Milliseconds())
	switch {
	case res.failed > 0:
		entry.Warn("多语言发布回执收敛完成（有仍未成功的行，保留 pending 等待下一轮）")
	case res.claimed > 0:
		entry.Info("多语言发布回执收敛完成")
	default:
		entry.Debug("多语言发布回执收敛空转（没有未结案的回执）")
	}
}

// NotifyPendingReceipt 写路径的进程内快通道信号（主链事务落定后调用）。
//
// 容量 1、非阻塞发送：并发写入合并成一次收敛（多推几次不携带额外信息），
// 通道满时直接丢弃 —— 定时器兜底，丢信号只会让收敛晚一个间隔，不会让回执永久 pending。
// 未启动调度（用例、或未装配发布回执设施的进程）时这个信号没有接收方，同样无害。
//
// ⚠️ 只从**主链写路径**推：从收敛 / 结案路径内部推会自激成热循环
// （收敛 → 重放 → 推信号 → 立刻再收敛，永不空转结束）。page 侧同名方法的注释
// 记着同一个坑。
//
// 本模块多一道防线：收敛的批次重跑会**重新走主链写路径**（convergeInstanceBatch →
// RebuildInstance → publishAllLangs），page 侧没有这条回环，所以这里除调用点自律
// （recoverOneReceipt / completePublishReceipt / abortPublishReceipt 不推）之外，
// 还用 s.converging 把「恢复驱动的写入」挡在信号之外 —— 只认用户驱动的写入。
func (s *Service) NotifyPendingReceipt() {
	if s == nil || s.convergeWake == nil || s.converging.Load() > 0 {
		return
	}
	select {
	case s.convergeWake <- struct{}{}:
	default:
	}
}

// PendingReceiptStatus 只读观测：当前未收敛的 pending 回执数 + 本进程最近一次收敛时刻。
//
// 口径与本收敛例程的领取条件完全一致（presentation + switch_active）：数的是
// 「本实例会去收、且还没收掉的行」，据此判断「收敛是不是跟不上了」。
// 手工页面与路由变更的回执由各自的处理方负责，不计入这里。
func (s *Service) PendingReceiptStatus(ctx context.Context) (pending int64, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePresentation,
		Actions:    presentationReceiptActions,
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	return pending, lastConvergeAt, nil
}

// PendingReceiptBacklog 只读观测：待收敛积压的完整形状 —— 条数 + 最老一条已等待多久
// + 本进程最近一次收敛时刻。
//
// 与 PendingReceiptStatus 的分工：那个只给「条数 + 收敛时刻」（三元组形状由既有调用方钉住），
// 而条数看不出积压是不是在增长 —— 收掉 3 条又来 3 条，数字纹丝不动。
// **最老一条的年龄**才是「收敛跟不跟得上」的判据：它只会在真的收不动时持续变大。
//
// 口径与收敛例程严格一致（presentation + switch_active），数出来的就是「本实例会去收、
// 且还没收掉的行」；手工页面与路由变更的回执由各自的处理方负责，不计入这里。
//
// 两个查询不共用事务：观测允许读到中间态（收敛正在跑时条数可能少一条、年龄刚被清零），
// 这不影响「积压是否在增长」的判断。
func (s *Service) PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePresentation,
		Actions:    presentationReceiptActions,
	})
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	if pending <= 0 {
		return 0, 0, lastConvergeAt, nil
	}
	return pending, s.oldestPendingReceiptAge(ctx), lastConvergeAt, nil
}

// oldestPendingReceiptAge 本模块口径里最老一条 pending 回执已等待的时长；读不到时返回 0。
//
// 走 ListPendingReceipts（按 create_time ASC 的**只读**列举）：契约里唯一一条不加锁的列举能力。
// 不能用 ClaimPendingReceipts —— 它用 FOR UPDATE SKIP LOCKED **认领**行，健康检查和后台页面
// 走那条路等于让观测动作把待收敛的行锁住再放回去，直接干扰收敛（多实例下还会把行从
// 别的实例的批次里抢走）。
//
// 代价：只在条数不为 0 时调用，且这一次列举会把全部 pending 行拉回来。正常积压是个位数；
// 真积压成千上万条时，多这一趟读取相比「积压完全不可见」仍是划算的。
// 时间戳解析失败按「读不到」处理 —— 年龄是附加信息，不该连条数一起丢掉。
func (s *Service) oldestPendingReceiptAge(ctx context.Context) time.Duration {
	items, err := s.routes.ListPendingReceipts(ctx)
	if err != nil {
		logger.Scene("publication").With("err", err).
			Warn("读取最老未结案的多语言发布回执失败（积压条数仍可用，年龄暂缺）")
		return 0
	}
	for _, item := range items {
		// 列表按 create_time ASC：第一条命中本模块口径的就是最老的一条。
		if item.SourceType != pubcontract.ReceiptSourcePresentation || !isPresentationReceiptAction(item.Action) {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, item.CreatedAt)
		if perr != nil || ts.IsZero() {
			return 0
		}
		if age := time.Since(ts); age > 0 {
			return age
		}
		return 0
	}
	return 0
}

// StartPendingReceiptConvergenceScheduler 启动多语言发布回执收敛调度（进程内 goroutine + ticker）。
//
// 形状与 page_retention / order_expire / build_worker 的既有调度一致：先跑一次，再等间隔。
// 两个信号源由 select 合并：
//   - ticker：兜底（快通道信号丢失，或残留是别的实例留下的）；
//   - convergeWake：写路径在事务落定后推的进程内快通道，正常路径毫秒级收敛。
//
// 启动首跑直接做全量恢复（不限批 + 5 分钟预算）—— 它取代了原先装配处的裸启动恢复。
// 不能留着那个入口：同一段实现若有两个「启动时跑一次」的驱动源，启动瞬间会有两个
// goroutine 并发重放同一批回执（重放虽幂等，仍会白白多跑一遍、多占一次连接）。
func StartPendingReceiptConvergenceScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startPendingReceiptConvergenceScheduler(svc, pendingReceiptConvergeInterval)
}

// StartPendingReceiptConvergenceSchedulerWithInterval 同上，但可注入间隔。
//
// 给用例用：注入一个远大于用例时长的间隔，就能把「收敛确实由快通道驱动、
// 而不是定时器顺手做掉的」证成（见 public/test/presentation/unit/presentation_receipt_converge_test.go）。
func StartPendingReceiptConvergenceSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPendingReceiptConvergenceScheduler(svc, interval)
}

func startPendingReceiptConvergenceScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	if interval <= 0 {
		interval = pendingReceiptConvergeInterval
	}
	go func() {
		// 启动首跑：全量收一次上次进程留下的残留。
		startupCtx, cancelStartup := context.WithTimeout(context.Background(), pendingReceiptStartupTimeout)
		_, _, _ = svc.RecoverPendingPublications(startupCtx)
		cancelStartup()

		converge := func() {
			ctx, cancel := context.WithTimeout(context.Background(), pendingReceiptConvergeTimeout)
			defer cancel()
			_, _ = svc.ConvergePendingReceipts(ctx)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				converge()
			case <-svc.convergeWake:
				converge()
			}
		}
	}()
}

const (
	// presentationReceiptAction 访问面切换类回执的动作名。
	//
	// 取值等于 pubcontract.ReceiptActionSwitchActive，这里只留一个字面量常量供
	// 本文件构造领取词表（作用域限定在本模块，避免别的包
	// 再抄一遍字面量）。归属（source_type）与动作词的权威定义都在 publication 契约。
	presentationReceiptAction = pubcontract.ReceiptActionSwitchActive
	// artifactLocatorKeyPrefix 访问面 Locator.Key 的产物前缀（Inspect 还原 locator 后剥掉）。
	artifactLocatorKeyPrefix = "artifacts/"
)

// beginPublishReceipt 在**切换访问面之前**登记一条 pending 回执，返回回执 id。
//
// 登记失败必须让本次发布中止（返回错误，调用方不切换访问面）：没有回执就无从判定
// 「这次切换到底发生过没有」，崩溃窗口里只能人工比对符号链接与数据库（TX-009 的取舍，
// 与 page 侧一致）。routes 为 nil（降级装配 / 单元测试）时无账本可记账，返回空 id。
func (s *Service) beginPublishReceipt(ctx context.Context, inst *presentationmodel.InstanceEntity,
	path, lang, fromArtifactID, toArtifactID string) (receiptID string, err error) {
	if s.routes == nil {
		return "", nil
	}
	id, berr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID:      inst.ProjectID,
		Path:           path,
		PresentationID: inst.ID,
		FromArtifactID: strings.TrimSpace(fromArtifactID),
		ToArtifactID:   toArtifactID,
		Lang:           lang,
	})
	if berr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", path).
			Error(berr, "多语言发布回执登记失败（不切换访问面）")
		return "", berr
	}
	return id, nil
}

// completePublishReceipt 结案（访问面、路由与数据库三者已一致）。
func (s *Service) completePublishReceipt(ctx context.Context, receiptID string) error {
	if s == nil || s.routes == nil || strings.TrimSpace(receiptID) == "" {
		return nil
	}
	return s.routes.CompletePublishReceipt(ctx, receiptID)
}

// abortPublishReceipt 结案为已回滚（切换没发生，或无副作用的失败）。
func (s *Service) abortPublishReceipt(ctx context.Context, receiptID, reason string) {
	if s == nil || s.routes == nil || strings.TrimSpace(receiptID) == "" {
		return
	}
	if err := s.routes.AbortPublishReceipt(ctx, receiptID); err != nil {
		logger.Scene("publication").With("receiptId", receiptID).
			Error(err, "多语言发布回执结案失败（"+reason+"）")
	}
}

// RecoverPendingPublications（启动 / 运维全量恢复）与 ConvergePendingReceipts（定时 + 快通道）
// 共用同一段重放实现（convergePendingReceipts）。再写一份判定会让「定时收敛说已收敛、重启恢复又改一遍」。
//
// 判据与 page 侧同源（符号链接实际指向哪个产物），只在证据充分时补齐：
// 判定错会写出错误的发布账本，而「不判定、只告警」至少不会把状态改得更糟，
// 所以证据不足一律走回滚分支并记日志。

// convergeInstanceBatch 批次收敛：语言账本没铺满、或实例指针没推进的实例重跑一次发布。
//
// 为什么光靠回执不够：回执只覆盖「已经切过访问面」的语言。激活之前就失败的语言
// （例如第二种语言的文件激活失败）根本没有生效的切换，补齐无从谈起 —— 只有重跑整批
// 才能把它带上线。重建是幂等的（同字节产物复用产物行），因此重跑不堆产物、也不会
// 产生混合版本。
func (s *Service) convergeInstanceBatch(ctx context.Context, projectID, instanceID string) {
	inst, ierr := s.locateInstanceForReceipt(ctx, projectID, instanceID)
	if ierr != nil {
		return
	}
	if s.batchConverged(ctx, inst) {
		return
	}
	s.markBatchUnconverged(ctx, inst, errors.New("多语言发布批次未收敛（启动恢复重跑一次）"))
	if s.buildQueue != nil {
		if qerr := s.buildQueue.EnqueuePresentationBuild(ctx, instanceID, projectID); qerr != nil {
			logger.Scene("publication").With("instanceId", instanceID).
				Error(qerr, "启动恢复入队重建失败（实例保持 stale）")
		}
		return
	}
	if rerr := s.RebuildInstance(ctx, instanceID); rerr != nil {
		logger.Scene("publication").With("instanceId", instanceID).
			Error(rerr, "启动恢复重建失败（实例保持 stale）")
		return
	}
	logger.Scene("publication").With("instanceId", instanceID).
		Info("启动恢复：多语言发布批次已收敛")
}

// batchConverged 该实例是否已收敛到「全部启用语言都有账本行，且指针指着默认语言产物」。
//
// 判据刻意从严：查不到语言清单或账本时**本轮不处置**（返回 true）—— 恢复流程不该因为
// 读不到状态就触发重建（那会在启动时对每个实例白跑一次构建）。但语言集必须**严格读取**：
// 用可见回退的集合会把「每种启用语言都得有账本行」缩成「只查默认语言那一行」，见下方注释。
func (s *Service) batchConverged(ctx context.Context, inst *presentationmodel.InstanceEntity) bool {
	// 语言清单这里用**严格读取**（审计 I18N-02），但仍然不改变「读不到就本轮不处置」
	// 的既有口径 —— 两者是不同的事，别混成一句「按可见回退」：
	//
	//   · 不能用回退集合：可见回退给的是「只有默认语言一种」，而下面这个循环的语义是
	//     「每种启用语言都必须有账本行」。用回退集合就把检查缩成了只查默认语言那一行，
	//     于是**基于错误的事实宣称收敛**（恒为真的假收敛）—— 这正是本次要消灭的形态：
	//     判定用了降级后的语言集，而判定结果被当成事实。
	//   · 读取失败也不能返回 false：本函数在启动恢复里跑，一次读库抖动会替每个实例
	//     排一次注定失败的构建。返回 true 在这里的实际含义是「本轮不处置」，与下面
	//     ListPublications 失败的分支同口径（同样是记 Error + return true）。
	//
	// 也就是说：读不到 = 本轮什么都不做（不宣称收敛、也不据此重建）。真正会产出错误
	// 产物的发布路径另有兜底：publishAllLangs 与编译期 SiteCompileOptions 都按发布
	// 口径硬失败。
	langs, lerr := s.publishLangsOf(ctx, inst.ProjectID)
	if lerr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).
			Error(lerr, "读取站点语言清单失败，跳过本轮批次收敛判定（不宣称收敛，下轮重来）")
		return true
	}
	if len(langs) == 0 {
		return true
	}
	pubs, perr := s.m.ListPublications(ctx, inst.ID)
	if perr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).
			Error(perr, "读取语言账本失败，跳过批次收敛判定")
		return true
	}
	byLang := make(map[string]string, len(pubs))
	for _, pub := range pubs {
		if pub.ArtifactID != nil && strings.TrimSpace(*pub.ArtifactID) != "" {
			byLang[pub.Lang] = *pub.ArtifactID
		}
	}
	defaultLang := pipeline.DefaultLocale(ctx, s.project, inst.ProjectID)
	primary := byLang[defaultLang]
	if primary == "" {
		primary = byLang[langs[0]]
	}
	for _, lang := range langs {
		if byLang[lang] == "" {
			return false
		}
	}
	if primary == "" || inst.ActiveArtifactID == nil || *inst.ActiveArtifactID != primary {
		return false
	}
	return !inst.Stale
}

// recoverOneReceipt 判定单条回执。返回 true 表示已补齐，false 表示已标回滚。
func (s *Service) recoverOneReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	inst, ierr := s.locateInstanceForReceipt(ctx, item.ProjectID, item.SourceID)
	if ierr != nil {
		s.abortPublishReceipt(ctx, item.ID, "实例不存在")
		return false, nil
	}
	expectedHash, herr := s.artifactHashByID(ctx, item.ToArtifactID)
	if herr != nil || expectedHash == "" {
		s.abortPublishReceipt(ctx, item.ID, "回执记录的产物行不存在")
		return false, nil
	}
	// 过期回执：该语言的当前访问路径已经不是回执记录的位置（改过 URL、或站点语言
	// 清单变了）。按旧路径补齐会写出一条不属于当前发布的账本行，宁可只结案。
	if !s.receiptPathStillCurrent(ctx, inst, item) {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			Warn("未结案的多语言发布回执路径已过期，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "回执路径已非该语言的当前访问路径")
		return false, nil
	}
	actualHash := s.activeArtifactHash(item.Path)
	if actualHash == "" || actualHash != expectedHash {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的多语言发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}
	// 访问面已切换、账本没跟上：补齐路由登记与语言账本（两步都是幂等写）后结案。
	if rerr := s.registerRoute(ctx, inst, item.Path, item.ToArtifactID); rerr != nil {
		return false, rerr
	}
	if lang := strings.TrimSpace(item.Lang); lang != "" {
		if merr := s.m.MarkPublishedLang(ctx, presentationmodel.PublicationRecord{
			PresentationID: inst.ID, Lang: lang, ActivePath: item.Path,
			ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash, PublishedAt: time.Now().UTC(),
		}); merr != nil {
			return false, merr
		}
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
		Info("多语言发布在失败/崩溃前已切换访问面，已补齐路由登记")
	return true, nil
}

// artifactHashByID 按产物行 id 取内容哈希（空 id 视为无记录）。
//
// 走 GetArtifactHash 的列白名单而不是 GetArtifact：回执核对是逐条循环里的读，
// 只要一个哈希，没有理由把 manifest 那个 JSONB 也读出来。
func (s *Service) artifactHashByID(ctx context.Context, artifactID string) (string, error) {
	id := strings.TrimSpace(artifactID)
	if id == "" {
		return "", nil
	}
	hash, err := s.m.GetArtifactHash(ctx, id)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

// activeArtifactHash 访问面上该路径当前指向的产物哈希（未激活 / 重定向 / 读取失败一律空串）。
func (s *Service) activeArtifactHash(path string) string {
	if s == nil || s.publication == nil {
		return ""
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}
	// 只有「页面产物」才算本次切换生效：重定向（改 URL 的旧路径 301）不是本次发布的落点。
	if state.Kind != pipeline.PublicationPage {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, artifactLocatorKeyPrefix)
}

// receiptPathStillCurrent 回执记录的路径是否仍是该语言的当前访问路径。
func (s *Service) receiptPathStillCurrent(ctx context.Context, inst *presentationmodel.InstanceEntity,
	item pubcontract.PendingReceiptResp) bool {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" {
		// 无语言信息的回执（非本模块登记）：不做路径守卫，交由访问面判据决定。
		return true
	}
	rule := pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID)
	path, perr := pipeline.SitePath(rule, lang, s.instanceLogicalPath(ctx, inst))
	if perr != nil {
		return false
	}
	return path == item.Path
}

// locateInstanceForReceipt 定位回执所属实例：回执带工程时先直查，否则逐工程遍历
// （presentation_instances 带 FORCE 策略，无作用域查询在非超级角色下静默 0 行）。
func (s *Service) locateInstanceForReceipt(ctx context.Context, projectID, instanceID string) (*presentationmodel.InstanceEntity, error) {
	if id := strings.TrimSpace(projectID); id != "" {
		inst, err := s.m.GetInstance(ctx, id, instanceID)
		if err == nil {
			return inst, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return s.findOneInstanceAnyProject(ctx, instanceID)
}
