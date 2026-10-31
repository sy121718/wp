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

// 与 page 侧同形、同表、同状态机，区别只有两处：
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
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
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

// errInputVersionMoved 编译期间实例的**输入代次**被推进（保存草稿 / 译文扇出 / 依赖标记）：
// 本次产物烘的是旧输入，整批作废。
//
// 与 errPublishStateMoved 并列而不是合并：两者都要求「重新冻结再编译」，但成因不同，
// 日志与重试预算都要能分开看 —— 状态被推进说明并发发布了，版本被推进说明并发改了输入，
// 后者即使指纹完全相等也必须失败（改输入不换任何指针列）。
var errInputVersionMoved = errors.New("实例的编译输入代次已被推进")

// errPublishInputSuperseded 本次发布在冻结段已经把输入意图（改 URL 的新路径）持久化，
// 但提交段发现版本又被推进了：产物作废、**不重试**。
//
// 与 errInputVersionMoved 的区别只在"还能不能重试"：那个是零副作用失败（什么都没写），
// 重新冻结再编译是对的；这里的输入意图已经是用户的事实（控制面 url_path 已是新值），
// 重试只会拿同一份旧冻结再跑一遍，而意图不需要也不能再落第二次。对外仍是版本冲突
// （409），由调用方决定是否由用户重试。
var errPublishInputSuperseded = errors.New("改 URL 的输入意图已持久化，但本次产物基于过期输入")

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
	return presentationenums.ErrDetail(presentationenums.ErrBuildFailed, err.Error())
}

// beginReceiptInput 登记一条发布回执的入参（字段语义见 beginPublishReceipt 的注释）。
// beginReceiptInput 登记一条回执所需的全部事实。
//
// 它存在的理由：登记点有"逐语言 switch_active"与"整次改址一条 update_url"两种形态，
// 早先的定参版本漏掉了 action/oldPath/redirect/capturedInputVersion —— 那些字段
// 不是可选装饰，capturedInputVersion 是恢复侧 G2 的唯一来源（漏传即恒 0，判据永久失效）。
type beginReceiptInput struct {
	inst                 *presentationmodel.InstanceEntity
	path                 string
	oldPath              string
	redirect             bool
	lang                 string
	fromArtifactID       string
	toArtifactID         string
	capturedInputVersion int64
	action               string
	// oldPaths 全部旧路径（update_url 的完整清单；OldPath 只放首条兼容 page 消费者）。
	oldPaths []string
	// successfulReceipts 语言 → 本批次该语言的 switch_active 回执 id（仅 update_url 用）。
	// 恢复侧靠它判定「本次激活真的成功过」，不看时间、不看 URL 相等。
	successfulReceipts map[string]string
	// fromReceiptID 登记这一刻、该语言账本上**已经成功**的那条回执 id（-1 = 调用方没给）。
	//
	// 为什么必须在这里捕获而不是恢复时回查：恢复时读到的"当前成功身份"是**后来**的事实，
	// 用它顶替登记时的身份，会让任何一次后续成功都被读成"就是我自己"，于是本该判
	// superseded 的旧 attempt 反而去覆盖新状态。这是审计里最典型的"补造身份"。
	//
	// 取值由调用方在**实体行锁内**捕获（见 commitPublish），三态与 dto 定义一致：
	// 正十进制 = 前次成功身份；"0" = 确认没有前次成功；"" = 未知（历史回执）。
	fromReceiptID string
}

// oldPathIntent 改 URL 时**每一条**旧语言路径的处置意图（在激活之前登记成
// update_url 回执，激活成功后执行，失败留 pending 重放）。
//
// 逐语言而不是一整个：站点语言清单决定每个语言的访问路径，处置也逐条落库 ——
// 一条失败不该把兄弟语言的意图一起判死。
type oldPathIntent struct {
	lang    string
	oldPath string
	// newPath 本次要激活的新路径：既是回执的 Path（旧语义：update_url 的 Path 是
	// **新路径**），也是恢复时判定"意图是否已过期"的依据。
	newPath string
	// fromArtifactID 切换前该语言的活跃产物（恢复归档新产物行时用它找冻结源文档）。
	fromArtifactID string
	// newArtifactID 本次激活后的产物 id（登记时可能还没有产物行：改 URL 的产物是
	// 切换时现编译的，publication 侧对 update_url 放行空值）。
	newArtifactID string
	redirect      bool
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
	// oldPaths 改 URL 时每一条旧语言路径的处置意图（激活之前登记成 update_url 回执，
	// 激活成功后执行；见 oldPathIntent 与 replayOldPathReceipt）。空 = 本次不改路径。
	oldPaths []oldPathIntent
	// inputPath 非空 = 本次发布**同时是一次用户输入变更**（改 URL）：这个路径要在
	// 冻结事务里持久落库并推进 input_version（见 persistInputIntentTx）。
	//
	// 与 logicalPath 分开而不是"logicalPath 非空即输入"的理由：依赖失效的自动重建、
	// 换模板、保存独立文档等路径同样会带 logicalPath（沿用当前值），但它们**不是**
	// 输入变化，推进版本会把正在跑的并发批次无故判过期。只有改 URL 是真的新输入。
	inputPath string
	// trigger 触发源（迁移 673 的 ck_publish_attempts_trigger 取值）。
	// 只服务发布历史：它决定「这次是人工点的、换模板、改 URL、回滚，还是依赖失效自动重建」——
	// 自动重建一次可能几十条，混在人工发布里会淹掉人真正关心的那几条。
	// 空串由 publishTriggerOf 归一成 manual（未标触发源的老调用点按人工处理）。
	trigger string
	// mode 本次要落的渲染模式 / 独立文档变更（nil = 本次不改）。
	mode *instanceModePending
}

