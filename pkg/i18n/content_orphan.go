package i18n

// content_orphan.go — sys_translation 孤儿行的**检出**与**显式清理**（审计 I18N-024）。
//
// ── 孤儿行的定义（本文件采用的口径，先写死再谈清理）─────────────────────────────
// 孤儿行 = **工程级译文行**（project_id 非空）里满足下列任一条的行：
//
//	(A) hash 自洽破损：source_hash ≠ sha256hex(trim(source_text))。
//	    构建期取词一律用 ContentHash(当前原文) 去查，破损行的 hash 永远不可能被命中，
//	    它已不可能被任何页面使用；写入口（content_write.go 第一条硬约束）也拒绝写这种
//	    行，所以这类行只可能来自历史脏数据或人工改库。
//	(B) 源引用消失：该 (source_hash, context) 不在**该工程当前源文档的候选取值集合**里。
//	    候选集合由调用方传入（构建期候选收集 / 工作台的全站扫描）：源数据散落在页面
//	    草稿、引用块与 CMS 实体里，pkg/i18n 不认识它们，在这里重扫会跨层依赖业务模块。
//
// ── 两条刻意不做的选择 ────────────────────────────────────────────────────────
//
//  1. **不清理全局行**（project_id IS NULL）：全局行服务所有工程，单个工程的扫描结果
//     无法判断它是否还被别的工程使用 —— 清掉它等于删掉别的站点的人工翻译。
//     全局行的退役应由运维显式确认后单独处理（本文件不提供入口）。
//
//  2. **不自动清理**（审计 I18N-024 的明确要求）：源数据可能只是暂时缺失 ——
//     页面回滚到旧草稿、实体临时下架、全站扫描因超限被跳过。这些时刻译文行看起来就是
//     孤儿，而自动删除丢掉的是编辑者逐条人工翻译的成果，恢复它要重翻一遍。
//     因此：ScanOrphans 永远只读；PurgeOrphans 必须显式调用（运维命令 / 后台操作），
//     apply=false 是纯 dry run；两种情况下都输出「检出 N 行、清理 M 行」的计数。
//
// 复算性：同一份数据 + 同一个候选集合，ScanOrphans 结果可复现（纯读 SQL + 内存判定，
// 不含时间窗、不含随机采样）。判据 A 的最终裁决在 Go 侧用 ContentHash 复核，与写入口
// 是同一个函数 —— 不在 SQL 里重写一份 trim/hash 规则（Unicode 空白在 PG 的 btrim 与
// Go 的 TrimSpace 下并不一致，两套规则必然漂移）。

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// 孤儿判据（报告与日志里的取值）。
const (
	// OrphanReasonBrokenHash 判据 A：source_hash 与 source_text 不符。
	OrphanReasonBrokenHash = "hash-broken"
	// OrphanReasonUnreferenced 判据 B：源文档已不再引用该 (source_hash, context)。
	OrphanReasonUnreferenced = "unreferenced"
)

// defaultOrphanScanLimit 单次检出上限：孤儿清理是低频运维动作，一次拉太多行没有意义，
// 反而让「检出即审阅」失去可读性。
const defaultOrphanScanLimit = 5000

// orphanPurgeChunk 删除时的元组分块大小（每条 3 个参数，200 条 = 600 个参数，远离 PG 上限）。
const orphanPurgeChunk = 200

// OrphanScope 检出范围。
//
// ProjectID 必填：没有工程就没有参照系（工程级译文行与候选集合必须落在同一个工程里）。
// 空值直接报错而不是「退化为全局」—— 那会把清理范围悄悄放大到所有站点。
type OrphanScope struct {
	// ProjectID 目标工程（必填）。
	ProjectID string
	// Lang 目标语言；空 = 该工程的全部语言。
	Lang string
	// Keep 保留键集合（ContentIndexKey(hash, context)）。
	//
	// nil = 只跑判据 A（安全默认：没有任何外部输入时只清「绝对不可能被命中」的行）；
	// 非 nil 且非空 = 判据 A + 判据 B；
	// 非 nil 但为空 = 判据 B 被跳过：空集合几乎总是「扫描失败 / 超限」而不是「真的没有
	// 候选」，照它删会把整个工程的译文清空，因此当作未提供处理（见 OrphanReport.KeptKeys）。
	Keep map[string]bool
	// Limit 单次检出上限；<=0 用 defaultOrphanScanLimit。
	Limit int
}

// OrphanRow 一行孤儿译文（含检出判据，供运维判断该不该删）。
type OrphanRow struct {
	SourceHash string
	Context    string
	Lang       string
	SourceText string
	TargetText string
	Engine     string
	UpdatedAt  time.Time
	// Reason 判据取值：OrphanReasonBrokenHash / OrphanReasonUnreferenced。
	Reason string
}

