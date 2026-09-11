package builder

// theme_merge.go — 主题设置的分层合并（主题 → 页面 → 组件 三层继承的中间层）。
//
// 语义：每一层的「空」都表示继承上一层。
//   站点主题（工程激活主题）→ 页面 settings.themeOverride → 组件 props
// 页面只想改主色时不该被迫重写整份主题；主题日后改了字体，页面没显式改过的字体项
// 也要跟着变。所以合并必须在**键级别**做，而不是整份替换。

import (
	"encoding/json"
	"strings"
)

// MergeThemeSettings 以 base（站点主题）为底、override（页面覆盖）为上层深合并：
// 只有 override 里显式给值的键覆盖 base，其余继续跟随 base。
//
// 实现走 JSON 层深合并而不是逐字段赋值 —— 主题令牌有几十个键，逐字段手写必然漏，
// 加了新令牌还得记得回来补一行。两边都靠 omitempty 让空值不出现，天然就是「非空覆盖」。
func MergeThemeSettings(base, override *ThemeSettings) *ThemeSettings {
	switch {
	case override == nil:
		return base
	case base == nil:
		return override
	}
	merged := mergeThemeMap(themeToMap(base), themeToMap(override))
	out := &ThemeSettings{}
	raw, err := json.Marshal(merged)
	if err != nil {
		return override
	}
	if err := json.Unmarshal(raw, out); err != nil {
		// 合并结果反序列化失败（理论不可达）：退回 override，至少页面显式值不丢。
		return override
	}
	return out
}

// MergeThemeRawJSON 以主题原始 JSON 为底、页面覆盖原始 JSON 为上层深合并，
// **保留双方的每一个键**。两边都是空则返回空。
//
// 为什么不经过 ThemeSettings 结构体：结构体只认识当前版本的字段名，
// 「解析 → 序列化」会把历史版本或未来的键悄悄丢掉（测试数据里那个顶层 fontFamily
// 就是这么消失的）。快照是展示层数据，保真比规范化更重要。
func MergeThemeRawJSON(themeJSON, overrideJSON json.RawMessage) json.RawMessage {
	base := rawToThemeMap(themeJSON)
	override := rawToThemeMap(overrideJSON)
	switch {
	case len(base) == 0 && len(override) == 0:
		return nil
	case len(override) == 0:
		return themeJSON
	case len(base) == 0:
		return overrideJSON
	}
	out, err := json.Marshal(mergeThemeMap(base, override))
	if err != nil {
		return themeJSON
	}
	return out
}

// isEmptyThemeValue 判断覆盖值是否等于「未设置」（空串 / null / 空对象 / 空数组）。
func isEmptyThemeValue(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	case map[string]any:
		return len(val) == 0
	case []any:
		return len(val) == 0
	}
	return false
}

// rawToThemeMap 把原始 JSON 对象解析成 map（非对象/非法 JSON 视为空）。
func rawToThemeMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// themeToMap 把主题设置转成 map（omitempty 的空字段不出现，正好等于「未设置」）。
func themeToMap(t *ThemeSettings) map[string]any {
	raw, err := json.Marshal(t)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// mergeThemeMap 递归合并：对象逐键合并，标量与数组由 override 整体覆盖。
func mergeThemeMap(base, override map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, ov := range override {
		// 空值 = 未覆盖：面板把「留空」序列化成 ""（或 null / 空对象）时，
		// 语义仍是「这一项跟随站点主题」，不能拿空串把主题的值盖掉。
		if isEmptyThemeValue(ov) {
			continue
		}
		bv, ok := out[k]
		if !ok {
			out[k] = ov
			continue
		}
		bMap, bIsMap := bv.(map[string]any)
		oMap, oIsMap := ov.(map[string]any)
		if bIsMap && oIsMap {
			out[k] = mergeThemeMap(bMap, oMap)
			continue
		}
		out[k] = ov
	}
	return out
}
