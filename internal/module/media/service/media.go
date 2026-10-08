package mediaservice

// 级联策略（对标 WP 分类删除语义）：有子级拒绝删除；有附件时附件移入未分类后删除。

// 保持 /storage/<id>.<ext> 不变、内容整体替换、generation +1。
//
// 为什么必须保持扩展名：URL 由 <id>.<ext> 推导，换扩展名等于换 URL，
// 页面里的引用即刻失效——与「稳定引用」目标冲突。需要换格式时先删后传（新附件新 ID）。
//
// 原子性：先经 pkg/upload 完整校验链落盘到临时 key（魔数嗅探 / 大小限制 / O_EXCL），
// 再同设备 rename 覆盖目标路径——访客不会读到半个文件，也没有 404 窗口。

//   单图 GET /api/media/download?id=1        → <原文件名>_package.zip
//   批量 GET /api/media/download/batch?ids=1,2,3 → media_export_<日期>.zip
//   zip 内固定目录：original/（恒有原图）与四个变体目录 thumb/、small/、medium/、full/；
//   变体未 ready 的目录放 README.txt 说明生成状态。
//   service 只产出打包计划（zip 条目名 + 本地路径），handler 据此流式写响应，不落盘临时文件。

// 写入时机：**构建期**（pipeline 构建产物落地前），不是每次编辑。
// 理由：
//   1. 引用关系是「产物事实」——页面文档里写的 URL 未必都进产物（条件渲染、块内联、
//      CMS 集合展开），只有编译后的 HTML 才是权威引用集；
//   2. 编辑期高频写库会与后台元数据更新（alt/title）互相放大写放大，且草稿态引用
//      在未发布时不应阻止删除；
//   3. 构建期天然幂等：同一文档重复构建写入同一集合（差集为空，零写入）。
// 落点：page 模块 assembleCompile → media.SyncReferences（见 page_media_refs.go）。
//
// 读取侧：删除前查 refs（ListRefs），非空即拒绝；详情页可展示引用来源。

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/media/contract"
	"go_wp/internal/module/media/dto"
	"go_wp/internal/module/media/enums"
	"go_wp/internal/module/media/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
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
func (s *Service) Upload(ctx context.Context, file *multipart.FileHeader, categoryID *uint64) (*mediadto.AttachmentResp, error) {
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
		list := []mediadto.AttachmentResp{*resp}
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
	// 变体状态批量填充（一次 IN 查询，避免 N+1；失败不影响列表主数据）。
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
func (s *Service) Detail(ctx context.Context, req *mediadto.DetailReq) (*mediadto.AttachmentResp, error) {
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
	list := []mediadto.AttachmentResp{*resp}
	s.fillVariants(ctx, list)
	return &list[0], nil
}

// Delete 删除附件（软删除元数据）。
//
// 引用保护（02-B 第 4 能力）：删除前查 extra_info.refs（构建期写入的引用缓存），
// 命中即拒绝并给出「被 N 个页面引用」提示——避免删掉线上页面正在用的图。
// 引用缓存为空（未被任何构建产物引用）时才允许软删。
func (s *Service) Delete(ctx context.Context, req *mediadto.DeleteReq) error {
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
func (s *Service) CategoryTree(ctx context.Context) ([]mediadto.CategoryTreeNode, error) {
	categories, err := s.cm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildCategoryTree(categories, 0), nil
}

// --- 辅助函数 ---

func entityToResp(e *mediamodel.AttachmentEntity) *mediadto.AttachmentResp {
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
	return &mediadto.AttachmentResp{
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

// --- 分类 CRUD ---

// CreateCategory 新建分类（同父级下重名拒绝；code 自动生成保证唯一索引）。
func (s *Service) CreateCategory(ctx context.Context, req *mediadto.CategoryCreateReq) (*mediadto.CategoryTreeNode, error) {
	name := strings.TrimSpace(req.CategoryName)
	if name == "" {
		return nil, errors.New("分类名称不能为空")
	}
	if req.ParentID > 0 {
		if _, err := s.cm.GetCategory(ctx, req.ParentID); err != nil {
			return nil, errors.New("父级分类不存在")
		}
	}
	siblings, err := s.cm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range siblings {
		if c.ParentID == req.ParentID && strings.EqualFold(c.CategoryName, name) {
			return nil, errors.New("同级分类下已存在同名分类")
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

// UpdateCategory 更新分类（改名 / 移动父级 / 排序；移动防环：新父级不能是自己或自己的后代）。
func (s *Service) UpdateCategory(ctx context.Context, req *mediadto.CategoryUpdateReq) error {
	current, err := s.cm.GetCategory(ctx, req.ID)
	if err != nil {
		return errors.New("分类不存在")
	}
	// 查重基准必须是**移动后**的父级：一次请求同时改名 + 移动时按旧父级查重会漏检
	// 新父级下的同名分类（无 DB 唯一约束兜底），破坏「同父级唯一名」约束。
	newParent := current.ParentID
	if req.ParentID != nil {
		newParent = *req.ParentID
	}
	updates := map[string]any{"update_time": time.Now()}
	if req.CategoryName != nil && strings.TrimSpace(*req.CategoryName) != "" {
		name := strings.TrimSpace(*req.CategoryName)
		// 改名同级查重（与 CreateCategory 约束一致，排除自身）。
		siblings, err := s.cm.ListAll(ctx)
		if err != nil {
			return err
		}
		for _, c := range siblings {
			if c.ID != req.ID && c.ParentID == newParent && strings.EqualFold(c.CategoryName, name) {
				return errors.New("同级分类下已存在同名分类")
			}
		}
		updates["category_name"] = name
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if req.ParentID != nil {
		if newParent == req.ID {
			return errors.New("父级不能是自己")
		}
		if newParent > 0 {
			if _, err := s.cm.GetCategory(ctx, newParent); err != nil {
				return errors.New("目标父级分类不存在")
			}
			// 防环：newParent 不得位于以自己为根的子树内。
			all, err := s.cm.ListAll(ctx)
			if err != nil {
				return err
			}
			if isDescendant(all, req.ID, newParent) {
				return errors.New("不能移动到自己的子分类下")
			}
		}
		updates["parent_id"] = newParent
	}
	return s.cm.UpdateCategory(ctx, req.ID, updates)
}

// isDescendant 判断 target 是否位于 root 的子树内（all 为全量启用分类）。
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

// DeleteCategory 删除分类：有子级拒绝；有附件时附件移入未分类（category_id=NULL）。
func (s *Service) DeleteCategory(ctx context.Context, req *mediadto.CategoryDeleteReq) error {
	if _, err := s.cm.GetCategory(ctx, req.ID); err != nil {
		return errors.New("分类不存在")
	}
	hasChildren, err := s.cm.HasChildren(ctx, req.ID)
	if err != nil {
		return err
	}
	if hasChildren {
		return errors.New("请先删除或移动该分类下的子分类")
	}
	if err := s.am.DetachAttachments(ctx, req.ID); err != nil {
		return err
	}
	return s.cm.DeleteCategory(ctx, req.ID)
}

// --- 附件元数据更新 ---

// UpdateAttachment 更新附件（文件名 / 分类 / alt / 标题 / 描述；alt 等存 ExtraInfo JSON）。
func (s *Service) UpdateAttachment(ctx context.Context, req *mediadto.AttachmentUpdateReq) error {
	// 存在性校验（GetByID 带 status=1 过滤）。ExtraInfo 合并在 model 内以 SQL 原子完成，
	// 不再依赖这里读到的快照 —— 快照会与构建期 refs 写入竞态并互相覆盖。
	if _, err := s.am.GetByID(ctx, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("附件不存在")
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
				return errors.New("目标分类不存在")
			}
			updates["category_id"] = *req.CategoryID
		} else {
			updates["category_id"] = nil // 移入未分类
		}
	}
	// ExtraInfo 合并（alt/title/description）：只动这三个键，**不整列覆盖**。
	// 同列的 refs 是构建期写入的引用缓存，整列读-改-写会把它回退成旧值，
	// 导致删除保护失效（在用中的媒体被误删）。合并走 model 的 SQL 级 jsonb 操作。
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

// Replace 换图：内容替换 + URL 不变 + generation +1（图片类自动重建变体）。
//
// 返回语义：
//   - 内容与现状 md5 相同：不改动任何字段，直接返回现状（幂等，不 +1）；
//     但仍重发一次失效通知（幂等分支注释里写了为什么）；
//   - 内容变化：落盘覆盖 → generation +1 → md5/大小/MIME 回填 → 图片类重建变体 →
//     **标记引用方待重建**（变体名带内容指纹，不通知引用方则新图永远不可见）；
//   - 扩展名与现有 file_path 不一致：拒绝（见文件头说明）。
func (s *Service) Replace(ctx context.Context, id uint64, file *multipart.FileHeader) (res *mediadto.AttachmentResp, err error) {
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
		list := []mediadto.AttachmentResp{*resp}
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
	list := []mediadto.AttachmentResp{*resp}
	s.fillVariants(ctx, list)
	return &list[0], nil
}

// zipDirOriginal 等四个 zip 内固定目录名。
const (
	zipDirOriginal = "original"
	zipDirFull     = "full"
	zipDirThumb    = "thumb"
	zipDirSmall    = "small"
	zipDirMedium   = "medium"
)

// sanitizeZipName 清洗 zip 内条目名：去掉路径分隔符与控制字符，防止 zip-slip / 目录逃逸，
// 清洗后为空时回退 "file"。
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

// variantTypeToZipDir 变体类型 → zip 目录名。
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

// zipDirVariantType zip 目录名 → 变体类型（README 文案用）。
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

// variantStatusText 变体状态文案（README 用，按产物语言取词）。
var variantStatusTexts = []struct{ Status, Key, Fallback string }{
	{mediamodel.VariantStatusPending, "admin.media.variantStatus.pending", "排队中（pending）"},
	{mediamodel.VariantStatusProcessing, "admin.media.variantStatus.processing", "生成中（processing）"},
	{mediamodel.VariantStatusFailed, "admin.media.variantStatus.failed", "生成失败（failed）"},
}

func variantStatusText(status, lang string) string {
	for _, s := range variantStatusTexts {
		if s.Status == status {
			return i18n.Translate(s.Key, s.Fallback, lang)
		}
	}
	return i18n.Translate(mediaenums.VariantStatusMissing, "无变体记录（missing）", lang)
}

// buildVariantReadme 生成变体目录的 README.txt 内容（该变体未 ready 时占位说明）。
//
// 文案按 lang 取词：README 会跟着 zip 落到用户机器上，是**导出产物**的一部分，
// 不该恒定是中文（词条缺失时回落代码里的中文原文）。
func buildVariantReadme(dirName, status, lang string) string {
	tr := func(key, fallback string) string { return i18n.Translate(key, fallback, lang) }
	var b strings.Builder
	b.WriteString(tr(mediaenums.PackageTitle, "go_wp 媒体资源包"))
	b.WriteString("\n====================\n")
	// 占位符是命名形态（{name}）：词条可被运营在后台改，裸 % 会让 Sprintf 输出乱码。
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageDir, "目录：{name}",
		map[string]string{"name": dirName}))
	b.WriteString("\n")
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageVariantType, "内容：{name} 变体",
		map[string]string{"name": zipDirVariantType(dirName)}))
	b.WriteString("\n")
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageStatus, "状态：{name}",
		map[string]string{"name": variantStatusText(status, lang)}))
	b.WriteString("\n")
	b.WriteString(tr(mediaenums.PackageVariantUnavailable, "说明：该变体文件当前不可用，原图见 original/ 目录。"))
	b.WriteString("\n")
	b.WriteString(tr(mediaenums.PackageRegenHint, "可在媒体库详情页点「重新生成变体」，生成完成后重新下载。"))
	b.WriteString("\n")
	return b.String()
}

// appendAttachmentEntries 把单个附件的五目录结构追加进 plan：
// prefix 为空表示单图包根目录，批量模式为 <stem>_<id>/ 子目录。
func (s *Service) appendAttachmentEntries(ctx context.Context, plan *mediadto.DownloadPlan, att *mediamodel.AttachmentEntity, prefix, lang string) error {
	if att.StorageType != "local" {
		return errors.New(mediaenums.ErrDownloadStorageNotLocal)
	}

	join := func(parts ...string) string {
		// 过滤空段：单图包 prefix 为空，避免拼出前导斜杠 "/original/..."。
		nonEmpty := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				nonEmpty = append(nonEmpty, part)
			}
		}
		return strings.Join(nonEmpty, "/")
	}

	// 1) original/：原图（恒有）。原文件缺失时降级为 README 说明，不阻断整包。
	origName := sanitizeZipName(att.FileName)
	if srcPath, err := localObjectPath(attachmentStorageKey(att)); err != nil {
		plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
			Name:    join(prefix, zipDirOriginal, "README.txt"),
			Content: i18n.Translate(mediaenums.PackageOriginalPathInvalid, "原图存储路径非法，无法打包。\n", lang),
		})
	} else if _, statErr := os.Stat(srcPath); statErr != nil {
		plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
			Name:    join(prefix, zipDirOriginal, "README.txt"),
			Content: i18n.Translate(mediaenums.PackageOriginalMissing, "原图物理文件已缺失，仅剩元数据。\n", lang),
		})
	} else {
		plan.Entries = append(plan.Entries, mediadto.DownloadEntry{
			Name: join(prefix, zipDirOriginal, origName),
			Path: srcPath,
		})
	}

	// 2) thumb/ small/ medium/ full/：ready 的变体直接打文件；未 ready 打 README 说明。
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

