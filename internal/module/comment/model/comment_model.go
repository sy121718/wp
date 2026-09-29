// Package commentmodel — comment 模块的表访问单元（Repository，不是 DDD Domain Model）。
//
// 一张表：comments（带 project_id，受 RLS FORCE 策略约束，策略由迁移 466 同批铺）。
//
// 每个方法都在 rls.InProjectScope / TransactionScoped 内执行：**依赖策略而不是
// 「调用方记得加 Where」** —— 漏加 project_id 条件的形态是**静默可见别人的评论**
// （不是报错），这正是策略存在的理由。按 id 的审核写入同样要在作用域内：
// 策略是行级的，不设 app.project_id 时 `WHERE id = ?` 也一行都动不了。
//
// 本文件不含任何业务规则：状态机（谁能把 pending 改成什么）、归属校验（这条评论是不是
// 这个人的）、限流与防刷全在 service。model 只做「按给定条件读写本模块的表」。
package commentmodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/pkg/rls"
)

// Entity comments 表的一行。
//
// 不声明列型（internal/architecture/model_gorm_tag_test.go 守门）：全部是 gorm 能推断的
// 基础类型与 time.Time，没有 json.RawMessage 那类需要显式 type: 的列。
type Entity struct {
	// ID 纯内部流水主键（不对外暴露；见迁移 466 的主键选型说明）。
	ID int64 `gorm:"column:id;primaryKey"`
	// ProjectID 工程归属（RLS 策略的作用域列）。
	ProjectID string `gorm:"column:project_id"`
	// EntityType 被评论实体的类型（白名单由拥有该实体的模块声明，校验在 service）。
	EntityType string `gorm:"column:entity_type"`
	// EntityID 被评论实体的 id（字符串：商品是 uuid、将来可能有大整数）。
	EntityID string `gorm:"column:entity_id"`
	// UserID 评论者 = 访客账号 id（users.id）。
	UserID uint64 `gorm:"column:user_id"`
	// ParentID 回复目标（顶层评论 id）；nil = 顶层评论。
	ParentID *int64 `gorm:"column:parent_id"`
	// Body 评论正文（纯文本）。
	Body string `gorm:"column:body"`
	// Status 审核状态（commentenums.Status*）。
	Status string `gorm:"column:status"`
	// AuthorIPHash 来源 IP 的带盐哈希（不存明文）；空串 = 拿不到 IP。
	AuthorIPHash string `gorm:"column:author_ip_hash"`
	// ReviewedBy 审核人（sys_admin.id）；未审核为 nil。
	ReviewedBy *uint64 `gorm:"column:reviewed_by"`
	// ReviewedAt 审核时刻；未审核为 nil。
	ReviewedAt *time.Time `gorm:"column:reviewed_at"`

	CreateTime time.Time  `gorm:"column:create_time"`
	UpdateTime time.Time  `gorm:"column:update_time"`
	DeletedAt  *time.Time `gorm:"column:deleted_at"`
}

// TableName 显式绑定表名（不靠 gorm 的复数化推断）。
func (Entity) TableName() string { return "comments" }

// Model comment 模块的表访问单元。
type Model struct {
	db *gorm.DB
}

// NewModel 构造（db 为整个模块共用的连接）。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 评论表句柄（内部实现细节，只允许被本 model 的仓储方法消费 —— service 不得调用）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

