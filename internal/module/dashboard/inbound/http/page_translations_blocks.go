package dashboardhttp

// page_translations_blocks.go — 翻译工作台的块内文本（多语言 P5b 缺口补齐，docs/06-D §15.11/§15.12）。
//
// 判断与理由（任务 P5b 缺口 2）：块内文本**应当**出现在页面翻译工作台里。
//   - 页面产物本来就含页眉/页脚块与 core.globalref 内联块的文本（构建期装配），
//     工作台又是唯一的译文录入入口；不列出则这段文本永远无法翻译，
//     且完成度会显示 100% 而页眉仍是中文（错误的完成度信号）。
//   - 写入路径天然是全局的：sys_translation 主键 (source_hash, context, lang)，
//     保存后调用 page.MarkStaleForI18n 全站标记待重建——共享文本语义已由 P5c 支撑。
//   - 行上标注来源（页眉块/页脚块/全局块）与复用提示，避免「在 A 页改动了全站页眉」
//     被误读成本页局部修改。
//
// 与构建期同源：候选一律来自 builder.CollectContentCandidates（块文档同样过这一份
// 白名单与跳过规则），不另写扫描逻辑。

import (
	"context"
	"strings"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// 块内文本的来源标签（工作台行徽章）。
const (
	translationOriginHeader = "页眉块"
	translationOriginFooter = "页脚块"
	translationOriginBlock  = "全局块"
)

// blockCandidateInfo 块内候选集合：候选列表 + 每个 (hash, context) 的来源标签。
type blockCandidateInfo struct {
	candidates []builder.ContentCandidate
	origin     map[string]string // ContentIndexKey(hash, context) → 来源标签
}

// collectBlockCandidates 收集本页引用块（页眉/页脚绑定 + core.globalref，递归）的候选。
//
// 块不可用（已删除/模板块/解析失败）时跳过：与构建期一致降级，不阻断工作台渲染。
// 同一块只解析一次（visited 兼作引用环保护）。
// projectID 必须一起传：block.Detail 把工程归属当作必填的越权防护 scope，
// 只给块 ID 会拿到「参数缺失」—— 而这一层是「读不到就跳过」的降级路径，
// 症状不是报错，而是工作台里**块内文本一条都不列**（完成度还会误报 100%）。
func (h *Handle) collectBlockCandidates(ctx context.Context, projectID string, page *builder.Page) blockCandidateInfo {
	info := blockCandidateInfo{origin: map[string]string{}}
	if h == nil || h.blocks == nil || page == nil {
		return info
	}
	cache := map[string]*builder.Page{}
	visited := map[string]bool{}
	seen := map[string]bool{}

	var walk func(blockID, origin string)
	walk = func(blockID, origin string) {
		blockID = strings.TrimSpace(blockID)
		if blockID == "" || visited[blockID] {
			return
		}
		visited[blockID] = true
		blockPage := h.blockPageOf(ctx, projectID, blockID, cache)
		if blockPage == nil {
			return
		}
		for _, cand := range builder.CollectContentCandidates(blockPage) {
			key := i18n.ContentIndexKey(i18n.ContentHash(cand.Source), cand.Context)
			if _, ok := info.origin[key]; !ok {
				info.origin[key] = origin
			}
			dedupe := cand.Context + "\x00" + cand.Source
			if seen[dedupe] {
				continue
			}
			seen[dedupe] = true
			info.candidates = append(info.candidates, cand)
		}
		// 块内再引用块：沿用同一来源标签（外层来源即编辑者看到的入口）。
		for _, nested := range builder.ReferencedBlockIDs(blockPage.Root) {
			walk(nested, origin)
		}
	}
	// 槽位绑定逐个 walk，来源标签按槽位区分：编辑者要能看出某段文字来自页眉还是公告条。
	slotBindings := page.Settings.Structure.SlotBindings()
	for _, slot := range builder.SortedSlots(slotBindings) {
		origin := translationOriginBlock
		switch slot {
		case builder.SlotHeader:
			origin = translationOriginHeader
		case builder.SlotFooter:
			origin = translationOriginFooter
		}
		walk(slotBindings[slot], origin)
	}
	for _, ref := range builder.ReferencedBlockIDs(page.Root) {
		walk(ref, translationOriginBlock)
	}
	return info
}

// blockPageOf 解析块文档为 builder.Page（cache 为单次调用内缓存；不可用返回 nil）。
func (h *Handle) blockPageOf(ctx context.Context, projectID, blockID string, cache map[string]*builder.Page) *builder.Page {
	if cache != nil {
		if p, ok := cache[blockID]; ok {
			return p
		}
	}
	block, err := h.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: projectID, ID: blockID})
	if err != nil || block == nil || len(block.Document) == 0 {
		logger.Scene("page").With("block", blockID).Warn("工作台读取块文档失败，已跳过该块的文本")
		if cache != nil {
			cache[blockID] = nil
		}
		return nil
	}
	page, perr := builder.ParsePage(block.Document)
	if perr != nil {
		logger.Scene("page").With("block", blockID).Error(perr, "工作台解析块文档失败，已跳过该块的文本")
		page = nil
	}
	if cache != nil {
		cache[blockID] = page
	}
	return page
}

// mergeContentCandidates 合并本页与块内候选（按 (context, source) 去重，保持确定性顺序）。
func mergeContentCandidates(pageCands, blockCands []builder.ContentCandidate) []builder.ContentCandidate {
	if len(blockCands) == 0 {
		return pageCands
	}
	seen := make(map[string]bool, len(pageCands)+len(blockCands))
	out := make([]builder.ContentCandidate, 0, len(pageCands)+len(blockCands))
	for _, c := range pageCands {
		seen[c.Context+"\x00"+c.Source] = true
		out = append(out, c)
	}
	for _, c := range blockCands {
		if seen[c.Context+"\x00"+c.Source] {
			continue
		}
		seen[c.Context+"\x00"+c.Source] = true
		out = append(out, c)
	}
	return out
}
