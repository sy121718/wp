package builder

// content_i18n.go — 构建器内联文本的内容翻译接入（多语言 P5b，docs/06-D §7.7）。
//
// 链路（每页每语言一次）：
//
//	CollectContentCandidates(AST) → ShouldTranslateContent 过滤 → ContentHash 集合
//	→ i18n.NewContentTranslator（一次 SQL，装配层执行）→ WithContentTranslator
//	→ 渲染期 applyContentTranslation 逐字段取词（命中用译文，未命中回退原文）
//
// 三条硬约束：
//  1. 只有组件 Translatable 白名单里的字段参与（core.TranslatableFields，决策 F6）；
//  2. 一次查库：组件渲染期零查库（§7.7），取词器在内存索引上工作；
//  3. 原文不动：Page Document / AST 字节不改，替换只作用于本次渲染的 props 副本。
//
// 嵌套字段 context 按决策 D11「不带索引」：core.list.text（数组元素同名字段共用语境），
// 由 core.TranslatableFields + ContentContext 的「类型.字段名」拼装保证。

import (
	"bytes"
	"cmp"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"go_wp/internal/builder/components/globalref"
	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

// ContentCandidate 一个可翻译取值（context + 原文）。
//
// Context 为「组件类型.字段名」（如 core.button.text）；Source 为作者填写的原文。
type ContentCandidate struct {
	Context string
	Source  string
}

// SEO 文本字段的翻译语境（审计 I18N-014）。
//
// 定义成常量而不是两处字面量：写入口（工作台）与读入口（构建期）的语境一旦分叉，
// 译文会安静地存进去、永远读不出来，而页面上完全看不出区别。
const (
	SEOTitleContext       = "page.seo.title"
	SEODescriptionContext = "page.seo.description"
)

// CollectContentCandidates 遍历页面文档收集可翻译候选（去重，确定性顺序）。
//
// 供两处复用（同源，避免「收集」与「替换」两套白名单判断漂移）：
//   - 装配层：构造 ContentTranslator 的 hash 集合、判定本页是否用到内容翻译；
//   - 构建期：applyContentTranslation 逐字段替换。
//
// 返回顺序：按 (context, source) 字典序（确定性，便于测试与日志）。
// 不含跳过规则命中的取值（纯数字/纯符号/空白，决策 F7）——它们不进翻译表、不计缺失。
func CollectContentCandidates(p *Page) []ContentCandidate {
	if p == nil {
		return nil
	}
	return CollectContentCandidatesOfRoots(p.Root)
}

// CollectContentCandidatesOfRoots 收集一组根节点（页面 root 或块文档 root）的候选。
//
// 与 CollectContentCandidates 同一份白名单与去重规则，供「块文档候选收集」复用
// （页眉/页脚块与 core.globalref 引用块的文本同样是本页产物的可翻译文本）。
func CollectContentCandidatesOfRoots(roots []*core.Node) []ContentCandidate {
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []ContentCandidate
	for _, n := range roots {
		collectNodeCandidates(n, seen, &out)
	}
	sortCandidates(out)
	return out
}

// BlockRootFunc 解析块 ID → 块文档 root 节点（候选收集用）。
//
// 与 core.BlockResolver.ResolveBlockRoot 同形，但用函数类型表达，使装配层
// （blockResolverAdapter）与工作台各自注入自己的解析器，共用同一份收集逻辑。
// 返回错误 / 空 roots 表示块不可用，按渲染期同一降级语义跳过（不计缺失、不阻断）。
type BlockRootFunc func(blockID string) ([]*core.Node, error)

// CollectContentCandidatesForDocument 收集文档 + 页眉/页脚绑定块 + globalref 内联块的可翻译候选。
//
// page 与 presentation 装配层共用：extraBlockIDs 来自 settings.structure，
// 块展开经 resolve 回调（与渲染期 BlockResolver 同源）。
func CollectContentCandidatesForDocument(p *Page, resolve BlockRootFunc) []ContentCandidate {
	if p == nil {
		return nil
	}
	// 槽位绑定统一从 SlotBindings 取（含 Slots 里的新槽位）：逐字段读会让
	// 「公告条里的文本」进不了候选集合 —— 表现是那块文案永远不翻译，而构建照常成功。
	bindings := p.Settings.Structure.SlotBindings()
	extra := make([]string, 0, len(bindings))
	for _, slot := range SortedSlots(bindings) {
		extra = append(extra, bindings[slot])
	}
	return AppendSEOCandidates(p, CollectContentCandidatesDeep(p, extra, resolve))
}

// AppendSEOCandidates 把 SEO 文本字段并入候选集合（审计 I18N-014）。
//
// 两条候选的来源是 Settings 而不是 AST，组件侧的 Translatable 白名单管不到
// （与菜单标签同一类盲区），必须在候选集合这一层补。
//
// **工作台与构建期必须看到同一份候选**：构建期走 CollectContentCandidatesForDocument，
// 工作台走 CollectContentCandidates（只扫 AST）。不补这一步，作者在工作台里根本看不到
// SEO 字段可填，而构建期又期待有译文 —— 表现是「功能像是没做」，而不是报错。
func AppendSEOCandidates(p *Page, cands []ContentCandidate) []ContentCandidate {
	if p == nil {
		return cands
	}
	out := make([]ContentCandidate, 0, 2+len(cands))
	if t := strings.TrimSpace(p.Settings.SEO.Title); t != "" {
		out = append(out, ContentCandidate{Context: SEOTitleContext, Source: t})
	}
	if d := strings.TrimSpace(p.Settings.SEO.Description); d != "" {
		out = append(out, ContentCandidate{Context: SEODescriptionContext, Source: d})
	}
	return append(out, cands...)
}

// CollectContentCandidatesDeep 收集「页面文档 + 构建期内联块」的全部可翻译候选。
//
// 为什么需要它（docs/06-D §15.11 遗留项）：块是**构建期展开**的——
//   - settings.structure 绑定的页眉/页脚块（extraBlockIDs）经 compileBlockFragment 编译；
//   - 页面文档内的 core.globalref 节点经 BlockResolver 内联展开。
//
// 两者渲染期都会走 applyContentTranslation，但其原文 hash 不在「只扫本页 AST」的
// CollectContentCandidates 结果里 → 取词器索引未预载 → 回退原文并计入 Misses。
// 本函数把块内文本并入同一份候选集合，装配层据此**一次**构造取词器（每页每语言
// 一次批量查库的约束不变）。
//
// 遍历规则：页面 root + extraBlockIDs + 二者中 core.globalref 引用的块递归展开；
// 同一块 ID 只解析一次（visited 去重，同时天然阻断 A→B→A 的引用环）。
// 块解析失败与渲染期一致降级（跳过该块，不阻断构建）。
func CollectContentCandidatesDeep(p *Page, extraBlockIDs []string, resolve BlockRootFunc) []ContentCandidate {
	if p == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []ContentCandidate
	for _, n := range p.Root {
		collectNodeCandidates(n, seen, &out)
	}
	visited := map[string]bool{}
	var walk func(roots []*core.Node)
	load := func(blockID string) {
		blockID = strings.TrimSpace(blockID)
		if resolve == nil || blockID == "" || visited[blockID] {
			return
		}
		visited[blockID] = true
		roots, err := resolve(blockID)
		if err != nil || len(roots) == 0 {
			return
		}
		walk(roots)
	}
	walk = func(roots []*core.Node) {
		for _, r := range roots {
			collectNodeCandidates(r, seen, &out)
		}
		for _, id := range ReferencedBlockIDs(roots) {
			load(id)
		}
	}
	for _, id := range ReferencedBlockIDs(p.Root) {
		load(id)
	}
	for _, id := range extraBlockIDs {
		load(id)
	}
	sortCandidates(out)
	return out
}

// ReferencedBlockIDs 返回 root 树内 core.globalref 节点引用的块 ID（去重、字典序）。
//
// 只读扫描、不解析块：装配层据此判断「本页是否引用了块」（内容译文依赖登记），
// 以及驱动 CollectContentCandidatesDeep 的递归展开。
func ReferencedBlockIDs(roots []*core.Node) []string {
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(n *core.Node)
	walk = func(n *core.Node) {
		if n == nil {
			return
		}
		if n.Type == globalref.Type {
			if id := blockIDOf(n.Props); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	sort.Strings(out)
	return out
}

// blockIDOf 从 globalref 节点 props 读取 blockId（非法/缺失返回空串）。
func blockIDOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var props struct {
		BlockID string `json:"blockId"`
	}
	if err := json.Unmarshal(raw, &props); err != nil {
		return ""
	}
	return strings.TrimSpace(props.BlockID)
}

// sortCandidates 候选排序：按 (context, source) 字典序（确定性，便于测试与日志）。
func sortCandidates(out []ContentCandidate) {
	slices.SortFunc(out, func(a, b ContentCandidate) int {
		return cmp.Or(strings.Compare(a.Context, b.Context), strings.Compare(a.Source, b.Source))
	})
}

// ContentHashes 候选集合 → 去重后的 source_hash 列表（一次 SQL 的入参）。
//
// 空候选返回 nil（取词器据此不发查询，§7.7「hashes 为空不发查询」）。
func ContentHashes(cands []ContentCandidate) []string {
	if len(cands) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(cands))
	hashes := make([]string, 0, len(cands))
	for _, c := range cands {
		h := i18n.ContentHash(c.Source)
		if seen[h] {
			continue
		}
		seen[h] = true
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	return hashes
}

// collectNodeCandidates 递归收集单节点（含子树）的候选。
func collectNodeCandidates(n *core.Node, seen map[string]bool, out *[]ContentCandidate) {
	if n == nil {
		return
	}
	if fields := core.TranslatableFields(n.Type); len(fields) > 0 {
		collectValue(n.Type, "", n.Props, false, fields, seen, out)
	}
	for _, c := range n.Children {
		collectNodeCandidates(c, seen, out)
	}
}

// collectValue 递归处理一个 JSON 值，按白名单判定是否候选。
//
// 规则（与 applyContentTranslation 严格对称）：
//   - 字符串：key 在白名单 → 候选（context = 类型.字段名，不带索引，决策 D11）；
//   - 数组：标量元素继承父 key 的白名单标记（覆盖 headers/rows 这类纯文本容器），
//     对象元素不继承（回到按各自字段名判断，避免 items 里的 url 被误翻）；
//   - 对象：逐 key 递归，key 决定是否白名单（覆盖 items[].title 这类嵌套字段）。
func collectValue(typeName, key string, raw json.RawMessage, whitelisted bool, fields map[string]bool, seen map[string]bool, out *[]ContentCandidate) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return
	}
	switch trimmed[0] {
	case '"':
		if !whitelisted {
			return
		}
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return
		}
		if !i18n.ShouldTranslateContent(s) {
			return // 跳过规则（纯数字/纯符号/空白），决策 F7
		}
		ctx := i18n.ContentContext(typeName, key)
		if ctx == "" {
			return
		}
		dedupe := ctx + "\x00" + s
		if seen[dedupe] {
			return
		}
		seen[dedupe] = true
		*out = append(*out, ContentCandidate{Context: ctx, Source: s})
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return
		}
		for _, el := range arr {
			t := bytes.TrimSpace(el)
			if len(t) == 0 {
				continue
			}
			if t[0] == '{' {
				collectValue(typeName, key, el, false, fields, seen, out)
				continue
			}
			collectValue(typeName, key, el, whitelisted, fields, seen, out)
		}
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return
		}
		for k, v := range obj {
			collectValue(typeName, k, v, fields[k], fields, seen, out)
		}
	}
}

