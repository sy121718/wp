package mediaservice

//   上传切入点 EnsureVariantRecords 登记 pending 记录并投递 asynq 任务（scheduleVariants）；
//   GenerateVariants 同步生成，供 worker、详情页「重新生成」与存量回填复用；
//   任何变体失败一律降级为 status=failed 记录，不回滚主上传、不阻断主流程。

// 任务类型 media:generate_variants，payload 仅携带 attachmentID；
// handler 经 RegisterVariantTaskHandler 挂进现有队列 worker（pkg/queue facade），
// 不新起 worker 进程；handler 幂等（GenerateVariants 重跑先清旧记录）。

// 变体生成使用纯 Go 库（disintegration/imaging 缩放 + 标准库 image/jpeg 有损编码），
// 只服务本模块的变体产物，不做通用图片工具。防御约定：
//   - 处理前用标准库 image.DecodeConfig 读 header，解码失败或任一边长超过
//     maxVariantSourceEdge（6000px）则拒绝生成（调用方将该变体记为 failed），不阻塞上传；
//   - 编码统一 JPEG（有损，质量 variantJPEGQuality）：此前用 nativewebp 无损编码，
//     实测 1280px 变体可达 1.7MB，体积是页面变慢的主因；JPEG 同尺寸通常 150~300KB；
//   - JPEG 不支持透明：带 alpha 的图先合成到白底（flattenToOpaque），避免透明区变黑；
//   - svg/gif 不参与变体生成（svg 为矢量无需位图，gif 多帧转码丢帧），在调用方过滤。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"

	mediato "go_wp/internal/module/media/dto"
	"go_wp/internal/module/media/enums"
	"go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/queue"
	"go_wp/pkg/upload"
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

// variantObjectKey 由原图存储 key 推导变体的**预期**存储 key：
// 与原图同目录，命名 <stem>_<variantType>.jpg（变体统一有损 JPEG 编码）。
//
// 这是记录登记时的初始值，**不是最终落盘名** —— 真实产物名带内容指纹
// （variantObjectKeyFingerprinted），生成成功后回填 file_path。
// 保留「可推导」这条性质：zip 打包与存量排查不依赖额外约定。
func variantObjectKey(sourceKey string, variantType string) string {
	dir, stem := splitVariantSourceKey(sourceKey)
	return joinVariantKey(dir, stem+"_"+variantType+".jpg")
}

// variantObjectKeyFingerprinted 带内容指纹的变体存储 key：
// <stem>_<variantType>-<generation>-<hash8>.jpg
//
// 为什么必须带指纹（PERF-007 收口）：变体 URL 稳定时，浏览器只能靠启发式缓存
// （无 Cache-Control + 有 Last-Modified 时大致取「距今时间的 10%」），换图之后
// 要么长时间显示旧图、要么每次浏览都回源重验证 —— 二选一，且随浏览器漂移。
// 指纹把 URL 与内容绑定后，变体才可以发 immutable 永久缓存：
// 换图 → generation+1 → URL 变 → 缓存自然失效，不需要任何清理动作。
func variantObjectKeyFingerprinted(sourceKey string, variantType string, generation int, hash8 string) string {
	dir, stem := splitVariantSourceKey(sourceKey)
	return joinVariantKey(dir, fmt.Sprintf("%s_%s-%d-%s.jpg", stem, variantType, generation, hash8))
}

// splitVariantSourceKey 拆原图存储 key 为「存储相对目录 + 去掉扩展名的词干」。
func splitVariantSourceKey(sourceKey string) (dir string, stem string) {
	slash := strings.TrimPrefix(filepath.ToSlash(sourceKey), "/")
	base := filepath.Base(slash)
	dir = filepath.Dir(slash)
	if dir == "." {
		dir = ""
	}
	return dir, strings.TrimSuffix(base, filepath.Ext(base))
}

// joinVariantKey 拼存储相对 key（目录为空时不带前导斜杠）。
func joinVariantKey(dir, name string) string {
	if dir == "" {
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
	// Key 带内容指纹的存储相对 key —— 必须回填进记录：
	// 探测侧（ProbeImageVariants）与对账侧都从这里取真实路径，不看预期名。
	Key    string
	Width  int
	Height int
	Size   int64
}

// produceVariant 生成单个变体文件：按类型缩放/重编码 → JPEG 有损编码 →
// 算内容指纹定名 → 落盘，返回真实 key 与宽高大小。
//
// 定名必须在编码**之后**：指纹取自产物字节，字节只有编码完成才知道。
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

// contentFingerprint 变体内容指纹：产物字节 sha256 的前 8 位小写 hex。
//
// 8 位 hex（32 bit）在「单附件四槽位」的规模下碰撞概率可忽略，
// 同时让文件名保持人类可读 —— 对账时能一眼看出是哪一代产物。
func contentFingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:4])
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

// variantRecordsFor 按探测结果构造四条变体初始记录（纯函数，不写库）。
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

