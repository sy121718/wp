package routers

// assembly_navigation_translation.go — 导航译文写链的装配适配（navigation 译文 MVC）。
//
// 这里只有三类东西，全都属于「装配层的职责」，不属于任何单个业务模块：
//
//  1. **三个同事务端口的真实注入**：导航译文整表提交要把「写译文 + 标手工页面 +
//     标自动发布实例 + 入队重建」放进同一个数据库事务。三个动作分属 page / presentation /
//     build 三个模块，所以由装配层把它们的 **…Tx** 能力收窄成 navigation 契约里的端口，
//     交给服务层构造的 TranslationWriter。三个适配器共用调用方传进来的 *gorm.DB ——
//     自己另开一个 tx 就不再是同一个事务，失效会在提交后才被发现丢了。
//
//     导航 service 不能直接 import 这三个模块（跨模块只走 contract），
//     适配器的落点因此只能是装配层；它们全是十行以内的转发，不带任何业务判断。
//
//  2. **presentation 的重建能力**（StaleRebuildPort）：menu_stale 执行器与旧失效链共用。
//     构造侧实现在 presentation 模块内，装配层只做「有没有」的断言与 fail-fast。
//
//  3. **menu_stale 的队列执行器**：菜单标签译文变了，受影响的手工页面与自动发布实例
//     要在事务提交后**真正重建**（而不是再入队一轮）。它必须严格：任一页 / 任一实例
//     重建失败就返回错误，由 build worker 记 Failed；不用「尽力而为」版本顶替
//     （那会把失败吞成成功，见 executeMenuStaleJob 的注释）。
//
// 顺序约束（可回归的判据）：registerMenuStaleExecutor 必须在 StartWorkers 之前调用 ——
// worker 抢到的第一条 menu_stale 任务若还没有执行器，会被 build worker 判成
// ErrExecutorMissing 并**直接 Failed 且不重试**。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildmodel "go_wp/internal/module/build/model"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
)

// 本文件的失败事实（执行器返回即由 build worker 记 Failed，不改任务状态机）。
var (
	// errMenuStaleJobNil 任务为 nil（理论上 worker 不会传 nil，仍显式判）。
	errMenuStaleJobNil = errors.New("menu_stale 任务为空")
	// errMenuStaleSourceID menu_stale 的 source_id 形状非法（不是「工程 uuid:kind」）。
	errMenuStaleSourceID = errors.New("menu_stale 任务的 source_id 形状非法")
	// errMenuStaleProjectMismatch source_id 里的工程与任务自身的 ProjectID 不一致。
	errMenuStaleProjectMismatch = errors.New("menu_stale 任务的工程与 source_id 不一致")
	// errMenuStaleKindMissing kind 为空（无从定位菜单位置）。
	errMenuStaleKindMissing = errors.New("menu_stale 任务缺少菜单位置")
)

// —— 同事务端口适配器 ——

// pageMenuStalePort 把 page 模块的同事务菜单标记收窄成 navigation 的 PageStalePort。
//
// 为什么由装配层转手：导航只知道自己变了「哪个菜单位置」（kind），
// 「哪些页面依赖这个位置」是 page 模块的投影知识，跨模块只能走端口。
type pageMenuStalePort struct {
	svc pagecontract.PageService
}

// MarkMenuStaleTx 在同一事务内标记受影响页面（实现 navigationcontract.PageStalePort）。
func (p pageMenuStalePort) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, kind string, at time.Time) (int64, error) {
	marker, ok := p.svc.(pagecontract.PageMenuStalePort)
	if !ok {
		// 端口未实现：返回错误而不是 0 —— 「没标任何页面」与「这页模块不会标」是两件事，
		// 前者可能是正常的 0 行，后者是装配缺陷，必须整笔失败。
		return 0, errors.New("页面模块未实现同事务菜单标记（PageMenuStalePort）")
	}
	return marker.MarkMenuStaleTx(ctx, tx, projectID, kind, at)
}

// presentationMenuStalePort 把 presentation 模块的同事务实例标记收窄成
// navigation 的 PresentationStalePort。
type presentationMenuStalePort struct {
	svc presentationcontract.PresentationService
}

