package pagehttp

// page_translations_index.go — 全站可翻译内容索引（多语言 P5c，docs/06-D §7.8 决策 F11/F12）。
//
// 用途（两个都需要「全站 (source_hash, context) 视图」）：
//  1. 跨页面复用提示：一行译文改一次，用到它的所有页面同时变，必须让编辑者看见
//     （「↳ 还用在另外 N 个页面（修改后全站同步生效）」）；
//  2. 全站翻译完成度：分母 = 全站去重后的 (source_hash, context) 条数，
//     分子 = 其中在目标语言已有译文的条数。
//
// 为什么不是一条 SQL：候选集合由「组件白名单 + 跳过规则」决定，白名单在 Go 里
// （core.TranslatableFields），SQL 侧无法表达；因此只能把全站草稿文档读回来，
// 用与构建期同一个函数 builder.CollectContentCandidates 收集。
//
// 代价与取舍（详见 docs/06-D §15.12）：
//   - 一次扫描 = 一条 SELECT 取回全站 pages.draft_document（JSONB）+ 全量 JSON 解析，
//     内存占用与「页面数 × 文档大小」同阶；
//   - 进程内缓存 TTL 30s，页面数超过 siteContentScanPageLimit 时主动跳过全站统计
//     （工作台退化为「本页维度」并在页面上说明），避免大站把后台拖垮；
//   - 更彻底的做法是新增「候选使用表」（page_id, source_hash, context，草稿保存时维护），
//     代价是每次草稿写入多一次索引维护与一张新表，本轮不做（遗留项）。
//
// 块内文本已纳入本索引（docs/06-D §15.14）：扫描页面草稿时同时记录「哪些页面引用了哪些块」
// （settings.structure 页眉/页脚绑定 + core.globalref 节点），再按块文档收集候选，
// 归属到引用它的页面——这样工作台行、复用提示与全站完成度分母三者口径一致。

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"go_wp/internal/builder"
	"go_wp/pkg/i18n"
)

const (
	// siteContentIndexTTL 全站索引缓存有效期（后台页面可接受的陈旧窗口）。
	siteContentIndexTTL = 30 * time.Second
	// siteContentScanPageLimit 全站扫描的页数上限：超过则跳过全站统计。
	siteContentScanPageLimit = 1000
	// siteContentReuseHintMax 复用提示里最多列出的页面路径数（超出用「等 N 个」）。
	siteContentReuseHintMax = 6
)

// errSiteContentIndexSkipped 全站扫描被跳过（页数超限 / page 契约缺失）。
var errSiteContentIndexSkipped = errors.New("全站翻译统计已跳过")

// siteContentIndex 全站可翻译内容索引。
type siteContentIndex struct {
	// keys 去重后的 (source_hash + NUL + context) 键，字典序。
	keys []string
	// hashes 去重后的 source_hash（批量取译文用），字典序。
	hashes []string
	// usage 键 → 出现该 (hash, context) 的页面路径（去重，字典序）。
	usage map[string][]string
	// pages 参与扫描的页面数。
	pages int
	// skipped 是否因页数超限跳过（true 时 keys/hashes 为空）。
	skipped bool
}

// total 全站可翻译条目数（完成度分母）。
func (s *siteContentIndex) total() int {
	if s == nil {
		return 0
	}
	return len(s.keys)
}

// reusePagesOf 返回该键出现的页面数（含本页；0 = 未统计）。
func (s *siteContentIndex) reusePagesOf(key string) int {
	if s == nil {
		return 0
	}
	return len(s.usage[key])
}

// reusePathsOf 返回该键出现的页面路径（已排序）。
func (s *siteContentIndex) reusePathsOf(key string) []string {
	if s == nil {
		return nil
	}
	return s.usage[key]
}

// siteContentIndexCache 进程内缓存（pagesAdminHandle 持有）。
type siteContentIndexCache struct {
	mu  sync.Mutex
	at  time.Time
	idx *siteContentIndex
}

// siteContentIndexOf 取全站索引（命中缓存直接返回，过期重建）。
func (h *pagesAdminHandle) siteContentIndexOf(ctx context.Context) (*siteContentIndex, error) {
	if h == nil {
		return nil, errSiteContentIndexSkipped
	}
	h.siteIndex.mu.Lock()
	defer h.siteIndex.mu.Unlock()
	if h.siteIndex.idx != nil && time.Since(h.siteIndex.at) < siteContentIndexTTL {
		return h.siteIndex.idx, nil
	}
	idx, err := h.buildSiteContentIndex(ctx)
	if err != nil {
		return nil, err
	}
	h.siteIndex.idx, h.siteIndex.at = idx, time.Now()
	return idx, nil
}

