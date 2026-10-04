package blockservice

// block_crud.go — 全局块增删改查与响应装配。
// 写路径统一做字段归一化、引用保护与 stale 传播；入口只带 id 的一律先过工程作用域定位。

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
)

// List 列出工程全部块（kind/category/reuseMode 可选过滤）。
func (s *Service) List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 projectID）是调用方错误，与「工程下无块」/资源不存在区分开。
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrParamRequired
	}
	category := strings.TrimSpace(req.Category)
	if category != "" && !categoryPattern.MatchString(category) {
		return nil, ErrInvalidCategory
	}
	reuseMode, err := normalizeReuseMode(req.ReuseMode)
	if err != nil {
		return nil, err
	}
	entities, err := s.model.ListByProject(ctx, req.ProjectID, strings.TrimSpace(req.Kind), category, reuseMode)
	if err != nil {
		return nil, err
	}
	res = make([]blockdto.BlockResp, 0, len(entities))
	for i := range entities {
		res = append(res, blockResp(&entities[i]))
	}
	return res, nil
}

// Detail 按 ID 查询块。
//
// ProjectID 非空时按工程作用域直查（REST 契约入口）；为空表示**只带 id 的入口**，
// 逐工程探测归属（见 block_scope.go 的 locateBlockEntity）—— 工作台块编辑
// （/workbench?block=ID）、块预览与 /admin/blocks/save-content 三条路径的请求里
// 本来就没有工程参数。
//
// 这里曾经一律要求 ProjectID，于是那三条路径全部拿到 ErrParamRequired，调用方再把它
// 折叠成 404「全局块不存在」：块在列表页看得见（List 带工程参数，是好的），点「编辑」或
// 新建后的跳转却永远打不开编辑器 —— 表现为「块明明在，却报不存在」。
func (s *Service) Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrParamRequired
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		entity, lerr := s.getExistingBlock(ctx, req.ID)
		if lerr != nil {
			return nil, lerr
		}
		return blockRespPtr(entity), nil
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	entity, err := s.model.GetByID(ctx, strings.TrimSpace(req.ID), req.ProjectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return blockRespPtr(entity), nil
}

// Create 新建块（同工程名称唯一；文档走页面文档同构校验）。
func (s *Service) Create(ctx context.Context, req *blockdto.CreateReq) (res *blockdto.BlockResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, ErrNameRequired
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	kind, err := normalizeKind(req.Kind)
	if err != nil {
		return nil, err
	}
	category, err := normalizeCategory(req.Category)
	if err != nil {
		return nil, err
	}
	reuseMode, err := normalizeReuseMode(req.ReuseMode)
	if err != nil {
		return nil, err
	}
	document, err := validateDocument(req.Document)
	if err != nil {
		return nil, err
	}
	// 判重：参数化单条查询（LOWER(name)），避免拉全量 Document(jsonb) 后内存 EqualFold。
	// 并发下同名仍可能穿透（需 DB 唯一索引兜底，见 model 层 ExistsByName 说明）。
	exists, err := s.model.ExistsByName(ctx, req.ProjectID, strings.TrimSpace(req.Name))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDuplicate
	}
	now := time.Now().UTC()
	// id 显式生成：blocks.id 是 uuid、DDL 带 DEFAULT gen_random_uuid()，但 gorm 对
	// string 主键的零值会**显式写入空串**（不像 int 那样交给 identity），
	// 落到 PG 就是 22P02 invalid input syntax for type uuid。与 project / page 的创建一致。
	entity := &blockmodel.BlockEntity{
		ID:        uuid.NewString(),
		ProjectID: req.ProjectID,
		Name:      strings.TrimSpace(req.Name), Kind: kind, Category: category, ReuseMode: reuseMode, Document: document,
		CreateTime: now, UpdatedAt: now,
	}
	if err = s.model.Create(ctx, entity); err != nil {
		return nil, err
	}
	return blockRespPtr(entity), nil
}