// publishFreeze 一次发布会话在锁内冻结出来的不可变输入。
//
// 「不可变」是就本次编译而言：编译阶段只读它，不再回库读语言清单或模板
// （唯一例外是编译期实体字段 —— 那本来就是构建期数据源，见 renderHTML）。
type publishFreeze struct {
	references *pipeline.ReferenceSnapshot
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
	// inputVersion 冻结时实例的编译输入代次（迁移 699）。
	//
	// 它回答的是「我这份产物烘的是哪一代输入」，与 fingerprint 回答的
	//「实例行还是不是我锁内看到的那一行」是**两个问题**：
	//   - fingerprint 是快照式的**行比对**，任何一次成功提交都会换掉指针列，于是它只能
	//     判定「有人在我之后提交过」，判定不了「有人在我之后只是改了输入」（改输入不换指针）；
	//   - inputVersion 是单调代次，只由输入变化推进，因此「编译期间有人保存了草稿」
	//     这件事只有它看得见 —— 而那种情况下我的产物就是旧字节，必须整批失败重来。
	//
	// 第二阶段在行锁内复验它（WHERE input_version = $expect），0 行即本次批次作废。
	inputVersion int64
	// fingerprint 冻结时实例行的状态指纹，提交前逐列比对（见 L4）。
	fingerprint instanceStateFingerprint
	// inputPersisted 冻结段是否已把本次输入意图（改 URL 的新路径）持久化。
	//
	// 为真时 inputVersion 是**本次提交自己刚推进出来的**
	// （PersistPublishInputURLTx 的 RETURNING），提交段的版本谓词比较的是同一代，
	// 不自拒；后到的并发批次在行锁内看到更大的版本才判冲突。
	inputPersisted bool
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
		return nil, timing, presentationenums.Err(presentationenums.ErrNotFound)
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
			return nil, timing, presentationenums.Err(presentationenums.ErrNotFound)
		}
		return nil, timing, gerr
	}
	*inst = *fresh

	// 输入意图线性化（改 URL 专用，见 publishIntent.inputPath）：持实例锁、把新路径
	// **和** input_version+1 一次落库，再以返回的新版本作为本次批次的 expect。
	//
	// 放在这里（而不是提交事务里）的三个理由：
	//   1) 锁已经持有 —— 意图的线性化点就是这把锁，先落意图再编译，「用户说要改」
	//      这件事在编译开始前就不可否认（崩溃也只丢产物，不丢意图）；
	//   2) 编译慢（锁外几十秒）：若等到提交段才推进，期间另一个改 URL 会拿同一个版本
	//      冻结、并在我们之后提交，静默覆盖 —— 正是本方法要消除的交错；
	//   3) 提交段的版本谓词（WriteInstanceActivationTx 的 WHERE input_version = $expect）
	//      必须与产物烘的输入同代。返回的新版本就是那一代，因此既推进又不自拒。
	//
	// 失败语义：落库成功之后编译失败**不回滚** —— 控制面路径已是新值、旧产物仍在线、
	// 实例随后被标 stale 等待重建。这是"输入已变、产物待追"的正常形态（不要伪装成回滚）。
	inputPersisted := false
	//
	// 注意：**无条件**推进，不做「与当前逻辑路径相同就跳过」的短路。短路的两个后果都不对：
	//   1) 冻结段（这里）读到的旧路径与提交段宣称的输入意图可能不是同一份 —— 调用方在
	//      进锁之前就判过 samePath，但等锁期间路径可能已被别人改走，短路会让本次提交
	//      拿旧路径当"没变化"，随后版本谓词放行一次**用户没要求的路径变更**；
	//   2) 少一次 +1 就少一个判定代次，两个并发改 URL 可能都通过谓词（后者静默覆盖）。
	// 空操作（真的没有变化）由调用方在进锁前用 ErrSamePath 拒绝，不需要在这里猜。
	if in := strings.TrimSpace(intent.inputPath); in != "" {
		at := time.Now().UTC()
		var newVersion int64
		terr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
			v, perr := s.m.PersistPublishInputURLTx(tx, fresh.ProjectID, fresh.ID, in, at)
			if perr != nil {
				return perr
			}
			newVersion = v
			return nil
		})
		if terr != nil {
			return nil, timing, terr
		}
		fresh.InputVersion = newVersion
		fresh.URLPath = in
		*inst = *fresh
		inputPersisted = true
	}

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
	references, refErr := s.freezeReferences(ctx, inst.ProjectID, tpl.Document)
	if refErr != nil {
		return nil, timing, refErr
	}
	logicalPath := pipeline.LogicalPathOf(ctx, s.project, inst.ProjectID, intent.logicalPath)
	if logicalPath == "" {
		logicalPath = s.instanceLogicalPath(ctx, inst)
	}
	return &publishFreeze{
		references:  references,
		langs:       langs,
		rule:        pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID),
		logicalPath: logicalPath,
		defaultLang: pipeline.DefaultLocale(ctx, s.project, inst.ProjectID),
		tpl:         tpl,
		mode:        intent.mode,
		// 版本与指纹取自**同一行**（同一次锁内重读）：两处各读一次会让它们描述
		// 两个不同的时刻，边界上正好漏掉一次推进。
		inputVersion:   fresh.InputVersion,
		fingerprint:    fingerprintOfInstance(fresh),
		inputPersisted: inputPersisted,
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
	ctx = pipeline.WithReferenceSnapshot(ctx, frozen.references)
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
	frozen *publishFreeze, results []langBuildResult, oldPathPlan []oldPathIntent) (primaryArtifactID string, timing phaseTiming, err error) {
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
			return "", timing, presentationenums.Err(presentationenums.ErrNotFound)
		}
		return "", timing, gerr
	}
	if !frozen.fingerprint.equal(fingerprintOfInstance(current)) {
		return "", timing, errPublishStateMoved
	}
	// 输入代次复验（迁移 699）：指纹完全相等也可能「输入变了但指针没动」——
	// 保存草稿、译文扇出、依赖标记都只推进 input_version + 标 stale，不换指针列。
	// 少了这一句，编译期间别人保存的草稿会被本次旧产物覆盖掉（审计 F4/F13）。
	//
	// 这里是**锁内读到的权威值**（current 就是锁内重读出来的那一行），真正的
	// 不可拆区间在 publishOneLang → finalizeMultiLangBatch 的事务里还会用
	// WHERE input_version = $expect 再判一次（见 WriteInstanceActivationTx）：
	// 本段校验负责「编译已经过期就别做副作用」，事务里的谓词负责「落库那一刻仍未过期」。
	if current.InputVersion != frozen.inputVersion {
		// 输入意图已经在冻结段持久化（inputPersisted）：这次不等价于「我的编译没发生」，
		// 而是「我按某一代输入编译的产物不能上线，但意图已经是用户的事实」。
		// 返回一个**不可重试**的错误 —— 重试只会拿着同一份旧冻结再跑一遍，
		// 而意图不必（也不能）再落第二次。
		if frozen.inputPersisted {
			return "", timing, errPublishInputSuperseded
		}
		return "", timing, errInputVersionMoved
	}

	// 本次发布前各语言的活跃产物（回执里的 from）：恢复与审计据此比对「从哪个产物切到
	// 哪一个」。必须在登记账本产物之前读取 —— 之后读到的是本次新写的行。
	previousArtifacts := s.publishedArtifactsByLang(ctx, inst.ID)
	// 前次成功身份（本次 attempt 的"从哪一条回执接替"）：与 previousArtifacts 同一时刻读取，
	// 都必须在账本被本次写入覆盖之前。这一对事实一起回答"我取代了谁、我从哪个产物出发"。
	// currentReceiptIDs(lang) 空串 = 账本行还没有 receipt_id（700 之前的历史行）：
	// 与"没有前次成功"不同，登记时按未知上送，消费方退回 artifact 身份核验。
	currentReceiptIDs := s.publishedReceiptIDsByLang(ctx, inst.ID, results)

	// 收据登记放在**事务之外、FS 之前**：回执必须能在崩溃窗口里回答「这次切换发生过没有」，
	// 而它自己是 publication 模块的表。放进本事务会让「回执可见」与「符号链接已切」
	// 同时提交，崩溃在 rename 之后就变成两边都没记 —— 那正是 TX-009 要消除的窗口。
	// 所以顺序是：登记（已提交）→ 事务内 [行锁 → 复验 → FS 激活 → 账本/指针] → 结案。
	receipts := make(map[string]string, len(results))
	for i := range results {
		b := &results[i]
		// capturedInputVersion = 本次产物烘的那一代（冻结段读到的权威值；改 URL 时是
		// 冻结段刚推进出来的新代）。传 0 会让恢复侧的 G2 永久失效（见 capturedInputVersionOf）。
		id, berr := s.beginPublishReceipt(ctx, beginReceiptInput{
			inst: inst,
			path: b.accessPath, lang: b.lang, fromArtifactID: previousArtifacts[b.lang],
			toArtifactID: b.artifactID, capturedInputVersion: frozen.inputVersion,
			fromReceiptID: currentReceiptIDs[b.lang],
		})
		if berr != nil {
			// 一条都没切：把已登记的兄弟回执结案为「未生效」，不留 pending 噪声。
			for _, rid := range receipts {
				s.abortPublishReceipt(ctx, rid, "同批次前序回执登记失败")
			}
			return "", timing, fmt.Errorf("登记 %s 的发布回执失败（未切换访问面）: %w", b.accessPath, berr)
		}
		receipts[b.lang] = id
	}

	// 改 URL：**整次改址一条** update_url 回执，在激活之前登记。
	//
	// 为什么必须在激活之前：切换不可回滚，而"旧路径该 301 还是下线"只在这条回执里；
	// 若等新路径生效之后再登记，进程在两者之间被杀就永久丢掉这次意图 —— 旧路径
	// 既不是新路径、也没被处置，谁都不会再去发现它。
	//
	// 为什么一条而不是每条旧路径一条：这是 page 侧既有的 update_url 形态（一次改址 =
	// 一条回执，含新旧路径与 redirect），收敛也按整次处置。逐语言拆开会让"同一次改址"
	// 在库里呈现成 N 条互不隶属的意图，恢复时无法判断它们是否属于同一次提交。
	// 各语言的旧路径都记在同一份 receipt_data 里（oldPaths 数组），由重放逐条执行。
	//
	// 为什么与 switch_active 分开：switch_active 的恢复是"按链接证据补齐数据库"，
	// 结论只可能是补齐或 superseded；update_url 的恢复是"执行外部副作用"（301/下线），
	// 判据完全不同（见 replayOldPathReceipt），混进同一套判据会写错账本。
	//
	// 失败语义：登记失败即中止（返回错误，**不切访问面**）。没有回执就没有
	// "旧路径该怎么处置"这份事实，宁可整次不生效，也不要留下一个没人认领的旧路径。
	// 关联事实：这条 update_url 回执记录**本次改址由哪几条 switch_active 回执构成**
	// （successfulReceipts：语言 → 回执 id）。它是恢复侧判定「本次激活到底成功了没有」
	// 的唯一持久依据 —— 不看时间、不看 URL 相等，只认那几条回执的最终状态。
	//
	// 之所以必须先登记 switch_active 再登记它：关联里引用的 id 必须是已存在的行，
	// 写入方（publication.CreateReceipt）不校验外键，写一个不存在的 id 会让恢复永远判不成功。
	oldPathReceiptID := ""
	if len(oldPathPlan) > 0 {
		first := oldPathPlan[0]
		paths := make([]string, 0, len(oldPathPlan))
		for _, plan := range oldPathPlan {
			paths = append(paths, plan.oldPath)
		}
		// 字段语义沿用 page 侧既有约定，**不偷换**：
		//   Path = 新路径（激活后的线上路径），OldPath = 旧路径（多语言时取首条，
		//   完整清单进 receipt_data 的 oldPaths），Redirect = 旧路径是否 301。
		id, oerr := s.beginPublishReceipt(ctx, beginReceiptInput{
			inst: inst,
			path: first.newPath, oldPath: first.oldPath, redirect: first.redirect,
			oldPaths: paths, successfulReceipts: receipts,
			lang: first.lang, fromArtifactID: first.fromArtifactID, toArtifactID: first.newArtifactID,
			capturedInputVersion: frozen.inputVersion, action: pubcontract.ReceiptActionUpdateURL,
			// 旧路径处置意图的"从哪条成功回执接替"：取清单首条语言的前任身份。
			// 这条回执不推进任何语言的账本，它的身份事实只用于回答"这次改址取代了谁"，
			// 因此与同批 switch_active 共用同一份捕获（同一次提交里不许有两份身份来源）。
			fromReceiptID: currentReceiptIDs[first.lang],
		})
		if oerr != nil {
			for _, rid := range receipts {
				s.abortPublishReceipt(ctx, rid, "同批次旧路径处置回执登记失败")
			}
			return "", timing, fmt.Errorf("登记 %s 的旧路径处置失败（未切换访问面）: %w", first.oldPath, oerr)
		}
		oldPathReceiptID = id
	}

	now := time.Now().UTC()
	var primaryArtifactID2, snapID string
	// 第二阶段：**一个** 事务包住「锁实体行 → 复验版本 → FS 激活每种语言 → 账本与指针落库」。
	//
	// 为什么不可拆（规划草案 §2.3 的 P2→P3→P4）：锁与谓词证明「没有更新的输入插进来」，
	// 而激活是文件系统副作用、无法回滚。若先解锁再激活，锁期间被推进的版本就再也拦不住
	// 这次覆盖；若激活后再开事务写库，两个想覆盖彼此的批次可以各自激活一半语言。
	// 持锁期间只有一次 os.Rename/语言 + 一次提交。
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.m.LockInstanceForActivationTx(ctx, tx, inst.ProjectID, inst.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				return presentationenums.Err(presentationenums.ErrNotFound)
			}
			return lerr
		}
		// 锁内权威复验：版本必须还是冻结时那一个。
		if locked.InputVersion != frozen.inputVersion {
			return errInputVersionMoved
		}
		// 快照 + 产物行 + 依赖 + 批次记录的绑定/路径：都在**同一事务**里，
		// 因此它们要么与激活一起生效，要么一起没有（FS 已经切了的情况由回执恢复补齐）。
		primaryID, ssid, perr := s.persistMultiLangArtifactsTx(ctx, tx, inst, frozen.tpl, results,
			now, frozen.logicalPath, frozen.defaultLang, frozen.mode)
		if perr != nil {
			return perr
		}
		primaryArtifactID2, snapID = primaryID, ssid
		// 逐语言：先切访问面（不可回滚的副作用），再写路由与账本 —— 三者同事务，
		// 任一失败即整笔回滚，回执保持 pending 交由恢复按访问面证据补齐。
		for i := range results {
			b := &results[i]
			if aerr := s.activate(b.accessPath, b.built); aerr != nil {
				return aerr
			}
			if rerr := s.registerRouteTx(ctx, tx, inst, b.accessPath, b.artifactID); rerr != nil {
				return fmt.Errorf("登记 %s 的路由占用失败: %w", b.accessPath, rerr)
			}
			// 成功 attempt 的身份与激活**同一次提交**落库（receipt_id 列，迁移 700）：
			// 这条账本行从此能回答「是哪一次 attempt 让它成立的」。只比 artifact_id
			// 区分不出同版本重建复用同一产物行的两个不同 attempt。
			if merr := s.m.MarkPublishedLangTx(tx, presentationmodel.PublicationRecord{
				PresentationID: inst.ID, Lang: b.lang, ActivePath: b.accessPath,
				ArtifactID: b.artifactID, ArtifactHash: b.built.Hash,
				ReceiptID: receipts[b.lang], PublishedAt: now,
			}); merr != nil {
				return fmt.Errorf("写入 %s 的发布账本失败: %w", b.accessPath, merr)
			}
		}
		// 收尾：指针只在全部语言都结案后前进（失败分支不会走到这里）。
		return s.finalizeMultiLangBatchTx(ctx, tx, inst, snapID, primaryArtifactID2, now, frozen.inputVersion)
	})
	if txErr != nil {
		// 输入意图已在冻结段持久化（改 URL）时，即使这里是"零副作用"的版本判死，
		// 也不能把旧路径处置回执结案为"没发生"：意图是用户的、已落库的，
		// 留在 pending 让收敛按证据处置（switch_active 会被判 superseded/rolled_back，
		// update_url 会被重放）—— 否则旧路径永远没人管。
		s.abortBatchAfterFailure(ctx, inst, receipts, txErr, frozen.inputPersisted)
		// 旧路径处置回执**一律保留 pending**，不在这里结案：它是否该执行由收敛例程
		// 按关联的成功回执最终状态判定（见 replayOldPathReceipt）。此处作废会在
		// 「FS 已切、事务回滚」的窗口里永久丢掉这次处置意图。
		if oldPathReceiptID != "" || frozen.inputPersisted {
			s.NotifyPendingReceipt()
		}
		return "", timing, txErr
	}
	// 事务已提交：账本、指针、路由三者与访问面一致，回执可以结案。
	for _, rid := range receipts {
		if cerr := s.completePublishReceipt(ctx, rid); cerr != nil {
			logger.Scene("build").With("instanceId", inst.ID).
				Warn("多语言发布回执结案失败（状态已一致，留待收敛幂等补齐）: " + cerr.Error())
			s.NotifyPendingReceipt()
		}
	}
	// 新路径已生效（账本/指针/路由与访问面一致）。现在执行**已在激活前登记**的旧路径
	// 处置：回执是事实来源，执行失败只把它留在 pending —— 收敛例程会重放同一处置
	// （redirect 是覆盖同路径的幂等写、deactivate 本身就幂等）。
	//
	// 返回事实：处置未完成不是"整次修改失败"（新路径确实生效了），但也不能静默 ——
	// 见 OldPathPending。
	oldPathPending := 0
	if oldPathReceiptID != "" {
		// 走与收敛**完全相同**的实现（runOldPathDisposal）：判定、目标路径复核与全部旧路径
		// 副作用都在同一次实体行锁内完成。快通道不能"自己再判一遍、然后在锁外执行" ——
		// 那正是 check → 解锁 → 副作用的竞态窗口。
		pending, derr := s.runOldPathDisposal(ctx, pubcontract.PendingReceiptResp{
			ID: oldPathReceiptID, SourceType: pubcontract.ReceiptSourcePresentation,
			SourceID: inst.ID, ProjectID: inst.ProjectID,
			Path: inst.URLPath, OldPath: firstOldPath(oldPathPlan), OldPaths: oldPathList(oldPathPlan),
			Redirect:     len(oldPathPlan) > 0 && oldPathPlan[0].redirect,
			ToArtifactID: primaryArtifactID2, Action: pubcontract.ReceiptActionUpdateURL,
		}, primaryArtifactID2, receipts)
		if !pending {
			oldPathPending++
			s.lastOldPathPending.Store(time.Now().Unix())
			s.NotifyPendingReceipt()
			if derr != nil {
				logger.Scene("build").With("instanceId", inst.ID).With("receiptId", oldPathReceiptID).
					Warn("旧路径处置未完成（新路径已生效，回执保留待收敛重放）: " + derr.Error())
			}
		}
	}
	pinged := make([]string, 0, len(results))
	for i := range results {
		pinged = append(pinged, results[i].accessPath)
	}
	s.notifyIndexNow(ctx, inst, pinged...)
	return primaryArtifactID2, timing, nil
}

