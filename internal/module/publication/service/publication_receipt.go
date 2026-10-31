package pubservice

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"gorm.io/gorm"
)

func (s *Service) RollbackReceipts(ctx context.Context) (count int64, err error) {
	return s.model.RollbackPendingReceipts(ctx, time.Now().UTC())
}

func receiptAction(action, fallback string) string {
	if strings.TrimSpace(action) == "" {
		return fallback
	}
	return action
}

var errRouteOccupied = pubmodel.ErrRouteOccupied

type receiptPayload struct {
	To string `json:"to,omitempty"`
}

const publishReceiptAction = "switch_active"

type publishReceiptData struct {
	ProjectID string `json:"projectId,omitempty"`
	Lang      string `json:"lang,omitempty"`
	OldPath   string `json:"oldPath,omitempty"`
	Redirect  bool   `json:"redirect,omitempty"`

	CapturedInputVersion int64 `json:"capturedInputVersion,omitzero"`

	OldPaths []string `json:"oldPaths,omitempty"`

	SuccessfulReceipts map[string]string `json:"successfulReceipts,omitempty"`

	FromReceiptID string `json:"fromReceiptId,omitempty"`

	PathArtifacts map[string]pubdto.ReceiptPathIdentity `json:"pathArtifacts,omitempty"`

	BusinessCommitted bool `json:"businessCommitted,omitempty"`
}

