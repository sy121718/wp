package projectservice

// theme_bundle_doc.go — 主题包文档处理：块引用改写与媒体引用扫描（审计 VIS-014）。
//
// 块 id 渗进文档内容的位置（与迁移 209 的 fn_rewrite_block_ids 清单一致）：
//   - props.blockId（globalref 节点，root 树任意深度）
//   - settings.structure.headerBlockId / footerBlockId
//   - settings.slots.*（槽位名 → 块 id）
//
// 两个方向各一份实现，共用同一套键名判据：
//   - 导出：块 id → 包内 key（包里不留原 id）
//   - 导入：包内 key → 新分配的 id
//
// 为什么用 json.Number 解码：文档里可能有 int64 级数值（节点尺寸、计数），
// 默认解码成 float64 会把大整数悄悄改写（9007199254740993 → 9007199254740992）。
// 保真优先于便利。

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// blockRefKeys 文档里承载块 id 的固定键名。
var blockRefKeys = map[string]struct{}{
	"blockId":       {},
	"headerBlockId": {},
	"footerBlockId": {},
}

// decodeBundleJSON 解码 JSON（保留数字字面量）。
func decodeBundleJSON(raw []byte) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// encodeBundleJSON 编码 JSON（紧凑输出，键按字典序 —— jsonb 本身不保序，
// 顺序变化不影响语义，但保证同一输入产出同一字节）。
func encodeBundleJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// rewriteBundleBlockRefs 递归改写文档里的块引用。
//
// 语义与迁移 209 的 SQL 实现一致：
//   - 键名为 blockId / headerBlockId / footerBlockId 且值是字符串 → 命中映射则替换，未命中原样保留；
//   - 键名为 slots 且值是对象 → 只映射其**值**（槽位名是任意字符串，不可能做键名判据）；
//   - 其余键递归；未命中的值原样保留（透传不丢弃，含未知键）。
func rewriteBundleBlockRefs(v any, mapping map[string]string) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			if k == "slots" {
				if obj, ok := child.(map[string]any); ok {
					slots := make(map[string]any, len(obj))
					for slot, raw := range obj {
						if s, ok := raw.(string); ok {
							if mapped, hit := mapping[s]; hit {
								slots[slot] = mapped
								continue
							}
						}
						slots[slot] = raw
					}
					out[k] = slots
					continue
				}
				out[k] = rewriteBundleBlockRefs(child, mapping)
				continue
			}
			if _, isRef := blockRefKeys[k]; isRef {
				if s, ok := child.(string); ok {
					if mapped, hit := mapping[s]; hit {
						out[k] = mapped
						continue
					}
				}
			}
			out[k] = rewriteBundleBlockRefs(child, mapping)
		}
		return out
	case []any:
		out := make([]any, 0, len(val))
		for _, child := range val {
			out = append(out, rewriteBundleBlockRefs(child, mapping))
		}
		return out
	default:
		return v
	}
}

// collectBundleBlockRefs 收集文档里出现的全部块引用值（去重升序）。
func collectBundleBlockRefs(v any) []string {
	found := map[string]struct{}{}
	var walk func(node any)
	walk = func(node any) {
		switch val := node.(type) {
		case map[string]any:
			for k, child := range val {
				if k == "slots" {
					if obj, ok := child.(map[string]any); ok {
						for _, raw := range obj {
							if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
								found[s] = struct{}{}
							}
						}
						continue
					}
					walk(child)
					continue
				}
				if _, isRef := blockRefKeys[k]; isRef {
					if s, ok := child.(string); ok && strings.TrimSpace(s) != "" {
						found[s] = struct{}{}
						continue
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range val {
				walk(child)
			}
		}
	}
	if v == nil {
		return nil
	}
	walk(v)
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// collectThemeMediaURLs 从文档字节里提取媒体引用 URL（/storage/... 形态）。
//
// 扫描原始字节而不是遍历结构：媒体引用散落在图片 src、图集 items[].url、
// 卡片 imageSrc、富文本 HTML 片段等几十处字段里，字段名清单会随组件演进漂移；
// 而「产物里出现的媒体地址」只有一种形态，扫字节不会漏。终止符与媒体模块
// 收集产物引用时保持一致。
func collectThemeMediaURLs(raw []byte) []string {
	const prefix = "/storage/"
	seen := map[string]struct{}{}
	rest := string(raw)
	for {
		idx := strings.Index(rest, prefix)
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		end := 0
		for end < len(rest) {
			if isThemeMediaURLStop(rest[end]) {
				break
			}
			end++
		}
		url := rest[:end]
		rest = rest[end:]
		if url == prefix || strings.Contains(url, "..") {
			continue
		}
		seen[url] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for u := range seen {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// isThemeMediaURLStop 媒体 URL 的终止字节：双引号(0x22)/单引号(0x27)/右括号(0x29)/
// 尖括号(0x3c/0x3e)/空白(0x20/0x0a/0x0d/0x09)。
// 用码点常量而不是字符字面量：这是字节级判据，写成码点不会随源码里的引号转义写法走样。
func isThemeMediaURLStop(c byte) bool {
	switch c {
	case 0x22, 0x27, 0x29, 0x3c, 0x3e, 0x20, 0x0a, 0x0d, 0x09:
		return true
	}
	return false
}

// storageRelativePath 把 /storage/... URL 解析为存储根下的相对路径。
//
// 与媒体模块的反查口径一致：去掉查询串与锚点、去前导斜杠、拒绝上溯段。
// 返回 false 表示这条 URL 不是本地上传目录下的文件（外链、data: 等）。
func storageRelativePath(url string) (string, bool) {
	const prefix = "/storage/"
	raw := strings.TrimSpace(url)
	idx := strings.Index(raw, prefix)
	if idx < 0 {
		return "", false
	}
	raw = raw[idx+len(prefix):]
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" || strings.Contains(raw, "..") {
		return "", false
	}
	return raw, true
}