// Update 更新块（名称/类型/文档整树保存）。
func (s *Service) Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开（与 List/Detail 语义一致）。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrParamRequired
	}
	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	name := entity.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	kind := entity.Kind
	if strings.TrimSpace(req.Kind) != "" {
		if kind, err = normalizeKind(req.Kind); err != nil {
			return nil, err
		}
	}
	category := entity.Category
	if strings.TrimSpace(req.Category) != "" {
		if category, err = normalizeCategory(req.Category); err != nil {
			return nil, err
		}
	}
	reuseMode := entity.ReuseMode
	if strings.TrimSpace(req.ReuseMode) != "" {
		if reuseMode, err = normalizeReuseMode(req.ReuseMode); err != nil {
			return nil, err
		}
		// global→template 切换防御：仍被引用的 global 块一旦切成 template，
		// 引用页面将悬空（stale 不再传播），必须先解除引用。
		if entity.ReuseMode == blockmodel.ReuseGlobal && reuseMode == blockmodel.ReuseTemplate {
			if usages, inUse := s.blockRefState(ctx, entity.ID); inUse {
				return nil, blockcontract.NewBlockInUseError(usages)
			}
		}
	}
	document := entity.Document
	if len(req.Document) > 0 {
		if document, err = validateDocument(req.Document); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	// 写路径带工程作用域（DB-009 第二批）：entity 是上面定位到的块，自带 ProjectID。
	if err = s.model.UpdateDocument(ctx, entity.ProjectID, entity.ID, name, kind, category, reuseMode, document, now); err != nil {
		return nil, err
	}
	entity.Name, entity.Kind, entity.Category, entity.ReuseMode, entity.Document, entity.UpdatedAt = name, kind, category, reuseMode, document, now
	s.propagateStale(ctx, entity.ID, entity.ReuseMode)
	return blockRespPtr(entity), nil
}

// Delete 删除块。docs/02-D §9：
//   - reuse_mode=template：页面已持有独立副本，删除无副作用，不传播；
//   - reuse_mode=global：仍被页面（globalref/structure）或主题槽位引用时默认拒绝（ErrBlockInUse），
//     Force=true 强制删除并传播（引用页面退化为无该块，下次构建 globalref 降级占位）。
func (s *Service) Delete(ctx context.Context, req *blockdto.DeleteReq) (err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开（与 List/Detail 语义一致）。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrParamRequired
	}
	// 先确认存在：避免 model.Delete RowsAffected=0 静默成功，
	// 与 Detail/Update 的「不存在 → ErrBlockNotFound」语义保持一致。
	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity.ReuseMode == blockmodel.ReuseGlobal && !req.Force {
		if usages, inUse := s.blockRefState(ctx, entity.ID); inUse {
			// 明细随错误返回：拒绝必须可定位（哪一类引用、哪些实体），
			// 日志与页面提示共用同一份清单，不允许各拼一套。
			return blockcontract.NewBlockInUseError(usages)
		}
	}
	if err = s.model.Delete(ctx, entity.ProjectID, entity.ID); err != nil {
		return err
	}
	s.propagateStale(ctx, req.ID, entity.ReuseMode) // req.ID 为 dto 字符串形式，与引用检查口径一致
	return nil
}

// getExistingBlock 按块 id 定位块（请求只带 id 的入口：更新 / 删除 / 复制 AST）。
//
// blocks 带 FORCE 策略（迁移 215），作用域只能落到具体工程，所以这里**逐工程探测出归属**
// 再返回（见 block_scope.go）：原先的「不限工程」直查在换非超级角色后是静默的
// ErrRecordNotFound —— 表现为「块明明在，却报不存在」，且没有任何错误日志。
func (s *Service) getExistingBlock(ctx context.Context, id string) (e *blockmodel.BlockEntity, err error) {
	// 非法形状的 id 直接判「不存在」，别让它落到 PG 上：blocks.id 是 uuid 列
	// （迁移 209 把 201 的 bigint 改回来了 —— 它对外有接口、且 blockId 写进页面文档，
	// 按主键选型判据属于「对外实体」），非法输入会让 PG 报 22P02 并冒成 500。
	//
	// 判「不存在」而不是「参数错误」：这三个调用方对这两种情况的处理本来就一样，
	// 多一个 400 分支只会逼每条调用路径判断两种错。
	trimmed := strings.TrimSpace(id)
	if _, perr := uuid.Parse(trimmed); perr != nil {
		return nil, ErrNotFound
	}
	return s.locateBlockEntity(ctx, trimmed)
}

func (s *Service) requireProject(ctx context.Context, projectID string) error {
	exists, err := s.projects.Exists(ctx, projectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	return nil
}

func blockResp(e *blockmodel.BlockEntity) blockdto.BlockResp {
	return blockdto.BlockResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Kind: e.Kind, Category: e.Category,
		ReuseMode: e.ReuseMode, Document: e.Document, CreatedAt: utils.NewJSONTime(e.CreateTime), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
}

func blockRespPtr(e *blockmodel.BlockEntity) *blockdto.BlockResp {
	r := blockResp(e)
	return &r
}
