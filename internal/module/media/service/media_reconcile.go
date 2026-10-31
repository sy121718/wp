package mediaservice

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

	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
)

const (
	reconcileDefaultLimit = 200
	reconcileMaxLimit     = 2000
)

const variantBackfillDefaultLimit = 2000

var (
	mediaOriginalRe = regexp.MustCompile(`^(\d+)\.[A-Za-z0-9]{1,10}$`)

	mediaVariantRe = regexp.MustCompile(`^(\d+)_(?:thumb|small|medium|full|webp)(?:-\d+-[0-9a-f]{8})?\.jpg$`)

	mediaBareIDRe = regexp.MustCompile(`^(\d+)$`)
)

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

type ReconcileReq struct {
	Limit int

	SkipFiles bool
}

const (
	ReconcileKindMissingFile = "missing_file"

	ReconcileKindOrphanFile = "orphan_file"

	ReconcileKindSupersededVariant = "superseded_variant"

	ReconcileKindDraftRow = "draft_row"
)

type ReconcileItem struct {
	Kind   string `json:"kind"`
	Table  string `json:"table"`
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
}

type ReconcileReport struct {
	StorageRoot string `json:"storageRoot"`

	AttachmentsTotal   int64 `json:"attachmentsTotal"`
	AttachmentsScanned int   `json:"attachmentsScanned"`
	VariantsScanned    int   `json:"variantsScanned"`
	FilesScanned       int   `json:"filesScanned"`

	UnattributedFiles int             `json:"unattributedFiles"`
	MissingFiles      []ReconcileItem `json:"missingFiles"`

	OrphanFiles []ReconcileItem `json:"orphanFiles"`

	SupersededVariants []ReconcileItem `json:"supersededVariants"`
	DraftRows          []ReconcileItem `json:"draftRows"`

	Truncated bool `json:"truncated"`

	Note string `json:"note"`
}

func normalizeLimit(v int) int {
	if v <= 0 {
		return reconcileDefaultLimit
	}
	if v > reconcileMaxLimit {
		return reconcileMaxLimit
	}
	return v
}

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

	truncatedAttachments := total > int64(limit)
	attachments, err := s.am.ListForAudit(ctx, limit)
	if err != nil {
		return nil, err
	}
	report.AttachmentsScanned = len(attachments)

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
			continue
		}
		if att.Status == mediamodel.AttachmentStatusDisabled && key == "" {
			report.DraftRows = appendItem(report.DraftRows, ReconcileItem{
				Kind: ReconcileKindDraftRow, Table: "sys_attachment", ID: att.ID, Name: att.FileName,
				Detail: "草稿态且无路径：上传两阶段登记未走完（或换图前的老草稿）",
			}, limit, &report.Truncated)
			continue
		}
		if att.Status != mediamodel.AttachmentStatusEnabled {
			continue
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

	walkErr := walkStorageFiles(root, func(rel string, _ int64) {
		report.FilesScanned++
		if _, ok := known[rel]; ok {
			return
		}

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

func appendItem(list []ReconcileItem, item ReconcileItem, limit int, truncated *bool) []ReconcileItem {
	if len(list) >= limit {
		*truncated = true
		return list
	}
	return append(list, item)
}

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

func normalizeStorageKey(key string) string {
	k := strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	return strings.TrimLeft(k, "/")
}

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

func fileExists(abs string) bool {
	fi, err := os.Stat(abs)
	return err == nil && fi.Mode().IsRegular()
}

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

type VariantBackfillReq struct {
	Limit int
}

type VariantBackfillItem struct {
	AttachmentID uint64 `json:"attachmentId"`
	FileName     string `json:"fileName"`
	Reason       string `json:"reason"`
	Result       string `json:"result"`
}

type VariantBackfillReport struct {
	Scanned   int                   `json:"scanned"`
	Fixed     int                   `json:"fixed"`
	Skipped   int                   `json:"skipped"`
	Items     []VariantBackfillItem `json:"items"`
	Truncated bool                  `json:"truncated"`
}

func (s *Service) ReplayVariantBackfill(ctx context.Context, req *VariantBackfillReq) (*VariantBackfillReport, error) {
	if req == nil {
		req = &VariantBackfillReq{}
	}

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
	mediaReconcileInterval = 24 * time.Hour

	mediaReconcileTimeout = 10 * time.Minute
)

type mediaReconcileRoundResult struct {
	reconcileErr     error
	storageRoot      string
	attachmentsTotal int64
	filesScanned     int
	variantsScanned  int
	missingFiles     int
	orphanFiles      int

	supersededVariants int
	draftRows          int
	unattributed       int
	truncated          bool

	backfillErr     error
	backfillScanned int
	backfillFixed   int
	backfillSkipped int
}

func (r mediaReconcileRoundResult) inconsistent() int {
	return r.missingFiles + r.orphanFiles + r.draftRows
}

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

func catchMediaActionPanic(target *error) {
	if r := recover(); r != nil {
		*target = fmt.Errorf("panic: %v", r)
	}
}

func runMediaReconcileLoop(round func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = mediaReconcileInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {

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

func safeMediaReconcileRound(round func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("media").Error(fmt.Errorf("panic: %v", r),
				"媒体存储巡检单轮 panic（已收敛，下一轮照常；不影响进程）")
		}
	}()
	round()
}

func StartMediaReconcileScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	startMediaReconcileScheduler(svc, mediaReconcileInterval)
}

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
