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
// 与原图同目录，命名 <stem>_<variantType>.webp——webp 全尺寸用 _webp 后缀，
// 避免与「原图本身即 .webp」的场景重名冲突；文件名可推导，zip 打包/排查不依赖额外约定。
func variantObjectKey(sourceKey string, variantType string) string {
	dir := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(filepath.ToSlash(sourceKey), "/")))
	base := filepath.Base(strings.TrimPrefix(filepath.ToSlash(sourceKey), "/"))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	name := stem + "_" + variantType + ".webp"
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

// produceVariant 生成单个变体文件：按类型缩放/重编码 → webp 编码 → 落盘，
// 返回变体实际宽高与文件大小。
func produceVariant(src image.Image, variantType string, key string) (variantProduceResult, error) {
	img, err := buildVariantImage(src, variantType)
	if err != nil {
		return variantProduceResult{}, err
	}
	data, err := encodeWebPBytes(img)
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

// EnsureVariantRecords 上传切入点：图片类附件登记三条变体初始记录。
// header 探测通过 → pending（等待异步/同步生成）；探测失败或超大 → failed（跳过生成，
// 对应规格「边长 >6000px 或解码失败则跳过变体（status=failed），不阻塞上传」）。
// 仅记录日志与状态，任何错误都不影响上传主流程。
func (s *Service) EnsureVariantRecords(ctx context.Context, att *mediamodel.AttachmentEntity) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("media").With("attachment_id", att.ID).Error(fmt.Errorf("panic: %v", r), "变体登记异常（已降级）")
		}
	}()

	sourceKey := attachmentStorageKey(att)
	if sourceKey == "" || !variantEligible(att.FileType, att.FileName) {
		return
	}

	status := mediamodel.VariantStatusPending
	if srcPath, perr := localObjectPath(sourceKey); perr != nil {
		status = mediamodel.VariantStatusFailed
		logger.Scene("media").With("attachment_id", att.ID).Error(perr, "变体源路径非法，跳过生成")
	} else if f, oerr := os.Open(srcPath); oerr != nil {
		status = mediamodel.VariantStatusFailed
		logger.Scene("media").With("attachment_id", att.ID).Error(oerr, "打开上传原图失败，变体跳过")
	} else {
		_, _, derr := probeImage(f)
		_ = f.Close()
		if derr != nil {
			status = mediamodel.VariantStatusFailed
			logger.Scene("media").With("attachment_id", att.ID).Error(derr, "图片探测未通过，变体跳过")
		}
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
	if err := s.vm.DeleteByAttachment(ctx, attachmentID); err != nil {
		return nil, err
	}

	failAll := func(reason error) {
		logger.Scene("media").With("attachment_id", attachmentID).Error(reason, "变体生成整体跳过")
		for _, vt := range mediamodel.VariantTypes() {
			rec := s.insertVariant(ctx, attachmentID, vt, variantObjectKey(sourceKey, vt), mediamodel.VariantStatusFailed)
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

	for _, vt := range mediamodel.VariantTypes() {
		key := variantObjectKey(sourceKey, vt)
		rec := s.insertVariant(ctx, attachmentID, vt, key, mediamodel.VariantStatusProcessing)
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
				"mime_type":   "image/webp",
				"update_time": time.Now(),
			})
			rec.Status = mediamodel.VariantStatusReady
		}
		res = append(res, variantEntityToResp(rec))
	}
	return res, nil
}

// insertVariant 插入一条指定状态的变体记录（GenerateVariants 内部流水使用）。
func (s *Service) insertVariant(ctx context.Context, attachmentID uint64, vt string, key string, status string) *mediamodel.MediaVariantEntity {
	rec := &mediamodel.MediaVariantEntity{
		AttachmentID: attachmentID,
		VariantType:  vt,
		FilePath:     key,
		Status:       status,
	}
	if err := s.vm.Create(ctx, rec); err != nil {
		// 记录插入失败不阻断文件生成：状态照常返回，日志留痕。
		logger.Scene("media").With("attachment_id", attachmentID).With("variant", vt).Error(err, "变体记录插入失败")
	}
	return rec
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
		URL:         "/storage/" + strings.TrimPrefix(filepath.ToSlash(e.FilePath), "/"),
		FileSize:    e.FileSize,
		Width:       width,
		Height:      height,
		MimeType:    mime,
	}
}
