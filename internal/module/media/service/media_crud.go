package mediaservice

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	mediato "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"

	"gorm.io/gorm"
)

// compensateDelete 上传中途失败时清掉半成品附件记录。
//
// 补偿本身失败不能改变原始错误（调用方要看到的是上传失败原因），但必须留下
// 可观测记录：原实现的补偿调用把失败一起吞掉，遇到补偿失败会在库里留下没有任何
// 线索的孤儿记录。
func (s *Service) compensateDelete(ctx context.Context, id uint64) {
	if err := s.am.HardDelete(ctx, id); err != nil {
		logger.Scene("media").With("attachmentId", id).
			Error(err, "上传失败补偿删除附件记录失败（可能留下孤儿记录）")
	}
}

// Upload 上传文件并记录附件元数据（02-B 媒体中心：稳定引用 + 上传去重）。
//
// 流程（迁移 067 之后的语义）：
//  1. 流式算 md5（复用 multipart 的多次 Open，不改动 pkg/upload 的校验路径）；
//  2. 「md5 + file_type」命中启用中的附件 → 直接复用已有记录：不重复落盘、不重复入库，
//     响应带回原 URL 与变体信息（Duplicate=true 供前端提示）；
//  3. 未命中 → 两阶段登记：先插入草稿记录拿主键 ID，再用 ID 命名落盘
//     <id>.<ext>（URL 与文件内容解耦，换图后 URL 不变），最后回填路径/URL/大小并置为启用；
//     落盘或回填失败即物理删除草稿记录，不留半成品。
//
// 存量附件路径不动：历史随机名继续按原 file_path 提供访问，构建期探测按 file_path 反查。
func (s *Service) Upload(ctx context.Context, file *multipart.FileHeader, categoryID *uint64) (*mediato.AttachmentResp, error) {
	if file == nil {
		return nil, errors.New(mediaenums.ErrUploadEmpty)
	}
	if categoryID != nil && *categoryID > 0 {
		if _, err := s.cm.GetCategory(ctx, *categoryID); err != nil {
			return nil, errors.New("目标分类不存在")
		}
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	fileType := classifyType(ext, file.Header.Get("Content-Type"))

	md5hex, err := fileMD5(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}

	// 去重命中：同一份内容（md5 + 类型）只存一份，直接复用已有附件的 URL 与变体。
	if existing, derr := s.am.GetByMD5AndType(ctx, md5hex, fileType); derr == nil && existing != nil {
		resp := entityToResp(existing)
		resp.Duplicate = true
		list := []mediato.AttachmentResp{*resp}
		s.fillVariants(ctx, list)
		return &list[0], nil
	}

	// 两阶段登记：先入库拿 ID（草稿态 status=0，对外查询不可见），再用 ID 命名落盘。
	mimeType := file.Header.Get("Content-Type")
	entity := &mediamodel.AttachmentEntity{
		CategoryID:  categoryID,
		FileName:    file.Filename,
		FilePath:    "",
		FileSize:    file.Size,
		FileType:    fileType,
		MimeType:    &mimeType,
		StorageType: "local",
		MD5:         &md5hex,
		Status:      mediamodel.AttachmentStatusDisabled,
		CreateTime:  time.Now(),
	}
	if err := s.am.Create(ctx, entity); err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}
	if entity.ID == 0 {
		s.compensateDelete(ctx, entity.ID)
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, errors.New("附件主键未回填"))
	}

	src, err := file.Open()
	if err != nil {
		s.compensateDelete(ctx, entity.ID)
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}
	defer src.Close()

	// 稳定命名：<id>.<ext>（无扩展名时为 <id>）。ID 全局唯一，不会与存量随机名冲突。
	objectKey := fmt.Sprintf("%d%s", entity.ID, ext)
	result, err := upload.Upload(ctx, upload.File{
		Filename:    file.Filename,
		Reader:      src,
		Size:        file.Size,
		ContentType: file.Header.Get("Content-Type"),
	}, upload.Request{ObjectKey: objectKey})
	if err != nil {
		s.compensateDelete(ctx, entity.ID)
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}

	if err := s.am.AttachmentUpdate(ctx, entity.ID, map[string]any{
		"file_path":    result.Key,
		"storage_path": result.Key,
		"url":          result.URL,
		"file_size":    result.Size,
		"storage_type": result.Provider,
		"status":       mediamodel.AttachmentStatusEnabled,
		"update_time":  time.Now(),
	}); err != nil {
		s.compensateDelete(ctx, entity.ID)
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}

	// 回填响应字段（避免再查一次库）。
	entity.FilePath = result.Key
	entity.StoragePath = &result.Key
	entity.URL = &result.URL
	entity.FileSize = result.Size
	entity.StorageType = result.Provider
	entity.Status = mediamodel.AttachmentStatusEnabled
	if entity.Generation == 0 {
		entity.Generation = 1
	}

	// 图片变体（048 改造）：图片类且非 svg/gif 时登记变体记录并投递异步生成任务；
	// 变体任何失败一律降级（failed 记录/日志），不回滚主上传、不影响本响应。
	s.EnsureVariantRecords(ctx, entity)
	s.scheduleVariants(ctx, entity.ID)

	return entityToResp(entity), nil
}