// abortBatchAfterFailure 提交事务失败后的收口：能判定「没切过访问面」的结案为未生效，
// 其余（可能已切）保留 pending 并推快通道，交由恢复按访问面证据补齐。
//
// 判据只有一条：本次事务是**整笔回滚**的，但 FS 激活不在事务里 —— 它可能已经生效。
// 因此除了「版本/状态在写入之前就被判死」这两种，其余一律留 pending：
// 宁可在收敛时多跑一次幂等重放，也不能把「已经切了」的现场声明成「没发生」。
//
// inputPersisted=true（改 URL 已在冻结段持久化意图）时，连这两种作废也保留 pending：
// 意图是用户的事实，判死它会让旧路径永久无人处置。
func (s *Service) abortBatchAfterFailure(ctx context.Context, inst *presentationmodel.InstanceEntity,
	receipts map[string]string, cause error, inputPersisted bool) {
	if inputPersisted {
		return
	}
	if errors.Is(cause, errInputVersionMoved) || errors.Is(cause, errPublishStateMoved) {
		// 判定发生在任何写入与任何 FS 副作用之前：确实没切过。
		for _, rid := range receipts {
			s.abortPublishReceipt(ctx, rid, "版本或状态复验未通过，未切换访问面")
		}
		return
	}
	s.markBatchUnconverged(ctx, inst, cause)
	s.NotifyPendingReceipt()
}

// registerRouteTx 事务内登记线上路径占用（active，同归属者幂等）。
//
// 与 registerRoute 的差别只有事务边界：路由行归 publication 模块，但「切了访问面」与
// 「路由账本承认这个路径属于我」必须同进同出（AGENTS.md 跨模块写同事务），
// 所以这里走 publication 的 ActivateTx 而不是 Activate（后者自己提交）。
func (s *Service) registerRouteTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, urlPath, artifactID string) error {
	if s.routes == nil {
		return nil
	}
	_, err := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
		ProjectID:      inst.ProjectID,
		Path:           urlPath,
		PresentationID: inst.ID,
		ArtifactID:     artifactID,
	})
	return err
}

// finalizeMultiLangBatchTx 整批成功后的收尾（事务形态）：推进指针、清 stale、记发布时间。
//
// 与 finalizeMultiLangBatch 的差别有两点，都是这次才必须的：
//   - 走 WriteInstanceActivationTx，WHERE input_version = $expect —— 落库那一刻再确认
//     一次「输入没被推进」，把「编译用的是旧输入、却当成新结果落下」变成显式错误；
//   - 与语言账本、路由登记同事务：指针前进与「哪些语言已上线」不能各自提交。
func (s *Service) finalizeMultiLangBatchTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, snapID, primaryArtifactID string,
	now time.Time, expectInputVersion int64) error {
	if strings.TrimSpace(primaryArtifactID) == "" || strings.TrimSpace(snapID) == "" {
		return errors.New("多语言发布缺少默认语言产物或快照，无法推进实例指针")
	}
	inst.CurrentSnapshotID = &snapID
	inst.StagedSnapshotID = &snapID
	inst.StagedArtifactID = &primaryArtifactID
	inst.ActiveArtifactID = &primaryArtifactID
	inst.Stale = false
	inst.PublishedAt = &now
	inst.UpdatedAt = now
	if err := s.m.WriteInstanceActivationTx(tx, inst.ProjectID, inst, expectInputVersion); err != nil {
		if errors.Is(err, presentationmodel.ErrInputVersionMoved) {
			return errInputVersionMoved
		}
		return err
	}
	return nil
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
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	// 工程作用域（DB-009 第二批）：反查实例在本工程内进行。
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 反查实例（entity_id 匹配）。
	inst, err := s.findByEntityID(ctx, projectID, req.EntityID)
	if err != nil {
		return nil, presentationenums.Err(presentationenums.ErrNotFound)
	}
	// req.TemplateID 非空 = 切换绑定并重新发布（验收 4：切换模板重新发布后产物随之变化）。
	// 换底稿 = 放弃自定义（docs/04-C）：切换路径不应用实例文档，产物按新模板编译，
	// 模式与文档列在同一次发布的事务里一并清除（presentation_render.go 的
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
		// 带 templateId = 换模板并重发；不带 = 按当前绑定重发（保存 / 重新发布的入口）。
		trigger: rebuildTrigger(switching),
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
	// switch_active：本次发布把访问面切到了新产物（beginPublishReceipt 不传 action 时
	// 由 publication 侧默认成它）。
	presentationReceiptAction,
	// deactivate：实体删除/退役时**在动访问面之前**登记的下线意图。
	// 它没有目标产物，恢复方向是"撤链"而不是"补链"，且必须先看持久事实
	// businessCommitted：业务 DB 变更没提交时链接必须保持原样（见 recoverDeactivateReceipt）。
	pubcontract.ReceiptActionDeactivate,
	// update_url：改 URL 时**在激活之前**登记的旧路径处置意图（301 或下线）。
	// 它与 switch_active 分开领取、分开重放：前者按链接证据补齐数据库，后者执行一处
	// 外部副作用。混进同一套判据会写错账本（见 replayOldPathReceipt）。
	pubcontract.ReceiptActionUpdateURL,
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
			var done bool
			var rerr error
			switch item.Action {
			case pubcontract.ReceiptActionUpdateURL:
				done, rerr = s.replayOldPathReceipt(ctx, item)
			case pubcontract.ReceiptActionDeactivate:
				done, rerr = s.recoverDeactivateReceipt(ctx, item)
			default:
				done, rerr = s.recoverOneReceipt(ctx, item)
			}
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
//
// 字段语义严格沿用 publication 既有约定（page 侧消费者同一套，不新造动作、不颠倒字段）：
//   - Path     = **新路径**（本次激活的线上路径）；
//   - OldPath  = **旧路径**（要被 301 或下线的那个）；
//   - Redirect = 旧路径是否登记 301；
//   - Action 为空时由 publication 侧归一成 switch_active。
func (s *Service) beginPublishReceipt(ctx context.Context, in beginReceiptInput) (receiptID string, err error) {
	if s.routes == nil {
		return "", nil
	}
	inst := in.inst
	id, berr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID:      inst.ProjectID,
		Path:           in.path,
		PresentationID: inst.ID,
		FromArtifactID: strings.TrimSpace(in.fromArtifactID),
		ToArtifactID:   in.toArtifactID,
		Lang:           in.lang,
		Action:         in.action,
		OldPath:        strings.TrimSpace(in.oldPath),
		OldPaths:       normalizePathList(in.oldPaths),
		Redirect:       in.redirect,
		// 编译期捕获的输入代次（G2 的唯一来源）：绝不在这里回填"当前值"。
		CapturedInputVersion: in.capturedInputVersion,
		// 本次改址由哪几条 switch_active 回执构成（仅 update_url 用）：恢复侧据此
		// 判定"本次激活成功了没有"。空 = 没有关联事实（历史回执），恢复按保守口径处理。
		SuccessfulReceipts: in.successfulReceipts,
		// 前次成功身份：原样上送，**不在这里**用当前账本回填（见 beginReceiptInput.fromReceiptID）。
		FromReceiptID: in.fromReceiptID,
	})
	if berr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", in.path).
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

