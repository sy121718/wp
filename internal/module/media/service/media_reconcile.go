package mediaservice

// media_reconcile.go — 媒体「文件系统 ↔ 数据库」的对账与可重放补偿。
//
// # 为什么这里不能用事务
//
// 上传 / 换图 / 生成变体都同时动**两处持久化**：文件系统（字节）与 PostgreSQL（元数据）。
// 事务只能覆盖其中一处 —— 文件系统没有回滚语义，也没有跨系统两阶段提交：
//
//   - 把落盘放进事务：事务回滚时已经写下的字节留在磁盘上，成为「有文件没记录」；
//   - 把落盘放在事务前、失败就补删：进程在「落盘成功后、DB 提交前」退出（OOM / 宕机 /
//     部署重启）时补删代码根本没机会执行，同样留下孤儿文件。
//
// 所以这一段只做两件事，都不依赖事务：
//
//   1. **对账（ReconcileStorage）**：只读地把三类不一致找出来 ——
//      「有记录没文件」「有文件没记录」「草稿残留」，连**可定位的数据**
//      （表名 / id / 路径）一起打回给人。**不自动删、不自动合并、不丢行**：
//      自动处理会把「可能只是这次没扫到」直接变成不可逆的数据丢失。
//   2. **重放补偿（ReplayVariantBackfill）**：把「附件在、变体记录不齐 / 没就绪」的附件
//      重新投递变体生成任务。**幂等**（GenerateVariants 重跑先清旧记录再重建）
//      + **留痕**（每条都记结构化日志）+ **可重放**（重复调用不产生副作用叠加）。
//      复用既有队列与既有的生成入口，不另造一套重试。
//
// 单一真源口径（与 media_crud.go 头部一致）：
//   - 元数据真源 = sys_attachment（status=1 且 file_path 非空的行）；变体 = sys_media_variant；
//   - 内容真源 = 存储根目录下的文件字节。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
)

// 对账清单规模上限：本入口是人工触发的巡检，不是请求路径上的查询。
const (
	reconcileDefaultLimit = 200
	reconcileMaxLimit     = 2000
)

// variantBackfillDefaultLimit 变体补偿的默认批量，与对账的 200 **刻意不同**。
//
// 为什么补偿必须一次覆盖全库：它按 id 升序取前 N 个附件、再逐个判定 ——
// **已齐的附件同样占住窗口**。N 小于附件总数时，排在 N 之后的那批永远等不到补偿。
// 实测（2026-10-02，419 个附件、走默认 200）：首轮 fixed=198，第二轮就变成
// fixed=0 / skipped=200（同一批全部已齐），第三轮起完全空转 ——
// 存量永久停在「记录不齐」，而日志上每轮都显示「巡检正常」。
//
// 对账侧保留 200 是刻意的：那一轮要遍历整个存储目录，是固定成本，宁可截断。
// 补偿的判定本身极廉价：全部已齐时实测 200 个附件耗时 33ms，
// 只有真需要重建的那批才产生解码/编码开销。
//
// 附件数超过本上限时仍会截断 —— 届时的正解是把窗口筛成「只取记录不齐的附件」
// （SQL 层），而不是继续调大这个数字。
const variantBackfillDefaultLimit = 2000

// 反向归属的三个命名约定（只有命中才敢把文件算作某个附件的产物）。
//
// 为什么不能笼统写成「纯数字开头」：存量随机名是 <unixNano>_<6位hex>.<ext>
// （pkg/upload 的 buildObjectKey 默认分支），它同样以数字开头 —— 按「数字开头」归属
// 会把每一张历史图片都报成「附件 id 不存在」的孤儿，整份对账就没人看了。
var (
	// 原图：<id>.<ext>（上传时用主键命名，见 media_crud.go 的 objectKey 推导）。
	mediaOriginalRe = regexp.MustCompile(`^(\d+)\.[A-Za-z0-9]{1,10}$`)
	// 变体：<id>_<variantType>[-<generation>-<hash8>].jpg
	// （variantObjectKey 的预期名 / variantObjectKeyFingerprinted 的真实名）。
	//
	// 类型词必须**同时认两代**：
	//   · webp —— 历史槽位名。迁移 496 只在 DB 层把 variant_type 改名为 full，
	//     磁盘上那批 *_webp.jpg **不会被改名**（file_path 仍指向它们）。
	//     不认它们 = 这些文件从「可归属」退化成「无法归属」，对账能力凭空倒退。
	//   · full —— 迁移后的新名与今后生成的名字。small 是 2026-10-02 新增的中间档。
	// 生成侧只产出 thumb/small/medium/full（见 mediamodel.VariantTypes），
	// 认 webp 纯粹是为了**读懂历史**。
	//
	// 指纹段**必须可选**：磁盘上同时存在两代命名 —— 存量文件与历史产物引用的是
	// 旧名（无指纹），新生成的是带指纹名。只认新名会把全部存量变体报成「有文件没记录」。
	mediaVariantRe = regexp.MustCompile(`^(\d+)_(?:thumb|small|medium|full|webp)(?:-\d+-[0-9a-f]{8})?\.jpg$`)
	// 无扩展名原图：<id>。
	mediaBareIDRe = regexp.MustCompile(`^(\d+)$`)
)

