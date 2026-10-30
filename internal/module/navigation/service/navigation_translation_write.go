package navigationservice

// navigation_translation_write.go — 导航译文整表提交（事务化唯一流转，owner A / BE-NAV-WRITE）。
//
// 这一条连线替换掉此前 inbound/http 里三段自动提交的 writeTranslations：
//
//	旧：验工程（projects.List）→ 读来源 → 比现值（LoadDetails）→ Upsert（自动提交）→
//	    pages.MarkStaleForI18n（自动提交）→ menus.InvalidateMenuLabels（自开作用域 + go 重建）
//
// 旧路径的三个问题（都是静默不收敛，不是性能问题）：
//  1. 三段写各自提交：译文写成功、标记失败 → 「保存成功」的回执是真的，但产物永远停在旧标签；
//  2. 失效粒度是**全站**（MarkStaleForI18n）且位置靠 handler 自己拼 —— 移动端两个位置
//     既不在索引里、也不在派发里，改完页眉移动菜单看不到变化；
//  3. 派发发生在提交之后、由 handler 再调一次业务服务：本批主审已否决这种「commit 后
//     再派发」的形态（失败既不能回滚也不能如实回报）。
//
// 现在的顺序（每一步都是判据，顺序不可交换，逐步理由见行间注释）：
//
//	1  工程存在 + 语言合法                                → 400
//	2  删空 target 行（**不查来源、不计数**）；删完为空 → 200 written:0，**不开事务、零派发**
//	3  语境不对                                          → 400（逐条明细）
//	4  重复寻址（同 hash + 同语境出现两次）              → 409（明细两行的差异）
//	5  BEGIN → rls.ScopeTx(canonical)
//	6  内容写锁 LockProjectContentWrite                  （串行化同工程的译文写）
//	7  严格来源索引 OpenSourceIndex + Require()          （任一位置读失败即整笔失败）
//	8  未知 hash / 不可翻译                              → 400 / 409（整批拒绝，零写入）
//	9  LoadDetailsForProjectTx 读现值（工程优先、全局回落）
//	10 比对：同值且 engine=manual → 该行不改动
//	11 UpsertTx（ProjectID = canonical，单工程）
//	12 逐 kind：页标记 → 实例标记 → 可靠入队（**同一事务**）
//	13 COMMIT
//	14 返回 {Written, ChangedKinds}
//
// 全成或全败：3–12 任一步失败都回滚整笔（译文零写入、stale 不变、无任务），
// 绝不存在「保存了但没派发」或「派发了但没保存」的中间态。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
)

// TranslationWriter 译文整表写入的编排者（独立构造器注入依赖，不改既有 Service）。
//
// 为什么独立于 Service：
//   - Service 的构造器由装配层在很早的位置调用（NewService(m, projects)），本批不改它的
//     签名（否则所有调用点都要动，且与其它 owner 在途改动冲突）；
//   - 这条流转依赖**三个别的模块**的能力（页/实例标记、队列入队）与一个链路上不需要的
//     能力（来源索引）—— 把它们塞进 Service 的字段，会让「导航项 CRUD」也持有一堆用不到
//     的端口，装配期自检也就说不清「这个能力被谁用」。
//
// 注入的能力都是**消费者侧最小形状**：
//   - 本包内的 sourceIndexer（OpenSourceIndex）与 rowSource：来源索引的执行者；
//   - navigationcontract 的三个端口：跨模块只走 contract，不 import 别人的 service/model；
//   - i18n 的写能力用本文件内部的窄接口包一层，便于单测注入失败。
type TranslationWriter struct {
	// rows 菜单位置的行读取（生产由 model 提供；单测注入桩）。
	rows RowSourceTx
	// sources 共享来源索引（生产由 *Service.OpenSourceIndex 提供）。
	sources SourceIndexerTx
	// projects 工程清单（生产由 projectcontract.ProjectService 提供）。
	projects ProjectLister
	// content 译文存储（工程偏好读取、写锁、UPSERT）。
	content ContentStore
	// pages 手工页面侧的按位置失效（装配层注入；缺 = 装配缺陷，写事务里 fail closed）。
	pages navigationcontract.PageStalePort
	// instances 自动发布实例侧的按位置失效（同上）。
	instances navigationcontract.PresentationStalePort
	// queue 可靠重建任务入队（同上）。
	queue navigationcontract.MenuStaleEnqueuePort
	// tx 事务边界（生产由 navigation model 提供）。
	tx TxRunner
	// now 注入时钟（单测断言「标记时刻」用）。
	now func() time.Time
}