// recoverOneReceipt 判定单条未结案回执。返回 true 表示已收敛（补齐或按裁决结案），
// false 表示保留 pending 待下轮重放。
//
// 判据链（规划草案 §2.5 的 G1–G4，四条**按序**执行，任一不成立都不许往下写）：
//
//	G1 同实体行锁：与激活、与其它入口的标记串行，避免「读判定 → 写补齐」之间被人插队。
//	G2 输入版本：DB 里的 input_version 与回执捕获的版本相等才谈得上「这是同一次发布」。
//	   回执里没有捕获版本（旧回执 / 未升级的登记方）时按「未知」处理 —— 不据此判定 superseded。
//	G3 链接身份：访问面上该语言的符号链接必须**指向本回执的产物**。指向别处说明有人已
//	   切过；指向本产物才证明「切换成功了、只是 DB 没跟上」。
//	G4 DB 归属：语言账本里该语言的 artifact 已不是本回执的产物 → 本次 attempt 已被别的
//	   成功 attempt 取代。
//
// 由此得到三种处置，刻意**不是**「补齐 / 回滚」二选一：
//   - G3 成立 + G4 未被取代 → 向前补齐（路由 + 账本 + 指针）后结案：证据充分，切换确实生效。
//   - G3 成立但 G4 已被取代，或 G2 版本不等 → **本 attempt 被取代**，但访问面仍指着
//     own 产物：不能只标 rolled_back 了事（线上字节仍是本批次的字节，声明「没发生」是错的）。
//     处置是「持久可重放」：写账本到与链接一致 + 标 stale + 入队重建，收敛由下一轮发布完成。
//   - G3 不成立（链接指向别处 / 不存在）→ 切换确实没发生，标回滚（唯一允许终态的否定分支）。
//
// 返回 false（保留 pending）用于「证据暂时读不到」与「补齐写失败但可重试」两种情况：
// 启动恢复与定时收敛都会再来，宁可多跑一轮也不能把不确定判成终态。
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
	// G3 链接身份（FS 读，不需要锁）：访问面这一语言的链接当前指向哪个产物。
	actualHash := s.activeArtifactHash(item.Path)
	if actualHash == "" || actualHash != expectedHash {
		// 链接不指向本次产物：切换没发生（或已被他人覆盖）。这是**唯一**允许直接判否的分支。
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			With("actual", actualHash).With("expected", expectedHash).
			Warn("未结案的多语言发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}
	// 走到这里：切换**确实生效了**。剩下的判定必须在行锁内做（G1），因为要基于
	// 「DB 归属」决定是补齐还是判 superseded，而这两条读-判-写之间有并发入口。
	var converged bool
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.m.LockInstanceForActivationTx(ctx, tx, inst.ProjectID, inst.ID)
		if lerr != nil {
			return lerr
		}
		*inst = *locked
		// G2 版本 + G4 归属。
		captured := capturedInputVersionOf(item)
		versionMoved := captured > 0 && captured != locked.InputVersion
		superseded := s.langLedgerSupersededTx(ctx, tx, inst.ID, item)
		if versionMoved || superseded {
			// 被取代但链接仍是 own 产物：线上字节属于本批次，不能声明「没发生」。
			// 走「持久可重放」：账本对齐到链接事实、标 stale、交给重建收敛。
			return s.convergeSupersededAttemptTx(ctx, tx, inst, item, expectedHash, versionMoved, superseded)
		}
		// G3+G4 全成立：向前补齐（幂等写：路由 active 行、语言账本、指针）。
		if rerr := s.registerRouteTx(ctx, tx, inst, item.Path, item.ToArtifactID); rerr != nil {
			return rerr
		}
		// 补齐时把回执 id 一并写进账本：否则这行永远没有"成功身份"，下一轮收敛
		// 又只能退回 artifact_id 比较（G4' 第一档失效）。
		if merr := s.m.MarkPublishedLangTx(tx, presentationmodel.PublicationRecord{
			PresentationID: inst.ID, Lang: strings.TrimSpace(item.Lang), ActivePath: item.Path,
			ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash,
			ReceiptID: strings.TrimSpace(item.ID), PublishedAt: time.Now().UTC(),
		}); merr != nil {
			return merr
		}
		converged = true
		return nil
	})
	if txErr != nil {
		// 收敛失败不回退成「已回滚」：访问面已经切了，把回执判死会让下一轮彻底不再处理它。
		return false, txErr
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	if converged {
		logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
			Info("多语言发布在失败/崩溃前已切换访问面，已补齐路由登记与语言账本")
	}
	return true, nil
}

// replayOldPathReceipt 重放一条 update_url 旧路径处置回执（收敛例程的分派目标）。
//
// 语义与 switch_active **完全不同**，所以不共用判据：
//   - switch_active 的真相在文件系统里（链接指向谁），恢复是「按证据补齐数据库」；
//   - update_url 的真相是**这次意图**：旧路径该 301 到新路径、还是该下线。链接现在指向
//     什么并不改变这个意图，重放就是把它执行到底（幂等：PutRedirect 覆盖同路径，
//     Deactivate 本身幂等，路由行按 (path, owner) upsert）。
//
// 判据只有三条，且都不看库里的账本：
//  1. 回执必须带新路径（Path）与旧路径（OldPath，receipt_data.oldPath）、且两者不同；
//     缺了就没有可执行的事实 → 结案为 rolled_back（不是失败，重放也补不出来）。
//  2. 实例必须存在且仍是这个工程内的（越权/已删 → 结案）。
//  3. 实例的**当前**逻辑路径必须还是回执里的新路径：说明此后没人再改 URL。
//     若已不是（改了两次），这条旧路径的 301 目标已过期 —— 重放到一个错误的
//     目标比不重放更糟，结案即可（第二次改 URL 会自己登记它自己的旧路径处置）。
//
// 执行成功 → committed；执行失败 → 保留 pending 下轮重试（不吞错、不静默）。
func (s *Service) replayOldPathReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	return s.runOldPathDisposal(ctx, item, "", nil)
}

// oldPathOutcome 一次旧路径处置的终局判定（不执行任何副作用）。
type oldPathOutcome int

const (
	// oldPathRetry 证据不足或稍后会到：保留 pending，下一轮再判（**不是**终态）。
	oldPathRetry oldPathOutcome = iota
	// oldPathAbandon 明确终态：关联回执已 rolled_back / superseded / 缺失，或目标已过期。
	oldPathAbandon
	// oldPathExecute 三个条件都成立，可以执行处置。
	oldPathExecute
)

// runOldPathDisposal 是旧路径处置的**唯一实现**：判定 + 执行 + 结案。
//
// 关键约束：判定、路径复核与**全部**旧路径副作用都在**同一次实体行锁**持有期间完成。
// 早期版本把判定放在一个自开的只读事务里、解锁后再 disposeOldPath —— 那正是
// 「check → 解锁 → 副作用」的经典竞态：两次判定之间另一次改 URL 可以提交，旧路径
// 会被 301 到一个已经不是本次意图的目标。锁必须覆盖副作用，所以这里：
//   - 由本函数开事务、先 LockInstanceForActivationTx；
//   - 锁内判定成功身份 + 目标路径；
//   - 锁内执行副作用（路由写走 publication 的 *Tx 端口，与本次事务同一个连接，
//     不会另开连接与自己的行锁互等）；
//   - 锁内结案回执。
//
// 参数 fastPathReceipts 为空 = 收敛路径（从回执持久化的 successfulReceipts 读关联）；
// 非空 = 写路径快通道（刚登记的那批 switch_active 回执 id，与持久化内容同源）。
func (s *Service) runOldPathDisposal(ctx context.Context, item pubcontract.PendingReceiptResp,
	artifactID string, fastPathReceipts map[string]string) (bool, error) {
	if s == nil || s.publication == nil {
		return false, nil
	}
	// 字段语义与 page 侧一致：Path 是**新路径**，OldPath 是**旧路径**（不颠倒）。
	newPath := strings.TrimSpace(item.Path)
	oldPaths := receiptOldPaths(item)
	if len(oldPaths) == 0 || newPath == "" {
		s.abortPublishReceipt(ctx, item.ID, "旧路径处置回执缺少旧/新路径事实")
		return false, nil
	}
	inst, ierr := s.locateInstanceForReceipt(ctx, item.ProjectID, item.SourceID)
	if ierr != nil {
		s.abortPublishReceipt(ctx, item.ID, "实例不存在")
		return false, nil
	}

	links := fastPathReceipts
	if len(links) == 0 {
		links = item.SuccessfulReceipts
	}
	if len(links) == 0 {
		logger.Scene("publication").With("instanceId", inst.ID).With("receiptId", item.ID).
			Warn("旧路径处置回执没有关联的成功回执，无法证明激活成功（保留 pending 不处置）")
		return false, errOldPathWithoutSuccessEvidence
	}

	var (
		outcome  = oldPathRetry
		earlyErr error
	)
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.m.LockInstanceForActivationTx(ctx, tx, inst.ProjectID, inst.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				// 实例在收敛期间被删：意图已无意义，结案。
				outcome = oldPathAbandon
				return nil
			}
			return lerr
		}
		// 判定走 publication 的**读端口**（contract.ReceiptAttemptVerdictTx）：本函数持
		// 实体行锁，在同一个事务里回读那张归 publication 所有的回执表 —— 但绝不在
		// presentation 的 model 里写跨表 SQL（表只被一个模块读写，AGENTS.md）。
		// ownedLangs 是本模块账本的事实（语言 → success 回执 id），由账本主人给出。
		owned := make(map[string]string, len(links))
		for lang := range links {
			got, gerr := s.m.PublishedReceiptIDTx(tx, locked.ID, lang)
			if gerr != nil {
				return gerr
			}
			owned[lang] = got
		}
		verdict, verr := s.routes.ReceiptAttemptVerdictTx(ctx, tx, links, owned)
		if verr != nil {
			return verr
		}
		switch verdict {
		case pubcontract.ReceiptAttemptSucceeded:
			// 通过，继续目标路径复核。
		case pubcontract.ReceiptAttemptFailed:
			// 明确失败（rolled_back / superseded / 账本归属不符）：结案不执行。
			outcome = oldPathAbandon
			return nil
		default:
			// InProgress / NoEvidence：证据还没到（关联回执仍 pending 或暂缺），
			// **保留 pending 下一轮再判** —— 终态化会永久丢掉一条可能有效的意图。
			outcome = oldPathRetry
			return nil
		}
		// 身份判定（第三道，锁内）：关联回执"成功"只证明本次激活发生过，不证明**是我**
		// 后来居上的那一次。同一条产物行、同一个 URL 可以被连续两次 attempt 激活 ——
		// 产物 id 与路径都相同，只有成功身份能区分。这条回执登记时捕获了 FromReceiptID，
		// 于是三种结论互斥且可判：
		//   - 账本成功身份 == 我自己的 id → 我已落过账（幂等补处置）；
		//   - 账本还是我登记时的前任（或两边都"未知"）→ 我没被取代，可以前向执行；
		//   - 账本是一个**新的**身份 → 我已被取代，旧路径归那次 attempt 处置。
		idVerdict, yerr := s.receiptIdentityVerdictTx(ctx, tx, locked.ID, item, links)
		if yerr != nil {
			return yerr
		}
		switch idVerdict {
		case receiptIdentityOwned, receiptIdentityCurrent:
			// 继续。
		case receiptIdentitySuperseded:
			logger.Scene("publication").With("instanceId", locked.ID).With("receiptId", item.ID).
				Warn("旧路径处置意图已被后来的成功 attempt 取代，结案不处置")
			outcome = oldPathAbandon
			return nil
		default:
			// 身份事实冲突或非法（例如捕获到乱串）：受控错误，保留 pending 等人工/下一轮。
			logger.Scene("publication").With("instanceId", locked.ID).With("receiptId", item.ID).
				Warn("旧路径处置意图的身份事实无法判定（保留 pending）")
			outcome = oldPathRetry
			return nil
		}
		// 目标路径复核（锁内）：此后没人再改 URL 才继续。
		wantLogical := pipeline.LogicalPathOf(ctx, s.project, locked.ProjectID, newPath)
		current := pipeline.LogicalPathOf(ctx, s.project, locked.ProjectID, locked.URLPath)
		if current != wantLogical && current != newPath {
			logger.Scene("publication").With("instanceId", locked.ID).With("wanted", newPath).
				With("current", current).
				Warn("旧路径处置回执的目标路径已过期，结案不再重放")
			outcome = oldPathAbandon
			return nil
		}
		// 全部旧路径在锁内逐条执行（幂等：PutRedirect 覆盖同路径，Deactivate 本身幂等，
		// 路由行按 (path, owner) upsert）。
		if derr := s.disposeOldPathsTx(ctx, tx, locked, oldPaths, newPath,
			firstNonEmpty(artifactID, strings.TrimSpace(item.ToArtifactID)), item.Redirect); derr != nil {
			earlyErr = derr
			outcome = oldPathRetry
			return nil
		}
		outcome = oldPathExecute
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	switch outcome {
	case oldPathAbandon:
		s.abortPublishReceipt(ctx, item.ID, "关联的激活回执未成功或目标已过期，旧路径处置不再执行")
		return false, nil
	case oldPathExecute:
		if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
			return false, cerr
		}
		logger.Scene("publication").With("instanceId", inst.ID).With("oldPaths", oldPaths).
			With("newPath", newPath).With("redirect", item.Redirect).
			Info("旧路径处置已在实体锁内执行并结案")
		return true, nil
	}
	if earlyErr != nil {
		logger.Scene("publication").With("instanceId", inst.ID).With("oldPaths", oldPaths).
			With("receiptId", item.ID).Error(earlyErr, "旧路径处置失败（保留 pending，下一轮重试）")
		return false, earlyErr
	}
	return false, errOldPathWithoutSuccessEvidence
}