// mediaFileAttachmentID 把磁盘文件（相对存储根的斜杠路径）反向归属到附件 id。
// ok=false 表示无法归属（存量随机名、语义化原名、暂存残留等），调用方只计数不报孤儿。
func mediaFileAttachmentID(relPath string) (uint64, bool) {
	base := relPath
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	for _, re := range []*regexp.Regexp{mediaVariantRe, mediaOriginalRe, mediaBareIDRe} {
		m := re.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		if id := parseUint64(m[1]); id > 0 {
			return id, true
		}
	}
	return 0, false
}

// ReconcileReq 对账参数。
type ReconcileReq struct {
	// Limit 每类清单最多返回多少条（默认 200，上限 2000）。清单被截断时报告里显式说明。
	Limit int
	// SkipFiles 为 true 时只做「有记录没文件」这一半，不遍历存储目录（大目录上更便宜）。
	SkipFiles bool
}

// 不一致分类。
const (
	// ReconcileKindMissingFile 有记录没文件：DB 行指向的路径在磁盘上不存在。
	ReconcileKindMissingFile = "missing_file"
	// ReconcileKindOrphanFile 有文件没记录：磁盘文件不符合任何 DB 行的路径。
	ReconcileKindOrphanFile = "orphan_file"
	// ReconcileKindDraftRow 草稿残留：status=0 且 file_path 为空的上传半成品行。
	ReconcileKindDraftRow = "draft_row"
)

