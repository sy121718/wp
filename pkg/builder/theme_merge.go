package builder

import (
	json "go_wp/pkg/jsonx"
	"strings"
)

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

		return override
	}
	return out
}

func InjectThemeID(themeJSON json.RawMessage, themeID string) json.RawMessage {
	if strings.TrimSpace(themeID) == "" {
		return themeJSON
	}
	m := rawToThemeMap(themeJSON)
	if m == nil {

		return themeJSON
	}
	m["themeId"] = themeID
	out, err := json.Marshal(m)
	if err != nil {
		return themeJSON
	}
	return out
}

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

func mergeThemeMap(base, override map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, ov := range override {

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