// OrphanReport 检出结果（只读，不删任何行）。
type OrphanReport struct {
	ProjectID string
	Lang      string
	// Scanned 扫描到的工程级译文行数。
	Scanned int64
	// Broken 判据 A 命中数。
	Broken int64
	// Unreferenced 判据 B 命中数。
	Unreferenced int64
	// KeptKeys 是否真的提供了候选集合（false = 判据 B 未启用）。
	KeptKeys bool
	// Truncated 是否触到 Limit（结果不完整，仅供人工参考，不宜据此批量删除）。
	Truncated bool
	// Rows 命中的孤儿行（最多 Limit 条）。
	Rows []OrphanRow
}

// Orphans 孤儿行总数（= 「检出 N 行」的 N）。
func (r OrphanReport) Orphans() int64 { return r.Broken + r.Unreferenced }

// OrphanPurgeResult 清理结果（显式操作，输出「检出 N 行、清理 M 行」）。
type OrphanPurgeResult struct {
	// Detected 检出 N 行。
	Detected int64
	// Deleted 清理 M 行（DryRun 时恒为 0）。
	Deleted int64
	// DryRun 是否只预览（未显式确认 apply 时一律为 true）。
	DryRun bool
	// KeptKeys / Truncated 与 OrphanReport 同义，便于调用方判断结果可信度。
	KeptKeys  bool
	Truncated bool
	// Rows 被检出的行（apply 时即已删除的行）。
	Rows []OrphanRow
}

// OrphanPurgeText 输出「检出 N 行、清理 M 行」的统一文案（运维命令与后台共用）。
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

// orphanScanQuery 扫描某工程的工程级译文行（全局行不在清理范围，见文件头）。
// $1 = uuid（工程），$2 = text（语言，空串 = 全部语言），$3 = int（上限 + 1，用于探测截断）。
const orphanScanQuery = `SELECT source_hash, context, lang, source_text, target_text, engine, update_time
FROM sys_translation
WHERE project_id = $1::uuid AND ($2 = '' OR lang = $2)
ORDER BY update_time DESC
LIMIT $3`

// orphanScanRow 扫描投影。
type orphanScanRow struct {
	SourceHash string    `gorm:"column:source_hash"`
	Context    string    `gorm:"column:context"`
	Lang       string    `gorm:"column:lang"`
	SourceText string    `gorm:"column:source_text"`
	TargetText string    `gorm:"column:target_text"`
	Engine     string    `gorm:"column:engine"`
	UpdatedAt  time.Time `gorm:"column:update_time"`
}

// ScanOrphans 检出工程级孤儿译文（只读，不删除）。
//
// 判据 A 用 ContentHash 在 Go 侧复核（与写入口同一条规则）；判据 B 只在提供了非空
// 候选集合时启用。源数据暂时缺失造成的「假孤儿」不会被自动处理 —— 是否真的不再需要，
// 由运维看报告判断（见文件头）。
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
	// sys_translation 在迁移 215 的 RLS 名单里，策略只额外放行 project_id IS NULL 的全局行，
	// 而本判据针对的正是工程级行 —— 不设作用域时这条查询在非超级角色下恒 0 行，
	// 运维会读到「检出 0 行、清理 0 行」并以为库里没有孤儿。
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

// PurgeOrphans 清理工程级孤儿译文（审计 I18N-024：显式触发 + 计数输出）。
//
// apply=false：纯预览，Detected 为检出数、Deleted 恒为 0。
// apply=true ：按检出结果分块删除，返回实际删除行数。
//
// 删除是**按行的 (source_hash, context, lang) 元组**精确命中，不做范围删除 ——
// 「按 hash 删」会把同一原文在其它语境下的译文一起带走，「按时间删」根本不是孤儿判据。
// 全局行（project_id IS NULL）永远不在此方法的删除范围内。
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
	// 截断时仍删除已检出的那部分：它们本身是确定命中的行，调用方看到 Truncated
	// 应再跑一次（下一轮会检出剩下的）。
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

// deleteOrphanChunk 按元组分块删除（返回实际删除行数）。
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
	// 与 ScanOrphans 同理：不设作用域时 DELETE 影响 0 行且不报错，清理在日志里看起来是「成功」的。
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

// orphanRowOf 扫描行 → 孤儿行（附判据）。
func orphanRowOf(row orphanScanRow, reason string) OrphanRow {
	return OrphanRow{
		SourceHash: row.SourceHash, Context: row.Context, Lang: row.Lang,
		SourceText: row.SourceText, TargetText: row.TargetText, Engine: row.Engine,
		UpdatedAt: row.UpdatedAt, Reason: reason,
	}
}