// MarkInstanceMenuStaleTx 在同一事务内标记受影响自动发布实例（实现 navigationcontract.PresentationStalePort）。
func (p presentationMenuStalePort) MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, kind string, at time.Time) (int64, error) {
	marker, ok := p.svc.(presentationcontract.PresentationStalePort)
	if !ok {
		return 0, errors.New("自动发布实例模块未实现同事务菜单标记（PresentationStalePort）")
	}
	return marker.MarkInstanceMenuStaleTx(ctx, tx, projectID, kind, at)
}

// presentationNavigationStalePort 把 presentation 模块的**按菜单项**同事务标记收窄成
// navigation 的 MenuItemPresentationStalePort。
//
// 与 presentationMenuStalePort（按位置）是两个端口而不是一个带 kind 的重载：两者收的
// 输入不同（菜单位置 vs menu item id）、依赖键不同（menu:{project}:{kind} vs
// navigation:{itemID}）、命中的实例集合也不同。并成一个方法就得在适配器里猜调用方
// 到底给了哪种字符串 —— 而拼错的键只会静默 0 行。
type presentationNavigationStalePort struct {
	svc presentationcontract.PresentationService
}

// MarkInstanceNavigationStaleTx 在同一事务内标记「按项引用了该菜单项」的自动发布实例。
func (p presentationNavigationStalePort) MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, navigationID string, at time.Time) (int64, error) {
	marker, ok := p.svc.(presentationcontract.PresentationStalePort)
	if !ok {
		return 0, errors.New("自动发布实例模块未实现同事务按项菜单标记（PresentationStalePort）")
	}
	return marker.MarkInstanceNavigationStaleTx(ctx, tx, projectID, navigationID, at)
}

// buildMenuStalePort 把 build 模块的 menu_stale 入队收窄成 navigation 的
// MenuStaleEnqueuePort。入队必须与写入同事务：任何一边成功、另一边失败都会留下
// 「线上是旧标签但没人再重建」或「重建跑了但译文没落库」的分叉。
type buildMenuStalePort struct {
	svc buildcontract.BuildService
}

// EnqueueMenuStaleTx 在同一事务内入队（实现 navigationcontract.MenuStaleEnqueuePort）。
func (b buildMenuStalePort) EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, kind string) (int64, error) {
	return b.svc.EnqueueMenuStaleTx(ctx, tx, projectID, kind)
}

// navigationProjectLister 把工程契约的 List 收窄成 writer 需要的「只要 id」。
//
// 不直接传 projectcontract.ProjectService：writer 的端口形状是
// List(ctx) ([]service.ProjectEntry, error)，而 project 的返回是 dto 全集 ——
// 形状在此处对齐，service 因此不依赖 project 模块的 dto（那属于越级 import）。
func navigationProjectLister(projects projectcontract.ProjectService) navigationservice.ProjectListerFunc {
	return func(ctx context.Context) ([]navigationservice.ProjectEntry, error) {
		rows, err := projects.List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]navigationservice.ProjectEntry, 0, len(rows))
		for _, row := range rows {
			out = append(out, navigationservice.ProjectEntry{ID: row.ID})
		}
		return out, nil
	}
}

// navigationWritePorts 菜单 CRUD 与译文写路径**共用的同事务端口**（装配层建一次）。
//
// 为什么两处共用同一份构造：两条写路径都要「标页面 + 标实例 + 入队」，若各自包一层适配器，
// 哪天某个端口换了实现口径，就只剩一半的写路径跟上 —— 那正是「译文改了页面重建了、
// 菜单改了没重建」这类分叉的成因。
//
// Instances（presentation）是必需端口而非可选优化：实例的旧编译若不在同一事务里推进
// input_version，它晚到的产物会通过激活谓词、写进新 HTML 并把 stale 清掉；可靠入队只
// 保证「会重建一次」，保证不了「旧编译晚到不能覆盖」。版本屏障只能与菜单写同事务给出，
// 所以这里与译文写路径的 w.instances 用同一个适配器（presentationMenuStalePort）。
func navigationWritePorts(pageSvc pagecontract.PageService,
	presentationSvc presentationcontract.PresentationService,
	buildSvc buildcontract.BuildService) navigationservice.NavigationWriteDeps {
	return navigationservice.NavigationWriteDeps{
		Pages:     pageMenuStalePort{svc: pageSvc},
		Instances: presentationMenuStalePort{svc: presentationSvc},
		Items:     pageNavigationStalePort{svc: pageSvc},
		// 按项失效的实例端口：键 navigation:{itemID}，与位置键不是同一条。
		// 只接位置版会让「只引用某一支」的实例既不被标 stale、也没有版本屏障。
		ItemInstances: presentationNavigationStalePort{svc: presentationSvc},
		Queue:         buildMenuStalePort{svc: buildSvc},
	}
}