// TranslationWriterDeps 构造依赖（显式结构而不是长参数表：装配处一眼看得出缺哪一根）。
type TranslationWriterDeps struct {
	// Rows 菜单位置行读取（通常 navigationmodel.NewModel(db)）。
	Rows RowSourceTx
	// Sources 来源索引执行者（通常 *Service）。
	Sources SourceIndexerTx
	// Projects 工程清单（通常 projectcontract.ProjectService）。
	Projects ProjectLister
	// Content 译文存储（通常 i18n.NewContentWriter(db)）。
	Content ContentStore
	// Pages / Instances / Queue 三个跨模块端口（装配层注入；为 nil 时写操作 fail closed）。
	Pages     navigationcontract.PageStalePort
	Instances navigationcontract.PresentationStalePort
	Queue     navigationcontract.MenuStaleEnqueuePort
	// Tx 事务边界（通常 navigationmodel.NewModel(db)）。
	Tx TxRunner
	// Now 可选时钟（nil = time.Now）。
	Now func() time.Time
}

// NewTranslationWriter 构造译文写入编排者。
//
// 不返回 error、不做自检：缺依赖的判定发生在**调用时**（fail closed + 一条错误日志），
// 因为装配顺序在某些测试里是先构造后注入，构造期强校验会让那种用法变成构造失败，
// 而症状（「构造不了」）比「写入时明确拒绝」更难定位。
func NewTranslationWriter(deps TranslationWriterDeps) *TranslationWriter {
	w := &TranslationWriter{
		rows: deps.Rows, sources: deps.Sources, projects: deps.Projects, content: deps.Content,
		pages: deps.Pages, instances: deps.Instances, queue: deps.Queue, tx: deps.Tx, now: deps.Now,
	}
	if w.now == nil {
		w.now = func() time.Time { return time.Now().UTC() }
	}
	return w
}

// 编译期断言：HTTP 层与装配层只依赖 contract 的形状。
var _ navigationcontract.TranslationWriteService = (*TranslationWriter)(nil)

// —— 端口（消费者侧最小形状）——

// RowSourceTx 某工程某位置的菜单行（生产走 model.ListTx，**事务内**读）。
//
// 导出是为了让装配层能把 model 包进来（见 NewNavigationRowsTx）：接口方法仍然只有一个，
// 具体类型仍是包内私有 —— 装配层只拿到「能读事务内菜单行」这一条能力。
type RowSourceTx interface {
	// ListForKindTx 在调用方事务内按工程 + 位置列出菜单行。
	ListForKindTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (rows []*navigationmodel.NavigationEntity, err error)
}

// rowSource 本包内使用的同一条端口（别名，避免把导出名带进每个内部位置）。
type rowSource = RowSourceTx

// SourceIndexerTx 共享来源索引（生产由 *Service.OpenSourceIndexTx 提供）。
//
// 为什么索引要接受一个 tx 参数：索引本身由 Service 实现，而既有 OpenSourceIndex 的 model 读
// 是「自开作用域」的（rls.InProjectScope 会新开事务、另取连接）。本文件要求索引在**调用方
// 事务内**读到同一份快照，所以把它收敛成 Tx 形状 —— 见 *Service.OpenSourceIndexTx。
type SourceIndexerTx interface {
	OpenSourceIndexTx(ctx context.Context, tx *gorm.DB, projectID string) (navigationcontract.SourceIndex, error)
}

// ProjectLister 工程清单（只借「列 id」这一个只读能力，与 Service.projects 同源）。
type ProjectLister interface {
	List(ctx context.Context) (res []ProjectEntry, err error)
}

// ProjectEntry 工程条目的最小形状（只用 ID）。
//
// 为什么不用 projectdto.ProjectResp：那会让 navigation/service 直接依赖 project 模块的
// dto（越级）。装配层用 NewProjectLister 把 []projectdto.ProjectResp 映射成本形状。
type ProjectEntry struct {
	ID string
}

// ProjectListerFunc 把「列工程」的一个函数变成端口（装配层内联适配，省一个文件）。
type ProjectListerFunc func(ctx context.Context) ([]ProjectEntry, error)

// List 实现 ProjectLister。
func (f ProjectListerFunc) List(ctx context.Context) ([]ProjectEntry, error) { return f(ctx) }

