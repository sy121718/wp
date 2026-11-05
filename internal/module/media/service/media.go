package mediaservice

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
)

func (s *Service) compensateDelete(ctx context.Context, id uint64) {
	if err := s.am.HardDelete(ctx, id); err != nil {
		logger.Scene("media").With("attachmentId", id).
			Error(err, "上传失败补偿删除附件记录失败（可能留下孤儿记录）")
	}
}

func detectContentMIME(src multipart.File, declared string) string {
	head := make([]byte, 512)
	n, _ := src.Read(head)
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return declared
	}
	if detected := upload.DetectMIME(head[:n]); detected != "" {
		return detected
	}
	return declared
}

func (s *Service) Upload(ctx context.Context, file *multipart.FileHeader, categoryID *uint64) (*mediadto.AttachmentResp, error) {
	if file == nil {
		return nil, mediaenums.Err(mediaenums.ErrUploadEmpty)
	}
	if categoryID != nil && *categoryID > 0 {
		if _, err := s.cm.GetCategory(ctx, *categoryID); err != nil {
			return nil, mediaenums.Err(mediaenums.ErrFileCategoryNotFound)
		}
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	fileType := classifyType(ext, file.Header.Get("Content-Type"))

	md5hex, err := fileMD5(file)
	if err != nil {
		return nil, mediaenums.ErrDetail(mediaenums.ErrUploadFailed, err.Error())
	}

	if existing, derr := s.am.GetByMD5AndType(ctx, md5hex, fileType); derr == nil && existing != nil {
		resp := entityToResp(existing)
		resp.Duplicate = true
		list := []mediadto.AttachmentResp{*resp}
		s.fillVariants(ctx, list)
		return &list[0], nil
	}

	src, err := file.Open()
	if err != nil {
		return nil, mediaenums.ErrDetail(mediaenums.ErrUploadFailed, err.Error())
	}
	defer src.Close()

	mimeType := detectContentMIME(src, file.Header.Get("Content-Type"))

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
		return nil, mediaenums.ErrDetail(mediaenums.ErrUploadFailed, err.Error())
	}
	if entity.ID == 0 {
		s.compensateDelete(ctx, entity.ID)
		return nil, mediaenums.ErrDetail(mediaenums.ErrUploadFailed, "附件主键未回填")
	}

	objectKey := fmt.Sprintf("%d%s", entity.ID, ext)
	result, err := upload.Upload(ctx, upload.File{
		Filename:    file.Filename,
		Reader:      src,
		Size:        file.Size,
		ContentType: file.Header.Get("Content-Type"),
	}, upload.Request{ObjectKey: objectKey})
	if err != nil {
		s.compensateDelete(ctx, entity.ID)
		return nil, mediaenums.ErrDetail(mediaenums.ErrUploadFailed, err.Error())
	}

	entity.FilePath = result.Key
	entity.StoragePath = &result.Key
	entity.URL = &result.URL
	entity.FileSize = result.Size
	entity.StorageType = result.Provider
	entity.Status = mediamodel.AttachmentStatusEnabled
	if entity.Generation == 0 {
		entity.Generation = 1
	}

	variantStatus := s.probeVariantStatus(ctx, entity)

	updatedAt := time.Now()
	if terr := s.am.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.am.AttachmentUpdateTx(ctx, tx, entity.ID, map[string]any{
			"file_path":    result.Key,
			"storage_path": result.Key,
			"url":          result.URL,
			"file_size":    result.Size,
			"storage_type": result.Provider,
			"status":       mediamodel.AttachmentStatusEnabled,
			"update_time":  updatedAt,
		}); uerr != nil {
			return uerr
		}
		return s.vm.CreateBatchTx(ctx, tx, variantRecordsFor(entity, variantStatus))
	}); terr != nil {

		s.compensateDelete(ctx, entity.ID)
		return nil, mediaenums.ErrDetail(mediaenums.ErrUploadFailed, terr.Error())
	}

	s.scheduleVariants(ctx, entity.ID)

	return entityToResp(entity), nil
}

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

