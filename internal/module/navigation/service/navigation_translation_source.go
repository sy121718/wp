package navigationservice

// navigation_translation_source.go — 共享来源索引：四个菜单位置各自的「原文 → 译文寻址」事实。
//
// 为什么要有它：译文工作台的读侧（navigation_page_handle.go 的 build）与写侧
// （navigation_translations_handle.go 的 writeTranslations）此前各走一遍 Tree 再各自
// 从节点标题算 hash、各自维护「hash → kind」。同一段规则两份实现，谁先漂移谁就悄悄
// 少派发一个位置的失效（表现：某个位置的实例停在旧标签上，库与线上都看不出异常）。
// 本文件把这条规则收成**一份**：四 kind 全收集、hash 只算一次、结果不可变。
//
// 与既有 Tree 的差别（不是改造它）：Tree 的 resolveSourceTitles **吞掉** ResolveSource 的
// 错误（回退记录自身 title/path）—— 那是构建期「单个来源实体缺失不能整页失败」的口径，
// 本文件**原样保留 Tree 与 build 消费者的行为**（resolveSourceTitles 一行不改）。
// 但控制面**写完整性**不能继承这份宽容：写侧要可证明「这段原文此刻就是它」，所以索引在
// 自己的解析里严格传播 resolver 错误（失败整个 kind 丢弃 → Require 失败），绝不把构建期的
// 回退语义搬进写侧判据。两者是同一 resolver、同一「成功解析 / 无来源节点」口径的两条判据。
//
// 层归属：service —— 它做的是「多表/多端口拼装 + 事实」，不出成品句、不取词。
// 只用 Service 上既有的私有字段（s.sources / s.m），因此不改任何既有文件即可公开。