// ContentStore 译文存储（pkg/i18n ContentWriter 的窄切片；*i18n.ContentWriter 直接满足）。
type ContentStore interface {
	// LoadDetailsForProjectTx 事务内读现值（工程行优先、全局行回落）。
	LoadDetailsForProjectTx(ctx context.Context, tx *gorm.DB, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	// UpsertTx 事务内 UPSERT（单工程，绝不混工程）。
	UpsertTx(ctx context.Context, tx *gorm.DB, items []i18n.ContentWriteItem) (written int, err error)
}

// TxRunner 事务边界（生产由 navigationmodel.Model 提供；*navigationmodel.Model 直接满足）。
type TxRunner interface {
	// Transaction 开启事务并执行 fn；fn 返回 error 即回滚。
	Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
}

// —— 唯一入口 ——

// WriteTranslations 整表提交译文：校验 → 锁 → 来源 → 现值 → 写入 → 三处失效，全成或全败。
//
// 返回的回执只含事实（Written / ChangedKinds），不拼成品句：文案由 pkg/response 在出口
// 按请求语言取词。
func (w *TranslationWriter) WriteTranslations(ctx context.Context, req *navigationcontract.TranslationWriteReq) (*navigationcontract.TranslationWriteResult, error) {
	if w == nil {
		return nil, navigationenums.ErrTranslationWriteUnavailableError()
	}
	if req == nil {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	projectID, lang := strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.Lang)
	// 归一成 uuid 规范形式：工程 id 的大小写变体进 uuid 列就是两个工程，而装配层与
	// 其它 owner 传的是规范形式 —— 不归一会出现「锁的是 A、写的是 a」。
	canonical, perr := uuid.Parse(projectID)
	if perr != nil {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	projectID = canonical.String()
	// 语言白名单：写进一个不存在的语言码会得到一行永远不参与构建的译文 ——
	// 库里看得到、页面上看不到，排查成本极高。
	if !navigationTranslationLangSupported(lang) {
		return nil, facing.WithStatus(navigationenums.Err(navigationenums.ErrInvalidParam), http.StatusBadRequest)
	}

	// 1. 需要的依赖在**动库之前**逐条检查：缺一个就整体拒绝，不去半路才发现。
	if err := w.requireDeps(); err != nil {
		return nil, err
	}

	// 2. 工程必须真实存在：否则来源索引会读出一份空集合，每一行都被判成「原文已变化」，
	//    用户看到的错误指向错误的原因（「刷新后重试」永远修不好）。
	if err := w.requireProject(ctx, projectID); err != nil {
		return nil, err
	}

	// 3. 空 target = 本次不改这一行：**先丢掉，再去查来源**。反过来的话，一条「清空输入」的
	//    行会因为原文已删而让整批判失败 —— 而它本来什么都不打算改。
	kept := make([]navigationcontract.TranslationWriteRow, 0, len(req.Rows))
	for _, row := range req.Rows {
		target := strings.TrimSpace(row.Target)
		if target == "" {
			continue
		}
		kept = append(kept, navigationcontract.TranslationWriteRow{
			Context: strings.TrimSpace(row.Context), Hash: strings.TrimSpace(row.Hash), Target: target,
		})
	}
	if len(kept) == 0 {
		// 零改动：不开事务、不读来源、不派发。回执 written:0 是**事实**而不是「跳过」。
		return &navigationcontract.TranslationWriteResult{Written: 0, ChangedKinds: []string{}}, nil
	}

	// 4. 语境与重复：两条都在**开事务前**判 —— 它们只依赖请求本身，不需要任何库内事实。
	contextName := navigationLabelContext()
	if err := validateContexts(kept, contextName); err != nil {
		return nil, err
	}
	if err := rejectDuplicates(kept, contextName); err != nil {
		return nil, err
	}

	var result *navigationcontract.TranslationWriteResult
	err := w.tx.Transaction(ctx, func(tx *gorm.DB) error {
		// 5. 作用域：navigations / sys_translation 都是 FORCE RLS 表，无作用域的写会静默落空。
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		// 6. 内容写锁：把同工程的译文写串行化（键在 pkg/i18n 内归一成规范 uuid）。
		//    它只覆盖**译文写**，不覆盖菜单 CRUD —— 并发语义见文件末尾的说明。
		if lerr := i18n.LockProjectContentWrite(ctx, tx, projectID); lerr != nil {
			return lerr
		}
		// 7. 严格来源索引：任一位置读失败即整笔失败（Require）。
		index, ierr := w.sources.OpenSourceIndexTx(ctx, tx, projectID)
		if ierr != nil {
			return ierr
		}
		if rerr := index.Require(); rerr != nil {
			return rerr
		}

		// 8. 逐行解析来源：未知 hash / 不可翻译都整批拒绝（AGENTS.md：批量要么全成要么全败，
		//    不推算 skipped、不返回逐条回执）。
		items := make([]i18n.ContentWriteItem, 0, len(kept))
		keys := make([]string, 0, len(kept))
		kindsOf := make([][]string, 0, len(kept))
		if verr := validateSources(kept, contextName, projectID, lang, index, &items, &keys, &kindsOf); verr != nil {
			return verr
		}

		// 9. 事务内读现值（工程行优先、全局行回落 —— 口径由 pkg/i18n 的 SQL 决定，这里不复制）。
		hashes := make([]string, 0, len(items))
		for _, it := range items {
			hashes = append(hashes, it.SourceHash)
		}
		before, berr := w.content.LoadDetailsForProjectTx(ctx, tx, projectID, lang, hashes)
		if berr != nil {
			return berr
		}

		// 10. 比对：同值且已经是 manual 的行**不改动**（不计 written、不派发失效）。
		//     注意判据是「值相同 **且** 引擎是 manual」：值相同但引擎是 machine/orphan 时
		//     仍然要写 —— 那是「把机器译文确认为人工译文」，是一次真实变化。
		pending := make([]i18n.ContentWriteItem, 0, len(items))
		changedKinds := make([]string, 0, len(navigationSourceKinds))
		seenKind := make(map[string]bool, len(navigationSourceKinds))
		for i, item := range items {
			prev := before[keys[i]]
			if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
				continue
			}
			pending = append(pending, item)
			for _, kind := range kindsOf[i] {
				if !seenKind[kind] {
					seenKind[kind] = true
					changedKinds = append(changedKinds, kind)
				}
			}
		}
		if len(pending) == 0 {
			// 全部与库内一致：不写、不标、不入队（回执 written:0）。事务照常提交（空提交）。
			result = &navigationcontract.TranslationWriteResult{Written: 0, ChangedKinds: []string{}}
			return nil
		}

		// 11. UPSERT：ProjectID 逐条给规范形式（pkg/i18n 的归一要求单工程一致）。
		written, uerr := w.content.UpsertTx(ctx, tx, pending)
		if uerr != nil {
			return uerr
		}

		// 12. 三处失效与译文**同一事务**：页标记 → 实例标记 → 可靠入队。
		//     kind 按规范顺序输出，保证回执与日志跨次调用字节级一致。
		kinds := sortKinds(changedKinds)
		at := w.now()
		for _, kind := range kinds {
			if _, merr := w.pages.MarkMenuStaleTx(ctx, tx, projectID, kind, at); merr != nil {
				return merr
			}
			if _, merr := w.instances.MarkInstanceMenuStaleTx(ctx, tx, projectID, kind, at); merr != nil {
				return merr
			}
			if _, qerr := w.queue.EnqueueMenuStaleTx(ctx, tx, projectID, kind); qerr != nil {
				return qerr
			}
		}
		result = &navigationcontract.TranslationWriteResult{Written: written, ChangedKinds: kinds}
		return nil
	})
	if err != nil {
		// 归口：业务错误原样上抛（key + 兜底 + 状态码由声明处给），
		// 其余（表名 / SQLSTATE / 约束名）只进日志、对外统一成「译文保存失败」。
		return nil, w.classify(ctx, projectID, lang, err)
	}
	if result == nil {
		return &navigationcontract.TranslationWriteResult{Written: 0, ChangedKinds: []string{}}, nil
	}
	logger.SceneCtx("navigation", ctx).With("project", projectID).With("lang", lang).
		With("written", result.Written).With("kinds", strings.Join(result.ChangedKinds, ",")).
		Info("导航译文已写入并同事务派发失效")
	return result, nil
}

// classify 把事务内返回的错误分成三类：业务拒绝（原样）、存储失败（归口 500）、其余（原样）。
func (w *TranslationWriter) classify(ctx context.Context, projectID, lang string, err error) error {
	var blocked *TranslationRejectedError
	if errors.As(err, &blocked) {
		// 逐条拒绝（不可翻译）：明细已经在错误里，出口会放进 data.items。
		return err
	}
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		// 声明处给过 key 的业务错误（未知 hash / 语境非法 / 来源读取失败 / 锁参数非法）。
		// 判据走 facing.FactOf 而不是 facing.IsBusinessError：后者只看错误文本的**形态**，
		// 而 wrapped 的 fmt.Errorf 也会命中形态判据 —— 那正是要归口成「保存失败」的那一类。
		return err
	}
	logger.SceneCtx("navigation", ctx).With("project", projectID).With("lang", lang).
		Error(err, "导航译文写入失败，事务已回滚")
	return navigationenums.ErrTranslationWriteFailedError()
}