// receiptIdentityVerdict 一条 attempt 的"我是否被后来的成功取代"三态判定。
type receiptIdentityVerdict int

const (
	// receiptIdentityUnknown 事实不可判（捕获身份非法 / 账本与关联互相矛盾）：保留 pending。
	receiptIdentityUnknown receiptIdentityVerdict = iota
	// receiptIdentityOwned 账本上的成功身份**就是这条回执自己**：已落过账，幂等补处置。
	receiptIdentityOwned
	// receiptIdentityCurrent 账本上的成功身份仍是我登记时的前任（或两边都未知）：
	// 我没被取代，允许前向执行。
	receiptIdentityCurrent
	// receiptIdentitySuperseded 账本上是一个**新的**成功身份：旧路径归那次 attempt。
	receiptIdentitySuperseded
)

// receiptIdentityVerdictTx 在实体行锁内判定"这次 attempt 是否被后来的成功取代"。
//
// 为什么不能只比产物 id / URL：同一条产物行、同一个路径可以被连续两次 attempt 激活
// （同输入重建 → 命中同一 artifact hash 复用产物行），两者完全同形。只有"成功身份"
// （publication_receipts.id，迁移 700 落在 presentation_publications.receipt_id）能区分。
//
// 判定口径（与 dto.BeginPublishReceiptReq.FromReceiptID 的注释一一对应）：
//  1. 账本成功身份 == 本回执 id → owned（幂等）。
//  2. 账本成功身份 == 捕获的前任身份 → current（我没被取代）。
//  3. 捕获为 "0"（确认没有前科）且账本仍无身份 → current。
//  4. 捕获为空（未知，历史回执）：账本也无身份 → current；账本有身份 → 只知道"有人在
//     我之后成功过"，但不知道是不是我自己（历史行不带身份）→ 退回 artifact 比较，
//     相等视为 current（仅历史兼容，**不声称与身份判定等价**），不等则 superseded。
//  5. 其它 → superseded。
//
// 非法捕获值（非十进制、负数、超范围）不做纠正：直接判 unknown，保留 pending 交给
// 受控处理 —— 把一个乱串归成 "0" 或 "" 都是伪造身份事实。
func (s *Service) receiptIdentityVerdictTx(ctx context.Context, tx *gorm.DB, instanceID string,
	item pubcontract.PendingReceiptResp, links map[string]string) (receiptIdentityVerdict, error) {
	langs := make([]string, 0, len(links))
	for lang := range links {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	if len(langs) == 0 {
		return receiptIdentityUnknown, nil
	}
	own := strings.TrimSpace(item.ID)
	fromRaw := item.FromReceiptID
	var from int64 = -1
	if v := strings.TrimSpace(fromRaw); v != "" {
		parsed, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil || parsed < 0 {
			// 非法身份：不猜、不归一，判 unknown 保留 pending。
			return receiptIdentityUnknown, nil
		}
		from = parsed
	}
	sawOwn, sawFrom, sawOther := false, false, false
	for _, lang := range langs {
		ledger, lerr := s.m.PublishedReceiptIDTx(tx, instanceID, lang)
		if lerr != nil {
			return receiptIdentityUnknown, lerr
		}
		ledger = strings.TrimSpace(ledger)
		switch {
		case ledger == "":
			// 账本没有身份：不是 superseded。继续看其它语言（多语言时可能只写了一半）。
			continue
		case own != "" && ledger == own:
			sawOwn = true
		case from >= 0 && ledger == strconv.FormatInt(from, 10):
			sawFrom = true
		case from == -1:
			// 捕获未知（历史回执缺 from_receipt_id）：只能退回 artifact 身份比较。
			// 这是**历史兼容档**，不与身份判定等价：同产物被重复激活时它答不出
			// "谁取代谁"，所以只在缺失捕获事实时才用。
			ledgerArtifact, aerr := s.m.PublishedArtifactIDTx(tx, instanceID, lang)
			if aerr != nil {
				return receiptIdentityUnknown, aerr
			}
			if strings.TrimSpace(ledgerArtifact) != "" &&
				strings.TrimSpace(ledgerArtifact) != strings.TrimSpace(item.ToArtifactID) {
				sawOther = true
			} else {
				sawFrom = true
			}
		default:
			sawOther = true
		}
	}
	switch {
	case sawOther:
		// 任一条语言出现了新身份：整次改址的旧路径处置都归那次 attempt。
		return receiptIdentitySuperseded, nil
	case sawOwn:
		return receiptIdentityOwned, nil
	default:
		_ = sawFrom
		return receiptIdentityCurrent, nil
	}
}

// receiptOldPaths 回执里要处置的全部旧路径：receipt_data.oldPaths 优先，退回单值 OldPath。
//
// 多语言改址会一次处置多条旧路径；早期版本只读 item.OldPath（首条），恢复时另外
// 几条语言的旧链接永远不会被处置/下线 —— 与持久化的意图不符。
func receiptOldPaths(item pubcontract.PendingReceiptResp) []string {
	seen := make(map[string]bool, len(item.OldPaths)+1)
	out := make([]string, 0, len(item.OldPaths)+1)
	for _, p := range item.OldPaths {
		v := strings.TrimSpace(p)
		if v == "" || seen[v] || strings.EqualFold(v, strings.TrimSpace(item.Path)) {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if v := strings.TrimSpace(item.OldPath); v != "" && !seen[v] &&
		!strings.EqualFold(v, strings.TrimSpace(item.Path)) {
		out = append(out, v)
	}
	return out
}

// normalizePathList 归一旧路径清单（去空白、去重）；全空返回 nil（不出该键）。
//
// 不去重会重复执行同一处置（虽然幂等，但每次多一次 FS 写与路由 upsert）；
// 不丢空值会让重放把空路径当真实目标。与 publication 侧同名函数各自 5 行，
// 放在各自模块内是因为它们服务两个不同的写入口（跨模块共享会把这两个模块绑在一起）。
func normalizePathList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstOldPath / oldPathList 把改址计划摊平成回执视图所需的两条事实。
//
// 快通道与本函数共用同一份视图，因此"快通道执行几条旧路径"与"恢复执行几条"必然一致。
func firstOldPath(plan []oldPathIntent) string {
	if len(plan) == 0 {
		return ""
	}
	return plan[0].oldPath
}

func oldPathList(plan []oldPathIntent) []string {
	out := make([]string, 0, len(plan))
	for _, p := range plan {
		out = append(out, p.oldPath)
	}
	return out
}

// firstNonEmpty 取第一个非空值（快通道给产物 id，收敛路径退回回执里的 to）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// disposeOldPathsTx 在**调用方事务**内逐条处置旧路径。
//
// FS 与路由写都在这里（service 编排，不塞进 model）：model 只管表，FS 符号链接与
// 产物落盘属于发布内核，路由行通过 publication 的 *Tx 端口写 —— 与本事务同一个连接，
// 因此不会另开连接去等自己已持有的实例行锁。
// 路径锁：整批旧路径在**一次** withPaths 里处置（不是逐条各取一次锁）。逐条取锁会让
// "第 1 条已下线、第 2 条被别人抢走"变成半截状态，而多条旧路径本来就属于同一次改址意图。
// 已持有 DB 实体行锁；路径锁在其内层，store 与路由写内部都不取本锁，故不重入。
//
// **归属复验必须在 withPaths 之内**（主审退回点）：withPaths 之外读到的归属只是
// "我来排队之前"的快照 —— 跨实体不共享实体锁，另一个实体完全可以在"我读完 → 我拿到
// 路径锁"之间接管这条路径。因此这里的顺序被固定成：
//
//	withPaths(旧路径集合)
//	  → 逐条重读路由归属（publication 的 Tx 读端口）
//	  → 逐条重读访问面链接归属（store.Inspect）
//	  → 判定这条路径此刻仍归本实例，才动它
//
// 复验结论与副作用处在**同一个锁期**，判定之后没有任何第三方能插进来。
func (s *Service) disposeOldPathsTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, oldPaths []string, newPath, artifactID string, withRedirect bool) error {
	return s.withPaths(oldPaths, func() error {
		for _, oldPath := range oldPaths {
			if oerr := s.reverifyOldPathOwnershipTx(ctx, tx, inst, oldPath); oerr != nil {
				return oerr
			}
			if derr := s.disposeOldPathTx(ctx, tx, inst, oldPath, newPath, artifactID, withRedirect); derr != nil {
				return derr
			}
		}
		return nil
	})
}

// reverifyOldPathOwnershipTx 在**路径锁内**复验一条旧路径此刻仍归本实例。
//
// 两道事实缺一不可：
//   - 路由行归属（公共真相，跨模块只能读）：route 不存在 → 没有占用方，处置无害；
//     存在但 owner 不是本实例/本工程 → 已被接管，必须报可重试错误（**不删**）；
//   - 访问面链接归属（访问面不查库，只能看链接本身）：Kind 为 none 或仍指向本实例
//     产物链上的目标 → 允许；其它 → 已被接管。
//
// 报 errOldPathFSTargetForeign（可重试）：本轮 DB 未改动、FS 未改动，
// 下一轮重放时若对方让出该路径就能正常收敛。返回任何错误都会让外层事务回滚。
func (s *Service) reverifyOldPathOwnershipTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, oldPath string) error {
	if s.routes != nil {
		owner, oerr := s.routes.RouteOwnerTx(ctx, tx, inst.ProjectID, oldPath)
		if oerr != nil && !errors.Is(oerr, gorm.ErrRecordNotFound) {
			return oerr
		}
		if oerr == nil && owner != nil {
			if owner.ProjectID != inst.ProjectID {
				return fmt.Errorf("%w: 旧路径 %s 已属于其它工程", errOldPathFSTargetForeign, oldPath)
			}
			if owner.PresentationID == nil || *owner.PresentationID != inst.ID {
				return fmt.Errorf("%w: 旧路径 %s 的路由占用已转给其它实体", errOldPathFSTargetForeign, oldPath)
			}
		}
	}
	return nil
}

// disposeOldPathTx 单条旧路径处置的事务内形态（redirect：301 产物 + 激活 + 路由 redirect；
// 否则：下线访问面 + 释放路由占用）。
//
// **顺序为什么是"先 DB 后 FS"**（与早先的实现相反）：
// 路由写是条件写（owner 精确匹配 / redirect 行 upsert 撞唯一键），它在本事务持有行锁期间
// 决定"这条路径此刻归不归我处置"。若先改 FS 再写 DB，那条条件写失败时符号链接已经指向
// 新目标、DB 里却还是别人的占用 —— 访问面被改成了没人承认的状态，且回滚不了（rename 不可逆）。
// 先 DB：失败 → 返回错误 → 整个实体事务回滚（含 301 产物的登记），FS 一动没动。
//
// **但 DB 成功不等于 FS 安全**：符号链接是路径级共享资源，跨实体没有共享的路径锁
// （本函数持有的是**本实体**行锁）。所以条件写之后还要在 FS 侧再核一次目标：
//   - 下线：链接不存在（已删/从未激活）→ 不动；rel 目标仍指向本实体自己的产物
//     → 删；指向别的产物 → 说明该路径已被其他实体接管，**不删**并报错让整批重试，
//     删掉它等于替别人下架线上页面。
//   - 301：若已有链接指向一份 redirect 产物且指向同一目标 → 幂等跳过；指向其他产物
//     （含另一个实体的页面产物）→ 报错。
//
// **check → 改 FS 之间不再有跨实体窗口**：整批旧路径在 disposeOldPathsTx 里由一次
// withPaths 包住（pipeline.PublicationPathLocker，按归一物理路径分片），锁期内别的实体
// 连不上同一条路径，因此"条件写后被人抢占、我再 rename 覆盖"这条时序已不成立。
// 上面那两条 FS 复核仍然保留：它们防的是**历史遗留**的异常链接（别人的产物已经在那儿），
// 不是并发窗口。
func (s *Service) disposeOldPathTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, oldPath, newPath, artifactID string, withRedirect bool) error {
	if withRedirect {
		ra, rerr := pipeline.NewRedirectArtifact(newPath, 301)
		if rerr != nil {
			return rerr
		}
		loc, perr := s.store.PutRedirect(ra)
		if perr != nil {
			return perr
		}
		if s.routes != nil {
			if _, derr := s.routes.RedirectTx(ctx, tx, &pubcontract.RedirectReq{
				ProjectID: inst.ProjectID, OldPath: oldPath,
				PresentationID: inst.ID, ArtifactID: artifactID,
			}); derr != nil {
				return derr
			}
		}
		if oerr := s.oldPathFSTargetAllowsRedirect(oldPath, newPath); oerr != nil {
			return oerr
		}
		return s.publication.Activate(oldPath, loc)
	}
	if s.routes != nil {
		if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
			ProjectID: inst.ProjectID, Path: oldPath, PresentationID: inst.ID,
		}); derr != nil {
			return derr
		}
	}
	if oerr := s.oldPathFSTargetAllowsDeactivate(oldPath); oerr != nil {
		return oerr
	}
	return s.publication.Deactivate(oldPath)
}