// ReconcileItem 一条可定位的不一致（给人工判断用，不带任何自动处理动作）。
//
// 全部字段都是**可定位数据**：表名 + 主键 + 路径 / 文件名，让人能直接去查那一条。
type ReconcileItem struct {
	Kind   string `json:"kind"`
	Table  string `json:"table"`
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

// ReconcileReport 对账报告（只读；不含任何修复动作的结果）。
type ReconcileReport struct {
	StorageRoot string `json:"storageRoot"`
	// AttachmentsTotal 附件行总数；AttachmentsScanned 本次实际读到的行数（截断时小于总数）。
	AttachmentsTotal   int64 `json:"attachmentsTotal"`
	AttachmentsScanned int   `json:"attachmentsScanned"`
	VariantsScanned    int   `json:"variantsScanned"`
	FilesScanned       int   `json:"filesScanned"`
	// UnattributedFiles 不能按命名约定归属到附件 id 的磁盘文件数（存量随机名），不计入孤儿。
	UnattributedFiles int             `json:"unattributedFiles"`
	MissingFiles      []ReconcileItem `json:"missingFiles"`
	OrphanFiles       []ReconcileItem `json:"orphanFiles"`
	DraftRows         []ReconcileItem `json:"draftRows"`
	// Truncated 任一类清单被 Limit 截断（true 时不要据此断言「只剩这些」）。
	Truncated bool `json:"truncated"`
	// Note 不可自动判定的部分说明（例如附件行过多导致反向对账整体跳过）。
	Note string `json:"note"`
}

// normalizeLimit 归一化清单上限。
func normalizeLimit(v int) int {
	if v <= 0 {
		return reconcileDefaultLimit
	}
	if v > reconcileMaxLimit {
		return reconcileMaxLimit
	}
	return v
}

// ReconcileStorage 只读对账：把「有记录没文件」「有文件没记录」「草稿残留」找出来并报告。
//
// 幂等且无副作用（不写库、不动文件），可以随时重复跑；结果**打回给人**，
// 本方法不做任何删除 / 合并 / 补写 —— 冲突与不一致一律由操作者决定怎么处理。
func (s *Service) ReconcileStorage(ctx context.Context, req *ReconcileReq) (*ReconcileReport, error) {
	if req == nil {
		req = &ReconcileReq{}
	}
	limit := normalizeLimit(req.Limit)
	root := filepath.Clean(upload.LocalDir())
	report := &ReconcileReport{
		StorageRoot:  root,
		MissingFiles: []ReconcileItem{},
		OrphanFiles:  []ReconcileItem{},
		DraftRows:    []ReconcileItem{},
	}

	total, err := s.am.CountForAudit(ctx)
	if err != nil {
		return nil, err
	}
	report.AttachmentsTotal = total
	// 截断时反向对账（文件 → DB）不做：已知集合不全会把大量正常文件报成孤儿，
	// 一份大概率误报的清单比没有清单更糟。
	truncatedAttachments := total > int64(limit)
	attachments, err := s.am.ListForAudit(ctx, limit)
	if err != nil {
		return nil, err
	}
	report.AttachmentsScanned = len(attachments)

	// 已知路径集合（相对存储根、统一正斜杠、不带前导斜杠）。
	known := make(map[string]struct{}, len(attachments)*2)
	attachmentIDs := make(map[uint64]struct{}, len(attachments))
	for i := range attachments {
		att := &attachments[i]
		attachmentIDs[att.ID] = struct{}{}
		key := normalizeStorageKey(attachmentStorageKey(att))
		if key != "" {
			known[key] = struct{}{}
		}
		if att.StorageType != "" && att.StorageType != "local" {
			continue // 远端存储的文件不在本地根目录，跳过文件存在性判定。
		}
		if att.Status == mediamodel.AttachmentStatusDisabled && key == "" {
			report.DraftRows = appendItem(report.DraftRows, ReconcileItem{
				Kind: ReconcileKindDraftRow, Table: "sys_attachment", ID: att.ID, Name: att.FileName,
				Detail: "草稿态且无路径：上传两阶段登记未走完（或换图前的老草稿）",
			}, limit, &report.Truncated)
			continue
		}
		if att.Status != mediamodel.AttachmentStatusEnabled {
			continue // 软删除行不参与文件存在性判定（文件按设计保留）。
		}
		if key == "" {
			continue
		}
		abs, perr := localObjectPath(key)
		if perr != nil {
			report.MissingFiles = appendItem(report.MissingFiles, ReconcileItem{
				Kind: ReconcileKindMissingFile, Table: "sys_attachment", ID: att.ID, Name: att.FileName,
				Path: key, Detail: "存储路径非法（越界或为空）：" + perr.Error(),
			}, limit, &report.Truncated)
			continue
		}
		if !fileExists(abs) {
			report.MissingFiles = appendItem(report.MissingFiles, ReconcileItem{
				Kind: ReconcileKindMissingFile, Table: "sys_attachment", ID: att.ID, Name: att.FileName,
				Path: key, Detail: "启用中的附件记录指向的文件不存在（访问面会 404）",
			}, limit, &report.Truncated)
		}
	}

	// 变体行：ready 却无文件 = 响应式图片会给出打不开的 srcset。
	variants, verr := s.vm.ListForAudit(ctx, limit*len(mediamodel.VariantTypes()))
	if verr != nil {
		return nil, verr
	}
	report.VariantsScanned = len(variants)
	for i := range variants {
		v := &variants[i]
		key := normalizeStorageKey(v.FilePath)
		if key == "" {
			continue
		}
		known[key] = struct{}{}
		if v.Status != mediamodel.VariantStatusReady {
			continue
		}
		abs, perr := localObjectPath(key)
		if perr != nil || !fileExists(abs) {
			report.MissingFiles = appendItem(report.MissingFiles, ReconcileItem{
				Kind: ReconcileKindMissingFile, Table: "sys_media_variant", ID: v.ID,
				Name: v.VariantType, Path: key, Detail: "变体标记为 ready 但文件不存在（srcset 会给出打不开的地址）",
			}, limit, &report.Truncated)
		}
	}

	if req.SkipFiles {
		report.Note = "本次跳过文件系统遍历（SkipFiles）：只报告「有记录没文件」这一半"
		s.logReconcile(report)
		return report, nil
	}
	if truncatedAttachments {
		report.Truncated = true
		report.Note = "附件行数超过本次扫描上限，已跳过「有文件没记录」的反向对账（已知集合不全会大量误报）"
		s.logReconcile(report)
		return report, nil
	}

	// 反向：文件 → DB。命中命名约定 <id>.<ext> / <id>_<type>.jpg 才敢归属；
	// 其余（存量随机名）只计数，不当作孤儿。
	walkErr := walkStorageFiles(root, func(rel string, _ int64) {
		report.FilesScanned++
		if _, ok := known[rel]; ok {
			return
		}
		// 换图暂存残留（pkg/upload 的 tmp/replace_*，见 media_replace.go）：
		// 命名不遵循 <id>.<ext>，但位置固定 —— 单独识别，不混进「无法归属」。
		if strings.HasPrefix(rel, "tmp/") {
			report.OrphanFiles = appendItem(report.OrphanFiles, ReconcileItem{
				Kind: ReconcileKindOrphanFile, Table: "文件系统", Path: rel,
				Detail: "换图暂存残留（tmp/replace_*）：正常流程会在 rename 后清掉，留下的说明那次换图中断了",
			}, limit, &report.Truncated)
			return
		}
		id, ok := mediaFileAttachmentID(rel)
		if !ok {
			report.UnattributedFiles++
			return
		}
		if _, exists := attachmentIDs[id]; exists {
			// 附件在，但这条路径不被任何记录引用（例如换图 / 重跑后残留的旧变体文件）。
			report.OrphanFiles = appendItem(report.OrphanFiles, ReconcileItem{
				Kind: ReconcileKindOrphanFile, Table: "文件系统", ID: id, Path: rel,
				Detail: "附件存在但没有任何记录引用这个路径（重跑 / 换图后的残留文件？）",
			}, limit, &report.Truncated)
			return
		}
		report.OrphanFiles = appendItem(report.OrphanFiles, ReconcileItem{
			Kind: ReconcileKindOrphanFile, Table: "文件系统", ID: id, Path: rel,
			Detail: "文件名里的附件 id 在 sys_attachment 中不存在（两阶段登记中断留下的孤儿文件？）",
		}, limit, &report.Truncated)
	})
	if walkErr != nil {
		return nil, walkErr
	}
	s.logReconcile(report)
	return report, nil
}

// appendItem 追加一条清单项，超过 limit 时只把 Truncated 置真（不静默丢弃语义）。
func appendItem(list []ReconcileItem, item ReconcileItem, limit int, truncated *bool) []ReconcileItem {
	if len(list) >= limit {
		*truncated = true
		return list
	}
	return append(list, item)
}

// logReconcile 留痕：对账结论进结构化日志（含各类计数），便于「谁在什么时候跑过」可查。
func (s *Service) logReconcile(r *ReconcileReport) {
	logger.Scene("media").
		With("storage_root", r.StorageRoot).
		With("attachments_total", r.AttachmentsTotal).
		With("attachments_scanned", r.AttachmentsScanned).
		With("variants_scanned", r.VariantsScanned).
		With("files_scanned", r.FilesScanned).
		With("missing_files", len(r.MissingFiles)).
		With("orphan_files", len(r.OrphanFiles)).
		With("draft_rows", len(r.DraftRows)).
		With("unattributed_files", r.UnattributedFiles).
		With("truncated", r.Truncated).
		Warn("媒体存储对账完成（只读：一律打回人工处理，不自动删文件）")
}

// normalizeStorageKey 归一化存储相对 key（正斜杠、去前导斜杠、去首尾空白）。
func normalizeStorageKey(key string) string {
	k := strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	return strings.TrimLeft(k, "/")
}

// walkStorageFiles 遍历存储根下的普通文件，回调收到「相对根的斜杠路径」。
// 根目录不存在时按「没有任何文件」处理（不是错误：从未上传过的站点就是这个状态）。
func walkStorageFiles(root string, fn func(rel string, size int64)) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		fn(strings.ReplaceAll(filepath.ToSlash(rel), "\\", "/"), info.Size())
		return nil
	})
}

