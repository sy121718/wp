package pagemodel

// page_langs_model.go — 页面级语言排除（迁移 491：pages.excluded_langs）。
//
// 语义：被排除的语言**本页不产出**，也不进语言切换器 / hreflang / sitemap；
// 空数组 = 全部站点语言都产出（默认行为，存量行零变更）。

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// StringArray PG 原生 text[] 的 Go 映射。
//
// 与其它模块的同名类型是**有意重复**（跨模块不得 import 对方 model）。
//
// 为什么不用现成方案（两种都**实测**失败了）：
//
//	· 裸 []string：GORM 经 pgx 编码成元组字面量 ('a','b')，PG 报 malformed array literal (22P02)；
//	· pgtype.FlatArray[string]：在 pgx **原生**路径下可用，但 GORM 走 database/sql 路径，
//	  它不被识别、仍编码成 record，PG 报 "column excluded_langs is of type text[] but expression
//	  is of type record" (42804)。
//
// 所以本类型直接产出 / 解析 PG 数组字面量，不依赖 driver 的编码约定。

type StringArray []string

// Value 实现 driver.Valuer：nil 与空切片都写 '{}'（列是 NOT NULL DEFAULT '{}'）。
func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	return encodePGTextArray(a), nil
}

// Scan 实现 sql.Scanner：接受 PG 返回的数组字面量文本。
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

// encodePGTextArray 编码成 PG 数组字面量 {"a","b"}。
//
// 元素一律加引号并转义反斜杠与双引号 —— 标签里出现逗号、空格、引号都不会破坏结构。
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

// decodePGTextArray 解析 PG 数组字面量（处理引号、反斜杠转义、空数组）。
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

// UpdateExcludedLangs 覆盖写入本页的排除语言集合（空切片 = 全部站点语言都产出）。
//
// 自足入口（自带事务与工程作用域）；事务内的调用方走 UpdateExcludedLangsTx。
func (m *Model) UpdateExcludedLangs(ctx context.Context, projectID, pageID string, langs []string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.UpdateExcludedLangsTx(ctx, tx, projectID, pageID, langs)
	})
}

// UpdateExcludedLangsTx 在**调用方给的事务句柄**上覆盖写入排除语言集合。
//
// 只覆盖这一列（不用 Save/Updates(struct)）：结构体更新会把零值字段一并写回，
// 而这里是「整组替换」语义的单个字段。空切片与 nil 都写 '{}'（列 NOT NULL DEFAULT '{}'，
// 写 NULL 会触发约束错误）。
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
		Updates(map[string]any{
			"excluded_langs": val,
			"update_time":    gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 页面不存在 / 已删 / 不属于该工程 —— 静默 0 行会让上层以为保存成功。
		return gorm.ErrRecordNotFound
	}
	return nil
}