// Transaction 透传事务：跨聚合 / 跨模块编排由 service 决定边界（AGENTS.md §写操作的事务与回滚）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// TransactionScoped 开启事务**并设置工程作用域**（形态与 membership / page 的同名方法一致）。
//
// 为什么不分开暴露：作用域的设置与事务边界是同一件事（set_config(..., is_local => true)
// 只在事务内有效）。让调用方自己「开事务 + 记得调 rls.ScopeTx」的组合，迟早会出现
// 「开了事务忘了设作用域」—— 那种写法在非超级角色下每条语句匹配 0 行**且不报错**。
func (m *Model) TransactionScoped(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Insert 插入一条评论（自足事务 + 工程作用域），回填自增 id。
func (m *Model) Insert(ctx context.Context, e *Entity) error {
	if e == nil {
		return gorm.ErrInvalidData
	}
	return m.TransactionScoped(ctx, e.ProjectID, func(tx *gorm.DB) error {
		return m.InsertTx(ctx, tx, e)
	})
}

// InsertTx 在调用方事务内插入一条评论。
//
// 为什么要 Tx 变体：提交评论在断言上是「一次写入」，但事务边界归 service
// （AGENTS.md：model 暴露 Transaction 透传、由 service 决定边界）—— 将来若加上
// 「评论计数」「审核流水」，追加的写会落在同一个事务里，不必改这一层的形态。
func (m *Model) InsertTx(ctx context.Context, tx *gorm.DB, e *Entity) error {
	if e == nil || tx == nil {
		return gorm.ErrInvalidData
	}
	now := time.Now()
	if e.CreateTime.IsZero() {
		e.CreateTime = now
	}
	if e.UpdateTime.IsZero() {
		e.UpdateTime = now
	}
	if strings.TrimSpace(e.Status) == "" {
		// 兜底：service 之外的调用点（测试 / 将来的导入）也必须落到「先审后发」的默认态。
		e.Status = commentenums.StatusPending
	}
	return tx.WithContext(ctx).Model(&Entity{}).Create(e).Error
}

// ListTopLevel 按 (工程, 实体类型, 实体 id) 列出**已通过**的顶层评论（时间倒序 + 分页）。
//
// 只出 approved：pending / rejected / spam 不进任何公开面（先审后发口径，
// 见 enums 的模块注释）。调用方（service）拿到的 SQL 里没有可配置的状态参数 ——
// 「公开列表只看已通过」不应该是调用方记得传的东西。
func (m *Model) ListTopLevel(ctx context.Context, projectID, entityType, entityID string, limit, offset int) (list []*Entity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		list, ierr = m.ListTopLevelTx(ctx, tx, projectID, entityType, entityID, limit, offset)
		return ierr
	})
	return list, err
}

// ListTopLevelTx 在调用方事务内列出已通过的顶层评论。
func (m *Model) ListTopLevelTx(ctx context.Context, tx *gorm.DB, projectID, entityType, entityID string, limit, offset int) (list []*Entity, err error) {
	err = tx.WithContext(ctx).Model(&Entity{}).
		Where("project_id = ? AND entity_type = ? AND entity_id = ? AND status = ? AND parent_id IS NULL AND deleted_at IS NULL",
			projectID, entityType, entityID, commentenums.StatusApproved).
		Order("create_time DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&list).Error
	return list, err
}

// CountTopLevel 已通过的顶层评论总数（分页用）。
func (m *Model) CountTopLevel(ctx context.Context, projectID, entityType, entityID string) (total int64, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&Entity{}).
			Where("project_id = ? AND entity_type = ? AND entity_id = ? AND status = ? AND parent_id IS NULL AND deleted_at IS NULL",
				projectID, entityType, entityID, commentenums.StatusApproved).
			Count(&total).Error
	})
	return total, err
}

// ListRepliesOf 列出这些顶层评论下**已通过**的回复（时间正序：先回应的在前）。
//
// 空 parentIDs 直接返回空切片、不查库：`IN ()` 在 PG 里是语法错误，
// 而「这一页顶层评论都没有回复」是完全正常的形态（新站点几乎必然遇到）。
func (m *Model) ListRepliesOf(ctx context.Context, projectID, entityType, entityID string, parentIDs []int64) (list []*Entity, err error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&Entity{}).
			Where("project_id = ? AND entity_type = ? AND entity_id = ? AND status = ? AND parent_id IN ? AND deleted_at IS NULL",
				projectID, entityType, entityID, commentenums.StatusApproved, parentIDs).
			Order("create_time ASC, id ASC").
			Find(&list).Error
	})
	return list, err
}

// GetApproved 取一条**已通过**的评论（回复目标校验用：回复只能挂在本实体下已通过/待审的评论上）。
//
// 返回 (nil, nil) 表示不存在：调用方据此拒绝，而不是把它当内部错误。
func (m *Model) GetByID(ctx context.Context, projectID string, id int64) (e *Entity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var row Entity
		ferr := tx.WithContext(ctx).Model(&Entity{}).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", id, projectID).
			First(&row).Error
		if ferr == gorm.ErrRecordNotFound {
			return nil
		}
		if ferr != nil {
			return ferr
		}
		e = &row
		return nil
	})
	return e, err
}