func (s *Service) List(ctx context.Context, req *mediadto.ListReq) (*mediadto.ListResp, error) {
	limit := req.GetLimit()
	var list []mediamodel.AttachmentEntity
	var total int64
	var err error
	if req.Cursor == "" {
		list, total, err = s.am.List(ctx, req.FileType, req.CategoryID, req.Uncategorized, req.Search, req.GetOffset(), limit)
	} else {
		after, afterID, decodeErr := decodeMediaCursor(req.Cursor)
		if decodeErr != nil {
			return nil, decodeErr
		}
		list, total, err = s.am.ListAfter(ctx, req.FileType, req.CategoryID, req.Uncategorized, req.Search, after, afterID, limit)
	}
	if err != nil {
		return nil, err
	}

	resps := make([]mediadto.AttachmentResp, 0, len(list))
	for _, e := range list {
		resps = append(resps, *entityToResp(&e))
	}

	s.fillVariants(ctx, resps)

	res := &mediadto.ListResp{Total: total, Page: req.GetPage(), Limit: limit, List: resps}
	if len(resps) == limit && len(resps) > 0 {
		last := list[len(list)-1]
		res.NextCursor = encodeMediaCursor(last.CreateTime, last.ID)
	}
	return res, nil
}

func encodeMediaCursor(created time.Time, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%d", created.UTC().Format(time.RFC3339Nano), id)))
}

func decodeMediaCursor(cursor string) (time.Time, uint64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, mediaenums.Err(mediaenums.ErrMediaCursorInvalid)
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 {
		return time.Time{}, 0, mediaenums.Err(mediaenums.ErrMediaCursorInvalid)
	}
	created, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, mediaenums.Err(mediaenums.ErrMediaCursorInvalid)
	}
	var id uint64
	if _, err = fmt.Sscan(parts[1], &id); err != nil || id == 0 {
		return time.Time{}, 0, mediaenums.Err(mediaenums.ErrMediaCursorInvalid)
	}
	return created, id, nil
}