// buildSiteContentIndex 扫描全站草稿文档建索引（调用方持锁）。
func (h *pagesAdminHandle) buildSiteContentIndex(ctx context.Context) (*siteContentIndex, error) {
	if h.pages == nil {
		return nil, errSiteContentIndexSkipped
	}
	drafts, err := h.pages.ListDrafts(ctx)
	if err != nil {
		return nil, err
	}
	if len(drafts) > siteContentScanPageLimit {
		return &siteContentIndex{skipped: true, pages: len(drafts)}, nil
	}

	idx := &siteContentIndex{usage: map[string][]string{}, pages: len(drafts)}
	seenKey := make(map[string]bool)
	seenHash := make(map[string]bool)
	seenPage := make(map[string]map[string]bool)
	// add 登记一个候选（去重 + 记录出现该 (hash, context) 的页面路径）。
	add := func(cand builder.ContentCandidate, path string) {
		hash := i18n.ContentHash(cand.Source)
		key := i18n.ContentIndexKey(hash, cand.Context)
		if !seenKey[key] {
			seenKey[key] = true
			idx.keys = append(idx.keys, key)
		}
		if !seenHash[hash] {
			seenHash[hash] = true
			idx.hashes = append(idx.hashes, hash)
		}
		if seenPage[key] == nil {
			seenPage[key] = map[string]bool{}
		}
		if !seenPage[key][path] {
			seenPage[key][path] = true
			idx.usage[key] = append(idx.usage[key], path)
		}
	}
	// 块引用：块 ID → 引用该块的页面路径（块内文本的「出现在哪些页面」由此得出）。
	blockPaths := map[string][]string{}
	// blockProject 块 id → 所属工程：块查询要求工程 scope（越权防护的必填项），
	// 而这里遍历的是**全站**草稿（可能跨工程），所以按引用它的页面把工程记下来。
	// 块属于工程、id 全局唯一，同一个块不会出现在两个工程里。
	blockProject := map[string]string{}
	blockPathSeen := map[string]map[string]bool{}
	for i := range drafts {
		doc := drafts[i].DraftDocument
		if len(doc) == 0 {
			continue
		}
		page, perr := builder.ParsePage(doc)
		if perr != nil {
			// 单页解析失败不拖垮全站统计：跳过该页（构建期会自行报错）。
			continue
		}
		path := drafts[i].DraftPath
		if path == "" {
			path = drafts[i].ID
		}
		// SEO 文本字段不在 AST 里（组件侧白名单管不到），但构建期会取它们的译文 ——
		// 工作台必须看到同一份候选，否则作者根本找不到这两个字段可填（审计 I18N-014）。
		for _, cand := range builder.AppendSEOCandidates(page, builder.CollectContentCandidates(page)) {
			add(cand, path)
		}
		refs := builder.ReferencedBlockIDs(page.Root)
		// 槽位绑定统一从 SlotBindings 取：漏掉新槽位的表现是「公告条里的文案在翻译页面上找不到」，
		// 运营只能手工去找是哪个块。
		bindings := page.Settings.Structure.SlotBindings()
		for _, slot := range builder.SortedSlots(bindings) {
			refs = append(refs, bindings[slot])
		}
		for _, blockID := range refs {
			if blockPathSeen[blockID] == nil {
				blockPathSeen[blockID] = map[string]bool{}
			}
			if blockPathSeen[blockID][path] {
				continue
			}
			blockPathSeen[blockID][path] = true
			blockPaths[blockID] = append(blockPaths[blockID], path)
			if _, ok := blockProject[blockID]; !ok {
				blockProject[blockID] = drafts[i].ProjectID
			}
		}
	}
	// 块内文本同样进全站索引（分母/复用提示与工作台行保持一致）：
	// 块被哪些页面引用，其文本就算出现在哪些页面；块内再引用块沿用同一批页面。
	if h.blocks != nil && len(blockPaths) > 0 {
		cache := map[string]*builder.Page{}
		visited := map[string]bool{}
		queue := make([]string, 0, len(blockPaths))
		for blockID := range blockPaths {
			queue = append(queue, blockID)
		}
		sort.Strings(queue)
		for len(queue) > 0 {
			blockID := queue[0]
			queue = queue[1:]
			if visited[blockID] {
				continue
			}
			visited[blockID] = true
			paths := blockPaths[blockID]
			blockPage := h.blockPageOf(ctx, blockProject[blockID], blockID, cache)
			if blockPage == nil {
				continue
			}
			for _, cand := range builder.CollectContentCandidates(blockPage) {
				for _, path := range paths {
					add(cand, path)
				}
			}
			for _, nested := range builder.ReferencedBlockIDs(blockPage.Root) {
				if visited[nested] {
					continue
				}
				if blockPathSeen[nested] == nil {
					blockPathSeen[nested] = map[string]bool{}
				}
				for _, path := range paths {
					if blockPathSeen[nested][path] {
						continue
					}
					blockPathSeen[nested][path] = true
					blockPaths[nested] = append(blockPaths[nested], path)
				}
				queue = append(queue, nested)
			}
		}
	}
	sort.Strings(idx.keys)
	sort.Strings(idx.hashes)
	for key := range idx.usage {
		sort.Strings(idx.usage[key])
	}
	return idx, nil
}

// reuseHint 组装复用提示文案（行内提示 + 展开用页面路径）。
//
// 返回 (其他页面数, 该文本出现的页面总数, 展开文案)。仅本页出现时返回 0。
func reuseHint(paths []string, currentPath string) (others int, total int, hint string) {
	total = len(paths)
	if total <= 1 {
		return 0, total, ""
	}
	others = total - 1
	if currentPath != "" {
		found := false
		for _, p := range paths {
			if p == currentPath {
				found = true
				break
			}
		}
		if !found {
			// 当前页不在索引里（例如草稿路径刚改过）：总数即其他页面数。
			others = total
		}
	}
	shown := paths
	if len(shown) > siteContentReuseHintMax {
		shown = shown[:siteContentReuseHintMax]
	}
	hint = joinPaths(shown)
	if len(paths) > len(shown) {
		hint = hint + " 等页面"
	}
	return others, total, hint
}

// joinPaths 以「、」连接页面路径（模板侧不再做拼接逻辑）。
func joinPaths(paths []string) string {
	out := ""
	for i, p := range paths {
		if i > 0 {
			out += "、"
		}
		out += p
	}
	return out
}
