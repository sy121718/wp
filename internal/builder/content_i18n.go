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
	"encoding/json"
	"sort"

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
	seen := map[string]bool{}
	var out []ContentCandidate
	for _, n := range p.Root {
		collectNodeCandidates(n, seen, &out)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Context != out[j].Context {
			return out[i].Context < out[j].Context
		}
		return out[i].Source < out[j].Source
	})
	return out
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