// navigationMenuItemStale 装配层需要的收窄：页面模块的「按菜单项同事务标记」。
//
// 在装配侧声明而不是往 pagecontract 里加接口：只有导航写路径用这条，页面的契约面
// 为此多一个方法等于让每个实现都得回答「你标不标菜单项」。
type navigationMenuItemStale interface {
	MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB, projectID, navigationID string) (int64, error)
}

// pageNavigationStalePort 把 page 模块的**按菜单项**同事务标记收窄成 navigation 的
// MenuItemStalePort（依赖键 navigation:{itemID}，与按位置键不是同一条）。
type pageNavigationStalePort struct {
	svc pagecontract.PageService
}

// MarkNavigationStaleTx 在同一事务内标记「按项引用了该菜单项」的页面。
func (p pageNavigationStalePort) MarkNavigationStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, navigationID string) (int64, error) {
	marker, ok := p.svc.(navigationMenuItemStale)
	if !ok {
		return 0, errors.New("页面模块未实现同事务按项菜单标记（navigation 契约 MenuItemStalePort）")
	}
	return marker.MarkNavigationStaleTx(ctx, tx, projectID, navigationID)
}

// newNavigationTranslationWriter 构造导航译文整表提交的写服务（生产唯一构造点）。
//
// 六组依赖全部是真实实现（没有桩、没有 nil 兜底）：写服务在依赖缺失时整体 fail closed，
// 而「缺了哪个」要在装配期就能看见，不要等到第一次保存译文才发现。
//   - rows         导航 model 的事务内行读取（NewNavigationRowsTx 收窄）；
//   - sources      同一 navigation Service 的**事务内**来源索引；
//   - projects     工程清单（只取 id）；
//   - content      译文存储（pkg/i18n ContentWriter：与读侧工作台同一份实现）；
//   - tx           事务边界（navigation model 的 Transaction）；
//   - 三个端口       page / presentation / build 的同事务能力，全部指向同一个 *gorm.DB。
func newNavigationTranslationWriter(db *gorm.DB, navigationSvc navigationcontract.NavigationService,
	projects projectcontract.ProjectService, pageSvc pagecontract.PageService,
	presentationSvc presentationcontract.PresentationService, buildSvc buildcontract.BuildService,
	content navigationContentWriter) *navigationservice.TranslationWriter {
	model := navigationmodel.NewModel(db)
	// 来源索引要的是 navigation Service 的**事务内**打开能力（OpenSourceIndexTx）：
	// 它带全部四个 kind、严格来源解析与逐位置失败事实，装配层不复制这套算法。
	//
	// 来源索引是**必需**依赖：它不是可选增强，而是「行 → 可写目标」的唯一解析口。
	// 用 `, _` 丢弃断言结果会让装配缺陷（Service 改了实现、忘了这层收窄）悄悄退成
	// Sources=nil，症状是第一次保存译文才 500「写服务未就绪」—— 那时已经离装配期很远了。
	// 所以在装配期 fail-fast：类型不匹配直接 panic，与上面三组端口同一条口径。
	sourceOpener, ok := navigationSvc.(navigationSourceIndexTx)
	if !ok {
		panic("导航模块未实现事务内来源索引（navigationSourceIndexTx）—— 译文写服务无法解析可写目标")
	}
	return navigationservice.NewTranslationWriter(navigationservice.TranslationWriterDeps{
		Rows:      navigationservice.NewNavigationRowsTx(model),
		Sources:   sourceOpener,
		Projects:  navigationProjectLister(projects),
		Content:   content,
		Pages:     pageMenuStalePort{svc: pageSvc},
		Instances: presentationMenuStalePort{svc: presentationSvc},
		Queue:     buildMenuStalePort{svc: buildSvc},
		Tx:        model,
	})
}