// errOldPathFSTargetForeign 旧路径在文件系统上的当前目标不属于本实体。
//
// 可重试（不是终态）：DB 条件写已成功、FS 未动，下一轮重放时路由已经归本实体，
// 只剩 FS 侧复核 —— 若期间对方让出该路径，本轮就能正常收敛。
var errOldPathFSTargetForeign = errors.New("旧路径的访问面链接已被其他产物接管")

// oldPathFSTargetAllowsDeactivate 下线前在 FS 侧复核：这条链接是不是本实体的。
//
// 判据只有"链接现在指向什么"，没有别的真相来源（访问面不查库）。
//   - Kind == none（链接不存在）→ 允许（幂等：没人占就没什么可下线）；
//   - 其它 kind → 一律不允许。我们不知道 "本实体自己的产物" 是否已经被另一实体
//     接管成同样的链接，也没有共享路径锁来保证 check→remove 之间不变；
//     于是在无法证明归属时选择**不动 FS**（报可重试错误），而不是删掉别人的线上页面。
//
// 这里刻意不比较 locator key：改址后旧路径的目标产物仍然是本实体上次发布的产物，
// 但同一份产物也可能被别的路径/实体复用（内容寻址）。比较 key 只能证明"指向同一份
// 字节"，不能证明"这条链接归我"，把它当归属证据正是要避免的过度声称。
func (s *Service) oldPathFSTargetAllowsDeactivate(oldPath string) error {
	if s.publication == nil {
		return nil
	}
	st, ierr := s.publication.Inspect(oldPath)
	if ierr != nil {
		// 读不出来（目标不可达 / 非符号链接占位）：那是需要人看的异常，
		// 不在本轮猜测性删除。
		return ierr
	}
	if st == nil || st.Kind == pipeline.PublicationNone {
		return nil
	}
	logger.Scene("publication").With("oldPath", oldPath).With("kind", st.Kind).
		Warn("旧路径已被其他产物占用，跳过下线（保留 pending 重放）")
	return errOldPathFSTargetForeign
}

// oldPathFSTargetAllowsRedirect 301 前在 FS 侧复核：不要把别人的访问面覆盖成我们的 301。
//
//   - Kind == none → 允许（建立 301 链接）；
//   - Kind == redirect 且已经指向同一目标 → 允许（幂等重放，rename 覆盖同一目标无害）；
//   - 其它（指向别的页面产物 / 指向另一条 301）→ 不允许，报可重试错误。
//
// 与下线相同的限制：本实体行锁不保护跨实体的路径，check→rename 之间的窗口由
// 路由条件写先落库（DB 先于 FS）把竞争面压到最小，但不声称完全消除。
func (s *Service) oldPathFSTargetAllowsRedirect(oldPath, newPath string) error {
	if s.publication == nil {
		return nil
	}
	st, ierr := s.publication.Inspect(oldPath)
	if ierr != nil {
		return ierr
	}
	if st == nil || st.Kind == pipeline.PublicationNone {
		return nil
	}
	if st.Kind == pipeline.PublicationRedirect && st.Redirect != nil &&
		strings.TrimSpace(st.Redirect.TargetPath) == strings.TrimSpace(newPath) {
		return nil
	}
	logger.Scene("publication").With("oldPath", oldPath).With("newPath", newPath).
		With("kind", st.Kind).
		Warn("旧路径的访问面已经指向别的目标，跳过 301 覆盖（保留 pending 重放）")
	return errOldPathFSTargetForeign
}

// errOldPathWithoutSuccessEvidence 旧路径处置回执缺少"激活成功"的证据。
//
// 它是**可重试**的错误（不是终态）：证据可能在本轮稍后才落库（例如关联回执的
// 结案发生在写路径的快通道里），下一轮收敛再判即可。返回 nil 会被当成"已处理"，
// 从而把一条可能有效的意图永久结案掉。
var errOldPathWithoutSuccessEvidence = errors.New("旧路径处置回执缺少关联的成功回执证据")

// convergeSupersededAttemptTx 被取代的 attempt 的收敛（事务内，行锁已持有）。
//
// 「被取代」有两种成因，处置相同：链接上是我们烘的字节，但 DB 的推进已经属于别的 attempt
// （或输入版本已前进）—— 此时真相是「线上跑着一份旧输入的结果」，而系统知道怎么做：
// 让 DB 承认链接事实（账本对齐，保证账本与访问面不失配），把实例标 stale，入队在锁外完成。
//
// 为什么不能只标 rolled_back（规划草案明确否决）：那会让这条回执永久消失，而链接仍指着
// 一份不会被重建的旧产物 —— 用户看到「已发布」的实际是过期内容，且再没有任何东西会去发现它。
//
// 为什么不在这里重新激活或回滚 FS：本函数在事务里、行锁内，而 FS 操作不可回滚；重建是
// 幂等的，交给发布链在锁外做才能保持「同一事务里不出现第二次不可逆副作用」。
func (s *Service) convergeSupersededAttemptTx(ctx context.Context, tx *gorm.DB,
	inst *presentationmodel.InstanceEntity, item pubcontract.PendingReceiptResp,
	expectedHash string, versionMoved, superseded bool) error {
	// 账本对齐：链接上确实是本产物，就让账本记录它（否则账本与访问面会各说各话，
	// 后面 batchConverged 永远判不收敛）。
	if lang := strings.TrimSpace(item.Lang); lang != "" {
		// 账本对齐到**链接事实**：收据 id 也写成这条回执的 —— 库里从此能回答
		// "这一行是哪个 attempt 的字节"，同时 superseded 事实由 stale 承载。
		if merr := s.m.MarkPublishedLangTx(tx, presentationmodel.PublicationRecord{
			PresentationID: inst.ID, Lang: lang, ActivePath: item.Path,
			ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash,
			ReceiptID: strings.TrimSpace(item.ID), PublishedAt: time.Now().UTC(),
		}); merr != nil {
			return merr
		}
	}
	// 标 stale 用的是**不推进版本**的变体：这里不是「输入变了」，而是「线上需要重建」。
	// 推进版本会把正在跑的发布判成过期，而它们编译的输入未必有问题。
	if _, serr := s.m.MarkStaleWithoutVersion(ctx, inst.ProjectID, []string{inst.ID}, time.Now().UTC()); serr != nil {
		return serr
	}
	logger.Scene("publication").With("instanceId", inst.ID).With("url", item.Path).
		With("versionMoved", versionMoved).With("superseded", superseded).
		Warn("未结案的多语言发布回执被同版本的新 attempt 或更新的输入取代；链接仍指向本产物，已对齐账本并安排重建")
	return nil
}

// langLedgerSupersededTx 语言账本里该语言的产物是否已被别的 attempt 取代（G4）。
//
// 判据：「账本已有该语言行，且它的 artifact 不是本回执的产物」。
// 账本无行**不算取代** —— 那正是「切换成功、账本没写」的待补齐形态。
func (s *Service) langLedgerSupersededTx(ctx context.Context, tx *gorm.DB,
	instanceID string, item pubcontract.PendingReceiptResp) bool {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" || strings.TrimSpace(item.ToArtifactID) == "" {
		return false
	}
	// 判据分两档（G4'）：
	//  1) 账本行带 receipt_id（迁移 700 之后写的）且不等于本回执 id → 取代。
	//     这一档能区分「同版本重建复用了同一条产物行」的两个不同 attempt —— 它们的
	//     artifact_id 完全相同，只比产物会误判成「未被取代」。
	//  2) 账本行没有 receipt_id（本列上线前的历史行）→ 退回 artifact_id 比较（仅历史兼容）。
	ledgerReceiptID, rerr := s.m.PublishedReceiptIDTx(tx, instanceID, lang)
	if rerr != nil {
		return false
	}
	if ledgerReceiptID != "" {
		return ledgerReceiptID != strings.TrimSpace(item.ID)
	}
	artifactID, err := s.m.PublishedArtifactIDTx(tx, instanceID, lang)
	if err != nil || artifactID == "" {
		return false
	}
	return artifactID != strings.TrimSpace(item.ToArtifactID)
}

