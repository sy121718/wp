// Package pubmodel 实现 publication 模块 page_routes 与 publication_receipts
// 表持久化：URL 占用状态流转与发布回执记录。
package pubmodel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const (
	tableNamePageRoutes = "page_routes"
	tableNameReceipts   = "publication_receipts"
)

// 路由占用类型（docs/02-domain.md §page_routes）。
const (
	RouteReserved = "reserved"
	RouteActive   = "active"
	RouteRedirect = "redirect"
)

// 回执状态（docs/03-pipeline.md §9 publication_receipts）。
const (
	ReceiptPending    = "pending"
	ReceiptCommitted  = "committed"
	ReceiptRolledBack = "rolled_back"
)

// RouteEntity 对应 page_routes 表。
type RouteEntity struct {
	ProjectID      string    `gorm:"column:project_id;primaryKey"`
	Path           string    `gorm:"column:path;primaryKey"`
	PageID         *string   `gorm:"column:page_id"`
	PresentationID *string   `gorm:"column:presentation_id"`
	RouteKind      string    `gorm:"column:route_kind;not null"`
	ArtifactID     *string   `gorm:"column:artifact_id"`
	UpdatedAt      time.Time `gorm:"column:update_time;not null"`
}

func (RouteEntity) TableName() string { return tableNamePageRoutes }

// ReceiptEntity 对应 publication_receipts 表：发布回执（故障恢复依据）。
type ReceiptEntity struct {
	ID           int64           `gorm:"column:id;primaryKey"`
	SourceType   string          `gorm:"column:source_type;not null"`
	SourceID     string          `gorm:"column:source_id;not null"`
	Action       string          `gorm:"column:action;not null"`
	Path         string          `gorm:"column:path;not null"`
	FromArtifact *string         `gorm:"column:from_artifact_id"`
	ToArtifact   *string         `gorm:"column:to_artifact_id"`
	ReceiptState string          `gorm:"column:receipt_state;not null"`
	ReceiptData  json.RawMessage `gorm:"column:receipt_data;type:jsonb;not null"`
	CreateTime   time.Time       `gorm:"column:create_time;not null"`
	CompletedAt  *time.Time      `gorm:"column:completed_at"`
}

func (ReceiptEntity) TableName() string { return tableNameReceipts }

// Model 封装 publication 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewPublicationModel 创建 Publication Model。
func NewPublicationModel(db *gorm.DB) *Model { return &Model{db: db} }

// RouteDB 返回已绑定 page_routes 表的 GORM 实例。
func (m *Model) RouteDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RouteEntity{})
}

// ReceiptDB 返回已绑定 publication_receipts 表的 GORM 实例。
func (m *Model) ReceiptDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReceiptEntity{})
}

// Transaction 在数据库事务中执行给定函数；路由与回执必须原子提交。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// ListActivePaths 列出项目下全部已激活路由路径（升序，用于 sitemap 生成）。
func (m *Model) ListActivePaths(ctx context.Context, projectID string) (paths []string, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&RouteEntity{}).
			Where("project_id = ? AND route_kind = ?", projectID, RouteActive).
			Order("path ASC").
			Pluck("path", &paths).Error
	})
	return paths, err
}

// ListActiveRoutes 列出项目下全部已激活路由行（路径升序，含归属者与更新时间）。
//
// 与 ListActivePaths（只取路径）分开，而不是让调用方按路径再查一次：站点级 feed
// 需要行上的两样东西 —— 「哪条路径是内容详情页」（presentation 归属）与「该路径
// 最近一次激活时刻」（feed 的发布时间）。拆成两次查询，调用方还要按路径对回去，
// 白跑一趟数据库且多一处可能对不上的口径。
func (m *Model) ListActiveRoutes(ctx context.Context, projectID string) (routes []RouteEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&RouteEntity{}).
			Where("project_id = ? AND route_kind = ?", projectID, RouteActive).
			Order("path ASC").
			Find(&routes).Error
	})
	return routes, err
}