// navigationSourceIndexTx 装配层需要的收窄：navigation Service 的「事务内打开来源索引」。
//
// 在装配侧声明而不是塞进 NavigationService：构建期适配器、MCP、菜单管理页都不用这条，
// 把「译文写要什么」放进公共服务接口等于让每个实现都得回答这个问题。
type navigationSourceIndexTx interface {
	OpenSourceIndexTx(ctx context.Context, tx *gorm.DB, projectID string) (navigationcontract.SourceIndex, error)
}

// navigationContentWriter 译文存储的窄切片（*i18n.ContentWriter 直接满足）。
//
// 用接口声明是为了让构造函数的参数形状自解释（只借「事务内读现值 + 事务内 UPSERT」
// 两条能力），也让装配层不把整个 ContentWriter 交出去。
type navigationContentWriter interface {
	LoadDetailsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string,
		hashes []string) (map[string]i18n.ContentTargetInfo, error)
	UpsertTx(ctx context.Context, tx *gorm.DB, items []i18n.ContentWriteItem) (int, error)
}

// —— menu_stale 队列执行器 ——

// menuStaleJob menu_stale 任务的解码结果（工程 id + 菜单位置）。
//
// 为什么要解码：任务的 SourceID 是「工程 uuid:kind」的复合键（build 契约规定），
// 而重建要用的是**工程 id** 与**位置**两件事。把整串当实体 id 会拼出一个不存在的
// 依赖键 —— 标记 0 行、入队照旧成功，失败被伪装成「没有受影响的东西」。
type menuStaleJob struct {
	ProjectID string
	Kind      string
}

// decodeMenuStaleJob 解码并核验一个 menu_stale 任务。
//
// 核验三件事，缺一条就整笔失败（由 build worker 记 Failed，而不是静默跳过）：
//   - SourceID 形状合法（严格两段，首段 uuid）；
//   - 工程 id 与任务自己的 ProjectID 一致 —— 不一致说明任务被串了工程，
//     按哪个工程重建都会写到别人名下；
//   - kind 非空（取值白名单由模型层判，这里只挡空值，不自造第三份列表）。
//
// 任务的 InputVersion 是**任务序列号**（迁移 698，会被 retention 清理、会倒退），
// 与实体版本无关：这里不读它，更不拿它当「要不要重建」的判据。
func decodeMenuStaleJob(job *builddto.Job) (menuStaleJob, error) {
	if job == nil {
		return menuStaleJob{}, errMenuStaleJobNil
	}
	projectID, kind, ok := buildcontract.SplitMenuStaleSourceID(job.SourceID)
	if !ok {
		return menuStaleJob{}, fmt.Errorf("%w: %q", errMenuStaleSourceID, job.SourceID)
	}
	if !strings.EqualFold(strings.TrimSpace(job.ProjectID), projectID) {
		return menuStaleJob{}, fmt.Errorf("%w: 任务 %q / source_id %q", errMenuStaleProjectMismatch, job.ProjectID, projectID)
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return menuStaleJob{}, errMenuStaleKindMissing
	}
	return menuStaleJob{ProjectID: projectID, Kind: kind}, nil
}

