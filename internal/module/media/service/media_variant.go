package mediaservice

// media_variant.go — 图片变体生成（thumb / medium / webp）：
//   上传切入点 EnsureVariantRecords 登记 pending 记录并投递 asynq 任务（scheduleVariants）；
//   GenerateVariants 同步生成，供 worker、详情页「重新生成」与存量回填复用；
//   任何变体失败一律降级为 status=failed 记录，不回滚主上传、不阻断主流程。

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	mediato "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"

	"gorm.io/gorm"
)

// variantExcludedExts 不参与变体生成的图片扩展名：
// svg 为矢量（无需位图变体），gif 为多帧（转码丢帧），按设计规格排除。
var variantExcludedExts = map[string]struct{}{
	".svg": {},
	".gif": {},
}

// variantEligible 判断附件是否参与变体生成（图片类且非 svg/gif）。
func variantEligible(fileType string, fileName string) bool {
	if fileType != "image" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if _, excluded := variantExcludedExts[ext]; excluded {
		return false
	}
	return true
}

// variantObjectKey 由原图存储 key 推导变体存储 key：
// 与原图同目录，命名 <stem>_<variantType>.jpg（变体统一有损 JPEG 编码）。
// 文件名可推导，zip 打包/排查不依赖额外约定。
func variantObjectKey(sourceKey string, variantType string) string {
	dir := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(filepath.ToSlash(sourceKey), "/")))
	base := filepath.Base(strings.TrimPrefix(filepath.ToSlash(sourceKey), "/"))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	name := stem + "_" + variantType + ".jpg"
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

// localObjectPath 把存储相对 key 解析为本地绝对路径（local provider 语义），
// 并防御 key 被污染后逃逸存储根目录（Clean + 根前缀校验）。
func localObjectPath(key string) (string, error) {
	slash := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(key)), "/")
	cleaned := filepath.Clean(filepath.FromSlash(slash))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("非法存储路径")
	}
	root, err := filepath.Abs(upload.LocalDir())
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(root, cleaned))
	if err != nil {
		return "", err
	}
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", errors.New("存储路径越界")
	}
	return abs, nil
}

// writeLocalFile 服务端直接落盘变体产物（受信服务端内容，不经上传校验入口）：
// O_TRUNC 支持重新生成覆盖旧文件，O_NOFOLLOW 防最终分量 symlink 覆盖，
// 防御语义与 pkg/upload local provider 一致。
func writeLocalFile(absPath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("创建变体目录失败: %w", err)
	}
	fd, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	defer fd.Close()
	_, err = fd.Write(data)
	return err
}

// fillVariants 给附件响应批量填充变体状态（一次 IN 查询；查询失败静默跳过，不影响主数据）。
func (s *Service) fillVariants(ctx context.Context, resps []mediato.AttachmentResp) {
	if len(resps) == 0 {
		return
	}
	ids := make([]uint64, 0, len(resps))
	for i := range resps {
		ids = append(ids, resps[i].ID)
	}
	m, err := s.vm.ListByAttachmentIDs(ctx, ids)
	if err != nil {
		return
	}
	for i := range resps {
		if vs := m[resps[i].ID]; len(vs) > 0 {
			resps[i].Variants = variantEntitiesToResp(vs)
		}
	}
}

// variantEntitiesToResp 变体实体列表 → DTO 列表。
func variantEntitiesToResp(list []mediamodel.MediaVariantEntity) []mediato.VariantResp {
	out := make([]mediato.VariantResp, 0, len(list))
	for i := range list {
		out = append(out, variantEntityToResp(&list[i]))
	}
	return out
}

// attachmentStorageKey 取附件的存储相对 key（StoragePath 为空时回退 FilePath）。
func attachmentStorageKey(att *mediamodel.AttachmentEntity) string {
	if att.StoragePath != nil && strings.TrimSpace(*att.StoragePath) != "" {
		return *att.StoragePath
	}
	return att.FilePath
}

// variantProduceResult 单个变体的生成结果（回填记录用）。
type variantProduceResult struct {
	Width  int
	Height int
	Size   int64
}