// requireDeps 逐条检查写路径必需的依赖。
func (w *TranslationWriter) requireDeps() error {
	missing := make([]string, 0, 7)
	if w.projects == nil {
		missing = append(missing, "projects")
	}
	if w.rows == nil {
		missing = append(missing, "rows")
	}
	if w.sources == nil {
		missing = append(missing, "sources")
	}
	if w.content == nil {
		missing = append(missing, "content")
	}
	if w.pages == nil {
		missing = append(missing, "pages")
	}
	if w.instances == nil {
		missing = append(missing, "instances")
	}
	if w.queue == nil {
		missing = append(missing, "queue")
	}
	if w.tx == nil {
		missing = append(missing, "tx")
	}
	if len(missing) == 0 {
		return nil
	}
	// 装配缺陷：重试永远不会成功，所以给「未就绪」而不是「保存失败」——
	// 后者会让运维照着「请稍后重试」去等一个直到发版才会消失的问题。
	logger.Scene("navigation").With("missing", strings.Join(missing, ",")).
		Warn("导航译文写服务装配不全，拒绝写入")
	return navigationenums.ErrTranslationWriteUnavailableError()
}

// requireProject 工程必须真实存在。
func (w *TranslationWriter) requireProject(ctx context.Context, projectID string) error {
	list, err := w.projects.List(ctx)
	if err != nil {
		// 读不到工程表 ≠ 工程不存在：显式失败，不伪装成参数错。
		logger.SceneCtx("navigation", ctx).With("project", projectID).Error(err, "读取工程清单失败，无法校验译文目标工程")
		return navigationenums.ErrTranslationWriteFailedError()
	}
	for _, p := range list {
		if strings.EqualFold(strings.TrimSpace(p.ID), projectID) {
			return nil
		}
	}
	return facing.WithStatus(navigationenums.Err(navigationenums.ErrInvalidParam), http.StatusBadRequest)
}

