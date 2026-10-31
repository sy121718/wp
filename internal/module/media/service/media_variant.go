package mediaservice

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"

	mediato "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/queue"
	"go_wp/pkg/upload"
)

var variantExcludedExts = map[string]struct{}{
	".svg": {},
	".gif": {},
}

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

func variantObjectKey(sourceKey string, variantType string) string {
	dir, stem := splitVariantSourceKey(sourceKey)
	return joinVariantKey(dir, stem+"_"+variantType+".jpg")
}

func variantObjectKeyFingerprinted(sourceKey string, variantType string, generation int, hash8 string) string {
	dir, stem := splitVariantSourceKey(sourceKey)
	return joinVariantKey(dir, fmt.Sprintf("%s_%s-%d-%s.jpg", stem, variantType, generation, hash8))
}

func splitVariantSourceKey(sourceKey string) (dir string, stem string) {
	slash := strings.TrimPrefix(filepath.ToSlash(sourceKey), "/")
	base := filepath.Base(slash)
	dir = filepath.Dir(slash)
	if dir == "." {
		dir = ""
	}
	return dir, strings.TrimSuffix(base, filepath.Ext(base))
}

func joinVariantKey(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

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

func variantEntitiesToResp(list []mediamodel.MediaVariantEntity) []mediato.VariantResp {
	out := make([]mediato.VariantResp, 0, len(list))
	for i := range list {
		out = append(out, variantEntityToResp(&list[i]))
	}
	return out
}

func attachmentStorageKey(att *mediamodel.AttachmentEntity) string {
	if att.StoragePath != nil && strings.TrimSpace(*att.StoragePath) != "" {
		return *att.StoragePath
	}
	return att.FilePath
}

type variantProduceResult struct {
	Key    string
	Width  int
	Height int
	Size   int64
}

func produceVariant(src image.Image, variantType string, sourceKey string, generation int) (variantProduceResult, error) {
	img, err := buildVariantImage(src, variantType)
	if err != nil {
		return variantProduceResult{}, err
	}
	data, err := encodeJPEGBytes(img)
	if err != nil {
		return variantProduceResult{}, err
	}
	key := variantObjectKeyFingerprinted(sourceKey, variantType, generation, contentFingerprint(data))
	absPath, err := localObjectPath(key)
	if err != nil {
		return variantProduceResult{}, err
	}
	if err := writeLocalFile(absPath, data); err != nil {
		return variantProduceResult{}, err
	}
	return variantProduceResult{
		Key:    key,
		Width:  img.Bounds().Dx(),
		Height: img.Bounds().Dy(),
		Size:   int64(len(data)),
	}, nil
}

func contentFingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:4])
}

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

