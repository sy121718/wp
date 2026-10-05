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

// 单一真源与对账口径（AGENTS.md「写操作的事务与回滚」的跨库分支）：
//
//   - **数据库是元数据真源**：一条 status=1 的 sys_attachment 行（file_path 指向磁盘）
//     就是「这个媒体存在」的唯一判据；访问面（/storage/<id>.<ext>）也由它推导。
//   - **文件系统是内容真源**：字节在磁盘上，DB 只存路径与校验值。
//   - 两者**不可能用一个事务覆盖** —— 文件系统没有回滚语义。所以这里采用
//     「两阶段 + 补偿」，并配套一个可跑的只读对账入口（media_reconcile.go 的
//     ReconcileStorage）把「有文件没记录」「有记录没文件」找出来**打回给人**。
//   - 补偿（compensateDelete）只在**同一进程**内尽力而为；进程在下述任一步之间退出，
//     留下的半成品由对账入口发现，**绝不自动删除**（自动删会把「可能只是没扫到」
//     直接变成数据丢失）。
//
// 两阶段的切分点固定在「主键可见」这一件事上：对象 key 由主键推导（<id>.<ext>），
// 所以草稿行必须先提交，落盘才可能发生。

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

// detectContentMIME 取「内容嗅探优先、声明值兜底」的 MIME，并把读取位置复位。
//
// 声明值不可信（见 Upload 内调用处注释）：判据应当是文件本身。嗅探不出具体类型
// （application/octet-stream）时才回落到声明值，那条路径不比原来更宽。
// 复位失败时直接回落声明值 —— 头部字节已被读走，后续 provider 写入会缺一段，
// 但那是上传本身的失败，不该在这里被掩成「MIME 判错」。
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

// Upload 上传文件并记录附件元数据（02-B 媒体中心：稳定引用 + 上传去重）。
//
// 流程（迁移 067 之后的语义）：
//  1. 流式算 md5（复用 multipart 的多次 Open，不改动 pkg/upload 的校验路径）；
//  2. 「md5 + file_type」命中启用中的附件 → 直接复用已有记录：不重复落盘、不重复入库，
//     响应带回原 URL 与变体信息（Duplicate=true 供前端提示）；
//  3. 未命中 → 两阶段登记：
//     第一段（独立提交）：插入草稿记录拿主键 ID（status=0，对外查询不可见）；
//     —— 这里是文件系统（跨库动作），不参与事务 ——
//     第二段（**同一个事务**）：用 ID 命名落盘 <id>.<ext>（URL 与文件内容解耦，
//     换图后 URL 不变）之后，回填路径/URL/大小并置为启用 **+ 登记变体记录**，
//     两者要么都成功要么都回滚；落盘或第二段失败即物理删除草稿记录，不留半成品。
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

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, err)
	}
	defer src.Close()

	// 落库的 MIME 以**内容**为准，与 pkg/upload 的校验同源：客户端声明的
	// Content-Type 不可信 —— curl 上传 .webp 给的是 application/octet-stream，
	// 照抄声明值会让媒体库、变体记录与下游都看到一个错的类型。
	mimeType := detectContentMIME(src, file.Header.Get("Content-Type"))

	// 两阶段登记：先入库拿 ID（草稿态 status=0，对外查询不可见），再用 ID 命名落盘。
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

	// 回填内存实体（避免再查一次库）：变体记录的 file_path 由它推导，必须在建记录之前。
	entity.FilePath = result.Key
	entity.StoragePath = &result.Key
	entity.URL = &result.URL
	entity.FileSize = result.Size
	entity.StorageType = result.Provider
	entity.Status = mediamodel.AttachmentStatusEnabled
	if entity.Generation == 0 {
		entity.Generation = 1
	}

	// 变体初始状态探测（只读文件系统）：放在事务**外**做，别让 os.Open / 解码
	// 这类慢动作占着连接与行锁。
	variantStatus := s.probeVariantStatus(ctx, entity)

	// 第二段：元数据回填 + 变体记录登记**同一个事务**。
	//
	// 为什么这两处必须同事务：只写其中一处就是半截状态 ——
	//   · 只回填元数据：附件已可见，但三个变体槽永远是空的（缩略图 404），
	//     而磁盘上没有任何东西提示「这里本该有变体记录」；
	//   · 只登记变体：草稿行（status=0）永远不可见，变体行却挂着，
	//     且附件 URL 为空 —— 一堆指向不存在 URL 的变体记录。
	// 事务内的两条写各自命中不同的表（sys_attachment / sys_media_variant），
	// 边界由 service 决定，句柄经 model 的 …Tx 方法往下传（AGENTS.md）。
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
		// 回滚已经发生（元数据与变体记录都没落库），补偿只负责清掉草稿行与磁盘文件。
		s.compensateDelete(ctx, entity.ID)
		return nil, fmt.Errorf("%s: %w", mediaenums.ErrUploadFailed, terr)
	}

	// 变体生成任务投递留在事务外：入队是队列侧（asynq/Redis）的写，跨系统动作，
	// 放进事务里会出现「任务先被 worker 取走、事务还没提交」的竞态
	//（worker 回库读不到附件 → 静默跳过 → 变体永远不生成）。生成入口本身幂等，
	// 所以「提交后再投递」失败也能由 media_reconcile.go 的重放入口补齐。
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

	resps := make([]mediato.AttachmentResp, 0, len(list))
	for _, e := range list {
		resps = append(resps, *entityToResp(&e))
	}
	// 变体状态批量填充（一次 IN 查询，避免 N+1；失败不影响列表主数据）。
	s.fillVariants(ctx, resps)

	res := &mediato.ListResp{Total: total, Page: req.GetPage(), Limit: limit, List: resps}
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
		return time.Time{}, 0, errors.New("媒体列表游标无效")
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 {
		return time.Time{}, 0, errors.New("媒体列表游标无效")
	}
	created, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, errors.New("媒体列表游标无效")
	}
	var id uint64
	if _, err = fmt.Sscan(parts[1], &id); err != nil || id == 0 {
		return time.Time{}, 0, errors.New("媒体列表游标无效")
	}
	return created, id, nil
}