// validateContexts 语境必须与导航标签的固定语境一致（整批拒绝，逐条明细）。
func validateContexts(rows []navigationcontract.TranslationWriteRow, contextName string) error {
	items := make([]facing.BlockedItem, 0, len(rows))
	for _, row := range rows {
		if row.Context == contextName {
			continue
		}
		// 语境错了不知道原文（hash 可能仍然合法，但不值得为它再读一次来源）：
		// name 回落成 hash —— 运维至少能定位到那一行。
		items = append(items, facing.BlockedItem{
			ID: row.Hash, Name: row.Hash,
			ReasonKey: navigationenums.ErrContextInvalid,
			Fallback:  navigationenums.FallbackFor(navigationenums.ErrContextInvalid),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return &TranslationRejectedError{
		Err:   facing.WithStatus(navigationenums.Err(navigationenums.ErrContextInvalid), http.StatusBadRequest),
		Items: items,
	}
}

// rejectDuplicates 同一批里同 hash + 同语境只能出现一次。
//
// 为什么拒绝而不是「后者覆盖前者」：两条不同的译文指向同一格数据时，最终写哪一条只能
// 由提交顺序决定，而顺序对运营不可见 —— 症状是「我填的译文被另一行盖掉了」，日志里
// 什么冲突都没有。明细给出**两行的译文**，让人工自己裁决。
func rejectDuplicates(rows []navigationcontract.TranslationWriteRow, contextName string) error {
	seen := make(map[string]int, len(rows)) // 索引键 → 首次出现的下标
	items := make([]facing.BlockedItem, 0, len(rows))
	for i, row := range rows {
		key := i18n.ContentIndexKey(row.Hash, contextName)
		first, ok := seen[key]
		if !ok {
			seen[key] = i
			continue
		}
		// name 给两行译文的对比：运营看到「Home / Start」才知道该删哪一行。
		items = append(items, facing.BlockedItem{
			ID:        row.Hash,
			Name:      fmt.Sprintf("%s / %s", rows[first].Target, row.Target),
			ReasonKey: navigationenums.ErrTranslationDuplicate,
			Fallback:  navigationenums.FallbackFor(navigationenums.ErrTranslationDuplicate),
		})
	}
	if len(items) == 0 {
		return nil
	}
	return &TranslationRejectedError{Err: navigationenums.ErrTranslationDuplicateError(), Items: items}
}

// validateSources 逐行按 hash 回查来源，产出待写项与「每行涉及的 kind」。
//
// 三条判据（顺序即语义）：
//  1. 未知 hash → 400：**绝不采信请求里带的文本**（客户端能发的只有 hash + 译文），
//     原文一律由服务端算；
//  2. 不可翻译（纯数字 / 纯符号 / 空）→ 409：请求形状合法，是库内菜单状态与断言不一致，
//     该刷新工作台去掉那一行，而不是去改请求形状；
//  3. kind 取该原文**出现过的全部位置**（同一段文字可以同时在页眉与页脚）——
//     只看提交行所在的位置会漏派发，症状是「保存成功、另一个位置的实例停在旧标签上」。
func validateSources(rows []navigationcontract.TranslationWriteRow, contextName, projectID, lang string,
	index navigationcontract.SourceIndex, items *[]i18n.ContentWriteItem, keys *[]string, kindsOf *[][]string) error {
	var unknown []facing.BlockedItem
	var untranslatable []facing.BlockedItem
	for _, row := range rows {
		entry, ok := index.Locate(row.Hash)
		if !ok {
			// 未知 hash 无原文：name 用 hash（64 位指纹本身可定位到构建日志）。
			unknown = append(unknown, facing.BlockedItem{
				ID: row.Hash, Name: row.Hash,
				ReasonKey: navigationenums.ErrSourceChanged,
				Fallback:  navigationenums.FallbackFor(navigationenums.ErrSourceChanged),
			})
			continue
		}
		if !i18n.ShouldTranslateContent(entry.Source) {
			untranslatable = append(untranslatable, facing.BlockedItem{
				ID: entry.Hash, Name: sourceNameOrHash(entry.Source, entry.Hash),
				ReasonKey: navigationenums.ErrNotTranslatable,
				Fallback:  navigationenums.FallbackFor(navigationenums.ErrNotTranslatable),
			})
			continue
		}
		*items = append(*items, i18n.ContentWriteItem{
			// ProjectID 必填：空串 = 全局共享行。译文必须落在本工程（「本工程优先、
			// 全局回落」的读口径要求写侧明确落在自己那一层），传给 upsert 后由 pkg/i18n
			// 按组设作用域；写全局行会让一个工程的译文泄漏到所有站点。
			ProjectID:  projectID,
			SourceHash: entry.Hash, Context: contextName, Lang: lang,
			SourceText: entry.Source, TargetText: row.Target, Engine: i18n.ContentEngineManual,
		})
		*keys = append(*keys, i18n.ContentIndexKey(entry.Hash, contextName))
		// Kinds 已经按规范顺序去重（索引保证）：这里只做拷贝，不外泄索引内部切片。
		*kindsOf = append(*kindsOf, append([]string{}, entry.Kinds...))
	}
	// 未知 hash 先判（它比「不可翻译」更根本：没有原文就谈不上可翻译性）。
	if len(unknown) > 0 {
		return &TranslationRejectedError{
			Err:   facing.WithStatus(navigationenums.Err(navigationenums.ErrSourceChanged), http.StatusBadRequest),
			Items: unknown,
		}
	}
	if len(untranslatable) > 0 {
		return &TranslationRejectedError{
			Err:   navigationenums.ErrNotTranslatableRejected(),
			Items: untranslatable,
		}
	}
	return nil
}

// sourceNameOrHash 明细里的人读名字：有服务端原文就用原文，没有就回落到 hash。
func sourceNameOrHash(source, hash string) string {
	if s := strings.TrimSpace(source); s != "" {
		return s
	}
	return hash
}

// navigationTranslationLangSupported 目标语言必须在受支持的语言集合里。
//
// 与 inbound/http 的 navigationLangSupported 同源（同一份 i18n.AvailableLangs），
// 这里再写一遍而不是引用它：inbound 属于出口层，service 引它会让依赖方向倒挂。
func navigationTranslationLangSupported(lang string) bool {
	for _, code := range i18n.AvailableLangs() {
		if code == lang {
			return true
		}
	}
	return false
}

// sortKinds 按 kind 规范顺序排序（未登记的排在最后，保证全序）。
func sortKinds(kinds []string) []string {
	out := append([]string{}, kinds...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && navigationKindRank(out[j]) < navigationKindRank(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
