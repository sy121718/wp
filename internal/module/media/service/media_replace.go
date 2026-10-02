package mediaservice

// media_replace.go — 换图（02-B 稳定引用的核心动作）：
// 保持 /storage/<id>.<ext> 不变、内容整体替换、generation +1。
//
// 为什么必须保持扩展名：URL 由 <id>.<ext> 推导，换扩展名等于换 URL，
// 页面里的引用即刻失效——与「稳定引用」目标冲突。需要换格式时先删后传（新附件新 ID）。
//
// 原子性：先经 pkg/upload 完整校验链落盘到临时 key（魔数嗅探 / 大小限制 / O_EXCL），
// 再同设备 rename 覆盖目标路径——访客不会读到半个文件，也没有 404 窗口。

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	mediato "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"

	"gorm.io/gorm"
)

// Replace 换图：内容替换 + URL 不变 + generation +1（图片类自动重建变体）。
//
// 返回语义：
//   - 内容与现状 md5 相同：不改动任何字段，直接返回现状（幂等，不 +1）；
//     但仍重发一次失效通知（幂等分支注释里写了为什么）；
//   - 内容变化：落盘覆盖 → generation +1 → md5/大小/MIME 回填 → 图片类重建变体 →
//     **标记引用方待重建**（变体名带内容指纹，不通知引用方则新图永远不可见）；
//   - 扩展名与现有 file_path 不一致：拒绝（见文件头说明）。
func (s *Service) Replace(ctx context.Context, id uint64, file *multipart.FileHeader) (res *mediato.AttachmentResp, err error) {
	if file == nil {
		return nil, errors.New(mediaenums.ErrUploadEmpty)
	}
	att, err := s.am.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	if att.StorageType != "local" {
		return nil, errors.New(mediaenums.ErrVariantStorageNotLocal)
	}

	currentKey := attachmentStorageKey(att)
	if strings.TrimSpace(currentKey) == "" {
		return nil, errors.New(mediaenums.ErrReplaceFailed)
	}
	newExt := strings.ToLower(filepath.Ext(file.Filename))
	oldExt := strings.ToLower(filepath.Ext(currentKey))
	if newExt != oldExt {
		return nil, errors.New(mediaenums.ErrReplaceExtMismatch)
	}

	md5hex, err := fileMD5(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrReplaceFailed, err)
	}
	// 内容未变：不动 generation，直接返回现状（前端可据此提示「内容相同」）。
	//
	// 但仍然走一次失效通知：这一次换图没有产生新 URL，可**上一次**同文件换图的
	// 通知可能失败（见 notifyRefsStale 的失败语义），而重传同一个文件是用户手边
	// 唯一的自愈动作 —— 在这里补一次通知，重传就能把「页面停在旧字节」修好。
	// 没有引用方时 ListRefs 返回空，这里的代价只有一条查询。
	if att.MD5 != nil && *att.MD5 == md5hex {
		if nerr := s.notifyRefsStale(ctx, att.ID); nerr != nil {
			return nil, fmt.Errorf("%s: %w", mediaenums.ErrReplaceStaleNotifyFailed, nerr)
		}
		resp := entityToResp(att)
		list := []mediato.AttachmentResp{*resp}
		s.fillVariants(ctx, list)
		return &list[0], nil
	}

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrReplaceFailed, err)
	}
	defer src.Close()

	// 临时 key：走完整上传校验链（嗅探/限流/O_EXCL），落在 storage 内保证 rename 同设备。
	tempKey := fmt.Sprintf("tmp/replace_%d_%d%s", id, time.Now().UnixNano(), newExt)
	staged, err := upload.Upload(ctx, upload.File{
		Filename:    file.Filename,
		Reader:      src,
		Size:        file.Size,
		ContentType: file.Header.Get("Content-Type"),
	}, upload.Request{ObjectKey: tempKey})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrReplaceFailed, err)
	}

	stagedAbs, err := localObjectPath(staged.Key)
	if err != nil {
		_ = os.Remove(stagedAbs)
		return nil, err
	}
	targetAbs, err := localObjectPath(currentKey)
	if err != nil {
		_ = os.Remove(stagedAbs)
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		_ = os.Remove(stagedAbs)
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrReplaceFailed, err)
	}
	// 同目录 rename 原子覆盖：URL 不变，无 404 窗口。
	if err := os.Rename(stagedAbs, targetAbs); err != nil {
		_ = os.Remove(stagedAbs)
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrReplaceFailed, err)
	}

	// generation 自增与 md5/大小/MIME 回填走**同一条 UPDATE**（model.ReplaceContentMeta）：
	// 分两条 SQL 会留下「磁盘已是新内容、DB 仍标旧 md5 与旧代数」的中间态 ——
	// 之后同内容上传会命中「内容未变」幂等分支返回错误现状，且没有自愈路径。
	mimeType := file.Header.Get("Content-Type")
	gen, err := s.am.ReplaceContentMeta(ctx, id, md5hex, staged.Size, mimeType, time.Now())
	if err != nil {
		logger.Scene("media").With("attachment_id", id).
			Error(err, "换图元数据回填失败：磁盘内容与 DB 元数据可能不一致，需人工核对")
		return nil, err
	}

	att.MD5 = &md5hex
	att.FileSize = staged.Size
	att.MimeType = &mimeType
	att.Generation = gen

	// 变体重建：原图内容变了，thumb/medium/full 必须重算（失败降级，不影响换图结果）。
	//
	// generation 已在上面推进过 —— 新变体名带新 generation 与新内容指纹，于是换图
	// 天然产出一组新 URL。旧变体文件**保留**：已发布产物里的 srcset 仍指向旧名，
	// 删了会让那些页面当场 404，而重建是异步的、且可能失败（构建报错 / worker 没跑 /
	// 队列堆积），回收旧文件必须排在「引用方都重建完」之后。
	// 保留的代价是对账会把它们列出来 —— 分类与处置见 media_reconcile.go 的
	// ReconcileKindSupersededVariant：单列一类、标注「可能仍被线上产物引用」，
	// 与真正的孤儿文件分开，不由巡检自动清理。两处的意图是同一句话：**留着，直到有人
	// 确认引用方已重建**。
	if variantEligible(att.FileType, att.FileName) {
		if _, gerr := s.GenerateVariants(ctx, id); gerr != nil {
			logger.Scene("media").With("attachment_id", id).Error(gerr, "换图后变体重建失败（已降级）")
		}
	}

	// 失效通知：把「引用了这个附件」的引用方标记为待重建（见 media_stale_notify.go）。
	//
	// 顺序是刻意的：先重建变体（新 URL 此时才存在），再通知引用方 —— 引用方重建时按
	// 现态取 srcset，反过来会产出一份仍指向旧名的产物。缺了这一步，旧 URL 会一直
	// 返回旧字节（immutable 长缓存），换图对访客等于没发生。
	//
	// 失败不吞：内容已经换掉，但「引用方不会更新」必须让操作者看见（错误文案说明
	// 内容已替换、重传同一文件可重试这条通知）。
	if nerr := s.notifyRefsStale(ctx, id); nerr != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrReplaceStaleNotifyFailed, nerr)
	}

	resp := entityToResp(att)
	list := []mediato.AttachmentResp{*resp}
	s.fillVariants(ctx, list)
	return &list[0], nil
}
