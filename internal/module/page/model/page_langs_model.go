package pagemodel

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	return encodePGTextArray(a), nil
}

func (a *StringArray) Scan(src any) error {
	if src == nil {
		*a = StringArray{}
		return nil
	}
	var raw string
	switch v := src.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("StringArray: 不支持的来源类型 %T", src)
	}
	parsed, err := decodePGTextArray(raw)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

func encodePGTextArray(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		esc := strings.ReplaceAll(v, "\\", "\\\\")
		esc = strings.ReplaceAll(esc, "\"", "\\\"")
		parts = append(parts, "\""+esc+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func decodePGTextArray(raw string) (StringArray, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "{}" {
		return StringArray{}, nil
	}
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, fmt.Errorf("StringArray: 非法数组字面量 %q", raw)
	}
	body := s[1 : len(s)-1]
	out := StringArray{}
	var cur strings.Builder
	inQuote, escaped, started := false, false, false
	for _, r := range body {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
			started = true
		case r == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
			started = false
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("StringArray: 数组字面量引号未闭合: %q", raw)
	}
	if started || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}

func (m *Model) UpdateExcludedLangs(ctx context.Context, projectID, pageID string, langs []string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.UpdateExcludedLangsTx(ctx, tx, projectID, pageID, langs)
	})
}

func (m *Model) UpdateExcludedLangsTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, langs []string) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	val := StringArray(langs)
	if val == nil {
		val = StringArray{}
	}
	res := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		Updates(mergeInputChange(map[string]any{
			"excluded_langs": val,
		}, time.Now().UTC()))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {

		return gorm.ErrRecordNotFound
	}
	return nil
}
