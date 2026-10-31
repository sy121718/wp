package i18n

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const (
	OrphanReasonBrokenHash = "hash-broken"

	OrphanReasonUnreferenced = "unreferenced"
)

const defaultOrphanScanLimit = 5000

const orphanPurgeChunk = 200

type OrphanScope struct {
	ProjectID string

	Lang string

	Keep map[string]bool

	Limit int
}

type OrphanRow struct {
	SourceHash string
	Context    string
	Lang       string
	SourceText string
	TargetText string
	Engine     string
	UpdatedAt  time.Time

	Reason string
}

type OrphanReport struct {
	ProjectID string
	Lang      string

	Scanned int64

	Broken int64

	Unreferenced int64

	KeptKeys bool

	Truncated bool

	Rows []OrphanRow
}

func (r OrphanReport) Orphans() int64 { return r.Broken + r.Unreferenced }

type OrphanPurgeResult struct {
	Detected int64

	Deleted int64

	DryRun bool

	KeptKeys  bool
	Truncated bool

	Rows []OrphanRow
}

func (r OrphanPurgeResult) OrphanPurgeText() string {
	mode := "仅预览（dry run）"
	if !r.DryRun {
		mode = "已执行删除"
	}
	text := fmt.Sprintf("检出 %d 行、清理 %d 行（%s）", r.Detected, r.Deleted, mode)
	if !r.KeptKeys {
		text += "；未提供候选集合，仅按 hash 破损判据检出"
	}
	if r.Truncated {
		text += "；结果被上限截断，请缩小范围后重跑"
	}
	return text
}

const orphanScanQuery = `SELECT source_hash, context, lang, source_text, target_text, engine, update_time
FROM sys_translation
WHERE project_id = $1::uuid AND ($2 = '' OR lang = $2)
ORDER BY update_time DESC
LIMIT $3`

type orphanScanRow struct {
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	Lang       string    `gorm:"column:lang"`
	SourceText string    `gorm:"column:source_text"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

func (w *ContentWriter) ScanOrphans(ctx context.Context, scope OrphanScope) (OrphanReport, error) {
	report := OrphanReport{ProjectID: strings.TrimSpace(scope.ProjectID), Lang: strings.TrimSpace(scope.Lang)}
	if w == nil || w.db == nil {
		return report, ErrContentWriteUnavailable
	}
	if report.ProjectID == "" {
		return report, ErrOrphanProjectEmpty
	}
	limit := scope.Limit
	if limit <= 0 {
		limit = defaultOrphanScanLimit
	}
	keptKeys := len(scope.Keep) > 0
	report.KeptKeys = keptKeys

	var rows []orphanScanRow

	if err := rls.InProjectScope(ctx, w.db, report.ProjectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Raw(orphanScanQuery, report.ProjectID, report.Lang, limit+1).Scan(&rows).Error
	}); err != nil {
		return report, err
	}
	if len(rows) > limit {
		report.Truncated = true
		rows = rows[:limit]
	}
	report.Scanned = int64(len(rows))

	for _, row := range rows {
		switch {
		case ContentHash(row.SourceText) != row.SourceHash:
			report.Broken++
			report.Rows = append(report.Rows, orphanRowOf(row, OrphanReasonBrokenHash))
		case keptKeys && !scope.Keep[ContentIndexKey(row.SourceHash, row.Context)]:
			report.Unreferenced++
			report.Rows = append(report.Rows, orphanRowOf(row, OrphanReasonUnreferenced))
		}
	}
	return report, nil
}

func (w *ContentWriter) PurgeOrphans(ctx context.Context, scope OrphanScope, apply bool) (OrphanPurgeResult, error) {
	result := OrphanPurgeResult{DryRun: !apply}
	report, err := w.ScanOrphans(ctx, scope)
	if err != nil {
		return result, err
	}
	result.Detected = report.Orphans()
	result.KeptKeys = report.KeptKeys
	result.Truncated = report.Truncated
	result.Rows = report.Rows
	if !apply || len(report.Rows) == 0 {
		return result, nil
	}

	for start := 0; start < len(report.Rows); start += orphanPurgeChunk {
		end := start + orphanPurgeChunk
		if end > len(report.Rows) {
			end = len(report.Rows)
		}
		n, derr := w.deleteOrphanChunk(ctx, report.ProjectID, report.Rows[start:end])
		if derr != nil {
			return result, derr
		}
		result.Deleted += n
	}
	return result, nil
}

func (w *ContentWriter) deleteOrphanChunk(ctx context.Context, projectID string, rows []OrphanRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tuples := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*3+1)
	for i, row := range rows {
		tuples = append(tuples, fmt.Sprintf("($%d, $%d, $%d)", i*3+1, i*3+2, i*3+3))
		args = append(args, row.SourceHash, row.Context, row.Lang)
	}
	args = append(args, projectID)
	sql := "DELETE FROM sys_translation t USING (VALUES " + strings.Join(tuples, ", ") + ") AS o(source_hash, context, lang) " +
		"WHERE t.source_hash = o.source_hash AND t.context = o.context AND t.lang = o.lang " +
		"AND t.project_id = $" + strconv.Itoa(len(args)) + "::uuid"

	var affected int64
	if err := rls.InProjectScope(ctx, w.db, projectID, func(tx *gorm.DB) error {
		res := tx.WithContext(ctx).Exec(sql, args...)
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		return nil
	}); err != nil {
		return 0, err
	}
	return affected, nil
}

func orphanRowOf(row orphanScanRow, reason string) OrphanRow {
	return OrphanRow{
		SourceHash: row.SourceHash, Context: row.Context, Lang: row.Lang,
		SourceText: row.SourceText, TargetText: row.TargetText, Engine: row.Engine,
		UpdatedAt: row.UpdatedAt, Reason: reason,
	}
}