func (s *Service) Detail(ctx context.Context, req *mediadto.DetailReq) (*mediadto.AttachmentResp, error) {
	if req == nil || req.ID == 0 {
		return nil, mediaenums.Err(mediaenums.MsgBadRequest)
	}
	e, err := s.am.GetByID(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, mediaenums.Err(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	resp := entityToResp(e)

	list := []mediadto.AttachmentResp{*resp}
	s.fillVariants(ctx, list)
	return &list[0], nil
}

func (s *Service) Delete(ctx context.Context, req *mediadto.DeleteReq) error {

	if req == nil || req.ID == 0 {
		return mediaenums.Err(mediaenums.MsgBadRequest)
	}
	_, err := s.am.GetByID(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mediaenums.Err(mediaenums.ErrAttachmentNotFound)
		}
		return err
	}

	refs, err := s.am.ListRefs(ctx, req.ID)
	if err != nil {
		return err
	}
	if len(refs) > 0 {

		return mediaenums.ErrDetail(mediaenums.ErrAttachmentReferenced, "被 "+summarizeRefs(refs))
	}

	return s.am.Delete(ctx, req.ID)
}

func (s *Service) CategoryTree(ctx context.Context) ([]mediadto.CategoryTreeNode, error) {
	categories, err := s.cm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildCategoryTree(categories, 0), nil
}

func entityToResp(e *mediamodel.AttachmentEntity) *mediadto.AttachmentResp {

	key := strings.TrimSpace(e.FilePath)
	if e.StorageType != "local" && e.URL != nil {
		key = strings.TrimSpace(*e.URL)
	} else if key == "" && e.URL != nil {

		if old, err := url.Parse(strings.TrimSpace(*e.URL)); err == nil {
			key = old.Path
		}
	}
	mediaURL := upload.StorageURL(key)
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
	return &mediadto.AttachmentResp{
		ID:          e.ID,
		CategoryID:  e.CategoryID,
		FileName:    e.FileName,
		FileSize:    e.FileSize,
		FileType:    e.FileType,
		MimeType:    mime,
		StorageType: e.StorageType,
		URL:         mediaURL,
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

func buildCategoryTree(categories []mediamodel.FileCategoryEntity, parentID uint64) []mediadto.CategoryTreeNode {
	var nodes []mediadto.CategoryTreeNode
	for _, c := range categories {
		if c.ParentID == parentID {
			node := mediadto.CategoryTreeNode{
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
		return []mediadto.CategoryTreeNode{}
	}
	return nodes
}

func (s *Service) CreateCategory(ctx context.Context, req *mediadto.CategoryCreateReq) (*mediadto.CategoryTreeNode, error) {
	name := strings.TrimSpace(req.CategoryName)
	if name == "" {
		return nil, mediaenums.Err(mediaenums.ErrFileCategoryNameRequired)
	}
	if req.ParentID > 0 {
		if _, err := s.cm.GetCategory(ctx, req.ParentID); err != nil {
			return nil, mediaenums.Err(mediaenums.ErrFileCategoryParentNotFound)
		}
	}
	siblings, err := s.cm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range siblings {
		if c.ParentID == req.ParentID && strings.EqualFold(c.CategoryName, name) {
			return nil, mediaenums.Err(mediaenums.ErrFileCategoryNameDuplicated)
		}
	}
	now := time.Now()
	entity := &mediamodel.FileCategoryEntity{
		CategoryName: name,
		CategoryCode: fmt.Sprintf("cat_%d", now.UnixNano()),
		ParentID:     req.ParentID,
		SortOrder:    req.SortOrder,
		Status:       1,
		CreateTime:   &now,
		UpdateTime:   &now,
	}
	if err := s.cm.CreateCategory(ctx, entity); err != nil {
		return nil, err
	}
	return &mediadto.CategoryTreeNode{
		ID:           entity.ID,
		CategoryName: entity.CategoryName,
		CategoryCode: entity.CategoryCode,
		ParentID:     entity.ParentID,
		SortOrder:    entity.SortOrder,
		Children:     []mediadto.CategoryTreeNode{},
	}, nil
}

func (s *Service) UpdateCategory(ctx context.Context, req *mediadto.CategoryUpdateReq) error {
	current, err := s.cm.GetCategory(ctx, req.ID)
	if err != nil {
		return mediaenums.Err(mediaenums.ErrFileCategoryNotFound)
	}

	newParent := current.ParentID
	if req.ParentID != nil {
		newParent = *req.ParentID
	}
	updates := map[string]any{"update_time": time.Now()}
	if req.CategoryName != nil && strings.TrimSpace(*req.CategoryName) != "" {
		name := strings.TrimSpace(*req.CategoryName)

		siblings, err := s.cm.ListAll(ctx)
		if err != nil {
			return err
		}
		for _, c := range siblings {
			if c.ID != req.ID && c.ParentID == newParent && strings.EqualFold(c.CategoryName, name) {
				return mediaenums.Err(mediaenums.ErrFileCategoryNameDuplicated)
			}
		}
		updates["category_name"] = name
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if req.ParentID != nil {
		if newParent == req.ID {
			return mediaenums.Err(mediaenums.ErrFileCategoryCycle)
		}
		if newParent > 0 {
			if _, err := s.cm.GetCategory(ctx, newParent); err != nil {
				return mediaenums.Err(mediaenums.ErrFileCategoryParentNotFound)
			}

			all, err := s.cm.ListAll(ctx)
			if err != nil {
				return err
			}
			if isDescendant(all, req.ID, newParent) {
				return mediaenums.Err(mediaenums.ErrFileCategoryCycle)
			}
		}
		updates["parent_id"] = newParent
	}
	return s.cm.UpdateCategory(ctx, req.ID, updates)
}

func isDescendant(all []mediamodel.FileCategoryEntity, root, target uint64) bool {
	childrenOf := map[uint64][]uint64{}
	for _, c := range all {
		childrenOf[c.ParentID] = append(childrenOf[c.ParentID], c.ID)
	}
	stack := []uint64{root}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, child := range childrenOf[cur] {
			if child == target {
				return true
			}
			stack = append(stack, child)
		}
	}
	return false
}

func (s *Service) DeleteCategory(ctx context.Context, req *mediadto.CategoryDeleteReq) error {
	if _, err := s.cm.GetCategory(ctx, req.ID); err != nil {
		return mediaenums.Err(mediaenums.ErrFileCategoryNotFound)
	}
	hasChildren, err := s.cm.HasChildren(ctx, req.ID)
	if err != nil {
		return err
	}
	if hasChildren {
		return mediaenums.Err(mediaenums.ErrFileCategoryHasChildren)
	}
	if err := s.am.DetachAttachments(ctx, req.ID); err != nil {
		return err
	}
	return s.cm.DeleteCategory(ctx, req.ID)
}

func (s *Service) UpdateAttachment(ctx context.Context, req *mediadto.AttachmentUpdateReq) error {

	if _, err := s.am.GetByID(ctx, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mediaenums.Err(mediaenums.ErrAttachmentNotFound)
		}
		return err
	}
	updates := map[string]any{"update_time": time.Now()}
	if req.FileName != nil && strings.TrimSpace(*req.FileName) != "" {
		updates["file_name"] = strings.TrimSpace(*req.FileName)
	}
	if req.CategoryID != nil {
		if *req.CategoryID > 0 {
			if _, err := s.cm.GetCategory(ctx, *req.CategoryID); err != nil {
				return mediaenums.Err(mediaenums.ErrFileCategoryNotFound)
			}
			updates["category_id"] = *req.CategoryID
		} else {
			updates["category_id"] = nil
		}
	}

	setExtra := map[string]any{}
	var delExtra []string
	for _, pair := range []struct {
		key string
		val *string
	}{{"alt", req.Alt}, {"title", req.Title}, {"description", req.Description}} {
		if pair.val == nil {
			continue
		}
		v := strings.TrimSpace(*pair.val)
		if v == "" {
			delExtra = append(delExtra, pair.key)
		} else {
			setExtra[pair.key] = v
		}
	}
	if len(setExtra) > 0 || len(delExtra) > 0 {
		if merr := s.am.MergeAttachmentExtra(ctx, req.ID, setExtra, delExtra); merr != nil {
			return merr
		}
	}
	return s.am.AttachmentUpdate(ctx, req.ID, updates)
}

func (s *Service) Replace(ctx context.Context, id uint64, file *multipart.FileHeader) (res *mediadto.AttachmentResp, err error) {
	if file == nil {
		return nil, mediaenums.Err(mediaenums.ErrUploadEmpty)
	}
	att, err := s.am.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, mediaenums.Err(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	if att.StorageType != "local" {
		return nil, mediaenums.Err(mediaenums.ErrVariantStorageNotLocal)
	}

	currentKey := attachmentStorageKey(att)
	if strings.TrimSpace(currentKey) == "" {
		return nil, mediaenums.Err(mediaenums.ErrReplaceFailed)
	}
	newExt := strings.ToLower(filepath.Ext(file.Filename))
	oldExt := strings.ToLower(filepath.Ext(currentKey))
	if newExt != oldExt {
		return nil, mediaenums.Err(mediaenums.ErrReplaceExtMismatch)
	}

	md5hex, err := fileMD5(file)
	if err != nil {
		return nil, mediaenums.ErrDetail(mediaenums.ErrReplaceFailed, err.Error())
	}

	if att.MD5 != nil && *att.MD5 == md5hex {
		if nerr := s.notifyRefsStale(ctx, att.ID); nerr != nil {
			return nil, mediaenums.ErrDetail(mediaenums.ErrReplaceStaleNotifyFailed, nerr.Error())
		}
		resp := entityToResp(att)
		list := []mediadto.AttachmentResp{*resp}
		s.fillVariants(ctx, list)
		return &list[0], nil
	}

	src, err := file.Open()
	if err != nil {
		return nil, mediaenums.ErrDetail(mediaenums.ErrReplaceFailed, err.Error())
	}
	defer src.Close()

	tempKey := fmt.Sprintf("tmp/replace_%d_%d%s", id, time.Now().UnixNano(), newExt)
	staged, err := upload.Upload(ctx, upload.File{
		Filename:    file.Filename,
		Reader:      src,
		Size:        file.Size,
		ContentType: file.Header.Get("Content-Type"),
	}, upload.Request{ObjectKey: tempKey})
	if err != nil {
		return nil, mediaenums.ErrDetail(mediaenums.ErrReplaceFailed, err.Error())
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
		return nil, mediaenums.ErrDetail(mediaenums.ErrReplaceFailed, err.Error())
	}

	if err := os.Rename(stagedAbs, targetAbs); err != nil {
		_ = os.Remove(stagedAbs)
		return nil, mediaenums.ErrDetail(mediaenums.ErrReplaceFailed, err.Error())
	}

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

	if variantEligible(att.FileType, att.FileName) {
		if _, gerr := s.GenerateVariants(ctx, id); gerr != nil {
			logger.Scene("media").With("attachment_id", id).Error(gerr, "换图后变体重建失败（已降级）")
		}
	}

	if nerr := s.notifyRefsStale(ctx, id); nerr != nil {
		return nil, mediaenums.ErrDetail(mediaenums.ErrReplaceStaleNotifyFailed, nerr.Error())
	}

	resp := entityToResp(att)
	list := []mediadto.AttachmentResp{*resp}
	s.fillVariants(ctx, list)
	return &list[0], nil
}

const (
	zipDirOriginal = "original"
	zipDirFull     = "full"
	zipDirThumb    = "thumb"
	zipDirSmall    = "small"
	zipDirMedium   = "medium"
)

func sanitizeZipName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	var b strings.Builder
	for _, r := range replacer.Replace(name) {
		if r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return out
}

func variantTypeToZipDir(vt string) string {
	switch vt {
	case mediamodel.VariantTypeThumb:
		return zipDirThumb
	case mediamodel.VariantTypeSmall:
		return zipDirSmall
	case mediamodel.VariantTypeMedium:
		return zipDirMedium
	case mediamodel.VariantTypeFull:
		return zipDirFull
	default:
		return ""
	}
}

func zipDirVariantType(dir string) string {
	switch dir {
	case zipDirThumb:
		return mediamodel.VariantTypeThumb
	case zipDirSmall:
		return mediamodel.VariantTypeSmall
	case zipDirMedium:
		return mediamodel.VariantTypeMedium
	case zipDirFull:
		return mediamodel.VariantTypeFull
	default:
		return ""
	}
}

func (s *Service) appendAttachmentEntries(ctx context.Context, plan *mediadto.DownloadPlan, att *mediamodel.AttachmentEntity, prefix, lang string) error {
	if att.StorageType != "local" {
		return mediaenums.Err(mediaenums.ErrDownloadStorageNotLocal)
	}

	join := func(parts ...string) string {

		nonEmpty := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				nonEmpty = append(nonEmpty, part)
			}
		}
		return strings.Join(nonEmpty, "/")
	}

	origName := sanitizeZipName(att.FileName)
	if srcPath, err := localObjectPath(attachmentStorageKey(att)); err != nil {
		plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
			Name:    join(prefix, zipDirOriginal, "README.txt"),
			Content: originalReadmeText(originalReadmePathInvalid, lang),
		})
	} else if _, statErr := os.Stat(srcPath); statErr != nil {
		plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
			Name:    join(prefix, zipDirOriginal, "README.txt"),
			Content: originalReadmeText(originalReadmeMissing, lang),
		})
	} else {
		plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
			Name: join(prefix, zipDirOriginal, origName),
			Path: srcPath,
		})
	}

	variants, err := s.vm.ListByAttachment(ctx, att.ID)
	if err != nil {
		return err
	}
	byType := make(map[string]mediamodel.MediaVariantEntity, len(variants))
	for _, v := range variants {
		byType[v.VariantType] = v
	}
	for _, vt := range mediamodel.VariantTypes() {
		dir := variantTypeToZipDir(vt)
		v, ok := byType[vt]
		if !ok || v.Status != mediamodel.VariantStatusReady {
			status := ""
			if ok {
				status = v.Status
			}
			plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
				Name:    join(prefix, dir, "README.txt"),
				Content: buildVariantReadme(dir, status, lang),
			})
			continue
		}
		absPath, perr := localObjectPath(v.FilePath)
		if perr != nil {
			plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
				Name:    join(prefix, dir, "README.txt"),
				Content: buildVariantReadme(dir, mediamodel.VariantStatusFailed, lang),
			})
			continue
		}
		plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
			Name: join(prefix, dir, sanitizeZipName(filepath.Base(filepath.ToSlash(v.FilePath)))),
			Path: absPath,
		})
	}
	return nil
}