// applyContentTranslation 返回 props 已按白名单替换为译文的节点副本。
//
// 无白名单 / 无取词函数 / 无替换时**返回原节点指针**（零开销、零影响：未接入内容
// 翻译的构建产物与改造前逐字节一致）。
//
// 原文不动（决策 F1）：只做浅拷贝 + 替换 Props 字节，AST 与 Page Document 不被修改。
func applyContentTranslation(n *core.Node, translate func(source, context string) string) *core.Node {
	if n == nil || translate == nil || len(n.Props) == 0 {
		return n
	}
	fields := core.TranslatableFields(n.Type)
	if len(fields) == 0 {
		return n
	}
	newProps, changed := translateProps(n.Type, n.Props, fields, translate)
	if !changed {
		return n
	}
	cp := *n
	cp.Props = newProps
	return &cp
}

// translateProps 逐字段替换 props 中的译文，返回新 props 字节与是否发生替换。
//
// 只重写「被替换的字符串字段」：未替换字段保持原始 RawMessage 字节（数字/布尔/
// 嵌套结构零精度损失）；整体仅在 changed 时重新序列化（map 键序由标准库排序，
// 对下游 json.Unmarshal 语义无影响）。
func translateProps(typeName string, raw json.RawMessage, fields map[string]bool, translate func(source, context string) string) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return raw, false
	}
	changed := false
	for key, val := range obj {
		nv, c := translateValue(typeName, key, val, fields[key], fields, translate)
		if c {
			obj[key] = nv
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false // 序列化失败：回退原文，绝不阻断构建
	}
	return out, true
}