// capturedInputVersionOf 回执里捕获的输入版本；未捕获（旧回执）返回 0。
//
// 0 是「未知」哨兵而不是合法版本：实体版本从 1 起推进（迁移 699 的 DEFAULT 0 只用于历史行），
// 用 0 表示未知能让 G2「版本不等」永不误判 —— 旧回执缺这一列时退回纯 G3+G4 判定。
//
// 值来自 publication 侧的 PendingReceiptResp.CapturedInputVersion（由 receipt_data 的
// capturedInputVersion 投影）。登记端必须交**编译捕获**的那一代 —— beginPublishReceipt
// 现在传的是冻结段读到的 frozen.inputVersion（改 URL 时是该段刚推进出来的新代）。
func capturedInputVersionOf(item pubcontract.PendingReceiptResp) int64 {
	if v := item.CapturedInputVersion; v > 0 {
		return v
	}
	return 0
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
//
// 无语言信息的回执（非本模块登记）不做路径守卫：那种回执的 Path 由别的模块决定，
// 用本模块的语言规则去比对只会把它一律判成"过期"。
func (s *Service) receiptPathStillCurrent(ctx context.Context, inst *presentationmodel.InstanceEntity,
	item pubcontract.PendingReceiptResp) bool {
	lang := strings.TrimSpace(item.Lang)
	if lang == "" {
		return true
	}
	rule := pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID)
	path, perr := pipeline.SitePath(rule, lang, s.instanceLogicalPath(ctx, inst))
	if perr != nil {
		return false
	}
	return path == item.Path
}

// locateInstanceForReceipt 定位回执所属实例：回执带工程时先直查，否则逐工程遍历。
//
// 为什么要遍历：presentation_instances 带 FORCE 策略（迁移 215），无作用域查询在非超级
// 角色下静默返回 0 行 —— 于是"实例不存在"与"没设作用域"看起来完全一样，回执会被误结案。
// 回执里的 projectID 是登记时写的，正常路径必然命中直查分支。
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

// ── 下线（删除 / 退役）意图：预登记 + 恢复 ────────────────────────────────────
//
// 状态机只有两件事要看：**业务 DB 变更提交了没有**、**链接现在指着谁**。
//
//	预登记(pending, businessCommitted 缺键)
//	  → 业务事务(实体行锁 + 路径集合复验 + 同 tx 写 businessCommitted=true + 删除/退役)
//	    ├ 提交成功 → 撤链 → 全部撤掉才 committed
//	    └ 提交失败 → businessCommitted 缺键仍在 → 回执 rolled_back，**链接保持原样**
//	崩溃/中断 → pending 放着，恢复在业务锁下重读本行再决定
//
// 为什么链接在提交成功之后才撤（与旧实现相反）：先撤链会把"实例还在、线上 404"
// 变成正常路径上都会出现的中途状态，而它的补偿（把链接改回去）自身失败时没有
// 任何持久事实可以重放。改成 DB 先提交后，撤销方向的唯一依据就是已提交的业务事实——
// 崩溃时要么"业务没提交、链接也没动"，要么"业务已提交、链接待撤"，两种都可由回执重放。

// deactivationIntent 一次下线意图的持久形态（登记之后不再改变）。
type deactivationIntent struct {
	ReceiptID     string
	ProjectID     string
	InstanceID    string
	Paths         []string
	PathArtifacts map[string]pubcontract.ReceiptPathIdentity
}

// pathIdentities 每条待下线路径此刻的归属事实：账本里的产物 id / 成功 attempt 身份，
// 以及访问面上链接的定位器。
//
// 三者都要：产物 id 证明"哪份字节"、回执 id 证明"哪一次成功"（同一产物可能被多次
// attempt 激活）、定位器证明"链接本身是不是我登记时那一条"。只看其中任何一个都会
// 把"别人的同字节产物"或"同一产物的新 attempt"误判成己方。
func (s *Service) pathIdentities(ctx context.Context, inst *presentationmodel.InstanceEntity,
	paths []string) map[string]pubcontract.ReceiptPathIdentity {
	out := make(map[string]pubcontract.ReceiptPathIdentity, len(paths))
	pubs, perr := s.m.ListPublicationsForInstance(ctx, inst.ID)
	if perr != nil {
		logger.Scene("build").With("instanceId", inst.ID).
			Warn("读取实例语言账本失败，下线意图的路径归属按链接事实记录: " + perr.Error())
		pubs = nil
	}
	for _, p := range paths {
		key := strings.TrimSpace(p)
		if key == "" {
			continue
		}
		var id pubcontract.ReceiptPathIdentity
		for _, pub := range pubs {
			if strings.TrimSpace(pub.ActivePath) != key {
				continue
			}
			if pub.ArtifactID != nil {
				id.ArtifactID = strings.TrimSpace(*pub.ArtifactID)
			}
			if pub.ReceiptID != nil {
				id.ReceiptID = strings.TrimSpace(*pub.ReceiptID)
			}
			break
		}
		if s.publication != nil {
			if st, ierr := s.publication.Inspect(key); ierr == nil && st != nil && st.Locator != nil {
				id.Locator = strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key)
			}
		}
		if id.ArtifactID != "" || id.ReceiptID != "" || id.Locator != "" {
			out[key] = id
		}
	}
	return out
}

// registerDeactivationIntent 在**动任何访问面之前**把下线意图持久登记成 pending 回执。
//
// 登记失败必须阻止整个操作：没有回执，就再也没有任何地方记着"这些链接原本属于谁"，
// 而删除/退役一旦提交就不可撤销。
//
// 这里不持有任何 DB 行锁：只做一次 INSERT（独立短事务），以免"持业务行锁再另开连接
// 写回执"变成自锁。
func (s *Service) registerDeactivationIntent(ctx context.Context, inst *presentationmodel.InstanceEntity,
	paths []string) (*deactivationIntent, error) {
	if s.routes == nil {
		return nil, errors.New("下线意图登记端口未装配")
	}
	// owner 与路径事实都来自**本模块的持久行**（实例 id / active_path），不接受请求入参自证：
	// 否则调用方传一个别人的实例 id 就能登记出"撤别人链接"的意图。
	if strings.TrimSpace(inst.ProjectID) == "" || strings.TrimSpace(inst.ID) == "" {
		return nil, errors.New("下线意图缺少实例归属事实")
	}
	ids := s.pathIdentities(ctx, inst, paths)
	id, rerr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID:      inst.ProjectID,
		Path:           strings.TrimSpace(inst.URLPath),
		PresentationID: inst.ID,
		Action:         pubcontract.ReceiptActionDeactivate,
		OldPaths:       append([]string(nil), paths...),
		OldPath:        firstPath(paths),
		PathArtifacts:  ids,
	})
	if rerr != nil {
		return nil, fmt.Errorf("登记下线意图失败（未改动访问面）: %w", rerr)
	}
	return &deactivationIntent{
		ReceiptID: id, ProjectID: inst.ProjectID, InstanceID: inst.ID,
		Paths: append([]string(nil), paths...), PathArtifacts: ids,
	}, nil
}

func firstPath(paths []string) string {
	for _, p := range paths {
		if v := strings.TrimSpace(p); v != "" {
			return v
		}
	}
	return ""
}

// deactivationPathsUnchanged 业务事务内复验：路径集合与登记时一致才允许继续。
//
// 不等意味着"登记之后实例的访问路径已经变了"（新建语言、改 URL、又发布了一次），
// 那时按登记的清单去撤链会撤掉刚上线的新链接。拒绝比"尽力而为"安全：调用方看到
// 一次明确失败，回执留在 pending，下一轮按当时的真实路径集合重新判断。
func deactivationPathsUnchanged(registered []string, current []string) error {
	want := make(map[string]struct{}, len(registered))
	for _, p := range normalizePathList(registered) {
		want[p] = struct{}{}
	}
	got := make(map[string]struct{}, len(current))
	for _, p := range normalizePathList(current) {
		got[p] = struct{}{}
	}
	if len(want) != len(got) {
		return fmt.Errorf("%w: 登记的下线路径集合与当前不一致", errPublishStateMoved)
	}
	for p := range want {
		if _, ok := got[p]; !ok {
			return fmt.Errorf("%w: 登记的下线路径集合与当前不一致", errPublishStateMoved)
		}
	}
	return nil
}

// completeDeactivationIntent 在业务事务**提交成功之后**把访问面收尾。
//
// 顺序是硬要求：先提交 DB 再动 FS。反过来的话，"第 k 条撤链失败"就留下半截
// （实例已删、部分链接仍在），而链接的撤除不可回滚；先 DB 后 FS 时失败只表现为
// "回执仍 pending、链接多留一会儿"，收敛会按回执里登记的路径幂等重放。
// 撤链失败不回滚业务（业务已提交且正确），错误如实返回调用方，回执留 pending。
func (s *Service) completeDeactivationIntent(ctx context.Context,
	inst *presentationmodel.InstanceEntity, intent *deactivationIntent) error {
	if intent == nil {
		return nil
	}
	if len(intent.Paths) == 0 {
		return s.completePublishReceipt(ctx, intent.ReceiptID)
	}
	// executed=false 表示回执已不在 pending（另一个收敛抢先完成）：终态，不再重复 Complete。
	executed, werr := s.withdrawDeactivatedLinksLocked(ctx, inst.ProjectID, inst.ID, intent.ReceiptID)
	if werr != nil {
		s.lastOldPathPending.Store(1)
		s.NotifyPendingReceipt()
		return fmt.Errorf("实例已删除，但访问面链接下线失败（回执 %s 保持 pending 待重放）: %w",
			intent.ReceiptID, werr)
	}
	if !executed {
		return nil
	}
	return s.completePublishReceipt(ctx, intent.ReceiptID)
}

// ensureDeactivationPathsUnchanged 在业务事务（已持稳定锁与行锁）内复验路径集合。
func (s *Service) ensureDeactivationPathsUnchanged(ctx context.Context,
	inst *presentationmodel.InstanceEntity, registered []string) error {
	return deactivationPathsUnchanged(registered, s.instanceActivePaths(ctx, inst))
}

// withdrawDeactivatedLinks 撤掉已确认不再使用的访问路径链接。
//
// 执行前提（调用方保证）：业务 DB 变更**已提交**、且已持有同一把业务实体锁 ——
// 所以这里只管"撤链"这一件事，不再做业务判据。路径锁包住全部路径，锁内逐条复验
// 归属，别人的链接一律不动。
func (s *Service) withdrawDeactivatedLinks(ctx context.Context, projectID, instanceID string,
	ids map[string]pubcontract.ReceiptPathIdentity, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return s.withPaths(paths, func() error {
		for _, p := range paths {
			if strings.TrimSpace(p) == "" {
				continue
			}
			if oerr := s.verifyDeactivatedPathOwner(ctx, projectID, instanceID, p, ids[p]); oerr != nil {
				return oerr
			}
			if derr := s.publication.Deactivate(p); derr != nil {
				return fmt.Errorf("撤掉访问路径 %s 失败: %w", p, derr)
			}
		}
		return nil
	})
}