import (
	"context"
	"sort"
	"strings"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// navigationSourceKinds 共享来源索引收集的全部菜单位置（顺序即规范顺序）。
//
// 四值来自迁移 285 的 navigation.kind CHECK 与 builder nav 组件的菜单下拉
// （internal/builder/components/nav/nav.go:117）：桌面与移动端各一个页眉、一个页脚。
// 工作台此前只遍历 header / footer，移动端两个位置既不能翻译也不能被失效命中 ——
// 本索引把它们一并收集，结果顺序固定 → 输出确定。
var navigationSourceKinds = []string{kindHeader, kindHeaderMobile, kindFooter, kindFooterMobile}

// navigationLabelContext 导航标签的固定语境（与构建期 resolver 逐字一致：见
// internal/pipeline/navigation_adapter.go 的 translateLabels 与
// inbound/http 的 navigationTranslationContext）。
func navigationLabelContext() string { return i18n.ContentContext("navigation", "label") }

// sourceIndex 共享来源索引的不可变实现。
type sourceIndex struct {
	entries  []navigationcontract.TranslationSourceEntry
	byHash   map[string]int
	kinds    []string
	failures []navigationcontract.KindReadError
	// nodes hash → 该原文出现的节点引用（读侧展示链接用；不影响任何事实判定）。
	//
	// 它只被 collectKindSources 追加，与 entries 的合并规则无关：同一段文字出现在多个
	// 位置 / 多个菜单项时，nodes 是**全部**节点，entries 的一条 Kinds 是去重后的位置。
	nodes map[string][]navigationcontract.NavigationNodeRef
}

// sourceProvider 返回本次索引使用的行端口。
//
// 注入点是一个**私有**字段（s.sourceRowsPort，声明在 Service 结构体里），不改
// NewService 签名、不加导出方法：装配侧行为与以前逐字一致（nil → model 直传），
// 而单测可以塞一个会失败的桩来验证「逐位置错误」的两种取用方式。
func (s *Service) sourceProvider() sourceRows {
	if s == nil {
		return nil
	}
	if s.sourceRowsPort != nil {
		return s.sourceRowsPort
	}
	if s.m == nil {
		return nil
	}
	return navigationRowsModel{m: s.m}
}

// 编译期断言：公开结果是 contract 的不可变形状。
var _ navigationcontract.SourceIndex = (*sourceIndex)(nil)

// OpenSourceIndex 打开某工程四个菜单位置的共享来源索引。
//
// 语义（逐条都是判据，不是描述）：
//  1. **四 kind 全收集**：任一位置读失败都记进 KindErrors，其余位置照常解析 ——
//     读侧因此能局部展示，而不用整页失败；
//  2. **返回错误只属于「一个位置都没读成」**（工程参数非法 / 端口未注入 / 四个位置全失败）。
//     部分失败由 KindErrors 承载，让调用方选读降级还是写整体失败（Require）；
//  3. **失败即整 kind 丢弃**：某位置要么整份并入（全部条目成功），要么一条都不并入 ——
//     绝不让失败位置的残缺条目成为「成功来源」；
//  4. **hash/语境唯一**：同一段原文只产出一条 entry，Kinds 汇总全部位置；
//     同 hash 不同原文（sha256 撞车，实际不可能）不随机覆盖 —— 保留先出现的规范顺序位置，
//     记 Error 日志并把冲突的两个位置都记成失败（写侧整体失败，读侧降级）；
//  5. **判据沿用既有语义**：原文 = 来源实体解析后的标题（同一 resolver、同一
//     「解析成功 / 无来源节点」口径）、hash = i18n.ContentHash、
//     「参与翻译」= i18n.ShouldTranslateContent。不另算一套指纹、不另立可翻译条件；
//  6. **确定性**：条目按 Hash 升序、Kinds 按规范顺序 → 同一输入两次调用字节级相同；
//  7. **ctx 取消传播**：取消 / 超时一律原样返回 ctx 错误，不伪装成「位置失败」。
func (s *Service) OpenSourceIndex(ctx context.Context, projectID string) (navigationcontract.SourceIndex, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	if s.sourceProvider() == nil {
		// 未装配的看板：显式失败，绝不返回一份「看起来是空的」索引
		//（空索引会让写侧把每一行都判成「原文已变化」，症状指向错误的原因）。
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	// nodes 必须与 byHash 一起初始化：第 133 行会无条件 append 进去，nil map 写入直接 panic
	//（写路径的 OpenSourceIndexTx 此前已经这样构造，两边保持一致）。
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
		collected, err := s.collectKindSources(ctx, projectID, kind, contextName)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				// 取消 / 超时不是「这个位置读不到」：整体中止，不让写侧把它当成业务失败去重试。
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
				// entry.Hash 由 collectKindSources 逐个节点填好（同一节点既入 entries 又入
				// nodes）。空标题的节点 hash 不是空串（是空串的 sha256），这里的判空只是
				// 防御 —— 真的为空时不建 nodes 条目，不让读侧按空 hash 取到一堆节点。
				index.nodes[entry.Hash] = append(index.nodes[entry.Hash], navigationcontract.NavigationNodeRef{
					ID: entry.NodeID, Kind: kind, Path: entry.Path,
				})
			}
		}
	}
	if len(index.kinds) == 0 {
		// 四个位置一个都没读成：这不是「没有菜单」，而是读不了 —— 显式失败。
		return nil, index.Require()
	}
	index.sortEntries()
	return index, nil
}