// fileExists 判断路径上是一个可读的普通文件。
func fileExists(abs string) bool {
	fi, err := os.Stat(abs)
	return err == nil && fi.Mode().IsRegular()
}

// parseUint64 宽松解析十进制正整数（解析失败返回 0）。
func parseUint64(raw string) uint64 {
	var v uint64
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + uint64(c-'0')
		if v > 1<<53 {
			return 0
		}
	}
	return v
}

// ---- 可重放的补偿入口 ----

// VariantBackfillReq 变体补偿参数。
type VariantBackfillReq struct {
	// Limit 本次最多处理多少个附件（默认 200，上限 2000）。分批跑即可把存量慢慢补齐。
	Limit int
}

// VariantBackfillItem 单个附件的补偿动作记录（留痕）。
type VariantBackfillItem struct {
	AttachmentID uint64 `json:"attachmentId"`
	FileName     string `json:"fileName"`
	Reason       string `json:"reason"`
	Result       string `json:"result"`
}

// VariantBackfillReport 补偿报告。
type VariantBackfillReport struct {
	// Scanned 本次检查的附件数；Fixed 实际重新投递的数量；Skipped 无需处理的数量。
	// 没有 Failed 字段：投递入口本身不会失败（队列不可用时它内部同步兜底并记日志），
	// 留一个恒为 0 的「失败数」只会让人以为这里能发现失败。
	Scanned   int                   `json:"scanned"`
	Fixed     int                   `json:"fixed"`
	Skipped   int                   `json:"skipped"`
	Items     []VariantBackfillItem `json:"items"`
	Truncated bool                  `json:"truncated"`
}