// fileMD5 流式计算上传文件的 md5（十六进制小写 32 位）。
// 独立 Open 一次读取，不影响随后 upload.Upload 的完整校验链路
// （魔数嗅探、大小限制、O_EXCL 落盘仍在原处生效）。
func fileMD5(file *multipart.FileHeader) (string, error) {
	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()
	h := md5.New()
	if _, err := io.Copy(h, src); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// List 分页查询附件列表。
func (s *Service) List(ctx context.Context, req *mediato.ListReq) (*mediato.ListResp, error) {
	list, total, err := s.am.List(ctx, req.FileType, req.CategoryID, req.Search, req.GetOffset(), req.GetLimit())
	if err != nil {
		return nil, err
	}

	resps := make([]mediato.AttachmentResp, 0, len(list))
	for _, e := range list {
		resps = append(resps, *entityToResp(&e))
	}
	// 变体状态批量填充（一次 IN 查询，避免 N+1；失败不影响列表主数据）。
	s.fillVariants(ctx, resps)

	return &mediato.ListResp{
		Total: total,
		Page:  req.GetPage(),
		Limit: req.GetLimit(),
		List:  resps,
	}, nil
}

// Detail 查询单个附件详情。
func (s *Service) Detail(ctx context.Context, req *mediato.DetailReq) (*mediato.AttachmentResp, error) {
	e, err := s.am.GetByID(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	resp := entityToResp(e)
	// 详情填充变体状态（thumb/medium/webp 徽标数据源）。
	list := []mediato.AttachmentResp{*resp}
	s.fillVariants(ctx, list)
	return &list[0], nil
}

// Delete 删除附件（软删除元数据）。
//
// 引用保护（02-B 第 4 能力）：删除前查 extra_info.refs（构建期写入的引用缓存），
// 命中即拒绝并给出「被 N 个页面引用」提示——避免删掉线上页面正在用的图。
// 引用缓存为空（未被任何构建产物引用）时才允许软删。
func (s *Service) Delete(ctx context.Context, req *mediato.DeleteReq) error {
	_, err := s.am.GetByID(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return err
	}

	refs, err := s.am.ListRefs(ctx, req.ID)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		// 首段为 enums key（前端可前缀匹配做多语言），后段为可读明细（含引用数量与来源）。
		return fmt.Errorf("%s: 被 %s", mediaenums.ErrAttachmentReferenced, summarizeRefs(refs))
	}

	return s.am.Delete(ctx, req.ID)
}

// CategoryTree 获取文件分类树。
func (s *Service) CategoryTree(ctx context.Context) ([]mediato.CategoryTreeNode, error) {
	categories, err := s.cm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildCategoryTree(categories, 0), nil
}

// --- 辅助函数 ---

func entityToResp(e *mediamodel.AttachmentEntity) *mediato.AttachmentResp {
	url := ""
	if e.URL != nil {
		url = *e.URL
	}
	mime := ""
	if e.MimeType != nil {
		mime = *e.MimeType
	}
	md5 := ""
	if e.MD5 != nil {
		md5 = *e.MD5
	}
	extra := ""
	if e.ExtraInfo != nil {
		extra = *e.ExtraInfo
	}
	return &mediato.AttachmentResp{
		ID:          e.ID,
		CategoryID:  e.CategoryID,
		FileName:    e.FileName,
		FileSize:    e.FileSize,
		FileType:    e.FileType,
		MimeType:    mime,
		StorageType: e.StorageType,
		URL:         url,
		MD5:         md5,
		ExtraInfo:   extra,
		CreateTime:  e.CreateTime.Format("2006-01-02 15:04:05"),
		Generation:  e.Generation,
	}
}

func classifyType(ext string, mimeType string) string {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp", ".ico":
		return "image"
	case ".mp4", ".webm", ".avi", ".mov", ".mkv":
		return "video"
	case ".mp3", ".wav", ".ogg", ".flac", ".aac":
		return "audio"
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".csv":
		return "document"
	default:
		if strings.HasPrefix(mimeType, "image/") {
			return "image"
		}
		if strings.HasPrefix(mimeType, "video/") {
			return "video"
		}
		if strings.HasPrefix(mimeType, "audio/") {
			return "audio"
		}
		return "other"
	}
}

func buildCategoryTree(categories []mediamodel.FileCategoryEntity, parentID uint64) []mediato.CategoryTreeNode {
	var nodes []mediato.CategoryTreeNode
	for _, c := range categories {
		if c.ParentID == parentID {
			node := mediato.CategoryTreeNode{
				ID:           c.ID,
				CategoryName: c.CategoryName,
				CategoryCode: c.CategoryCode,
				ParentID:     c.ParentID,
				SortOrder:    c.SortOrder,
				Children:     buildCategoryTree(categories, c.ID),
			}
			nodes = append(nodes, node)
		}
	}
	if nodes == nil {
		return []mediato.CategoryTreeNode{}
	}
	return nodes
}