func mediaDownloadLang(langs []string) string {
	if len(langs) > 0 {
		return strings.TrimSpace(langs[0])
	}
	return ""
}

func (s *Service) BuildDownloadPlan(ctx context.Context, attachmentID uint64, langs ...string) (plan *mediadto.DownloadPlan, err error) {
	lang := mediaDownloadLang(langs)
	att, err := s.am.GetByID(ctx, attachmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, mediaenums.Err(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	plan = &mediadto.DownloadPlan{
		FileName: sanitizeZipName(strings.TrimSuffix(att.FileName, filepath.Ext(att.FileName))) + "_package.zip",
		Entries:  []mediadto.DownloadEntry{},
	}
	if err := s.appendAttachmentEntries(ctx, plan, att, "", lang); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *Service) BuildBatchDownloadPlan(ctx context.Context, ids []uint64, langs ...string) (plan *mediadto.DownloadPlan, err error) {
	lang := mediaDownloadLang(langs)
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return nil, mediaenums.Err(mediaenums.ErrDownloadEmpty)
	}
	plan = &mediadto.DownloadPlan{
		FileName: "media_export_" + time.Now().Format("20060102") + ".zip",
		Entries:  []mediadto.DownloadEntry{},
	}
	for _, id := range ids {
		att, ferr := s.am.GetByID(ctx, id)
		if ferr != nil {
			if errors.Is(ferr, gorm.ErrRecordNotFound) {

				continue
			}
			return nil, ferr
		}
		stem := strings.TrimSuffix(filepath.Base(filepath.ToSlash(att.FileName)), filepath.Ext(att.FileName))
		prefix := sanitizeZipName(fmt.Sprintf("%s_%d", stem, att.ID))
		if aerr := s.appendAttachmentEntries(ctx, plan, att, prefix, lang); aerr != nil {
			return nil, aerr
		}
	}
	if len(plan.Entries) == 0 {
		return nil, mediaenums.Err(mediaenums.ErrDownloadEmpty)
	}
	return plan, nil
}

func normalizeIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *Service) References(ctx context.Context, id uint64) (res []mediadto.AttachmentRefResp, err error) {
	if _, err := s.am.GetByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, mediaenums.Err(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	refs, err := s.am.ListRefs(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]mediadto.AttachmentRefResp, 0, len(refs))
	for _, r := range refs {
		out = append(out, mediadto.AttachmentRefResp{Kind: r.Kind, ID: r.ID, Title: r.Title})
	}
	return out, nil
}

