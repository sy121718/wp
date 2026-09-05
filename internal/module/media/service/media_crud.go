package mediaservice

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	mediato "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/upload"

	"gorm.io/gorm"
)

// Upload 上传文件并记录附件元数据。
func (s *Service) Upload(ctx context.Context, file *multipart.FileHeader, categoryID *uint64) (*mediato.AttachmentResp, error) {
	if file == nil {
		return nil, errors.New(mediaenums.ErrUploadEmpty)
	}
	if categoryID != nil && *categoryID > 0 {
		if _, err := s.cm.GetCategory(ctx, *categoryID); err != nil {
			return nil, errors.New("目标分类不存在")
		}
	}

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}
	defer src.Close()

	ext := strings.ToLower(filepath.Ext(file.Filename))
	fileType := classifyType(ext, file.Header.Get("Content-Type"))

	result, err := upload.Upload(ctx, upload.File{
		Filename:    file.Filename,
		Reader:      src,
		Size:        file.Size,
		ContentType: file.Header.Get("Content-Type"),
	}, upload.Request{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}

	now := time.Now()
	mimeType := file.Header.Get("Content-Type")
	entity := &mediamodel.AttachmentEntity{
		CategoryID:  categoryID,
		FileName:    file.Filename,
		FilePath:    result.Key,
		FileSize:    result.Size,
		FileType:    fileType,
		MimeType:    &mimeType,
		StorageType: result.Provider,
		StoragePath: &result.Key,
		URL:         &result.URL,
		Status:      mediamodel.AttachmentStatusEnabled,
		CreateTime:  now,
	}

	if err := s.am.Create(ctx, entity); err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}

	return entityToResp(entity), nil
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
	return entityToResp(e), nil
}

// Delete 删除附件（软删除元数据）。
func (s *Service) Delete(ctx context.Context, req *mediato.DeleteReq) error {
	_, err := s.am.GetByID(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return err
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