// verifyDeactivatedPathOwner 路径锁内复验：这条路径此刻还归本实例登记的那份链接。
//
// 判据全部是"读当前事实"，不猜：
//   - 路由占用：不存在 → 没有占用方（撤链无害）；存在但不是本实例 → 已被接管，报可重试；
//   - 访问面链接：登记时没有定位器 → 只剩路由判据；有定位器 → 现在必须还是它，
//     否则说明链接已被别的产物覆盖，绝不能顺着下手。
func (s *Service) verifyDeactivatedPathOwner(ctx context.Context, projectID, instanceID, path string,
	registered pubcontract.ReceiptPathIdentity) error {
	if s.routes != nil {
		owner, oerr := s.routes.RouteOwnerTx(ctx, nil, projectID, path)
		if oerr != nil && !errors.Is(oerr, gorm.ErrRecordNotFound) {
			return oerr
		}
		if oerr == nil && owner != nil {
			if owner.ProjectID != projectID {
				return fmt.Errorf("%w: 路径 %s 已属于其它工程", errOldPathFSTargetForeign, path)
			}
			if owner.PresentationID == nil || *owner.PresentationID != instanceID {
				return fmt.Errorf("%w: 路径 %s 的路由占用已转给其它实体", errOldPathFSTargetForeign, path)
			}
		}
	}
	if s.publication == nil || registered.Locator == "" {
		return nil
	}
	st, ierr := s.publication.Inspect(path)
	if ierr != nil {
		return ierr
	}
	if st == nil || st.Kind == pipeline.PublicationNone || st.Locator == nil {
		// 链接已经不在（上一次重放撤掉了一半）：幂等，允许继续。
		return nil
	}
	if current := strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key); current != registered.Locator {
		return fmt.Errorf("%w: 路径 %s 的访问面链接已指向别的产物", errOldPathFSTargetForeign, path)
	}
	return nil
}

// recoverDeactivateReceipt 收敛一条下线意图（业务锁 → 重读本行事实 → 决定撤链或结案）。
//
// 领取结果是快照，不能当结论：ClaimPendingReceipts 没有租约，同一行可被多个收敛同时
// 领到；而业务事务可能正在写。所以顺序被固定成：
//
//	条带锁（同进程内与 Delete/Retire 互斥）
//	  → 业务实体锁（**无条件**先取同键顾问锁；行仍在再叠加行锁，缺行只忽略）
//	    → 经 publication 的 Tx 端口重读本回执的 state + businessCommitted + pathArtifacts
//	      → 业务未提交：保持 pending（业务可能尚未提交），**不动任何链接**
//	      → 业务已提交：重取稳定锁 + 锁内重读路径事实 → 路径锁内复验归属 → 撤链 → committed
func (s *Service) recoverDeactivateReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	if s == nil || s.publication == nil || s.routes == nil {
		return false, nil
	}
	receiptID, perr := strconv.ParseInt(strings.TrimSpace(item.ID), 10, 64)
	if perr != nil || receiptID <= 0 {
		return false, fmt.Errorf("下线回执 id 非法: %q", item.ID)
	}
	projectID := strings.TrimSpace(item.ProjectID)
	if projectID == "" || item.SourceID == "" {
		// 工程/实例是 owner 事实，缺了就没有可校验的归属 —— 只能结案。
		s.abortPublishReceipt(ctx, item.ID, "下线意图缺少工程/实例事实")
		return false, nil
	}

	lock := s.lockInstance("presentation", item.SourceID)
	lock.Lock()
	defer lock.Unlock()

	var (
		abandon bool
		done    bool
	)
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if aerr := s.lockPresentationEntityOrAdvisory(ctx, tx, projectID, item.SourceID); aerr != nil {
			return aerr
		}
		row, lerr := s.routes.LoadDeactivationReceiptTx(ctx, tx,
			pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourcePresentation, SourceID: item.SourceID},
			projectID, item.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				abandon = true
				return nil
			}
			return lerr
		}
		if row.ReceiptState != "pending" {
			// 已被并发结案（另一个收敛先完成）：本行没我什么事。
			abandon = true
			return nil
		}
		if !deactivationBusinessCommitted(row.ReceiptData) {
			// 业务变更还没提交 —— 注意这不等于"业务没发生"：预登记之后、业务事务取到
			// 同一把稳定锁之前的这段里，恢复会看到 false，而业务随时可能提交。
			// 因此这里**不能终态化**（abort 会把一个尚未开始的有效意图判死，
			// 业务随后提交成功却再无回执可撤链 → 永久残留）。保留 pending、不动访问面，
			// 等下一次收敛在业务锁下重读。明确的业务取消由业务侧自己 abort。
			return nil
		}
		done = true
		return nil
	})
	if txErr != nil {
		// 恢复侧的事务只读（不写业务行），出错一律原样上抛 —— 不会误 Abort，
		// 也不会被当成"业务未提交"而错误终态化。
		return false, txErr
	}
	if abandon {
		s.abortPublishReceipt(ctx, item.ID, "下线意图对应的业务变更未提交（或已被并发结案），链接保持原样")
		return false, nil
	}
	if done {
		// 撤链放在判定事务**之后**、条带锁仍持有时：事务里已经确认过"本回执 pending 且
		// 业务已提交"，而事务级锁随提交释放 —— 所以这里重取一次稳定锁再动手，不能顺着
		// 事务里的结论直接删链接。路径不传快照：由 withdrawDeactivatedLinksLocked 在
		// 锁内重读回执事实后自行投影（claim 快照可能已过期）。
		executed, werr := s.withdrawDeactivatedLinksLocked(ctx, projectID, item.SourceID, item.ID)
		if werr != nil {
			return false, werr
		}
		if !executed {
			// 已被另一个收敛结案：终态 no-op，不再重复 Complete。
			return false, nil
		}
		if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
			return false, cerr
		}
		return true, nil
	}
	return false, nil
}

// withdrawDeactivatedLinksLocked 在**判定事务之后再取一次稳定业务锁**的前提下撤链。
//
// 为什么不能顺着事务里的结论直接删链接：行锁/顾问锁都是事务级（pg_advisory_xact_lock
// 随提交释放），事务一提交锁就没了，而 FS 操作必须在锁内、且与"另一次发布把这条路径
// 换成别人的产物"互斥。所以这里重开一个短事务：取同一把稳定锁 → 经 publication 的 Tx
// 端口重读本行事实（state 仍 pending、businessCommitted 仍为真、pathArtifacts 是最终值）
// → 再在锁内做路径复验 + 撤链。重读是必需的：判定与撤链之间业务侧可能明确取消了。
//
// 返回值语义：executed=true 表示**本次调用真的撤了链接**（外层据此 complete）；
// executed=false 表示回执已不在可执行状态（已 committed / 已被并发结案）—— 那是终态，
// 外层不得再 Complete（重复终态化会把"另一次收敛已完成的结论"再写一遍）。
func (s *Service) withdrawDeactivatedLinksLocked(ctx context.Context, projectID, instanceID,
	receiptID string) (executed bool, err error) {
	txErr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		if aerr := s.lockPresentationEntityOrAdvisory(ctx, tx, projectID, instanceID); aerr != nil {
			return aerr
		}
		row, lerr := s.routes.LoadDeactivationReceiptTx(ctx, tx,
			pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourcePresentation, SourceID: instanceID},
			projectID, receiptID)
		if lerr != nil {
			return lerr
		}
		if row.ReceiptState != "pending" {
			// 已终态（另一个收敛已完成）：no-op，不是错误。
			return nil
		}
		if !deactivationBusinessCommitted(row.ReceiptData) {
			return fmt.Errorf("%w: 下线回执缺少已提交事实", errPublishStateMoved)
		}
		ids := deactivationPathArtifacts(row.ReceiptData)
		// 路径事实必须从**锁内重读的这一行**取，不能用领取（claim）时的快照：
		// claim 是短事务 SKIP LOCKED、没有租约，且登记与业务提交之间 pathArtifacts
		// 可能被 MarkDeactivationCommittedTx 覆盖成最终值。缺事实就保 pending 报错，
		// 不拿旧快照凑数（凑数会撤掉一条别人刚接管的路径）。
		lockedPaths := deactivationReceiptPaths(row.ReceiptData)
		if len(lockedPaths) == 0 {
			return fmt.Errorf("%w: 下线回执缺少路径事实", errPublishStateMoved)
		}
		executed = true
		return s.withdrawDeactivatedLinks(ctx, projectID, instanceID, ids, lockedPaths)
	})
	if txErr != nil {
		return false, txErr
	}
	return executed, nil
}

// lockPresentationEntityOrAdvisory 在事务内按**固定顺序**取业务实体锁：稳定锁 → 行锁。
//
//  1. 先**无条件**取同键事务级顾问锁（pg_advisory_xact_lock，键 presentation:{id}）；
//  2. 再试着取实例行锁；NotFound 只表示"行已不在"，忽略即可（删除是真 DELETE）。
//
// 为什么不能"行存在就只取行锁"：行锁与顾问锁是**两把不同的锁**。恢复 A 查到行、
// 取行锁；业务随后删掉该行（同一行锁在事务提交时释放）；恢复 B 此时只能拿顾问锁 ——
// 两把锁互不阻塞，A、B 会同时撤链并各自认为自己独占。无条件先取顾问锁后，
// 任何持有行锁的路径都必然也持有同一把顾问锁（Delete/Retire 已同序取锁），
// 于是"行被删掉的那一刻之后谁还能动手"这件事有唯一答案。
func (s *Service) lockPresentationEntityOrAdvisory(ctx context.Context, tx *gorm.DB,
	projectID, instanceID string) error {
	// 第 1 步：稳定锁无条件先取（这一句就是"同序"的全部含义，不可写成条件分支）。
	if aerr := s.m.LockPresentationEntityAdvisoryTx(ctx, tx, instanceID); aerr != nil {
		return aerr
	}
	// 第 2 步：行在就额外加行锁；行不在（已删/已软删）只忽略缺失，不当失败。
	if _, lerr := s.m.LockInstanceForActivationTx(ctx, tx, projectID, instanceID); lerr != nil {
		if errors.Is(lerr, gorm.ErrRecordNotFound) {
			return nil
		}
		return lerr
	}
	return nil
}

// deactivationReceiptPaths 从**锁内重读**的回执 receipt_data 投影完整待撤路径清单。
//
// 优先 oldPaths（登记时的全量清单），退回单值 oldPath（历史行），去重并跳过等于
// 主 path 的项（主 path 是"这次下线针对的实例路径"，不是要撤的第二条）。
func deactivationReceiptPaths(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var probe struct {
		Path     string   `json:"path"`
		OldPath  string   `json:"oldPath"`
		OldPaths []string `json:"oldPaths"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	main := strings.TrimSpace(probe.Path)
	out := make([]string, 0, len(probe.OldPaths)+1)
	for _, p := range normalizePathList(probe.OldPaths) {
		if p != main {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		if single := strings.TrimSpace(probe.OldPath); single != "" && single != main {
			out = append(out, single)
		}
	}
	return out
}

// deactivationBusinessCommitted 读回执 receipt_data.businessCommitted（缺键 = false）。
func deactivationBusinessCommitted(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		BusinessCommitted bool `json:"businessCommitted"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.BusinessCommitted
}

// deactivationPathArtifacts 读回执 receipt_data.pathArtifacts（解析失败返回空表）。
func deactivationPathArtifacts(raw json.RawMessage) map[string]pubcontract.ReceiptPathIdentity {
	if len(raw) == 0 {
		return nil
	}
	var probe struct {
		PathArtifacts map[string]pubcontract.ReceiptPathIdentity `json:"pathArtifacts"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	return probe.PathArtifacts
}