func (s *Service) SyncReferences(ctx context.Context, req *mediacontract.SyncRefsInput) (n int, err error) {
	if req == nil {
		return 0, mediaenums.Err(mediaenums.MsgBadRequest)
	}
	kind := strings.TrimSpace(req.RefKind)
	refID := strings.TrimSpace(req.RefID)
	if kind == "" || refID == "" {
		return 0, mediaenums.Err(mediaenums.MsgBadRequest)
	}

	target := make(map[uint64]struct{}, len(req.URLs))
	for _, raw := range req.URLs {
		if id, ok := s.attachmentIDByURL(ctx, raw); ok {
			target[id] = struct{}{}
		}
	}

	current, err := s.am.ListIDsByRef(ctx, kind, refID)
	if err != nil {
		return 0, err
	}
	currentSet := make(map[uint64]struct{}, len(current))
	for _, id := range current {
		currentSet[id] = struct{}{}
	}

	ref := mediamodel.AttachmentRef{Kind: kind, ID: refID, Title: strings.TrimSpace(req.RefTitle)}
	for id := range target {
		if _, ok := currentSet[id]; ok {
			continue
		}
		if _, aerr := s.am.AddRef(ctx, id, ref); aerr != nil {
			return n, aerr
		}
	}
	for _, id := range current {
		if _, ok := target[id]; ok {
			continue
		}
		if _, rerr := s.am.RemoveRef(ctx, id, kind, refID); rerr != nil {
			return n, rerr
		}
	}
	return len(target), nil
}