// EnsureVariantRecords 图片类附件登记四条变体初始记录（自足入口：探测 + 批量插入）。
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
		produced, gerr := produceVariant(src, vt, sourceKey, att.Generation)
		if gerr != nil {
			_ = s.vm.Update(ctx, rec.ID, map[string]any{"status": mediamodel.VariantStatusFailed, "update_time": time.Now()})
			logger.Scene("media").With("attachment_id", attachmentID).With("variant", vt).Error(gerr, "变体生成失败")
			rec.Status = mediamodel.VariantStatusFailed
		} else {
			// file_path 回填**带指纹的真实名**（不是登记时写的预期名）：
			// 探测（ProbeImageVariants）与对账都从这里取路径，不看预期名。
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

// ProbeImageVariants 按公开 URL（/storage/...）探测已就绪的图片变体，
// 供构建期响应式图片（srcset）使用：宽度取变体类型的标准边，
// URL 取记录里的**真实路径**（带内容指纹）；升序去重，
// 保证同一文档重复编译产物字节一致。
// 非媒体库 URL、附件不存在或变体未就绪返回 nil（调用方不输出 srcset）。
//
// 为什么 URL 由这里给出、而不是让调用方按宽度自己拼：变体名带
// <generation>-<hash8> 指纹，只有本模块（以及它查的记录）知道这两段。
func (s *Service) ProbeImageVariants(ctx context.Context, url string) []mediato.VariantRef {
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
	// full 变体与源图同尺寸，不参与 srcset（原图已由 src 兜底）。
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
	sort.Slice(refs, func(i, j int) bool { return refs[i].Width < refs[j].Width })
	return refs
}

// TaskMediaGenerateVariants asynq 任务类型常量。
const TaskMediaGenerateVariants = "media:generate_variants"

// VariantTaskPayload 变体生成任务载荷（JSON）。
type VariantTaskPayload struct {
	AttachmentID uint64 `json:"attachment_id"`
}

// variantTask 队列任务门面（固定 taskType，default 队列）。
var variantTask = queue.NewTask(TaskMediaGenerateVariants, queue.WithQueue("default"), queue.WithMaxRetry(3))

// scheduleVariants 投递异步变体生成任务；队列未就绪或入队失败时
// 同步生成兜底（仍不阻断上传——GenerateVariants 内部已降级失败项）。
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
	// 队列未启用（queue.enabled=false）或入队失败：同步生成兜底。
	if _, err := s.GenerateVariants(ctx, attachmentID); err != nil {
		logger.Scene("media").With("attachment_id", attachmentID).Error(err, "变体同步生成失败（已降级）")
	}
}

// handleVariantTask asynq handler：payload 反序列化 → 同步生成变体。
// 幂等由 GenerateVariants 保证；附件不存在/非图片/非本地存储等不可重试错误返回 nil，
// 避免无意义重试；DB 级错误原样返回交给 asynq 重试。
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
				// 不可重试的业务降级：记录日志后返回 nil。
				logger.Scene("media").With("attachment_id", p.AttachmentID).With("reason", err.Error()).Warn("变体任务终止（不可重试）")
				return nil
			}
			return err
		}
		return nil
	}
}

// RegisterVariantTaskHandler 把变体生成 handler 注册进队列 worker（路由装配时调用）。
// queue.Register 会重放到 provider（未 Init 时暂存，Init 时重放），无时序要求。
func RegisterVariantTaskHandler(db *gorm.DB) {
	queue.Register(TaskMediaGenerateVariants, handleVariantTask(db))
}

// 变体处理的包内哨兵错误（面向用户的文案统一经 mediaenums，由调用方包装）.
var (
	errInvalidImageDimensions = errors.New("图片尺寸非法")
	errImageTooLarge          = errors.New("图片尺寸超过变体生成上限")
	errUnknownVariantType     = errors.New("未知变体类型")
)

const (
	// maxVariantSourceEdge 变体源图任一边长上限：超过则跳过变体（防御
	// 超大图解码的内存/CPU 放大，6000px 与设计规格一致）。
	maxVariantSourceEdge = 6000

	// thumbVariantEdge 缩略图变体的 Fit 边长（320x320）。
	thumbVariantEdge = 320

	// mediumVariantEdge 调节尺寸变体的 Fit 边长（1280x1280）。
	mediumVariantEdge = 1280

	// smallVariantEdge 中间档变体的 Fit 边长（768x768）。
	//
	// 取值理由：组件的 sizes 是「≤640px 视口 100vw、其余 50vw」，DPR=2 的 375pt
	// 手机需要约 750 设备像素。取 640 会让这些机型仍然只能选 1280（规则是「满足
	// 所需的最小候选」，640 < 750 不满足），取 768 才真正落进这一档。
	smallVariantEdge = 768

	// variantJPEGQuality JPEG 变体编码质量：82 是「视觉无损」常用档，
	// 相比无损 webp 体积小 5~10 倍，是页面加载速度的关键。
	variantJPEGQuality = 82
)

// probeImage 读取图片 header 探测尺寸，返回 (宽, 高, err)。
// 解码失败或任一边超过 maxVariantSourceEdge 时返回错误——调用方据此
// 把变体记录标记为 failed 并跳过生成（status=failed 语义）。
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

// decodeImage 完整解码图片为 image.Image（调用方保证已通过 probeImage 防御）。
// AutoOrientation 按 EXIF 方向摆正，变体与视觉方向一致。
func decodeImage(r io.Reader) (image.Image, error) {
	return imaging.Decode(r, imaging.AutoOrientation(true))
}

// flattenToOpaque 把带 alpha 的图像合成到白底（JPEG 无透明通道）。
// 无 alpha 的图原样返回，避免不必要的拷贝。
func flattenToOpaque(img image.Image) image.Image {
	if _, ok := img.(*image.NRGBA); !ok {
		// 常见不透明类型（YCbCr / RGBA 且不透明）直接返回。
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

// encodeJPEGBytes 编码为有损 JPEG 字节切片（落盘写入用）。
func encodeJPEGBytes(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenToOpaque(img), &jpeg.Options{Quality: variantJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildVariantImage 按变体类型生成目标图像：
//
//	thumb  = imaging.Fit 320x320 Lanczos
//	small  = imaging.Fit 768x768 Lanczos
//	medium = imaging.Fit 1280x1280 Lanczos
//	full   = 原图尺寸（重编码为 JPEG）
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