// translateValue 递归替换一个 JSON 值中的可翻译字符串（规则与 collectValue 对称）。
func translateValue(typeName, key string, raw json.RawMessage, whitelisted bool, fields map[string]bool, translate func(source, context string) string) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, false
	}
	switch trimmed[0] {
	case '"':
		if !whitelisted {
			return raw, false
		}
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return raw, false
		}
		ctx := i18n.ContentContext(typeName, key)
		if ctx == "" {
			return raw, false
		}
		target := translate(s, ctx)
		if target == s || target == "" {
			return raw, false // 未命中回退原文 / 空译文：保持原字节
		}
		encoded, err := json.Marshal(target)
		if err != nil {
			return raw, false
		}
		return encoded, true
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return raw, false
		}
		changed := false
		for i, el := range arr {
			t := bytes.TrimSpace(el)
			if len(t) == 0 {
				continue
			}
			if t[0] == '{' {
				nv, c := translateValue(typeName, key, el, false, fields, translate)
				if c {
					arr[i] = nv
					changed = true
				}
				continue
			}
			nv, c := translateValue(typeName, key, el, whitelisted, fields, translate)
			if c {
				arr[i] = nv
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		out, err := json.Marshal(arr)
		if err != nil {
			return raw, false
		}
		return out, true
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return raw, false
		}
		changed := false
		for k, v := range obj {
			nv, c := translateValue(typeName, k, v, fields[k], fields, translate)
			if c {
				obj[k] = nv
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		out, err := json.Marshal(obj)
		if err != nil {
			return raw, false
		}
		return out, true
	}
	return raw, false
}