func (s *Service) attachmentIDByURL(ctx context.Context, mediaURL string) (uint64, bool) {
	raw := localMediaStorageKey(mediaURL)
	if raw == "" {
		return 0, false
	}
	if att, err := s.am.GetByFilePath(ctx, raw); err == nil && att != nil {
		return att.ID, true
	}
	v, err := s.vm.GetByFilePath(ctx, raw)
	if err != nil || v == nil {
		return 0, false
	}
	return v.AttachmentID, true
}

func localMediaStorageKey(raw string) string {
	raw = strings.TrimSpace(raw)
	base := upload.BaseURL()
	if !strings.HasPrefix(raw, "/storage/") && (base == "" || !strings.HasPrefix(raw, base+"/")) {
		return ""
	}
	return upload.StorageKey(raw)
}

func collectStorageURLs(html string) []string {
	const prefix = "/storage/"
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for {
		idx := strings.Index(html, prefix)
		if idx < 0 {
			break
		}
		start := idx
		for start > 0 && !mediaURLDelimiter(html[start-1]) {
			start--
		}
		end := idx
		for end < len(html) && !mediaURLDelimiter(html[end]) {
			end++
		}
		mediaURL := html[start:end]
		html = html[end:]
		if localMediaStorageKey(mediaURL) == "" {
			continue
		}
		if _, ok := seen[mediaURL]; ok {
			continue
		}
		seen[mediaURL] = struct{}{}
		out = append(out, mediaURL)
	}
	return out
}