// executeMenuStaleJob 执行一条 menu_stale 任务：同步严格重建受影响的页面与实例。
//
// 为什么用 Strict / Sync 版本而不是两条「尽力而为」的重建：
// 这里**本身就是队列消费侧**，把失败再推回队列或吞掉会让 worker 判定任务成功并
// MarkSucceeded —— 受影响页面于是永远停在旧菜单标签，而库里看不出任何异常。所以：
//   - 页面侧：先用**只读反查**取受影响 id（pagecontract.PageDependencyLookup.FindPagesByDependency，
//     命中口径与写侧的 MarkMenuStaleTx 逐字一致、且不碰 pages.stale 列），
//     再 RebuildStaleStrict 逐页严格重建，任一页失败即返回带页面 id 的错误；
//   - 规模：页面侧统一走 rebuildStaleInChunks，每块 ≤ menuStaleRebuildChunk
//     （= pagecontract.RebuildBatchCapacity，不写魔数）条 ——
//     直接一把塞给 RebuildStaleStrict 会触发它内部的「超 20 条入队 + ErrRebuildOverloaded」，
//     那条路径在本执行器里没有下一轮触发源（见 menuStaleRebuildChunk 的注释）；
//   - 实例侧：RebuildStaleSync 逐实例 RebuildInstance，任一失败即返回，永不再入队。
//
// 顺序与既有失效链一致（手工页面在前、自动发布实例在后）；两者互不依赖，换序不改结果。
func executeMenuStaleJob(ctx context.Context, job *builddto.Job,
	lookup pagecontract.PageDependencyLookup, rebuild menuStalePageRebuilder,
	instances presentationcontract.StaleRebuildPort) error {
	decoded, err := decodeMenuStaleJob(job)
	if err != nil {
		return err
	}
	// 只读反查（不标 stale、不写任何列）：依赖键的构造规则住在 pipeline.MenuKey 一处，
	// 装配层不自己拼 "menu:{project}:{kind}" —— 拼错的表现是「查不到受影响页面」，
	// 那与「本来就没有页面依赖它」在日志里长得一模一样。
	key := pipeline.MenuKey(decoded.ProjectID, decoded.Kind)
	refs, err := lookup.FindPagesByDependency(ctx, decoded.ProjectID, key.Kind, key.Key)
	if err != nil {
		// 查不出受影响集合 ≠ 没有受影响集合：当作失败，由 worker 记 Failed 留痕。
		return err
	}
	directIDs := make([]string, 0, len(refs))
	for _, ref := range refs {
		if id := strings.TrimSpace(ref.ID); id != "" {
			directIDs = append(directIDs, id)
		}
	}
	// 只按位置键反查覆盖不到按项引用：core.nav 绑定具体菜单项时登记的是
	// navigation:{itemID}（jetview.go 的 UseNavigation 分支），两种模式各记各的键，
	// 一个页面可以只依赖按项键、完全不依赖 menu:{project}:{kind}。导航 CRUD 改/删
	// 已有菜单项时会把这类页面标成 stale（markNavigationStaleInTx），本任务若只看
	// 位置键，就会「任务成功、按项页面永远 stale」—— 那是恢复不了的那一类。
	//
	// 收口方式不是另造队列，也不是给已有产物补依赖键（历史产物不会凭空多出一个键），
	// 而是把本工程当前**已标记 stale** 的页面整体并入这一轮：已上线与从未发布都要，
	// 判据取库里的 stale 列本身，而不是再猜一遍依赖键。位置键那条路径覆盖的是
	// 「本次改动的直接受影响面」，本段覆盖的是「库里已经承认该重建、但没被上面选中」，
	// 两者并集才是收敛；同一页重复出现在两个集合里由 RebuildStaleStrict 幂等吸收。
	staleIDs, serr := stalePageIDs(ctx, rebuild, decoded.ProjectID)
	if serr != nil {
		return serr
	}
	// 两个集合先取并集去重再分块：
	//  - 去重：同一页可能既被位置键选中、又已经在 stale 集里（导航写事务标过），重复送进
	//    RebuildStaleStrict 是白做一次编译（幂等但贵），并且会占掉块容量把真正的内容挤后；
	//  - 合并：位置键覆盖「本次改动的直接受影响面」，stale 集覆盖「库里已承认该重建、
	//    但没被位置键选中」（按项引用 navigation:{itemID} 正是这一类）。
	merged := mergePageIDs(directIDs, staleIDs)
	if err := rebuildStaleInChunks(ctx, rebuild, merged); err != nil {
		return err
	}
	if _, err := instances.RebuildStaleSync(ctx, decoded.ProjectID); err != nil {
		return err
	}
	return nil
}

