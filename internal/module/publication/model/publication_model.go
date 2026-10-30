// Package pubmodel 实现 publication 模块 page_routes 与 publication_receipts
// 表持久化：URL 占用状态流转与发布回执记录。
package pubmodel

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const (
	tableNamePageRoutes = "page_routes"
	tableNameReceipts   = "publication_receipts"
)

// 路由占用类型（AGENTS.md §page_routes）。
const (
	RouteReserved = "reserved"
	RouteActive   = "active"
	RouteRedirect = "redirect"
)

// 回执状态（AGENTS.md §9 publication_receipts）。
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
//
// 作用域必填（DB-009 切角色收口）：page_routes 带 FORCE 策略，裸查在非超级角色下静默
// 查不到 —— 调用方把「查不到」读成「这个路径没被占用」，于是重复占用被放行（写侧冲突），
// 改名 / 发布路径预检全部失真，且没有任何错误日志。
func (m *Model) GetRoute(ctx context.Context, projectID, path string) (e *RouteEntity, err error) {
	e = &RouteEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&RouteEntity{}).
			Where("project_id = ? AND path = ?", projectID, path).First(e).Error
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

// RouteOwnerTx 在**调用方事务**内读一条路径当前的占用归属（无行返回 gorm.ErrRecordNotFound）。
//
// 为什么必须有 Tx 形态：presentation 的旧路径处置/删除/退役持实体行锁，必须在
// "判定 → 副作用"这段里回读同一事务内的归属事实。用非事务 GetRoute 会走另一条连接，
// 既看不到本事务尚未提交的条件写，也可能与自己的行锁互等。
// 不新开事务、不写、不设作用域（作用域由调用方按 publication_route.go 文件头的约定设进 tx）。
func (m *Model) RouteOwnerTx(ctx context.Context, tx *gorm.DB, projectID, path string) (*RouteEntity, error) {
	e := &RouteEntity{}
	err := tx.WithContext(ctx).Model(&RouteEntity{}).
		Where("project_id = ? AND path = ?", projectID, path).First(e).Error
	if err != nil {
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
	// 作用域必填（DB-009 切角色收口）：裸查在非超级角色下恒为空集，调用方会以为
	// 「这个页面没有任何已激活路径」，于是 active 目录里的符号链接不会被解除 ——
	// 页面删了，线上还在服务（DB 路由行删掉不会让内容下线）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&RouteEntity{}).
			Where("project_id = ? AND page_id = ? AND route_kind IN ?",
				projectID, pageID, []string{RouteActive, RouteRedirect}).
			Pluck("path", &paths).Error
	})
	if err != nil {
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
	// 作用域必填（DB-009 切角色收口）：同 ListRoutePathsByPage —— 空集会让旧路径的
	// 符号链接留在 active 目录里继续 301，指向一个已经删除的实例。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&RouteEntity{}).
			Where("project_id = ? AND presentation_id = ? AND route_kind IN ?",
				projectID, presentationID, []string{RouteActive, RouteRedirect}).
			Pluck("path", &paths).Error
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// DeleteRoutesByPresentation 清理展示实例全部路径占用（实例删除时释放）。
// 幂等（无占用时 RowsAffected=0 不报错）。
//
// 与 DeleteRoutesByPresentationTx 共用同一份删除条件（见
// publication_route_tx_model.go 的 deleteRoutesByPresentationScope）。
// 作用域必填（DB-009 切角色收口）：非事务路径不自己设作用域时 DELETE 匹配 0 行且不报错，
// 实例删了、路径占用还在。*Tx 变体由调用方的事务负责设作用域。
func (m *Model) DeleteRoutesByPresentation(ctx context.Context, projectID, presentationID string) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.deleteRoutesByPresentationScope(tx.WithContext(ctx), projectID, presentationID).
			Delete(&RouteEntity{}).Error
	})
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
	// 作用域必填（DB-009 切角色收口）：这个判定是**写侧的前置守卫**，裸查恒答「没被占用」——
	// 两个实体争用同一路径都能通过预检，冲突被推迟到唯一索引上（错误信息从「路径已被占用」
	// 变成 23505）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&RouteEntity{}).Where("project_id = ? AND path = ?", projectID, path)
		if exclude := strings.TrimSpace(excludePageID); exclude != "" {
			q = q.Where("(page_id IS NULL OR page_id <> ?)", exclude)
		}
		if exclude := strings.TrimSpace(excludePresentationID); exclude != "" {
			q = q.Where("(presentation_id IS NULL OR presentation_id <> ?)", exclude)
		}
		return q.Count(&foreign).Error
	})
	if err != nil {
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

// ReceiptStatesTx 按 id 批量读取回执的最终状态（id 十进制串 → receipt_state）。
//
// 用途：调用方把"本次改址关联的成功回执"记在一条 update_url 回执里，判定是否执行
// 旧路径处置时必须回读这些 id 的**当前**状态。批量一次查完：逐条查会让判定开销随
// 语言数线性增长，且更容易读到互相不一致的快照。
//
// 不存在的 id 不出现在结果里（不报错）：调用方按 len(结果) != len(入参) 判"证据不足"，
// 这比在这里猜一个默认状态安全 —— 猜 pending 会让判定永久悬空，猜 committed 会误执行处置。
func (m *Model) ReceiptStatesTx(tx *gorm.DB, ids map[string]string) (map[string]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	wanted := make([]int64, 0, len(ids))
	strOf := make(map[int64]string, len(ids))
	for _, raw := range ids {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		wanted = append(wanted, id)
		strOf[id] = v
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	var rows []struct {
		ID    int64  `gorm:"column:id"`
		State string `gorm:"column:receipt_state"`
	}
	db := tx
	if db == nil {
		db = m.db
	}
	if err := db.WithContext(context.Background()).Model(&ReceiptEntity{}).
		Select("id", "receipt_state").Where("id IN ?", wanted).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[strOf[row.ID]] = row.State
	}
	return out, nil
}

// ReceiptAttemptVerdict 三态判定（定义在 dto：contract 要用它但不能 import model）。
type ReceiptAttemptVerdict = pubdto.ReceiptAttemptVerdict

const (
	ReceiptAttemptNoEvidence ReceiptAttemptVerdict = pubdto.ReceiptAttemptNoEvidence
	ReceiptAttemptInProgress ReceiptAttemptVerdict = pubdto.ReceiptAttemptInProgress
	ReceiptAttemptSucceeded  ReceiptAttemptVerdict = pubdto.ReceiptAttemptSucceeded
	ReceiptAttemptFailed     ReceiptAttemptVerdict = pubdto.ReceiptAttemptFailed
)

// ReceiptAttemptVerdictTx 判定一组关联回执（语言 → 同批 switch_active 回执 id）
// 是否整体成功：全部 committed 且**至少一条**在账本上留下了成功身份。
//
// 「账本归属」由调用方通过 ownedLangs 提供（presentation 侧传自己账本里的 receipt_id，
// page 侧传 page_publications 的 receipt_id）：本模块不读别人的账本表，只读自己的回执表。
//
// 返回三态而不是布尔，是为了让调用方区分「明确失败（可终态化）」与「证据还没到
// （必须留 pending 下一轮再判）」。早期把两者压成一个 bool，导致关联回执仍是 pending
// 时就把处置意图判死 —— 而那条回执的结案恰恰可能在本轮稍后才落库。
func (m *Model) ReceiptAttemptVerdictTx(tx *gorm.DB, links map[string]string,
	ownedLangs map[string]string) (ReceiptAttemptVerdict, error) {
	states, err := m.ReceiptStatesTx(tx, links)
	if err != nil {
		return ReceiptAttemptNoEvidence, err
	}
	want := 0
	missing := false
	interim := false
	for lang, raw := range links {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		want++
		st, ok := states[id]
		if !ok {
			// 关联里引用的行不存在：没有反证，也没有证据 —— 不能据此判死。
			missing = true
			continue
		}
		switch st {
		case ReceiptCommitted:
		case ReceiptPending:
			// 还没定：**保留可重试**，绝不终态化。
			interim = true
			delete(links, lang)
			_ = lang
		default:
			// rolled_back / superseded：明确失败。
			return ReceiptAttemptFailed, nil
		}
	}
	if want == 0 || missing || interim {
		return ReceiptAttemptInProgress, nil
	}
	// 全部 committed：还要求账本承认其中至少一条（本次 attempt 真的留下过成功身份）。
	for lang, raw := range links {
		if owned := strings.TrimSpace(ownedLangs[lang]); owned != "" && owned == strings.TrimSpace(raw) {
			return ReceiptAttemptSucceeded, nil
		}
	}
	return ReceiptAttemptFailed, nil
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

// ErrReceiptNotPending 回执状态条件未命中的受控哨兵（并发结案 / 状态已前进）。
var ErrReceiptNotPending = errors.New("publication: 回执已不在 pending 状态")

// MarkDeactivationCommittedTx 在**调用方业务事务内**记录「这次下线意图的业务变更已提交」。
//
// 为什么只写 receipt_data.businessCommitted 而不动 receipt_state：
//   - receipt_state 必须保持 pending，否则 ClaimPendingReceipts（只认 pending）永远领不到
//     那些「业务已提交、链接还没撤」的回执，撤销动作就没人做了；
//   - 反过来，先置 committed 再撤链接，等于把"已完成"记在了没完成之前。
//
// committed 只由 CompletePublishReceipt 在**全部链接撤掉之后**写。
//
// 为什么它与业务变更共用同一个 tx：标记本身也是持久事实。若标记单独提交，进程在
// 「标记已写、业务未提交」之间崩掉，恢复就会把一次**没发生**的删除当成"该撤链接"。
// 同 tx 让两者要么都成立、要么都不成立 —— 那时 pending 行的 businessCommitted 缺键，
// 恢复读到的就是"业务未提交"。
//
// 校验：id + source_type + source_id + action=deactivate + receipt_state=pending +
// receipt_data.projectId = 调用方工程（工程事实缺失也算不匹配，不放行）。
// 任何一条不满足都返回受控错误（0 行），由调用方拒绝整笔业务而不是静默继续。
// 句柄必须是真事务（见 requireRealTx）：自动提交的 db 句柄会让"同 tx"这件事悄悄失效。
func (m *Model) MarkDeactivationCommittedTx(tx *gorm.DB, owner pubdto.ReceiptOwner, projectID string,
	id int64, pathArtifacts map[string]pubdto.ReceiptPathIdentity) error {
	if terr := requireRealTx(tx); terr != nil {
		return terr
	}
	if id <= 0 {
		return ErrReceiptNotPending
	}
	if strings.TrimSpace(projectID) == "" {
		return ErrReceiptNotPending
	}
	payload := map[string]any{"businessCommitted": true}
	if len(pathArtifacts) > 0 {
		payload["pathArtifacts"] = pathArtifacts
	}
	encoded, merr := json.Marshal(payload)
	if merr != nil {
		return merr
	}
	// 单条 UPDATE：用 jsonb || 合并写业务事实，保留登记时的其余键（lang / oldPaths /
	// 关联回执 / fromReceiptId 一个都不能丢）。不用"读-改-写"是为了不把并发结案的
	// 窗口拉长到两次往返。
	// 归属校验必须用**调用方给的 owner 事实**（source_type + source_id + 工程 + id + action），
	// 不能只写 source_type IN ('page','presentation')：那样同工程里另一条实体的回执
	// 也能被这次标记命中，等于把 A 的业务提交事实写到 B 的意图上。
	result := tx.Model(&ReceiptEntity{}).
		Where("id = ? AND source_type = ? AND source_id = ? AND action = ? AND receipt_state = ?",
			id, owner.SourceType, owner.SourceID, pubdto.ReceiptActionDeactivate, ReceiptPending).
		Where("receipt_data->>'projectId' = ?", strings.TrimSpace(projectID)).
		Update("receipt_data", gorm.Expr("receipt_data || ?::jsonb", string(encoded)))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrReceiptNotPending
	}
	return nil
}

// LoadDeactivationReceiptTx 在**调用方事务内**（通常是已持有业务实体锁的那个事务）
// 重读一条 deactivate 回执的当前事实：状态 + 业务提交标记 + 每条路径的归属事实。
//
// 为什么不复用领取结果：ClaimPendingReceipts 只是短事务里 FIND ... SKIP LOCKED，
// 释放事务即释放行锁、没有租约 —— 同一行可以被多个收敛同时领到。所以领取返回的
// ReceiptEntity 只是"某一刻的快照"，拿它决定"要不要撤链接"会让两个收敛互相打脸。
// 唯一可信的读法是在**同一把业务锁**之下重读本行，本方法就是那个读。
func (m *Model) LoadDeactivationReceiptTx(tx *gorm.DB, owner pubdto.ReceiptOwner, projectID string,
	id int64) (e *ReceiptEntity, err error) {
	if terr := requireRealTx(tx); terr != nil {
		return nil, terr
	}
	if owner.IsZero() {
		return nil, errors.New("publication: 下线回执读取必须给出显式 owner")
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row ReceiptEntity
	// 工程条件**写进 SQL**、不做事后比较：事后比较时"recceipt_data 里没有 projectId"
	// 会得到空串，而 (空串 != expected) 的写法只要有一处分叉就变成放行 —— 缺事实必须
	// 等同于不匹配。与 Mark 用同一条谓词，两处判定不可能漂移。
	if err = tx.Model(&ReceiptEntity{}).
		Where("id = ? AND source_type = ? AND source_id = ? AND action = ?",
			id, owner.SourceType, owner.SourceID, pubdto.ReceiptActionDeactivate).
		Where("receipt_data->>'projectId' = ?", strings.TrimSpace(projectID)).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// requireRealTx 判定句柄是不是**真的在事务里**（不是自动提交的 db、不是 typed-nil）。
//
// 与该模块既有写路径同一口径（internal/module/build/model/build_model.go 的 requireTx、
// pkg/i18n/content_write.go）：本机 gorm.io/gorm@v1.31.2 里事务句柄只有
// *sql.Tx 与 *gorm.PreparedStmtTX 两种，PreparedStmtTX 内嵌的 Tx 可能是 typed-nil，
// 所以两者都判 nil。少了这层，传进来一个普通 *gorm.DB 也能"成功"，而
// "标记与业务同事务"这条硬约束就静默失效了。
func requireRealTx(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil || tx.Statement.ConnPool == nil {
		return errors.New("publication: 该操作必须在调用方真实事务内执行")
	}
	if err := tx.Error; err != nil {
		return err
	}
	switch conn := tx.Statement.ConnPool.(type) {
	case *sql.Tx:
		if conn == nil {
			return errors.New("publication: 该操作必须在调用方真实事务内执行")
		}
	case *gorm.PreparedStmtTX:
		if conn == nil || conn.Tx == nil {
			return errors.New("publication: 该操作必须在调用方真实事务内执行")
		}
	default:
		return errors.New("publication: 该操作必须在调用方真实事务内执行")
// receiptDataProjectID 已并入 LoadDeactivationReceiptTx 的 SQL 谓词（缺事实即不匹配）。
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
