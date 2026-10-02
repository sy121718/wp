package pagemodel

// page_translation_miss_model.go — 缺译报告的数据源（U2）。
//
// 来源是**产物自己的 Manifest**：`page_artifacts.manifest -> 'translationMisses'`
// （构建期写入，见 pipeline.ManifestTranslationMisses）。为什么查产物而不是实时算：
// 缺失是「这一份**已产出的字节**里有多少取词没命中」——它是构建期的事实，
// 实时重算会得出与线上字节不一致的第二份真相（译者补了译文但还没重建，实时算显示 0，
// 而线上那份产物仍然是回退原文）。

import (
	"context"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// TranslationMissRow 一个（页面 × 语言）的缺译情况。
//
// **两个量的量纲不同，不能相除**（见 U0 的结论）：
//   - Candidates 是**去重后的可翻译字段数**（builder.CollectContentCandidates 去重）；
//   - Misses 是**渲染期取词未命中的调用次数**（同一个字段被渲染多次就计多次）。
//
// 因此展示只给条数，不给百分比（misses > candidates 是正常的）。
type TranslationMissRow struct {
	PageID     string `gorm:"column:page_id"`
	DraftPath  string `gorm:"column:draft_path"`
	Lang       string `gorm:"column:lang"`
	Version    int64  `gorm:"column:version"`
	Misses     int64  `gorm:"column:misses"`
	Candidates int64  `gorm:"column:candidates"`
}

// ListTranslationMisses 列出该工程**每个（页面 × 语言）最新产物**的缺译情况，只含 misses > 0。
//
// 三个刻意的取舍：
//   - **每个 (page_id, lang) 只取最新版本**（DISTINCT ON + version DESC）：同一语言有多份
//     历史产物，全列出来会让同一个语言在报告里出现多次，运营无法判断「现在到底缺多少」；
//   - `(…)::bigint > 0` 放在外层子查询里：DISTINCT ON 必须先按 (page_id, lang, version DESC)
//     排序才能挑出最新一行，直接在 WHERE 里过滤会先砍掉候选行、挑出的「最新」是错的；
//   - 用 COALESCE 兜住没有该键的产物（默认语言产物本来就没有 content translation），
//     缺键 ≠ 0 缺失，但对“只列 misses>0”这个用途两者同样被排除。
func (m *Model) ListTranslationMisses(ctx context.Context, projectID string) (rows []TranslationMissRow, err error) {
	const sql = `
SELECT t.page_id, t.draft_path, t.lang, t.version, t.misses, t.candidates
  FROM (
    SELECT DISTINCT ON (a.page_id, a.lang)
           a.page_id, p.draft_path, a.lang, a.version,
           COALESCE((a.manifest->'translationMisses'->>'misses')::bigint, 0)     AS misses,
           COALESCE((a.manifest->'translationMisses'->>'candidates')::bigint, 0) AS candidates
      FROM page_artifacts a
      JOIN pages p ON p.id = a.page_id
     WHERE p.project_id = ? AND p.deleted_at IS NULL
     ORDER BY a.page_id, a.lang, a.version DESC
  ) t
 WHERE t.misses > 0
 ORDER BY t.misses DESC, t.draft_path ASC, t.lang ASC`
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Raw(sql, projectID).Scan(&rows).Error
	})
	return rows, err
}