func mediaURLDelimiter(c byte) bool {
	switch c {
	case '"', '\'', '(', ')', '<', '>', ' ', ',', '\n', '\r', '\t':
		return true
	}
	return false
}

func (s *Service) SyncReferencesFromHTML(ctx context.Context, refKind, refID, refTitle, html string) (int, error) {
	if strings.TrimSpace(refID) == "" || html == "" {
		return 0, nil
	}
	urls := collectStorageURLs(html)
	n, err := s.SyncReferences(ctx, &mediacontract.SyncRefsInput{
		RefKind: refKind, RefID: refID, RefTitle: refTitle, URLs: urls,
	})
	if err != nil {
		logger.Scene("build").With("ref_id", refID).Error(err, "媒体引用缓存同步失败")
		return n, err
	}
	return n, nil
}

const refKindPage = "page"

func summarizeRefs(refs []mediamodel.AttachmentRef) string {
	if len(refs) == 0 {
		return ""
	}
	allPage := true
	titles := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Kind != refKindPage {
			allPage = false
		}
		if r.Title != "" {
			titles = append(titles, r.Title)
		} else {
			titles = append(titles, r.Kind+":"+r.ID)
		}
	}
	head := fmt.Sprintf("%d 处引用", len(refs))
	if allPage {
		head = fmt.Sprintf("%d 个页面引用", len(refs))
	}
	if len(titles) > 3 {
		titles = append(titles[:3], "等")
	}
	return head + "（" + strings.Join(titles, "、") + "）"
}