// produceVariant 生成单个变体文件：按类型缩放/重编码 → JPEG 有损编码 → 落盘，
// 返回变体实际宽高与文件大小。
func produceVariant(src image.Image, variantType string, key string) (variantProduceResult, error) {
	img, err := buildVariantImage(src, variantType)
	if err != nil {
		return variantProduceResult{}, err
	}
	data, err := encodeJPEGBytes(img)
	if err != nil {
		return variantProduceResult{}, err
	}
	absPath, err := localObjectPath(key)
	if err != nil {
		return variantProduceResult{}, err
	}
	if err := writeLocalFile(absPath, data); err != nil {
		return variantProduceResult{}, err
	}
	return variantProduceResult{Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), Size: int64(len(data))}, nil
}

// probeVariantStatus 只读探测该附件变体的初始状态（不写库）：
// 不可变体（非图片 / svg / gif）返回空串，表示「不该有变体记录」；
// header 探测通过 → pending；探测失败、源路径非法或超大 → failed
// （对应规格「边长 >6000px 或解码失败则跳过变体（status=failed），不阻塞上传」）。
//
// 拆出来的理由：上传路径要把「探测」（读文件系统，慢）与「建记录」（写库，要进事务）
// 分开 —— 读盘不该占着事务与连接。EnsureVariantRecords 仍是一次做完，供存量回填/测试用。
func (s *Service) probeVariantStatus(ctx context.Context, att *mediamodel.AttachmentEntity) string {
	if att == nil {
		return ""
	}
	sourceKey := attachmentStorageKey(att)
	if sourceKey == "" || !variantEligible(att.FileType, att.FileName) {
		return ""
	}
	if srcPath, perr := localObjectPath(sourceKey); perr != nil {
		logger.Scene("media").With("attachment_id", att.ID).Error(perr, "变体源路径非法，跳过生成")
		return mediamodel.VariantStatusFailed
	} else if f, oerr := os.Open(srcPath); oerr != nil {
		logger.Scene("media").With("attachment_id", att.ID).Error(oerr, "打开上传原图失败，变体跳过")
		return mediamodel.VariantStatusFailed
	} else {
		_, _, derr := probeImage(f)
		_ = f.Close()
		if derr != nil {
			logger.Scene("media").With("attachment_id", att.ID).Error(derr, "图片探测未通过，变体跳过")
			return mediamodel.VariantStatusFailed
		}
	}
	return mediamodel.VariantStatusPending
}

// variantRecordsFor 按探测结果构造三条变体初始记录（纯函数，不写库）。
//
// status 为空串（不可变体）时返回 nil —— 调用方据此跳过写入。
// 纯函数的意义：同一批记录可以在**事务内**构造并随元数据回填一起提交，
// 不必在事务里做磁盘探测。
func variantRecordsFor(att *mediamodel.AttachmentEntity, status string) []*mediamodel.MediaVariantEntity {
	if att == nil || status == "" {
		return nil
	}
	sourceKey := attachmentStorageKey(att)
	if sourceKey == "" {
		return nil
	}
	records := make([]*mediamodel.MediaVariantEntity, 0, len(mediamodel.VariantTypes()))
	for _, vt := range mediamodel.VariantTypes() {
		records = append(records, &mediamodel.MediaVariantEntity{
			AttachmentID: att.ID,
			VariantType:  vt,
			FilePath:     variantObjectKey(sourceKey, vt),
			Status:       status,
		})
	}
	return records
}

