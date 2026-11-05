package core

import (
	"fmt"
	json "go_wp/pkg/jsonx"
)

type LegacyKey struct {
	From string

	To string
}

func ApplyLegacyKeys(raw json.RawMessage, legacy []LegacyKey) (json.RawMessage, error) {
	if len(raw) == 0 || len(legacy) == 0 {
		return raw, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {

		return raw, fmt.Errorf("旧字段名归一失败: %w", err)
	}

	changed := false
	for _, lk := range legacy {
		oldVal, hasOld := obj[lk.From]
		if !hasOld {
			continue
		}
		curVal, hasCur := obj[lk.To]

		if hasCur && !isJSONEmptyString(curVal) {
			delete(obj, lk.From)
			changed = true
			continue
		}

		obj[lk.To] = oldVal
		delete(obj, lk.From)
		changed = true
	}
	if !changed {
		return raw, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, fmt.Errorf("旧字段名归一后序列化失败: %w", err)
	}
	return out, nil
}

func isJSONEmptyString(v json.RawMessage) bool {
	return string(v) == `""`
}

type LegacyNormalizer interface {
	NormalizeLegacy(raw json.RawMessage) error
}

func ApplyLegacyKeysInto(raw json.RawMessage, dst any, legacy []LegacyKey) error {
	if len(raw) == 0 || len(legacy) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("旧字段名归一失败: %w", err)
	}
	patched := make(map[string]json.RawMessage, len(legacy))
	for _, lk := range legacy {
		oldVal, hasOld := obj[lk.From]
		if !hasOld {
			continue
		}
		curVal, hasCur := obj[lk.To]
		if hasCur && !isJSONEmptyString(curVal) {
			continue
		}
		patched[lk.To] = oldVal
	}
	if len(patched) == 0 {
		return nil
	}

	patch, err := json.Marshal(patched)
	if err != nil {
		return fmt.Errorf("旧字段名归一后序列化失败: %w", err)
	}
	if err := json.Unmarshal(patch, dst); err != nil {
		return fmt.Errorf("旧字段名归一后反序列化失败: %w", err)
	}
	return nil
}

func DecodeProps[P any](raw json.RawMessage) (P, error) {
	var p P
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	if n, ok := any(&p).(LegacyNormalizer); ok {
		if err := n.NormalizeLegacy(raw); err != nil {
			return p, err
		}
	}
	if err := normalizeMediaProps(&p); err != nil {
		return p, err
	}
	return p, nil
}