func (s *Service) GenerateVariants(ctx context.Context, attachmentID uint64) (res []mediato.VariantResp, err error) {
	att, err := s.am.GetByID(ctx, attachmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, mediaenums.Err(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	if !variantEligible(att.FileType, att.FileName) {
		return nil, mediaenums.Err(mediaenums.ErrAttachmentNotImage)
	}
	if att.StorageType != "local" {
		return nil, mediaenums.Err(mediaenums.ErrVariantStorageNotLocal)
	}
	sourceKey := attachmentStorageKey(att)
	srcPath, err := localObjectPath(sourceKey)
	if err != nil {
		return nil, err
	}

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

	src, derr := decodeFile(srcPath)
	if derr != nil {
		failAll(derr)
		return res, nil
	}

	for _, rec := range records {
		vt := rec.VariantType
		produced, gerr := produceVariant(src, vt, sourceKey, att.Generation)
		if gerr != nil {
			_ = s.vm.Update(ctx, rec.ID, map[string]any{"status": mediamodel.VariantStatusFailed, "update_time": time.Now()})
			logger.Scene("media").With("attachment_id", attachmentID).With("variant", vt).Error(gerr, "变体生成失败")
			rec.Status = mediamodel.VariantStatusFailed
		} else {

			_ = s.vm.Update(ctx, rec.ID, map[string]any{
				"status":      mediamodel.VariantStatusReady,
				"file_path":   produced.Key,
				"width":       produced.Width,
				"height":      produced.Height,
				"file_size":   produced.Size,
				"mime_type":   "image/jpeg",
				"update_time": time.Now(),
			})
			rec.Status = mediamodel.VariantStatusReady
			rec.FilePath = produced.Key
		}
		res = append(res, variantEntityToResp(rec))
	}
	return res, nil
}

func decodeFile(srcPath string) (image.Image, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeImage(f)
}

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

func (s *Service) ProbeImageVariants(ctx context.Context, url string) []mediato.VariantRef {
	rel := localMediaStorageKey(url)
	if rel == "" {
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

	refs := make([]mediato.VariantRef, 0, len(list))
	seen := make(map[int]bool, len(list))
	for _, v := range list {
		if v.Status != mediamodel.VariantStatusReady || strings.TrimSpace(v.FilePath) == "" {
			continue
		}
		var w int
		switch v.VariantType {
		case mediamodel.VariantTypeThumb:
			w = thumbVariantEdge
		case mediamodel.VariantTypeSmall:
			w = smallVariantEdge
		case mediamodel.VariantTypeMedium:
			w = mediumVariantEdge
		default:
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		refs = append(refs, mediato.VariantRef{
			URL:   upload.StorageURL(filepath.ToSlash(v.FilePath)),
			Width: w,
		})
	}
	slices.SortFunc(refs, func(a, b mediato.VariantRef) int { return cmp.Compare(a.Width, b.Width) })
	return refs
}

const TaskMediaGenerateVariants = "media:generate_variants"

type VariantTaskPayload struct {
	AttachmentID uint64 `json:"attachment_id"`
}

var variantTask = queue.NewTask(TaskMediaGenerateVariants, queue.WithQueue("default"), queue.WithMaxRetry(3))

func (s *Service) scheduleVariants(ctx context.Context, attachmentID uint64) {
	defer func() {
		if r := recover(); r != nil {
			logger.Scene("media").With("attachment_id", attachmentID).Error(fmt.Errorf("panic: %v", r), "变体任务投递异常（已降级）")
		}
	}()

	if queue.IsInited() {
		if err := variantTask.Enqueue(VariantTaskPayload{AttachmentID: attachmentID}); err == nil {
			return
		}
	}

	if _, err := s.GenerateVariants(ctx, attachmentID); err != nil {
		logger.Scene("media").With("attachment_id", attachmentID).Error(err, "变体同步生成失败（已降级）")
	}
}

func handleVariantTask(db *gorm.DB) func(ctx context.Context, payload []byte) error {
	return func(ctx context.Context, payload []byte) error {
		var p VariantTaskPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		if p.AttachmentID == 0 {
			return errors.New("变体任务载荷缺少 attachment_id")
		}
		svc := NewService(
			mediamodel.NewAttachmentModel(db),
			mediamodel.NewFileCategoryModel(db),
			mediamodel.NewMediaVariantModel(db),
		)
		_, err := svc.GenerateVariants(ctx, p.AttachmentID)
		if err != nil {
			switch err.Error() {
			case mediaenums.ErrAttachmentNotFound, mediaenums.ErrAttachmentNotImage, mediaenums.ErrVariantStorageNotLocal:

				logger.Scene("media").With("attachment_id", p.AttachmentID).With("reason", err.Error()).Warn("变体任务终止（不可重试）")
				return nil
			}
			return err
		}
		return nil
	}
}

func RegisterVariantTaskHandler(db *gorm.DB) {
	queue.Register(TaskMediaGenerateVariants, handleVariantTask(db))
}

var (
	errInvalidImageDimensions = errors.New("图片尺寸非法")
	errImageTooLarge          = errors.New("图片尺寸超过变体生成上限")
	errUnknownVariantType     = errors.New("未知变体类型")
)

const (
	maxVariantSourceEdge = 6000

	thumbVariantEdge = 320

	mediumVariantEdge = 1280

	smallVariantEdge = 768

	variantJPEGQuality = 82
)

func probeImage(r io.Reader) (int, int, error) {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, errInvalidImageDimensions
	}
	if cfg.Width > maxVariantSourceEdge || cfg.Height > maxVariantSourceEdge {
		return cfg.Width, cfg.Height, errImageTooLarge
	}
	return cfg.Width, cfg.Height, nil
}

func decodeImage(r io.Reader) (image.Image, error) {
	return imaging.Decode(r, imaging.AutoOrientation(true))
}

func flattenToOpaque(img image.Image) image.Image {
	if _, ok := img.(*image.NRGBA); !ok {

		switch img.(type) {
		case *image.YCbCr, *image.Gray:
			return img
		}
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, b, img, b.Min, draw.Over)
	return dst
}

func encodeJPEGBytes(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenToOpaque(img), &jpeg.Options{Quality: variantJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildVariantImage(src image.Image, variantType string) (image.Image, error) {
	switch variantType {
	case mediamodel.VariantTypeThumb:
		return imaging.Fit(src, thumbVariantEdge, thumbVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeSmall:
		return imaging.Fit(src, smallVariantEdge, smallVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeMedium:
		return imaging.Fit(src, mediumVariantEdge, mediumVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeFull:
		return src, nil
	default:
		return nil, errUnknownVariantType
	}
}