// collectKindSources 读一个位置的完整菜单树，做严格来源解析，产出该位置的**全部**条目。
//
// 先收完本 kind、只有全部成功才交给调用方并入索引：任何一步失败（读行 / 来源解析 /
// hash 冲突）都返回 error，调用方把这个位置整份丢弃并记成失败事实。
//
// 「无来源节点」（custom 项 / 未指定来源实体）与「解析器未装配」不是错误：它们沿用
// Tree 的语义（标题就是记录自身标题），不在这里捏造一个失败。
func (s *Service) collectKindSources(ctx context.Context, projectID, kind, contextName string) ([]navigationcontract.TranslationSourceEntry, error) {
	nodes, err := s.sourceNodesStrict(ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	return kindEntriesOf(ctx, nodes, kind, contextName)
}

// kindEntriesOf 一个位置的行 → 该位置的全部来源条目（读侧与写侧**唯一**一份实现）。
//
// 两条路径（OpenSourceIndex 自带事务读 / OpenSourceIndexTx 调用方事务读）只在「行从哪来」
// 上不同：条目怎么拼、hash 怎么算、NodeID/Path 怎么填必须逐字相同。此前写路径的 Tx 版本
// 自己拼了一遍且漏了 NodeID/Path/nodes，读侧能定位节点、写侧不能 —— 同一个索引两种事实，
// 正是本批要消灭的分叉，所以收敛到这一个函数。
func kindEntriesOf(ctx context.Context, nodes []*navigationdto.NavigationNode, kind, contextName string) ([]navigationcontract.TranslationSourceEntry, error) {
	entries := make([]navigationcontract.TranslationSourceEntry, 0, len(nodes))
	walkNavSources(nodes, func(node *navigationdto.NavigationNode) {
		title := strings.TrimSpace(node.Title)
		// 「是否参与翻译」**不在这里提前过滤**：ShouldTranslateContent 是读消费者的展示责任，
		// 索引这一层只回答「服务端这段原文在哪个位置」。提前丢掉不可翻译行会让写侧只剩
		// 一条「原文已变化（409）」，把「这段本来就不翻译」的原因指错 —— 写服务自己判
		// ShouldTranslateContent 再回不可翻译的业务错误。
		//
		// title 为空串时 hash 是空串的 sha256：它代表「这个位置有一个没有文字的菜单项」，
		// 位置事实仍然成立（读侧按 hash 反查节点时能对上），不伪造、也不丢弃。
		entries = append(entries, navigationcontract.TranslationSourceEntry{
			Hash: i18n.ContentHash(title), Source: title, Context: contextName, Kinds: []string{kind},
			// NodeID / Path 只服务读侧展示（索引按 hash 反查时能给出「这段文字出现在哪个
			// 节点的哪个链接」）；不参与 hash、不参与合并判定，也不进任何业务判据。
			NodeID: node.ID, Path: strings.TrimSpace(node.Path),
		})
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// sourceNodesStrict 读一个位置的菜单树并**严格**解析来源标题（索引专用）。
//
// 与 Tree 共用同一个 resolver、同一组「需要解析的节点」（非 custom 且有来源实体）、
// 同一「解析成功才覆盖」口径；唯一差别是解析错误**不再吞掉**：任一节点解析失败即整份
// 返回 error，由调用方把该位置记成失败事实。构建期消费者仍走 resolveSourceTitles 的
// 回退语义 —— 那份宽容是构建期的，不是控制面写完整性的判据。
func (s *Service) sourceNodesStrict(ctx context.Context, projectID, kind string) ([]*navigationdto.NavigationNode, error) {
	rows := s.sourceProvider()
	if rows == nil {
		return nil, navigationenums.Err(navigationenums.ErrProjectRequired)
	}
	list, err := rows.ListForKind(ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	nodes := navNodesOfRows(list)
	if s.sources == nil {
		// 解析器未装配：与 Tree 一致（标题保持记录自身），不是这个位置的读取失败。
		return nodes, nil
	}
	if err := s.resolveSourceTitlesStrict(ctx, projectID, nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// resolveSourceTitlesStrict 严格版来源标题回写：口径与 resolveSourceTitles 逐字相同
// （同一判定条件、只在 title/url 非空时覆盖），差别只在错误传播。
//
// 不修改既有 resolveSourceTitles（构建消费者仍用它），也不复制它的判定之外的规则：
// 「哪些节点需要解析」只有这一处写法，改口径时两处必须一起改。
func (s *Service) resolveSourceTitlesStrict(ctx context.Context, projectID string, nodes []*navigationdto.NavigationNode) error {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if needsSourceResolution(n) {
			title, url, err := s.sources.ResolveSource(ctx, projectID, n.SourceType, *n.SourceID)
			if err != nil {
				return err
			}
			if title != "" {
				n.Title = title
			}
			if url != "" {
				n.Path = url
			}
		}
		if err := s.resolveSourceTitlesStrict(ctx, projectID, n.Children); err != nil {
			return err
		}
	}
	return nil
}

// needsSourceResolution 哪些节点需要向来源实体解析标题/链接。
//
// 判据与 resolveSourceTitles 里的同一条逐字一致：非 custom 且带来源实体 ID。
// custom 项与未指定来源实体的项「没有来源可解析」—— 索引不会为此编造错误。
func needsSourceResolution(n *navigationdto.NavigationNode) bool {
	return n.SourceType != sourceCustom && n.SourceID != nil && *n.SourceID != ""
}

// merge 把一条来源并入索引：同 hash 合并 Kinds，绝不覆盖已有的 Source/Context。
//
// 「不随机覆盖」的落点：map 遍历无序，若同 hash 遇到不同原文就直接改 value，
// 最终值取决于遍历顺序 —— 那正是本批要消灭的不确定。这里只保留先出现的原文
// （规范 kind 顺序 + 树内顺序），并把**冲突的两个位置**都记成失败事实（写侧整体失败）。
func (s *sourceIndex) merge(ctx context.Context, entry navigationcontract.TranslationSourceEntry, kind, projectID string) {
	idx, ok := s.byHash[entry.Hash]
	if !ok {
		s.byHash[entry.Hash] = len(s.entries)
		s.entries = append(s.entries, entry)
		return
	}
	existing := &s.entries[idx]
	if existing.Source != entry.Source || existing.Context != entry.Context {
		logger.SceneCtx("navigation", ctx).With("project", projectID).With("kind", kind).
			With("hash", entry.Hash).Warn("导航原文哈希冲突：同一指纹对应不同原文，该位置按解析失败处理")
		// 先出现的那条已经进了索引（它的位置同样不可信），两边都记失败：
		// 只记后到的位置会让「保留谁」取决于遍历顺序。
		for _, k := range append(append([]string{}, existing.Kinds...), kind) {
			s.fail(kindSafe(k, kind))
		}
		return
	}
	for _, k := range existing.Kinds {
		if k == kind {
			return
		}
	}
	existing.Kinds = appendKind(existing.Kinds, kind)
}

// kindSafe 冲突标记里的 kind（空值不该进入失败表；正常路径不会为空）。
func kindSafe(k, fallback string) string {
	if strings.TrimSpace(k) == "" {
		return fallback
	}
	return k
}

// fail 记一条位置失败事实（同 kind 不重复记）。
func (s *sourceIndex) fail(kind string) {
	for _, f := range s.failures {
		if f.Kind == kind {
			return
		}
	}
	s.failures = append(s.failures, navigationcontract.KindReadError{
		Kind: kind, Err: navigationenums.ErrSourceKindDetail(kind),
	})
}

// appendKind 按 kind 规范顺序插入（不依赖调用顺序 → 输出确定）。
func appendKind(kinds []string, kind string) []string {
	rank := navigationKindRank(kind)
	pos := len(kinds)
	for i, k := range kinds {
		if navigationKindRank(k) > rank {
			pos = i
			break
		}
	}
	kinds = append(kinds, "")
	copy(kinds[pos+1:], kinds[pos:])
	kinds[pos] = kind
	return kinds
}

// navigationKindRank 位置的规范序号（未登记的位置排在最后，保证排序是全序）。
func navigationKindRank(kind string) int {
	for i, k := range navigationSourceKinds {
		if k == kind {
			return i
		}
	}
	return len(navigationSourceKinds)
}

// sortEntries 按 Hash 升序整理条目并重建 hash → 下标。
func (s *sourceIndex) sortEntries() {
	sort.Slice(s.entries, func(i, j int) bool { return s.entries[i].Hash < s.entries[j].Hash })
	s.byHash = make(map[string]int, len(s.entries))
	for i := range s.entries {
		s.byHash[s.entries[i].Hash] = i
		s.entries[i].Kinds = append([]string(nil), s.entries[i].Kinds...)
	}
}

// —— SourceIndexNodes（可选展示接口）：某段原文出现的全部节点 ——
//
// 它只回答展示用的事实（哪个节点、哪个链接），不参与任何业务判据；读侧断言不到就不展示
// 链接，功能不受影响。返回切片按树内前序、跨位置按规范 kind 顺序（与 entries 的收集顺序
// 一致），并做值拷贝。

// Nodes 该 hash 对应的全部节点引用（无节点时为空切片而不是 nil）。
func (s *sourceIndex) Nodes(hash string) []navigationcontract.NavigationNodeRef {
	if s == nil {
		return []navigationcontract.NavigationNodeRef{}
	}
	return append([]navigationcontract.NavigationNodeRef{}, s.nodes[strings.TrimSpace(hash)]...)
}

// 编译期断言：展示接口可用（读侧按它取链接）。
var _ navigationcontract.SourceIndexNodes = (*sourceIndex)(nil)

// —— SourceIndex 的只读访问器（值语义：切片一律拷贝，调用方改不坏索引）——

// Locate 按 hash 取唯一事实。
func (s *sourceIndex) Locate(hash string) (navigationcontract.TranslationSourceEntry, bool) {
	if s == nil {
		return navigationcontract.TranslationSourceEntry{}, false
	}
	idx, ok := s.byHash[strings.TrimSpace(hash)]
	if !ok {
		return navigationcontract.TranslationSourceEntry{}, false
	}
	return cloneEntry(s.entries[idx]), true
}

// Entries 全部条目（已按 Hash 升序）。
func (s *sourceIndex) Entries() []navigationcontract.TranslationSourceEntry {
	if s == nil {
		return []navigationcontract.TranslationSourceEntry{}
	}
	out := make([]navigationcontract.TranslationSourceEntry, 0, len(s.entries))
	for i := range s.entries {
		out = append(out, cloneEntry(s.entries[i]))
	}
	return out
}

// Kinds 本次解析涉及的菜单位置（规范顺序）。
func (s *sourceIndex) Kinds() []string {
	if s == nil {
		return []string{}
	}
	return append([]string{}, s.kinds...)
}

// KindErrors 逐位置读失败的事实（规范顺序，只含失败位置）。
func (s *sourceIndex) KindErrors() []navigationcontract.KindReadError {
	if s == nil {
		return []navigationcontract.KindReadError{}
	}
	return append([]navigationcontract.KindReadError{}, s.failures...)
}

// Require 写侧严格前缀：有任一位置读失败就整体失败。
//
// 聚合全部失败位置（明细为逗号列举，ASCII ": " 由 enums.ErrDetail 拼），
// 调用方用 errors.Is(err, enums.Err(ErrSourceKindFailed)) 判定、用 kind 明细定位；
// 要按位置取事实请直接读 KindErrors()（不从错误文本反解）。
func (s *sourceIndex) Require() error {
	if s == nil || len(s.failures) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(s.failures))
	seen := map[string]bool{}
	for _, f := range s.failures {
		if f.Kind == "" || seen[f.Kind] {
			continue
		}
		seen[f.Kind] = true
		kinds = append(kinds, f.Kind)
	}
	if len(kinds) == 0 {
		return navigationenums.ErrSourceKind()
	}
	sort.SliceStable(kinds, func(i, j int) bool {
		return navigationKindRank(kinds[i]) < navigationKindRank(kinds[j])
	})
	return navigationenums.ErrSourceKindDetail(strings.Join(kinds, ", "))
}

// cloneEntry 值语义拷贝（Kinds 是切片：不拷贝会让调用方改坏索引）。
func cloneEntry(e navigationcontract.TranslationSourceEntry) navigationcontract.TranslationSourceEntry {
	e.Kinds = append([]string{}, e.Kinds...)
	return e
}

// walkNavSources 前序遍历菜单树（null 节点跳过；子节点递归）。
func walkNavSources(nodes []*navigationdto.NavigationNode, visit func(*navigationdto.NavigationNode)) {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		visit(node)
		walkNavSources(node.Children, visit)
	}
}

// sourceKindFailure 记一条位置读失败的日志（原始错误只进日志，不外泄）。
func sourceKindFailure(ctx context.Context, projectID, kind string, err error) {
	// 取消 / 超时由 ctx 自己表达，不再制造噪音日志（它们也不会走到这里）。
	if ctx.Err() != nil {
		return
	}
	logger.SceneCtx("navigation", ctx).With("project", projectID).With("kind", kind).
		Error(err, "读取导航原文失败（该位置不进入共享来源索引）")
}
