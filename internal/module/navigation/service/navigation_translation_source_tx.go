package navigationservice

// navigation_translation_source_tx.go — 共享来源索引的**事务内**变体（写路径专用）。
//
// 为什么需要它：`OpenSourceIndex` 的行读取走 `model.List` → `rls.InProjectScope`，后者会
// **新开一个事务、另取一个连接**（见 pkg/rls），读到的是与调用方写事务无关的另一个快照。
// 写路径要求「同一连接、同一快照」，所以这里换一个行来源，规则一条不改。
//
// 与 OpenSourceIndex 的差别只有「行从哪读」：四 kind 全收集、失败整 kind 丢弃、hash 唯一、
// 节点事实、确定性排序全部走同一份实现（navigation_translation_source.go 的
// buildSourceIndex / collectKindSourceEntries）。本文件不复制这套算法。
//
// 层归属：service，只读事实，不取词、不拼句。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
)

// navigationRowsModelTx 生产实现：事务内直接转发 model.ListTx（与 Service.Tree 的读法同源，
// 只是换了连接）。它实现 TransactionWriterDeps.Rows 的形状，装配层用它就够了。
type navigationRowsModelTx struct {
	m *navigationmodel.Model
}

// ListForKindTx 按工程 + 位置在调用方事务内列出菜单行。
func (r navigationRowsModelTx) ListForKindTx(ctx context.Context, tx *gorm.DB, projectID, kind string) ([]*navigationmodel.NavigationEntity, error) {
	return r.m.ListTx(ctx, tx, projectID, kind)
}

// 编译期护栏：生产实现满足读写两边的端口形状。
var (
	_ sourceRowsTxPort = navigationRowsModelTx{}
	_ rowSource        = navigationRowsModelTx{}
)

// NewNavigationRowsTx 把 model 包成事务内行端口（装配层经 TranslationWriterDeps.Rows 注入）。
//
// 返回接口而不是具体类型：具体类型是包内私有的，装配层只该看到「能读事务内菜单行」
// 这一个能力，不该拿到可以随手转成别的东西的句柄。
func NewNavigationRowsTx(m *navigationmodel.Model) RowSourceTx { return navigationRowsModelTx{m: m} }

// sourceRowsTxPort 事务内行读取端口（生产由 model 提供；单测注入桩）。
//
// 它是 Service 上的一个**独立**私有字段：既有的 sourceRowsPort 被索引读路径与
// navigation_translation_source_test.go 共用，改它的语义会连带改读侧行为。
type sourceRowsTxPort interface {
	// ListForKindTx 在调用方事务内按工程 + 位置列出菜单行（sort_order 升序，同序按 id 升序）。
	ListForKindTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (rows []*navigationmodel.NavigationEntity, err error)
}