// mergePageIDs 合并多个 id 序列并去重，保留**首次出现**的顺序。
//
// 顺序有含义：位置键命中的页面排在前（它们的失效是本次改动直接造成的），stale 集里
// 其余页面在后（历史欠账）。分块失败时先做完的正是最贴近本次改动的那批。
//
// 空串与纯空白一律丢弃：它们来自上游读库，脏 id 送进严格重建会变成一条「重建页面 "":
// 记录不存在」的失败，把整批拖下水 —— 过滤在这里做一次，比每个调用点各判一次可靠。
func mergePageIDs(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range groups {
		for _, raw := range group {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// menuStaleRebuildChunk 每块交给 RebuildStaleStrict 的页面数 = 页面模块公开的批容量。
//
// 为什么必须对齐 page 的 RebuildBatchCapacity（不是随手抄一个 20）：那个上限一旦被触发，
// 超出的页面会被 enqueueOverflowBuildJobs **入队**，而严格模式随后立刻返回
// ErrRebuildOverloaded —— 任务记 Failed、整批页面保持 stale。worker 的 MarkFailed 只是
// 终结态（build_worker.go:63-75 + build_model.go:313-329），失败任务**不会自动重试**
// （重试只有人工入口 build_service.go:239 Retry），而这条 menu_stale 任务已终止、不会再来
// 下一轮。也就是说「超限靠入队 + 下轮收敛」在本执行器上不成立：没有触发源。
//
// 分块后本执行器自己负责规模控制：每块 ≤ 批容量，永不触发溢出入队，也就不会把「重建
// 再入队」当成这次完成。块间不 sleep、不并发 —— 顺序执行让失败边界可复现。
//
// 常量取值来自 pagecontract（单一来源），装配层不另写魔数：page 改了容量这里自动跟上。
const menuStaleRebuildChunk = pagecontract.RebuildBatchCapacity

// rebuildStaleInChunks 按 menuStaleRebuildChunk 分块严格重建，首个失败即返回。
//
// 为什么不是一把梭 RebuildStaleStrict(全部 id)：那个端口内部会截断到 20 条并把余下的
// 入队，严格模式下再抛 ErrRebuildOverloaded —— 调用方看到的错误里没有「哪些页没重建」，
// 只有「超限」两个字；分块后每块只有 ≤20 条，任何失败都带着具体页面 id 返回。
//
// 失败即返回、不吞、不伪造成功：已重建的块保持重建结果（重建是幂等的），未走到的块
// 页面仍是 stale —— 这是可观测的事实，任务 Failed 留痕，由人工重试或下一次菜单变更
// 重新触发。绝不在这里把剩余块入队后返回 nil。
func rebuildStaleInChunks(ctx context.Context, rebuild menuStalePageRebuilder, ids []string) error {
	for start := 0; start < len(ids); start += menuStaleRebuildChunk {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + menuStaleRebuildChunk
		if end > len(ids) {
			end = len(ids)
		}
		if err := rebuild.RebuildStaleStrict(ctx, ids[start:end]); err != nil {
			return fmt.Errorf("菜单失效重建第 %d-%d 页（共 %d）: %w", start+1, end, len(ids), err)
		}
	}
	return nil
}

// menuStalePageRebuilder 执行器对页面模块的**全部**要求：严格重建 + 待重建 id 清单。
//
// 故意只到这两个方法，不要 pagecontract.PageService 聚合接口：
//   - 执行器真正调用的就只有这两处（RebuildStaleStrict 与 ListStalePageIDs），
//     收窄到使用面才让依赖可读，测试桩也不用为了编译去实现 Create/List 等无关方法
//     （用聚合接口时桩必须假装实现整张表，等于把依赖藏进 nil 方法里）；
//   - 生产端 pageservice.Service 天然满足（两个方法都是它的真实方法），
//     装配时从 pagecontract.PageService 上做一次能力断言即可，不需要聚合接口放开。
type menuStalePageRebuilder interface {
	RebuildStaleStrict(ctx context.Context, ids []string) error
	ListStalePageIDs(ctx context.Context, projectID string) (ids []string, err error)
}

// menuStaleStalePageLister 执行器对页面模块的**只读**要求：本工程已标记 stale 的页面 id。
//
// 为什么只要 id 列表：本文件的职责是「把库里承认待重建的页交给严格重建」，页面标题、
// 路径、失败阶段这些展示字段在这里没有消费者；要全量行等于让装配层去解析一份 dto，
// 多出来的每个字段都是一个会漂移的耦合面。
//
// 为什么工程必填：pages 带 FORCE 策略，空工程 id 在非超级角色下读出空集（fail closed），
// 而「列表空」会被当成「本来就没有待重建页面」—— 那是把装配缺陷读成业务正常态。
type menuStaleStalePageLister interface {
	ListStalePageIDs(ctx context.Context, projectID string) (ids []string, err error)
}

// stalePageIDs 本工程当前**已标记 stale** 的页面 id（已上线与从未发布都要）。
//
// 为什么不再按 published 过滤：是否已上线与「该不该重建」是两件事 —— 对按项引用
// （navigation:{itemID}）的产物，标 stale 的是导航写事务，它既可能落在已上线页面，
// 也可能落在草稿页；只看草稿集会漏掉前一半（表现是「任务 succeeded、线上仍是旧菜单」）。
// 判据取库里的 stale 列本身，而不是在装配层再推一遍「谁该被重建」。
//
// 为什么走页面模块的只读端口而不是自己查 pages：stale 列的读写口径（含 rebuild_failed_*
// 的清理与 FORCE RLS 作用域）住在 page 模块，装配层复制一份 SQL 迟早与那边分叉 ——
// 分叉的表现正是「清单里看着有、执行器却不动」。
//
// 失败即返回错误（不降级成空清单）：「读不到待重建集合」与「没有待重建页面」是两件事，
// 混在一起会让 worker 把一次读取失败记成成功。
func stalePageIDs(ctx context.Context, rebuild menuStalePageRebuilder, projectID string) ([]string, error) {
	lister, ok := rebuild.(menuStaleStalePageLister)
	if !ok {
		// 缺这个端口不是「本次没有页面要重建」，而是装配不完整：整条任务失败并留痕，
		// 否则菜单变更从此悄悄只重建实例、不重建页面。
		return nil, errors.New("页面模块未暴露待重建页面 id 清单（ListStalePageIDs）")
	}
	ids, err := lister.ListStalePageIDs(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// registerMenuStaleExecutor 注册 menu_stale 执行器（必须在 StartWorkers 之前调用）。
//
// 端口不齐即 panic：执行器注册是构造参数式接线，没有「后补」的机会 —— 一旦 worker
// 先跑起来，缺执行器的任务会被判 ErrExecutorMissing 直接 Failed 且不重试。
func registerMenuStaleExecutor(buildSvc buildcontract.BuildService,
	pageSvc pagecontract.PageService, presentationSvc presentationcontract.PresentationService) {
	lookup, ok := pageSvc.(pagecontract.PageDependencyLookup)
	if !ok {
		panic("页面模块未实现按依赖只读反查（PageDependencyLookup）")
	}
	instances, ok := presentationSvc.(presentationcontract.StaleRebuildPort)
	if !ok {
		panic("自动发布实例模块未实现同步重建端口（StaleRebuildPort）")
	}
	// 执行器只要两个方法：严格重建 + 待重建 id 清单。二者缺任一即装配缺陷 ——
	// 少了 ListStalePageIDs，菜单变更就会「只重建位置键命中的页面、按项引用的页面永远 stale」；
	// 少了 RebuildStaleStrict，位置键那批直接无从落地。所以断言而非降级。
	rebuilder, ok := pageSvc.(menuStalePageRebuilder)
	if !ok {
		panic("页面模块未同时提供严格重建与待重建 id 清单（RebuildStaleStrict + ListStalePageIDs）")
	}
	buildSvc.RegisterExecutor(buildmodel.SourceTypeMenuStale,
		func(ctx context.Context, job *builddto.Job) error {
			return executeMenuStaleJob(ctx, job, lookup, rebuilder, instances)
		})
}
