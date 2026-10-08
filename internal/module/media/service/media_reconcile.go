package mediaservice

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
//   1. **对账（ReconcileStorage）**：只读地把四类不一致找出来 ——
//      「有记录没文件」「有文件没记录（真孤儿）」「被取代的历史产物」「草稿残留」，
//      连**可定位的数据**（表名 / id / 路径）一起打回给人。**不自动删、不自动合并、
//      不丢行**：自动处理会把「可能只是这次没扫到」直接变成不可逆的数据丢失；
//      尤其「被取代的历史产物」—— 它们可能仍被已发布产物引用，删了就是线上 404。
//   2. **重放补偿（ReplayVariantBackfill）**：把「附件在、变体记录不齐 / 没就绪」的附件
//      重新投递变体生成任务。**幂等**（GenerateVariants 重跑先清旧记录再重建）
//      + **留痕**（每条都记结构化日志）+ **可重放**（重复调用不产生副作用叠加）。
//      复用既有队列与既有的生成入口，不另造一套重试。
//
// 单一真源口径（与 media_crud.go 头部一致）：
//   - 元数据真源 = sys_attachment（status=1 且 file_path 非空的行）；变体 = sys_media_variant；
//   - 内容真源 = 存储根目录下的文件字节。

// 为什么必须有这个文件：media_reconcile.go 里的两个入口此前**没有任何调用方**，
// 判定写得再准，没人驱动就等于不存在。而这两件事都只能靠时间驱动：
//
//   · 只读对账（ReconcileStorage）：文件系统与 PostgreSQL 这两处持久化永远可能对不上
//     （见 media_reconcile.go 开头的「为什么这里不能用事务」）—— 不存在任何写路径
//     能发现它们，只能靠周期性巡检把不一致找出来打回给人。
//   · 变体补偿重放（ReplayVariantBackfill）：上传路径的投递失败后只剩 asynq 自身的
//     3 次重试；重试耗尽后那条附件会**永久**停在「变体不齐」（srcset 给出打不开的地址），
//     没有任何入口会再碰它。
//
// 形状与既有调度器逐项一致（page_publish_converge.go / analytics_retention.go /
// context.Background()、目标方法报错只记日志、任何 panic 一律 recover 不拖垮进程。
//
// 这里驱动的是本模块既有的两个入口，**一行判定都不在这里**（不新造第二份真相）：
// 对账口径在 media_reconcile.go 与 media_crud.go，补偿的幂等性由 GenerateVariants
// 「先清旧记录再重建」保证。本文件只回答「谁在什么时候驱动它」。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
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
	// ReconcileKindOrphanFile 有文件没记录，且**归属不到任何现存附件**：
	// 真正的孤儿（两阶段登记中断留下的文件、换图暂存残留）。
	ReconcileKindOrphanFile = "orphan_file"
	// ReconcileKindSupersededVariant 被取代的历史产物：文件归属得到现存附件，
	// 但没有任何记录再引用这个路径（换图 / 重新生成变体后留下的旧文件名）。
	//
	// 为什么不并进 orphan_file：**它不该被删**。变体名带内容指纹，已发布产物里的
	// srcset 指向换图前的那一批名字，而重建是异步的、且可能失败 —— 删掉这些文件
	// 等于让线上图片 404。与「孤儿」混在一张清单里，运维照单清理就会踩这个坑
	//（见 media_replace.go 里为什么保留旧变体文件：两处是同一个意图）。
	ReconcileKindSupersededVariant = "superseded_variant"
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
	// OrphanFiles 真孤儿：文件归属不到任何现存附件（两阶段登记中断、暂存残留）。
	// **只有这一类是可以直接删的** —— 它不被任何记录、也不被任何附件引用。
	OrphanFiles []ReconcileItem `json:"orphanFiles"`
	// SupersededVariants 被取代的历史产物（换图 / 重新生成变体后留下的旧文件名）。
	//
	// 单列一类的唯一目的是让运维**不把它当垃圾**：这批文件很可能仍被已发布产物引用，
	// 删了就是线上图片 404。判据与处置写在每条的 Detail 里（不要求读者去翻源码）。
	SupersededVariants []ReconcileItem `json:"supersededVariants"`
	DraftRows          []ReconcileItem `json:"draftRows"`
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