// mediaDownloadLang 取产物语言（可选变参）：空串 = 默认语言（i18n 内部按默认语言解析）。
func mediaDownloadLang(langs []string) string {
	if len(langs) > 0 {
		return strings.TrimSpace(langs[0])
	}
	return ""
}

// BuildDownloadPlan 构建单个附件的资源包打包计划（契约方法）。
func (s *Service) BuildDownloadPlan(ctx context.Context, attachmentID uint64, langs ...string) (plan *mediadto.DownloadPlan, err error) {
	lang := mediaDownloadLang(langs)
	att, err := s.am.GetByID(ctx, attachmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
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

// BuildBatchDownloadPlan 构建多个附件的资源包批量打包计划（契约方法）。
// 每图一个 <stem>_<id>/ 子文件夹（id 后缀防重名），子文件夹内同五目录。
func (s *Service) BuildBatchDownloadPlan(ctx context.Context, ids []uint64, langs ...string) (plan *mediadto.DownloadPlan, err error) {
	lang := mediaDownloadLang(langs)
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return nil, errors.New(mediaenums.ErrDownloadEmpty)
	}
	plan = &mediadto.DownloadPlan{
		FileName: "media_export_" + time.Now().Format("20060102") + ".zip",
		Entries:  []mediadto.DownloadEntry{},
	}
	for _, id := range ids {
		att, ferr := s.am.GetByID(ctx, id)
		if ferr != nil {
			if errors.Is(ferr, gorm.ErrRecordNotFound) {
				// 单个附件不存在跳过（软删除/已删除），不阻断整批。
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
		return nil, errors.New(mediaenums.ErrDownloadEmpty)
	}
	return plan, nil
}

// normalizeIDs 去重并保序清洗 id 集合（Query ids=1,2,3 解析后的规整）。
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

// References 查询附件的引用缓存（引用来源清单）。
func (s *Service) References(ctx context.Context, id uint64) (res []mediadto.AttachmentRefResp, err error) {
	if _, err := s.am.GetByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
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

// SyncReferences 构建期全量同步：把某引用方（页面/块）产物中的媒体 URL 集合
// 写进对应附件的 extra_info.refs，并解除该引用方不再引用的附件。
// 返回本次该引用方实际命中的附件数。差集增删，重复构建零写入（幂等）。
func (s *Service) SyncReferences(ctx context.Context, req *mediacontract.SyncRefsInput) (n int, err error) {
	if req == nil {
		return 0, errors.New(mediaenums.MsgBadRequest)
	}
	kind := strings.TrimSpace(req.RefKind)
	refID := strings.TrimSpace(req.RefID)
	if kind == "" || refID == "" {
		return 0, errors.New(mediaenums.MsgBadRequest)
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

// attachmentIDByURL 把产物中的媒体 URL 解析为附件 ID：
// 先按原图 file_path 反查，未命中再按变体 file_path 反查（页面可能直接引用 _thumb/_medium）。
// 非 /storage 前缀、路径含 ..、或查不到附件时返回 false（静默跳过，不影响构建）。
func (s *Service) attachmentIDByURL(ctx context.Context, url string) (uint64, bool) {
	const prefix = "/storage/"
	raw := strings.TrimSpace(url)
	if raw == "" {
		return 0, false
	}
	// 兼容绝对 URL（upload.base_url 配置了站点域名时的产物形态）。
	if idx := strings.Index(raw, prefix); idx >= 0 {
		raw = raw[idx+len(prefix):]
	} else if !strings.HasPrefix(raw, prefix) {
		return 0, false
	}
	// 去掉查询串与锚点（构建产物里可能带 ?v=xxx 之类的缓存参数）。
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" || strings.Contains(raw, "..") {
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

// collectStorageURLs 从 HTML 产物中提取媒体 URL（/storage/...）。
// 去重后返回；非媒体地址一律丢弃。用于构建期收集引用集合。
func collectStorageURLs(html string) []string {
	const prefix = "/storage/"
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for {
		idx := strings.Index(html, prefix)
		if idx < 0 {
			break
		}
		html = html[idx:]
		end := 0
		for end < len(html) {
			c := html[end]
			if c == '"' || c == '\'' || c == ')' || c == '<' || c == '>' || c == ' ' || c == '\n' || c == '\r' || c == '\t' {
				break
			}
			end++
		}
		url := html[:end]
		html = html[end:]
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		out = append(out, url)
	}
	return out
}

// SyncReferencesFromHTML 构建期入口：从产物 HTML 收集 /storage 引用并全量同步到 refs。
// 调用方（page 构建链）只需给出「引用方 + 产物字节」，URL 形态与反查逻辑留在媒体域内。
// 返回命中的附件数；失败返回 error 由调用方决定是否降级（构建主链不应被阻断）。
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

// refKindPage 页面引用方类型（与 media_reference 表的 ref_kind 取值口径一致）。
const refKindPage = "page"

// summarizeRefs 生成删除拦截提示：按引用方类型聚合计数。
// 全为页面时输出「N 个页面引用」，混合类型时输出「N 处引用」。
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