// EnsureVariantRecords 图片类附件登记三条变体初始记录（自足入口：探测 + 批量插入）。
//
// 上传主路径**不再直接调用它** —— 上传的第二段事务里要的是「同一批记录与元数据回填
// 一起提交」，所以那里改用 probeVariantStatus + variantRecordsFor + CreateBatchTx。
// 本方法保留给「已落库的存量附件补登记」与单测使用，行为与接入事务前逐字一致
// （记录插入失败只记日志、不回滚任何东西）。
func (s *Service) EnsureVariantRecords(ctx context.Context, att *mediamodel.AttachmentEntity) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("media").With("attachment_id", att.ID).Error(fmt.Errorf("panic: %v", r), "变体登记异常（已降级）")
		}
	}()

	records := variantRecordsFor(att, s.probeVariantStatus(ctx, att))
	if len(records) == 0 {
		return
	}
	if err := s.vm.CreateBatch(ctx, records); err != nil {
		logger.Scene("media").With("attachment_id", att.ID).Error(err, "变体记录登记失败（已降级，不回滚上传）")
	}
}

// GenerateVariants 同步生成指定附件的全部变体（契约方法，导出供 worker /
// 「重新生成」按钮 / 存量回填复用）。幂等：重跑先物理清空旧记录再重建；
// 单个变体失败记 failed 并继续其余变体。返回生成后的变体列表（含最终状态）。
func (s *Service) GenerateVariants(ctx context.Context, attachmentID uint64) (res []mediato.VariantResp, err error) {
	att, err := s.am.GetByID(ctx, attachmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	if !variantEligible(att.FileType, att.FileName) {
		return nil, errors.New(mediaenums.ErrAttachmentNotImage)
	}
	if att.StorageType != "local" {
		return nil, errors.New(mediaenums.ErrVariantStorageNotLocal)
	}
	sourceKey := attachmentStorageKey(att)
	srcPath, err := localObjectPath(sourceKey)
	if err != nil {
		return nil, err
	}

	// 幂等：重跑先清旧记录（规格：asynq handler 重跑先清旧记录）。
	//
	// 「清旧 + 登记新」必须**同一个事务**（AGENTS.md「写操作的事务与回滚」）：
	// 两步各自提交时，第二步失败就留下「旧变体没了、新变体也没有」的附件 ——
	// 三个变体槽全空，而磁盘上旧变体文件还在（没有任何记录能指向它们），
	// 只能靠人工再点一次「重新生成」恢复。
	//
	// 事务里只建记录（纯 SQL），文件的生成/写盘放在事务**外**逐个做：
	// 文件系统是跨库动作，不参与事务（为什么不能用事务覆盖见 media_reconcile.go 头部）。
	records := make([]*mediamodel.MediaVariantEntity, 0, len(mediamodel.VariantTypes()))
	for _, vt := range mediamodel.VariantTypes() {
		records = append(records, &mediamodel.MediaVariantEntity{
			AttachmentID: attachmentID,
			VariantType:  vt,
			FilePath:     variantObjectKey(sourceKey, vt),
			Status:       mediamodel.VariantStatusProcessing,
		})
	}
	if terr := s.vm.Transaction(ctx, func(tx *gorm.DB) error {
		if derr := s.vm.DeleteByAttachmentTx(ctx, tx, attachmentID); derr != nil {
			return derr
		}
		return s.vm.CreateBatchTx(ctx, tx, records)
	}); terr != nil {
		return nil, terr
	}

	failAll := func(reason error) {
		logger.Scene("media").With("attachment_id", attachmentID).Error(reason, "变体生成整体跳过")
		at := time.Now()
		// 三条一起标 failed：同事务，避免留下「一半 failed、一半 processing」——
		// processing 是不会自愈的中间态（没有别的东西会再来推进它）。
		if uerr := s.vm.Transaction(ctx, func(tx *gorm.DB) error {
			for _, rec := range records {
				if xerr := s.vm.UpdateTx(ctx, tx, rec.ID, map[string]any{
					"status": mediamodel.VariantStatusFailed, "update_time": at,
				}); xerr != nil {
					return xerr
				}
			}
			return nil
		}); uerr != nil {
			logger.Scene("media").With("attachment_id", attachmentID).Error(uerr, "变体整体跳过状态回写失败")
		}
		for _, rec := range records {
			rec.Status = mediamodel.VariantStatusFailed
			res = append(res, variantEntityToResp(rec))
		}
	}

	// header 防御：解码失败 / 任一边超 6000px → 三条 failed，跳过生成（降级不报错）。
	if f, oerr := os.Open(srcPath); oerr != nil {
		failAll(oerr)
		return res, nil
	} else {
		_, _, perr := probeImage(f)
		_ = f.Close()
		if perr != nil {
			failAll(perr)
			return res, nil
		}
	}

	// 解码一次，三个变体复用（缩放均在内存 image.Image 上进行）。
	src, derr := decodeFile(srcPath)
	if derr != nil {
		failAll(derr)
		return res, nil
	}

	for _, rec := range records {
		vt := rec.VariantType
		key := rec.FilePath
		produced, gerr := produceVariant(src, vt, key)
		if gerr != nil {
			_ = s.vm.Update(ctx, rec.ID, map[string]any{"status": mediamodel.VariantStatusFailed, "update_time": time.Now()})
			logger.Scene("media").With("attachment_id", attachmentID).With("variant", vt).Error(gerr, "变体生成失败")
			rec.Status = mediamodel.VariantStatusFailed
		} else {
			_ = s.vm.Update(ctx, rec.ID, map[string]any{
				"status":      mediamodel.VariantStatusReady,
				"file_path":   key,
				"width":       produced.Width,
				"height":      produced.Height,
				"file_size":   produced.Size,
				"mime_type":   "image/jpeg",
				"update_time": time.Now(),
			})
			rec.Status = mediamodel.VariantStatusReady
		}
		res = append(res, variantEntityToResp(rec))
	}
	return res, nil
}

// decodeFile 从本地路径解码原图（复用 decodeImage，AUTO 方向摆正）。
func decodeFile(srcPath string) (image.Image, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeImage(f)
}

// variantEntityToResp 变体实体 → DTO（url 按 local 存储语义拼接 /storage 前缀，
// 供前端直接预览缩略图；base_url 站点域名场景下为相对路径，不影响打包下载）。
func variantEntityToResp(e *mediamodel.MediaVariantEntity) mediato.VariantResp {
	mime := ""
	if e.MimeType != nil {
		mime = *e.MimeType
	}
	width, height := 0, 0
	if e.Width != nil {
		width = *e.Width
	}
	if e.Height != nil {
		height = *e.Height
	}
	return mediato.VariantResp{
		VariantType: e.VariantType,
		Status:      e.Status,
		FilePath:    e.FilePath,
		URL:         upload.StorageURL(filepath.ToSlash(e.FilePath)),
		FileSize:    e.FileSize,
		Width:       width,
		Height:      height,
		MimeType:    mime,
	}
}

// ProbeImageVariants 按公开 URL（/storage/...）探测已就绪的图片变体宽度列表，
// 供构建期响应式图片（srcset）使用：宽度取变体类型的标准边并与文件命名
// <stem>_<type>.jpg 一一对应；升序去重，保证同一文档重复编译产物字节一致。
// 非媒体库 URL、附件不存在或变体未就绪返回 nil（调用方不输出 srcset）。
func (s *Service) ProbeImageVariants(ctx context.Context, url string) []int {
	const prefix = "/storage/"
	if !strings.HasPrefix(url, prefix) {
		return nil
	}
	rel := strings.TrimPrefix(url, prefix)
	if rel == "" || strings.Contains(rel, "..") {
		return nil
	}
	att, err := s.am.GetByFilePath(ctx, rel)
	if err != nil || att == nil {
		return nil
	}
	list, err := s.vm.ListByAttachment(ctx, att.ID)
	if err != nil {
		return nil
	}
	// webp 变体与源图同尺寸，不参与 srcset（原图已由 src 兜底）。
	widths := make([]int, 0, len(list))
	seen := make(map[int]bool, len(list))
	for _, v := range list {
		if v.Status != mediamodel.VariantStatusReady {
			continue
		}
		var w int
		switch v.VariantType {
		case mediamodel.VariantTypeThumb:
			w = thumbVariantEdge
		case mediamodel.VariantTypeMedium:
			w = mediumVariantEdge
		default:
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		widths = append(widths, w)
	}
	sort.Ints(widths)
	return widths
}