// ReconcileStorage 只读对账：把「有记录没文件」「有文件没记录（真孤儿）」
// 「被取代的历史产物」「草稿残留」找出来并报告。
//
// 幂等且无副作用（不写库、不动文件），可以随时重复跑；结果**打回给人**，
// 本方法不做任何删除 / 合并 / 补写 —— 冲突与不一致一律由操作者决定怎么处理。
//
// 「被取代的历史产物」与「真孤儿」分开列（见 ReconcileKindSupersededVariant）：
// 前者删不得，后者才是可清理的。混在一起等于把「线上会 404」的风险藏进一份
// 看起来像垃圾清单的输出里。
func (s *Service) ReconcileStorage(ctx context.Context, req *ReconcileReq) (*ReconcileReport, error) {
	if req == nil {
		req = &ReconcileReq{}
	}
	limit := normalizeLimit(req.Limit)
	root := filepath.Clean(upload.LocalDir())
	report := &ReconcileReport{
		StorageRoot:        root,
		MissingFiles:       []ReconcileItem{},
		OrphanFiles:        []ReconcileItem{},
		SupersededVariants: []ReconcileItem{},
		DraftRows:          []ReconcileItem{},
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
			// 附件在、但这条路径不被任何记录引用：**不是垃圾**，是被取代的历史产物
			//（换图 / 重新生成变体后留下的旧文件名；软删附件的文件也落在这一类 ——
			//  删除附件同样按设计保留文件）。
			//
			// 单列一类而不是并进 OrphanFiles 的理由就是下面这句 Detail：已发布产物
			// 很可能仍引用它，删了会让线上图片 404。这与 media_replace.go「为什么保留
			// 旧变体文件」是同一个意图的两半。
			report.SupersededVariants = appendItem(report.SupersededVariants, ReconcileItem{
				Kind: ReconcileKindSupersededVariant, Table: "文件系统", ID: id, Path: rel,
				Detail: "被取代的历史产物（换图 / 重新生成变体后的旧文件名）：**可能仍被已发布产物引用，" +
					"删除会让线上图片 404**。先确认引用它的页面 / 实例都已重建（后台「待重建」清空），再逐个删。",
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
		With("superseded_variants", len(r.SupersededVariants)).
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

const (
	// mediaReconcileInterval 巡检间隔。
	//
	// 只读对账按天：一轮要遍历整个存储目录 + 扫一批附件与变体行，是固定成本；而
	// 不一致的来源（进程在「落盘成功、DB 提交前」崩掉 / 磁盘故障 / 手工改库）不是
	// 分钟级事件 —— 一天一次足以让不一致在一天内可见。更密只增加空转，更疏则让
	// 报告失去「当下状态」的意义。
	//
	// 变体补偿重放与对账共用这一个 ticker（一轮里跑两个动作）：
	//   · 它满足纳入调度的两个条件 —— 文档注释（media_reconcile.go）明写**幂等**
	//     （GenerateVariants 重跑先清旧记录再重建）+ **可重放**（只处理仍不齐的那批，
	//     修好的下次不再命中），且修的是**派生的、可重新生成的**图片变体数据，
	//     不是用户原始资产；正常路径（上传时投递 + asynq 3 次重试）才是主修复通道，
	//     调度器只是它耗尽之后的兜底。
	//   · 一天一次对它是安全的：不会与上传时刚投递的那批抢同一批附件，也不会让一次
	//     失败的变体生成影响超过一天（重放本身幂等，多跑一次最坏白生成一次）。
	mediaReconcileInterval = 24 * time.Hour
	// mediaReconcileTimeout 单轮巡检的超时。
	//
	// 比 webhook / plugin 的巡检宽：对账要遍历文件系统，而队列未启用时变体补偿的
	// 降级分支会**同步**生成变体（scheduleVariants 见 media_variant_task.go），
	// 两者叠加可能是分钟级。
	mediaReconcileTimeout = 10 * time.Minute
)

// mediaReconcileRoundResult 一轮巡检的结论（只放真实计数，供日志与用例断言）。
type mediaReconcileRoundResult struct {
	// 只读对账
	reconcileErr     error
	storageRoot      string
	attachmentsTotal int64
	filesScanned     int
	variantsScanned  int
	missingFiles     int
	orphanFiles      int
	// supersededVariants 被取代的历史产物（换图 / 重新生成变体后的旧文件名）。
	// **不是不一致**：它们是刻意保留的（已发布产物可能仍引用），不计入 inconsistent()。
	supersededVariants int
	draftRows          int
	unattributed       int
	truncated          bool

	// 变体补偿重放
	backfillErr     error
	backfillScanned int
	backfillFixed   int
	backfillSkipped int
}

// inconsistent 本轮只读对账发现的**需要人处理**的不一致条数（三类清单之和）。
//
// 「无法归属」的文件（存量随机名）不计入：它不是不一致，只是认不出归属 ——
// 把它算进来会让每一轮都报「有不一致」，真不一致就被淹掉了。
// 「被取代的历史产物」（superseded_variant）同样不计入，理由更强一层：
// 它是**设计结果**（换图必然留下旧名文件，见 media_replace.go），不是异常；
// 计进来会让每次换图之后的每一轮巡检都告警 —— 巡检就没人看了。
func (r mediaReconcileRoundResult) inconsistent() int {
	return r.missingFiles + r.orphanFiles + r.draftRows
}

// runMediaReconcileRound 跑一轮巡检：先只读对账，再幂等变体补偿重放。
//
// 两个动作**各自 recover**：一个只读、一个会改（投递任务），任一个 panic 都不该让
// 另一个在本轮被跳过 —— 否则「一个动作的 bug」会伪装成「另一个动作也正常」。
func runMediaReconcileRound(ctx context.Context, svc *Service) (res mediaReconcileRoundResult) {
	if svc == nil {
		return res
	}
	func() {
		defer catchMediaActionPanic(&res.reconcileErr)
		report, err := svc.ReconcileStorage(ctx, nil)
		if err != nil {
			res.reconcileErr = err
			return
		}
		res.storageRoot = report.StorageRoot
		res.attachmentsTotal = report.AttachmentsTotal
		res.filesScanned = report.FilesScanned
		res.variantsScanned = report.VariantsScanned
		res.missingFiles = len(report.MissingFiles)
		res.orphanFiles = len(report.OrphanFiles)
		res.supersededVariants = len(report.SupersededVariants)
		res.draftRows = len(report.DraftRows)
		res.unattributed = report.UnattributedFiles
		res.truncated = report.Truncated
	}()
	func() {
		defer catchMediaActionPanic(&res.backfillErr)
		report, err := svc.ReplayVariantBackfill(ctx, nil)
		if err != nil {
			res.backfillErr = err
			return
		}
		res.backfillScanned = report.Scanned
		res.backfillFixed = report.Fixed
		res.backfillSkipped = report.Skipped
	}()
	return res
}

// catchMediaActionPanic 把单个动作的 panic 收敛成该动作的错误（defer 用）。
func catchMediaActionPanic(target *error) {
	if r := recover(); r != nil {
		*target = fmt.Errorf("panic: %v", r)
	}
}

// runMediaReconcileLoop 调度循环本体：先跑一次，再按 interval 等间隔重复。
//
// 抽成「接受一轮动作」的形状是为了可测：目标方法要数据库与文件系统，模块内单测不碰
// 这些依赖，但**调度语义**（首跑 / 等间隔 / 单轮 panic 不致命）与动作内容无关，
// 可以独立断言 —— 这三件事写反了的表现都是「看起来在跑、其实什么都没做」。
//
// 返回的 stop 关闭后循环退出；生产装配不调用它（进程退出即结束），测试用它收尾。
func runMediaReconcileLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = mediaReconcileInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		// 首跑：与样板一致，先跑一次再等 ticker（不是先等一个间隔）。
		safeMediaReconcileRound(round)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				safeMediaReconcileRound(round)
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// safeMediaReconcileRound 执行一轮并把 panic 收敛成日志。
//
// goroutine 里未被 recover 的 panic 会直接终止整个进程，巡检任务没有这种权力：
// 一轮动作的 bug 只该让这一轮没有结论，下一个间隔照常再来。
func safeMediaReconcileRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("media").Error(fmt.Errorf("panic: %v", r),
				"媒体存储巡检单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

// StartMediaReconcileScheduler 启动媒体存储巡检调度（进程内 goroutine + ticker）。
func StartMediaReconcileScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startMediaReconcileScheduler(svc, mediaReconcileInterval)
}

// StartMediaReconcileSchedulerWithInterval 同上，但可注入间隔。
//
// 给用例用：注入远大于用例时长的间隔可以证成「首跑确实发生在启动时」，
// 注入毫秒级间隔可以证成「等间隔确实在驱动」——两者都不是单个断言能覆盖的。
func StartMediaReconcileSchedulerWithInterval(svc *Service, interval time.Duration) {
	startMediaReconcileScheduler(svc, interval)
}

func startMediaReconcileScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runMediaReconcileLoop(func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), mediaReconcileTimeout)
		defer cancel()
		res := runMediaReconcileRound(ctx, svc)
		logMediaReconcileRound(res, time.Since(start))
	}, interval)
}

