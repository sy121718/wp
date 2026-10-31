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

const (
	RouteReserved = "reserved"
	RouteActive   = "active"
	RouteRedirect = "redirect"
)

const (
	ReceiptPending    = "pending"
	ReceiptCommitted  = "committed"
	ReceiptRolledBack = "rolled_back"
)

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

type Model struct {
	db *gorm.DB
}

func NewPublicationModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) RouteDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RouteEntity{})
}

func (m *Model) ReceiptDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReceiptEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) ListActivePaths(ctx context.Context, projectID string) (paths []string, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&RouteEntity{}).
			Where("project_id = ? AND route_kind = ?", projectID, RouteActive).
			Order("path ASC").
			Pluck("path", &paths).Error
	})
	return paths, err
}

func (m *Model) ListActiveRoutes(ctx context.Context, projectID string) (routes []RouteEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&RouteEntity{}).
			Where("project_id = ? AND route_kind = ?", projectID, RouteActive).
			Order("path ASC").
			Find(&routes).Error
	})
	return routes, err
}

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

func (m *Model) RouteOwnerTx(ctx context.Context, tx *gorm.DB, projectID, path string) (*RouteEntity, error) {
	e := &RouteEntity{}
	err := tx.WithContext(ctx).Model(&RouteEntity{}).
		Where("project_id = ? AND path = ?", projectID, path).First(e).Error
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) ListRoutePathsByPage(ctx context.Context, projectID, pageID string) (paths []string, err error) {
	paths = []string{}

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

func (m *Model) ListRoutePathsByPresentation(ctx context.Context, projectID, presentationID string) (paths []string, err error) {
	paths = []string{}

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

func (m *Model) DeleteRoutesByPresentation(ctx context.Context, projectID, presentationID string) error {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.deleteRoutesByPresentationScope(tx.WithContext(ctx), projectID, presentationID).
			Delete(&RouteEntity{}).Error
	})
}

func (m *Model) IsPathOccupied(ctx context.Context, projectID, path, excludePageID, excludePresentationID string) (occupied bool, err error) {
	var foreign int64

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

func (m *Model) ListPendingReceipts(ctx context.Context) (list []ReceiptEntity, err error) {
	err = m.ReceiptDB(ctx).Where("receipt_state = ?", ReceiptPending).Order("create_time ASC").Find(&list).Error
	return list, err
}

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

type ReceiptAttemptVerdict = pubdto.ReceiptAttemptVerdict

const (
	ReceiptAttemptNoEvidence ReceiptAttemptVerdict = pubdto.ReceiptAttemptNoEvidence
	ReceiptAttemptInProgress ReceiptAttemptVerdict = pubdto.ReceiptAttemptInProgress
	ReceiptAttemptSucceeded  ReceiptAttemptVerdict = pubdto.ReceiptAttemptSucceeded
	ReceiptAttemptFailed     ReceiptAttemptVerdict = pubdto.ReceiptAttemptFailed
)

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

			missing = true
			continue
		}
		switch st {
		case ReceiptCommitted:
		case ReceiptPending:

			interim = true
			delete(links, lang)
			_ = lang
		default:

			return ReceiptAttemptFailed, nil
		}
	}
	if want == 0 || missing || interim {
		return ReceiptAttemptInProgress, nil
	}

	for lang, raw := range links {
		if owned := strings.TrimSpace(ownedLangs[lang]); owned != "" && owned == strings.TrimSpace(raw) {
			return ReceiptAttemptSucceeded, nil
		}
	}
	return ReceiptAttemptFailed, nil
}

func (m *Model) CountPendingReceipts(ctx context.Context, sourceType string, actions []string) (n int64, err error) {
	err = m.pendingReceiptsQuery(ctx, sourceType, actions).Count(&n).Error
	return n, err
}

func (m *Model) CreateReceipt(ctx context.Context, e *ReceiptEntity) error {
	return m.ReceiptDB(ctx).Create(e).Error
}

var ErrReceiptNotPending = errors.New("publication: 回执已不在 pending 状态")

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

	if err = tx.Model(&ReceiptEntity{}).
		Where("id = ? AND source_type = ? AND source_id = ? AND action = ?",
			id, owner.SourceType, owner.SourceID, pubdto.ReceiptActionDeactivate).
		Where("receipt_data->>'projectId' = ?", strings.TrimSpace(projectID)).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

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
	}
	return nil
}

func (m *Model) RollbackPendingReceipts(ctx context.Context, now time.Time) (int64, error) {
	result := m.ReceiptDB(ctx).
		Where("receipt_state = ?", ReceiptPending).
		Updates(map[string]any{"receipt_state": ReceiptRolledBack, "completed_at": now})
	return result.RowsAffected, result.Error
}

func (m *Model) FinishPendingReceipt(ctx context.Context, id int64, state string, now time.Time) error {
	return m.ReceiptDB(ctx).
		Where("id = ? AND receipt_state = ?", id, ReceiptPending).
		Updates(map[string]any{"receipt_state": state, "completed_at": now}).Error
}

func (m *Model) ListReferencedArtifactIDs(ctx context.Context) (ids []string, err error) {
	ids = []string{}
	err = m.RouteDB(ctx).Where("artifact_id IS NOT NULL").
		Distinct().Pluck("artifact_id", &ids).Error
	return ids, err
}