// OpenSourceIndexTx 在**调用方事务内**打开某工程四个菜单位置的共享来源索引。
//
// 与 OpenSourceIndex 的差别只有「行从哪读」：全部规则、全部错误分档、全部确定性保证
// 都是同一套判定（见 navigation_translation_source.go 的 7 条语义），这里逐条照搬。
//
// 并发语义（必须如实说清楚，不能在实现里假设它被覆盖）：
//   - 本方法只在**写路径的内容写锁之内**使用（i18n.LockProjectContentWrite 是事务级
//     advisory lock，随 COMMIT / ROLLBACK 释放）；
//   - 该锁**不**覆盖菜单 CRUD（导航项的增删改排序走自己的自动提交事务）。
//     因此「锁内读到的来源」是**本次读的快照**，不是「直到提交都不变」的承诺：
//     READ COMMITTED 下每个语句看到语句开始时已提交的数据，事务内多条语句之间会看到
//     别的事务新提交的行；
//   - 真实后果可控：来源变了（标题被改 / 菜单项被删），本次写仍落在那段**旧原文**的
//     哈希上 —— 那一行译文成为孤儿（源文本不再出现在任何产物里），而**不会**被错误地
//     套到新文字上（寻址是 hash，不是位置）；
//   - 反过来，若来源在「读之后、写之前」新增了**同 hash 的另一个位置**，本次派发会少标
//     那个位置一次 stale —— 那个位置的首次构建本来就会读到新菜单，且菜单侧写操作自己
//     也会派发一次 menu 失效（navigation 的 staleMenu 端口），不会停在旧字节。
//
// 想彻底消除这两条，需要「菜单 CRUD 也进同一把锁」或「提交前复验来源版本」—— 两者都超出
// 本批范围（见报告「需接线项」），所以这里**不宣称**强一致，只保证不产生错误展示。
func (s *Service) OpenSourceIndexTx(ctx context.Context, tx *gorm.DB, projectID string) (navigationcontract.SourceIndex, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if s == nil || tx == nil {
		// 未装配的看板：显式失败，绝不返回一份「看起来是空的」索引
		//（空索引会让写侧把每一行都判成「原文已变化」，症状指向错误的原因）。
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	if s.rowsTx == nil {
		// 行端口是**装配缺陷**，不是「四个位置都读不到」：不预先判会把同一个装配错误
		// 复述成 ErrSourceKindFailed: header, header_mobile, footer, footer_mobile ——
		// 那是「每次读都失败」的业务结论，真实原因是这个 Service 没有行来源。
		// 分档必须让调用方一眼看出该去查装配，而不是去查数据库或数据。
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	// 与 OpenSourceIndex 逐字相同的构造：nodes 必须一起初始化（第 111 行会无条件
	// append），否则写路径的索引没有节点事实、SourcesIndexNodes 断言拿到空表。
	index := &sourceIndex{
		byHash:   map[string]int{},
		nodes:    map[string][]navigationcontract.NavigationNodeRef{},
		kinds:    []string{},
		failures: []navigationcontract.KindReadError{},
	}
	contextName := navigationLabelContext()
	for _, kind := range navigationSourceKinds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		collected, err := s.collectKindSourcesTx(ctx, tx, projectID, kind, contextName)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				// 取消 / 超时不是「这个位置读不到」：整体中止，不让写侧当成业务失败去重试。
				return nil, cerr
			}
			sourceKindFailure(ctx, projectID, kind, err)
			index.failures = append(index.failures, navigationcontract.KindReadError{
				Kind: kind, Err: navigationenums.ErrSourceKindDetail(kind),
			})
			// 失败位置的残缺条目一概不并入（整 kind 丢弃）。
			continue
		}
		index.kinds = append(index.kinds, kind)
		for _, entry := range collected {
			index.merge(ctx, entry, kind, projectID)
			if entry.Hash != "" {
				// 节点引用与读侧同一条规则：hash 非空才建条目（空 hash 是「无文字菜单项」
				// 的指纹，照样入 entries，但不让读侧按空串取到一堆节点）。
				index.nodes[entry.Hash] = append(index.nodes[entry.Hash], navigationcontract.NavigationNodeRef{
					ID: entry.NodeID, Kind: kind, Path: entry.Path,
				})
			}
		}
	}
	if len(index.kinds) == 0 {
		return nil, index.Require()
	}
	index.sortEntries()
	return index, nil
}

// collectKindSourcesTx 事务内读一个位置的完整菜单树，做严格来源解析，产出该位置的**全部**条目。
//
// 与 collectKindSources 逐条同义（同一「参与翻译」口径：**不**在这里提前过滤不可翻译行、
// 空标题照样产出一条带空串 hash 的条目），只有行来源不同。
func (s *Service) collectKindSourcesTx(ctx context.Context, tx *gorm.DB, projectID, kind, contextName string) ([]navigationcontract.TranslationSourceEntry, error) {
	nodes, err := s.sourceNodesStrictTx(ctx, tx, projectID, kind)
	if err != nil {
		return nil, err
	}
	// 条目拼装与读侧共用 kindEntriesOf：同一段规则只有一份实现（NodeID / Path / hash /
	// 「不提前过滤不可翻译行」全部同源），本函数只负责「行从哪个事务读」。
	return kindEntriesOf(ctx, nodes, kind, contextName)
}

// sourceNodesStrictTx 事务内读一个位置的菜单树并严格解析来源标题。
//
// 严格口径与 sourceNodesStrict 相同：任一节点解析失败即整份返回 error（构建期
// resolveSourceTitles 的宽容语义**不属于**控制面写完整性）。解析器未装配时沿用 Tree 的语义
// （标题保持记录自身），不算这个位置的读取失败。
func (s *Service) sourceNodesStrictTx(ctx context.Context, tx *gorm.DB, projectID, kind string) ([]*navigationdto.NavigationNode, error) {
	rows := s.rowsTx
	if rows == nil {
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	list, err := rows.ListForKindTx(ctx, tx, projectID, kind)
	if err != nil {
		return nil, err
	}
	nodes := navNodesOfRows(list)
	if s.sources == nil {
		return nodes, nil
	}
	if rerr := s.resolveSourceTitlesStrict(ctx, projectID, nodes); rerr != nil {
		return nil, rerr
	}
	return nodes, nil
}