// ReviewQuery 后台审核队列的查询条件（条件一律以参数传入，方法内不写死业务条件）。
type ReviewQuery struct {
	// ProjectID 工程（必填：评论按工程隔离）。
	ProjectID string
	// Statuses 状态筛选；空 = 全部状态。
	Statuses []string
	// EntityType 实体类型筛选；空 = 全部类型。
	EntityType string
	// Keyword 正文关键词（模糊匹配，LIKE 元字符已转义）；空 = 不过滤。
	Keyword string
	// Limit / Offset 分页。
	Limit  int
	Offset int
}

// ListForReview 后台审核队列（按条件筛选 + 分页），返回本页行与符合条件的总数。
//
// 排序：**待审优先**（pending 排在最前，运营打开页面即是待办），其余按时间倒序 ——
// 这比「一律按时间倒序」更接近「审核队列」这个语义（时间倒序会让新提交的待审
// 沉到已通过的行下面）。排序键由 SQL 的 CASE 固定，不来自请求方。
func (m *Model) ListForReview(ctx context.Context, q ReviewQuery) (list []*Entity, total int64, err error) {
	err = m.TransactionScoped(ctx, q.ProjectID, func(tx *gorm.DB) error {
		base := func() *gorm.DB {
			db := tx.WithContext(ctx).Model(&Entity{}).
				Where("project_id = ? AND deleted_at IS NULL", q.ProjectID)
			if len(q.Statuses) > 0 {
				db = db.Where("status IN ?", q.Statuses)
			}
			if q.EntityType != "" {
				db = db.Where("entity_type = ?", q.EntityType)
			}
			if kw := strings.TrimSpace(q.Keyword); kw != "" {
				// ILIKE 的 % / _ / \ 必须转义：不转义时用户搜 "100%" 会匹配到任意以 100 开头的正文
				//（模糊匹配被放大成「前缀匹配」，而页面看起来完全正常）。
				db = db.Where("body ILIKE ? ESCAPE '\\'", "%"+escapeLike(kw)+"%")
			}
			return db
		}
		if cerr := base().Count(&total).Error; cerr != nil {
			return cerr
		}
		return base().
			Order("CASE WHEN status = '" + commentenums.StatusPending + "' THEN 0 ELSE 1 END ASC, create_time DESC, id DESC").
			Limit(q.Limit).Offset(q.Offset).
			Find(&list).Error
	})
	return list, total, err
}

// UpdateStatus 批量更新审核状态（单条 UPDATE：原子，无需逐行事务）。
//
// 状态值与审核人由 service 给定的**白名单值**传入（本层不判断合法性 —— 那是业务规则）。
// 只影响 id 在给定集合内、且属于该工程的行（RLS 的 WITH CHECK 也会再挡一次）。
// 返回受影响行数：调用方据此回带「成功 N 条」。
func (m *Model) UpdateStatus(ctx context.Context, projectID string, ids []int64, status string, reviewerID uint64) (affected int64, err error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now()
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		res := tx.WithContext(ctx).Model(&Entity{}).
			Where("id IN ? AND project_id = ? AND deleted_at IS NULL", ids, projectID).
			Updates(map[string]any{
				"status":      status,
				"reviewed_by": reviewerID,
				"reviewed_at": now,
				"update_time": now,
			})
		affected = res.RowsAffected
		return res.Error
	})
	return affected, err
}

// CountBySourceSince 统计某来源（IP 哈希）在给定时刻之后提交的条数。
//
// **不是限流本身**（限流是进程内的令牌桶，见 service）：这是「同一来源短期灌了多少条」的
// 事后取证口径，供限流触发时打日志用 —— 只有进程内计数的话，日志里说不出「他在多久内发了多少」。
func (m *Model) CountBySourceSince(ctx context.Context, projectID, authorIPHash string, since time.Time) (n int64, err error) {
	if strings.TrimSpace(authorIPHash) == "" {
		return 0, nil
	}
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&Entity{}).
			Where("project_id = ? AND author_ip_hash = ? AND create_time >= ?", projectID, authorIPHash, since).
			Count(&n).Error
	})
	return n, err
}

// escapeLike 转义 LIKE / ILIKE 的元字符（反斜杠最先处理，否则会把后面加的反斜杠再转义一次）。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}