// ReplayVariantBackfill 重放补偿：把「附件在、变体记录缺失或未就绪」的图片附件
// 重新投递变体生成任务。
//
// 幂等：底层 GenerateVariants 会先清空该附件的旧变体记录再重建（见 media_variant.go），
// 所以重复调用（或与上传时的异步任务并发）不会产生记录叠加，最坏只是多做一次生成。
// 可重放：按 Limit 一批批跑，每次只处理仍不齐的那些 —— 修好的下次调用不再命中。
// 留痕：每条动作记一行结构化日志，报告中带回逐条明细。
//
// 为什么不能做成事务：它修的是「事务覆盖不到的那一半」——磁盘上的变体文件与
// 队列里的任务。DB 侧的记录重建本身是原子的（GenerateVariants 内部的清旧+登记同事务），
// 但这个入口的语义是「把外部世界重新推向一致」，只能靠幂等重放，不能靠回滚。
func (s *Service) ReplayVariantBackfill(ctx context.Context, req *VariantBackfillReq) (*VariantBackfillReport, error) {
	if req == nil {
		req = &VariantBackfillReq{}
	}
	// 补偿用**自己的**默认批量，不复用 normalizeLimit（它默认对账的 200）：
	// ListForAudit 按 id 升序取前 N 个，**已齐的附件照样占窗口** —— 附件数超过 N 时，
	// 排在后面的那批永远等不到补偿。理由与实测见 variantBackfillDefaultLimit。
	limit := req.Limit
	if limit <= 0 {
		limit = variantBackfillDefaultLimit
	}
	if limit > reconcileMaxLimit {
		limit = reconcileMaxLimit
	}
	report := &VariantBackfillReport{Items: []VariantBackfillItem{}}

	total, err := s.am.CountForAudit(ctx)
	if err != nil {
		return nil, err
	}
	report.Truncated = total > int64(limit)
	attachments, err := s.am.ListForAudit(ctx, limit)
	if err != nil {
		return nil, err
	}
	report.Scanned = len(attachments)
	if len(attachments) == 0 {
		return report, nil
	}

	ids := make([]uint64, 0, len(attachments))
	for i := range attachments {
		ids = append(ids, attachments[i].ID)
	}
	byAttachment, verr := s.vm.ListByAttachmentIDs(ctx, ids)
	if verr != nil {
		return nil, verr
	}

	for i := range attachments {
		if ctx.Err() != nil {
			break
		}
		att := &attachments[i]
		if att.Status != mediamodel.AttachmentStatusEnabled || !variantEligible(att.FileType, att.FileName) {
			report.Skipped++
			continue
		}
		if att.StorageType != "" && att.StorageType != "local" {
			report.Skipped++
			continue
		}
		reason := variantBackfillReason(byAttachment[att.ID])
		if reason == "" {
			report.Skipped++
			continue
		}
		// 复用既有投递入口（队列可用即入队，否则同步兜底），不另造一套重试。
		s.scheduleVariants(ctx, att.ID)
		report.Fixed++
		report.Items = append(report.Items, VariantBackfillItem{
			AttachmentID: att.ID, FileName: att.FileName, Reason: reason,
			Result: "已重新投递变体生成（幂等：生成前先清旧记录）",
		})
		logger.Scene("media").With("attachment_id", att.ID).With("reason", reason).
			Info("变体补偿重放：已重新投递生成任务")
	}
	return report, nil
}

// variantBackfillReason 判断该附件的变体记录是否需要补偿，返回原因（不需要则空串）。
func variantBackfillReason(rows []mediamodel.MediaVariantEntity) string {
	if len(rows) == 0 {
		return "没有任何变体记录"
	}
	if len(rows) < len(mediamodel.VariantTypes()) {
		return "变体记录条数不齐"
	}
	for i := range rows {
		switch rows[i].Status {
		case mediamodel.VariantStatusReady:
		case mediamodel.VariantStatusFailed:
			return "存在 failed 变体"
		default:
			return "存在未就绪（pending/processing）变体"
		}
	}
	return ""
}

// SortReconcileItems 按 (kind, table, id, path) 稳定排序（报告可复现，便于人工逐条核对）。
func SortReconcileItems(list []ReconcileItem) {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Path < b.Path
	})
}