// logMediaReconcileRound 每轮记**一条**结构化日志（真实计数 + 耗时）。
//
// 与 service 内部日志的分工：ReconcileStorage 的 logReconcile 与 ReplayVariantBackfill
// 的逐条日志回答「这次动作做了什么」（带逐条明细），这一条回答「这一轮调度跑完了没、
// 两组动作各自的结论是什么」。两者读的是同一份报告结构体的字段，没有第二份判据。
//
// 分档：动作失败 → Error（结论不完整，下一轮重试）；只读对账发现不一致 → Warn
// （数据打回给人处理，本任务不自动修复）；重放了变体补偿 → Info（那是**幂等的派生数据**
// 自动修复，不是「要人处理的不一致」——把两者混成一个告警级别会让真不一致被淹掉）；
// 否则 Info。这里**只报告、绝不自动删文件**：对账的不一致一律打回给人
// （自动删会把「可能只是这次没扫到」变成不可逆的数据丢失），
// 唯一被自动处理的只有变体那个派生数据的幂等重放。
func logMediaReconcileRound(res mediaReconcileRoundResult, cost time.Duration) {
	entry := logger.Scene("media").
		With("storage_root", res.storageRoot).
		With("attachments_total", res.attachmentsTotal).
		With("files_scanned", res.filesScanned).
		With("variants_scanned", res.variantsScanned).
		With("missing_files", res.missingFiles).
		With("orphan_files", res.orphanFiles).
		With("superseded_variants", res.supersededVariants).
		With("draft_rows", res.draftRows).
		With("unattributed_files", res.unattributed).
		With("truncated", res.truncated).
		With("backfill_scanned", res.backfillScanned).
		With("backfill_fixed", res.backfillFixed).
		With("backfill_skipped", res.backfillSkipped).
		With("cost_ms", cost.Milliseconds())
	if err := errors.Join(res.reconcileErr, res.backfillErr); err != nil {
		entry.Error(err, "媒体存储巡检调度：本轮有动作失败，结论不完整（下一轮重试）")
		return
	}
	switch {
	case res.inconsistent() > 0:
		entry.Warn("媒体存储巡检完成：发现不一致，数据打回给人处理，本任务不自动修复")
	case res.backfillFixed > 0:
		entry.Info("媒体存储巡检完成：未发现不一致，已重新投递变体生成（幂等补偿）")
	default:
		entry.Info("媒体存储巡检完成：未发现不一致")
	}
}