// GetRoute 按 (projectID, path) 查询路由占用。
func (m *Model) GetRoute(ctx context.Context, projectID, path string) (e *RouteEntity, err error) {
	e = &RouteEntity{}
	if err = m.RouteDB(ctx).Where("project_id = ? AND path = ?", projectID, path).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ListPendingReceipts 读取全部 pending 回执（启动全量恢复扫描）。
//
// 不限量、不加锁：调用方（RecoverPendingPublications）在启动时一次收干净，
// 多实例并发由每条回执结案语句的 receipt_state = 'pending' 守卫兜住。
// 定时 / 快通道收敛走 ClaimPendingReceipts（分批 + SKIP LOCKED），不要在这里加 LIMIT ——
// 两种口径混用会让「启动恢复」在不经意间退化成「只收一批」。
func (m *Model) ListPendingReceipts(ctx context.Context) (list []ReceiptEntity, err error) {
	err = m.ReceiptDB(ctx).Where("receipt_state = ?", ReceiptPending).Order("create_time ASC").Find(&list).Error
	return list, err
}

// pendingReceiptsQuery 在查询上叠加「未结案 + 归属 + 动作」三个条件（领取与统计共用）。
//
// 三个条件都是调用方给的参数：本表叠着多套恢复职责（见 ReceiptsQueryReq 注释），
// 「哪些行该我管」由调用方决定，model 不写死业务口径。
func (m *Model) pendingReceiptsQuery(ctx context.Context, sourceType string, actions []string) *gorm.DB {
	q := m.ReceiptDB(ctx).Where("receipt_state = ?", ReceiptPending)
	if s := strings.TrimSpace(sourceType); s != "" {
		q = q.Where("source_type = ?", s)
	}
	if len(actions) > 0 {
		q = q.Where("action IN ?", actions)
	}
	return q
}

// ClaimPendingReceipts 领取一批待收敛的未结案回执（多实例安全）。
//
// 四个要点，少一个都会出错：
//   - FOR UPDATE SKIP LOCKED：同一瞬间只有一个实例能领到这批行，其余实例直接领下一批，
//     不排队、不重复（与 build_jobs 的取任务同一手法）；
//   - ORDER BY create_time ASC：先登记的先收敛 —— 残留越久越该先收，否则新残留会插队；
//   - LIMIT 由调用方给：一批一批来，不制造长事务，也不让一次收敛把启动链拖住；
//   - 覆盖索引：WHERE receipt_state = 'pending' + ORDER BY create_time 由迁移 267 的
//     部分索引 idx_publication_receipts_pending 直接服务，空转时是一次极廉价的索引扫描。
//
// 为什么锁不跨越重放（重放由调用方在领取事务之外做）：重放会经 page / publication
// 两个服务各写各的表，其中「回执结案」正是对本表这一行的 UPDATE —— 在外层事务里持锁
// 重放，那条 UPDATE 会从连接池取另一条连接、等自己持有的锁，直到死锁超时。
// 领取事务只回答「谁拿到哪一批」；DB 侧的真正互斥由结案语句的
// WHERE receipt_state = 'pending' 守卫给出（只有一个实例能改到行），
// 且重放的每一步都是幂等的（upsert / 内容寻址 / 按归属重复激活），
// 因此并发重叠只会浪费一次重放，不会写出错误状态。
func (m *Model) ClaimPendingReceipts(ctx context.Context, sourceType string, actions []string, limit int) (list []ReceiptEntity, err error) {
	if limit <= 0 {
		return nil, nil
	}
	list = make([]ReceiptEntity, 0, limit)
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&ReceiptEntity{}).Where("receipt_state = ?", ReceiptPending)
		if s := strings.TrimSpace(sourceType); s != "" {
			q = q.Where("source_type = ?", s)
		}
		if len(actions) > 0 {
			q = q.Where("action IN ?", actions)
		}
		return q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Order("create_time ASC").Limit(limit).Find(&list).Error
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

// CountPendingReceipts 统计仍未结案的回执数（可观测：健康检查 / 后台）。
//
// 与领取用同一套过滤条件，两者看到的必须是同一批行 —— 口径一旦分叉，
// 会出现「健康检查报 0 而实际有残留」这种最难查的一类不一致。
func (m *Model) CountPendingReceipts(ctx context.Context, sourceType string, actions []string) (n int64, err error) {
	err = m.pendingReceiptsQuery(ctx, sourceType, actions).Count(&n).Error
	return n, err
}

// ListReferencedArtifactIDs 返回全部被路由引用的产物行 ID（GC 保护集合）。
//
// page_routes.artifact_id 是访问面的实际指向：该产物文件被删除意味着线上直接 404，
// 且路由行本身不会因文件消失而失效 —— 必须显式纳入保护。
func (m *Model) ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error) {
	ids = []string{}
	err = m.RouteDB(ctx).Where("artifact_id IS NOT NULL").
		Distinct().Pluck("artifact_id", &ids).Error
	return ids, err
}
