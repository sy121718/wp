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

// ListRoutePathsByPage 返回页面已激活（active/redirect）的路径集合。
//
// 访问面（/site）直接服务 active 目录的文件系统状态：调用方清理页面时必须
// 按这些路径解除激活（删除符号链接），只删 DB 路由行不会让内容下线。
// 空结果返回空的非 nil 切片（调用方按 [] 序列化，不是 null）。
func (m *Model) ListRoutePathsByPage(ctx context.Context, projectID, pageID string) (paths []string, err error) {
	paths = []string{}
	if err = m.RouteDB(ctx).
		Where("project_id = ? AND page_id = ? AND route_kind IN ?",
			projectID, pageID, []string{RouteActive, RouteRedirect}).
		Pluck("path", &paths).Error; err != nil {
		return nil, err
	}
	return paths, nil
}

// ListRoutePathsByPresentation 返回展示实例已激活（active/redirect）的路径集合。
//
// 改过 URL 的实例有两条路径：新路径（active）与旧路径（redirect）。删除实例时
// 必须按这两条都解除访问面激活 —— 只删 DB 路由行会让旧路径的符号链接留在
// active 目录里继续 301，指向一个已经不存在的页面。
func (m *Model) ListRoutePathsByPresentation(ctx context.Context, projectID, presentationID string) (paths []string, err error) {
	paths = []string{}
	if err = m.RouteDB(ctx).
		Where("project_id = ? AND presentation_id = ? AND route_kind IN ?",
			projectID, presentationID, []string{RouteActive, RouteRedirect}).
		Pluck("path", &paths).Error; err != nil {
		return nil, err
	}
	return paths, nil
}

// DeleteRoutesByPresentation 清理展示实例全部路径占用（实例删除时释放）。
// 幂等（无占用时 RowsAffected=0 不报错）。
//
// 与 DeleteRoutesByPresentationTx 共用同一份删除条件（见
// publication_route_tx_model.go 的 deleteRoutesByPresentationScope）。
func (m *Model) DeleteRoutesByPresentation(ctx context.Context, projectID, presentationID string) error {
	return m.deleteRoutesByPresentationScope(m.db.WithContext(ctx), projectID, presentationID).
		Delete(&RouteEntity{}).Error
}

// IsPathOccupied 查询路径是否被其他实体占用（page_id 为空即展示实例占用，
// page_id 非 excludePageID 即他人页面占用），供页面创建 / 发布前预检。
//
// 三个排除条件都带默认值语义，由调用方原样传入（model 不写业务口径）：
//   - ExcludePageID 为空时**不能**加 uuid 比较条件：把空串当 uuid 传给 PG 会直接报
//     invalid input syntax for type uuid: ""（新建页预检不携带排除项，必踩此路径）；
//   - 展示实例改 URL 时排除自身：它的行 page_id 为 NULL，用 ExcludePageID
//     排除不掉自己，会把「自己占着旧路径」误判成冲突而无法改名。
func (m *Model) IsPathOccupied(ctx context.Context, projectID, path, excludePageID, excludePresentationID string) (occupied bool, err error) {
	var foreign int64
	q := m.RouteDB(ctx).Where("project_id = ? AND path = ?", projectID, path)
	if exclude := strings.TrimSpace(excludePageID); exclude != "" {
		q = q.Where("(page_id IS NULL OR page_id <> ?)", exclude)
	}
	if exclude := strings.TrimSpace(excludePresentationID); exclude != "" {
		q = q.Where("(presentation_id IS NULL OR presentation_id <> ?)", exclude)
	}
	if err = q.Count(&foreign).Error; err != nil {
		return false, err
	}
	return foreign > 0, nil
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

// —— 回执的写入口（非事务形态）——
//
// 与 publication_route_tx_model.go 的 CreateReceiptTx / MarkReceiptStateTx 成对：
// 事务形态由 service 透传外层句柄（发布链要它与路由写同生共死），本文件这三个是
// 独立登记 / 独立结案。两条路径都必须走 model 的具名方法 —— service 不得再拿
// ReceiptDB(ctx) 拼 .Create()/.Where()（AGENTS.md「model 层定位」）。
//
// 为什么不能只留事务形态：BeginPublishReceipt / RollbackReceipts 的语义恰恰是
// 「与后续步骤**不同生共死**」—— 回执先独立落库，后续步骤崩了才有据可查。

// CreateReceipt 写一条发布回执（pending），gorm 回填自增主键（调用方按 e.ID 返回回执 id）。
func (m *Model) CreateReceipt(ctx context.Context, e *ReceiptEntity) error {
	return m.ReceiptDB(ctx).Create(e).Error
}

// RollbackPendingReceipts 把全部 pending 回执标记为 rolled_back（启动恢复），返回处理行数。
//
// WHERE 只认 receipt_state = pending：已 committed / rolled_back 的行是既有结论，
// 恢复流程不得覆盖（进程重复启动时因此是幂等的）。
func (m *Model) RollbackPendingReceipts(ctx context.Context, now time.Time) (n int64, err error) {
	result := m.ReceiptDB(ctx).
		Where("receipt_state = ?", ReceiptPending).
		Updates(map[string]any{"receipt_state": ReceiptRolledBack, "completed_at": now})
	return result.RowsAffected, result.Error
}

// FinishPendingReceipt 把一条 pending 回执置为终态（committed / rolled_back）。
//
// WHERE 里的 receipt_state = pending 是**幂等守卫**：重复调用（重试）与并发结案都不会
// 覆盖已定稿的行。与 MarkReceiptStateTx 是同一列上的两种语义，不能互换 ——
// 那个按 id 无条件回写（写的是发布事务内刚建出来的 pending 行，必须能一起回滚），
// 这个只能「从 pending 前进」（跨进程收敛，行可能已被另一个实例结案）。
func (m *Model) FinishPendingReceipt(ctx context.Context, id int64, state string, now time.Time) error {
	return m.ReceiptDB(ctx).
		Where("id = ? AND receipt_state = ?", id, ReceiptPending).
		Updates(map[string]any{"receipt_state": state, "completed_at": now}).Error
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