// Detail 查询单个附件详情。
//
// sys_attachment 无 project_id 列（媒体库站点级共享），因此**不校验** projectId：
// 曾经把它当必填（「与多工程后台 API 口径一致」），但它既不参与查询也不参与过滤，
// 唯一效果是让没带这个参数的调用方（包括后台自己的页面）拿到一个 400 ——
// 看起来像「附件不存在 / 参数错了」，实际是要求了一个用不上的参数。
// dto 里保留该字段是为了将来真要做工程级隔离时不必改契约。
func (s *Service) Detail(ctx context.Context, req *mediato.DetailReq) (*mediato.AttachmentResp, error) {
	if req == nil || req.ID == 0 {
		return nil, errors.New(mediaenums.MsgBadRequest)
	}
	e, err := s.am.GetByID(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	resp := entityToResp(e)
	// 详情填充变体状态（thumb/medium/full 徽标数据源）。
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
	// projectId 同 Detail：媒体库不按工程隔离，故不校验（见上）。
	if req == nil || req.ID == 0 {
		return errors.New(mediaenums.MsgBadRequest)
	}
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
	// URL 在**读取时**按当前 upload.base_url 派生，不直接用库里那一列。
	//
	// 库里存的是上传当时的字符串：配了 base_url 才是绝对地址，没配就是
	// "/storage/<id>.<ext>"；换域名后存量行还指着旧主机。派生之后
	// 「配置一改、全站媒体链接同时跟上」，与变体走同一条路（见 variantEntityToResp）。
	// file_path 才是内容真源，url 列只作历史兜底（它的前缀形态已被 StorageURL 归一）。
	key := strings.TrimSpace(e.FilePath)
	if key == "" && e.URL != nil {
		key = *e.URL
	}
	url := upload.StorageURL(key)
	if url == "" && e.URL != nil {
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