func normalizePathList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeReceiptLinks(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for lang, id := range in {
		v := strings.TrimSpace(id)
		if v == "" {
			continue
		}
		out[lang] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toArtifactOptional(action string) bool {
	switch strings.TrimSpace(action) {
	case pubdto.ReceiptActionUpdateURL, pubdto.ReceiptActionDeactivate:
		return true
	default:
		return false
	}
}

func normalizePathIdentities(in map[string]pubdto.ReceiptPathIdentity) map[string]pubdto.ReceiptPathIdentity {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]pubdto.ReceiptPathIdentity, len(in))
	for raw, id := range in {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		clean := pubdto.ReceiptPathIdentity{
			ArtifactID: strings.TrimSpace(id.ArtifactID),
			ReceiptID:  strings.TrimSpace(id.ReceiptID),
			Locator:    strings.TrimSpace(id.Locator),
		}
		if clean.ArtifactID == "" && clean.ReceiptID == "" && clean.Locator == "" {
			continue
		}
		out[path] = clean
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeReceiptID(raw string) string {
	return strings.TrimSpace(raw)
}

func deactivationOwnerRejected(owner pubdto.ReceiptOwner, projectID string) bool {
	sourceType := strings.TrimSpace(owner.SourceType)
	sourceID := strings.TrimSpace(owner.SourceID)
	switch sourceType {
	case pubdto.ReceiptSourcePage, pubdto.ReceiptSourcePresentation:
		return false
	case pubdto.ReceiptSourceProject:
		return sourceID == "" || sourceID != strings.TrimSpace(projectID)
	default:
		return true
	}
}

func receiptOwnerOf(req *pubdto.BeginPublishReceiptReq) (routeOwner, error) {
	owner := req.Owner
	if owner == nil || owner.IsZero() {
		return parseRouteOwner(req.PageID, req.PresentationID)
	}
	sourceType := strings.TrimSpace(owner.SourceType)
	sourceID := strings.TrimSpace(owner.SourceID)
	if sourceType != pubdto.ReceiptSourceProject {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	if receiptAction(req.Action, publishReceiptAction) != pubdto.ReceiptActionDeactivate {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	if strings.TrimSpace(req.PageID) != "" || strings.TrimSpace(req.PresentationID) != "" {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	if sourceID == "" || sourceID != strings.TrimSpace(req.ProjectID) {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}

	return routeOwner{projectID: sourceID}, nil
}

func (s *Service) BeginPublishReceipt(ctx context.Context, req *pubdto.BeginPublishReceiptReq) (receiptID string, err error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return "", pubenums.Err(pubenums.ErrInvalidParam)
	}
	action := receiptAction(req.Action, publishReceiptAction)
	to := strings.TrimSpace(req.ToArtifactID)

	if to == "" && !toArtifactOptional(action) {
		return "", pubenums.Err(pubenums.ErrInvalidParam)
	}
	owner, perr := receiptOwnerOf(req)
	if perr != nil {
		return "", perr
	}

	captured := req.CapturedInputVersion
	if captured < 0 {
		captured = 0
	}
	data, merr := json.Marshal(publishReceiptData{
		ProjectID:            strings.TrimSpace(req.ProjectID),
		Lang:                 strings.TrimSpace(req.Lang),
		OldPath:              strings.TrimSpace(req.OldPath),
		OldPaths:             normalizePathList(req.OldPaths),
		Redirect:             req.Redirect,
		CapturedInputVersion: captured,
		SuccessfulReceipts:   normalizeReceiptLinks(req.SuccessfulReceipts),
		FromReceiptID:        normalizeReceiptID(req.FromReceiptID),
		PathArtifacts:        normalizePathIdentities(req.PathArtifacts),
	})
	if merr != nil {
		return "", merr
	}
	entity := &pubmodel.ReceiptEntity{
		SourceType:   owner.sourceType(),
		SourceID:     owner.sourceID(),
		Action:       action,
		Path:         req.Path,
		ReceiptState: pubmodel.ReceiptPending,
		ReceiptData:  data,
		CreateTime:   time.Now().UTC(),
	}
	if to != "" {
		entity.ToArtifact = &to
	}
	if id := strings.TrimSpace(req.FromArtifactID); id != "" {
		entity.FromArtifact = &id
	}
	if err = s.model.CreateReceipt(ctx, entity); err != nil {
		return "", err
	}
	return strconv.FormatInt(entity.ID, 10), nil
}

func (s *Service) CompletePublishReceipt(ctx context.Context, receiptID string) (err error) {
	return s.finishReceipt(ctx, receiptID, pubmodel.ReceiptCommitted)
}

func (s *Service) AbortPublishReceipt(ctx context.Context, receiptID string) (err error) {
	return s.finishReceipt(ctx, receiptID, pubmodel.ReceiptRolledBack)
}

func (s *Service) finishReceipt(ctx context.Context, receiptID, state string) (err error) {
	id := strings.TrimSpace(receiptID)
	if id == "" {
		return nil
	}
	rid, perr := strconv.ParseInt(id, 10, 64)
	if perr != nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.FinishPendingReceipt(ctx, rid, state, time.Now().UTC())
}

func (s *Service) MarkDeactivationCommittedTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
	projectID, receiptID string, pathArtifacts map[string]pubdto.ReceiptPathIdentity) error {
	if tx == nil || owner.IsZero() || deactivationOwnerRejected(owner, projectID) {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	rid, perr := strconv.ParseInt(strings.TrimSpace(receiptID), 10, 64)
	if perr != nil || rid <= 0 {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.MarkDeactivationCommittedTx(tx, owner, projectID, rid, pathArtifacts)
}

func (s *Service) LoadDeactivationReceiptTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
	projectID, receiptID string) (row *pubmodel.ReceiptEntity, err error) {
	if tx == nil || owner.IsZero() || deactivationOwnerRejected(owner, projectID) {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	rid, perr := strconv.ParseInt(strings.TrimSpace(receiptID), 10, 64)
	if perr != nil || rid <= 0 {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.LoadDeactivationReceiptTx(tx, owner, projectID, rid)
}

func (s *Service) ReceiptAttemptVerdictTx(ctx context.Context, tx *gorm.DB, links map[string]string,
	ownedLangs map[string]string) (pubdto.ReceiptAttemptVerdict, error) {
	return s.model.ReceiptAttemptVerdictTx(tx, links, ownedLangs)
}

func (s *Service) ListPendingReceipts(ctx context.Context) (list []pubdto.PendingReceiptResp, err error) {
	rows, lerr := s.model.ListPendingReceipts(ctx)
	if lerr != nil {
		return nil, lerr
	}
	return pendingReceiptResps(rows), nil
}

func (s *Service) ClaimPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (list []pubdto.PendingReceiptResp, err error) {
	limit := 0
	if req != nil {
		limit = req.Limit
	}
	if limit <= 0 {
		return nil, nil
	}
	rows, cerr := s.model.ClaimPendingReceipts(ctx, req.SourceType, req.Actions, limit)
	if cerr != nil {
		return nil, cerr
	}
	return pendingReceiptResps(rows), nil
}

func (s *Service) CountPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (n int64, err error) {
	sourceType, actions := "", []string(nil)
	if req != nil {
		sourceType, actions = req.SourceType, req.Actions
	}
	return s.model.CountPendingReceipts(ctx, sourceType, actions)
}

func pendingReceiptResps(rows []pubmodel.ReceiptEntity) []pubdto.PendingReceiptResp {
	list := make([]pubdto.PendingReceiptResp, 0, len(rows))
	for i := range rows {
		list = append(list, pendingReceiptRespOf(rows[i]))
	}
	return list
}

func pendingReceiptRespOf(row pubmodel.ReceiptEntity) pubdto.PendingReceiptResp {
	var data publishReceiptData
	if len(row.ReceiptData) > 0 {
		_ = json.Unmarshal(row.ReceiptData, &data)
	}
	item := pubdto.PendingReceiptResp{
		ID: strconv.FormatInt(row.ID, 10), SourceType: row.SourceType, SourceID: row.SourceID,
		ProjectID: data.ProjectID, Lang: data.Lang, Path: row.Path, Action: row.Action,
		OldPath: data.OldPath, OldPaths: data.OldPaths, Redirect: data.Redirect,

		CapturedInputVersion: data.CapturedInputVersion,
		SuccessfulReceipts:   data.SuccessfulReceipts,
		FromReceiptID:        data.FromReceiptID,
		PathArtifacts:        data.PathArtifacts,
		BusinessCommitted:    data.BusinessCommitted,
		CreatedAt:            row.CreateTime.Format(time.RFC3339),
	}
	if row.FromArtifact != nil {
		item.FromArtifactID = *row.FromArtifact
	}
	if row.ToArtifact != nil {
		item.ToArtifactID = *row.ToArtifact
	}
	return item
}
