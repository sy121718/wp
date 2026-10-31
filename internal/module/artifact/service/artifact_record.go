package artifactservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactmodel "go_wp/internal/module/artifact/model"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func normalizeLang(lang string) string {
	if l := strings.TrimSpace(lang); l != "" {
		return l
	}
	return i18n.GetDefaultLang()
}

func validateRecordReq(req *artifactdto.RecordReq) error {
	if req == nil {
		return artifactenums.Err(artifactenums.ErrInvalidArtifact)
	}
	if strings.TrimSpace(req.ArtifactID) == "" || strings.TrimSpace(req.PageID) == "" {
		return artifactenums.Err(artifactenums.ErrInvalidArtifact)
	}
	if strings.TrimSpace(req.ArtifactHash) == "" {
		return artifactenums.Err(artifactenums.ErrInvalidArtifact)
	}
	if req.Version <= 0 {
		return artifactenums.Err(artifactenums.ErrInvalidArtifact)
	}
	return nil
}

func (s *Service) Record(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error) {
	if err = validateRecordReq(req); err != nil {
		return nil, err
	}
	if existing, exists, existsErr := s.findByHash(ctx, req.PageID, req.ArtifactHash); existsErr != nil {
		return nil, existsErr
	} else if exists {

		return toResp(existing), nil
	}

	var parsedManifest struct {
		Files map[string]string `json:"files"`
	}

	if err = json.Unmarshal(req.Manifest, &parsedManifest); err != nil || len(parsedManifest.Files) == 0 {
		return nil, artifactenums.Err(artifactenums.ErrInvalidArtifact)
	}

	now := time.Now().UTC()

	entity := &artifactmodel.PageArtifactEntity{
		ID:                        req.ArtifactID,
		PageID:                    req.PageID,
		Version:                   req.Version,
		Lang:                      normalizeLang(req.Lang),
		SourceDocument:            req.SourceDocument,
		PageDocumentSchemaVersion: req.SchemaVersion,
		SourceHash:                req.SourceHash,
		BuildInputHash:            req.BuildInputHash,
		ArtifactProvider:          req.ArtifactProvider,
		ArtifactKey:               req.ArtifactKey,
		ArtifactHash:              req.ArtifactHash,
		CompilerVersion:           req.CompilerVersion,
		RegistryVersion:           req.RegistryVersion,
		Manifest:                  req.Manifest,
		PayloadState:              "available",
		Note:                      "",
		CreatedBy:                 defaultCreator(req.CreatedBy),
		CreatedAt:                 now,
	}

	err = s.model.Transaction(ctx, func(tx *gorm.DB) error {

		if cerr := s.model.CreateArtifactTx(ctx, tx, entity); cerr != nil {
			if database.IsUniqueViolation(cerr) {
				return errArtifactIdentityConflict
			}
			return cerr
		}

		seenHashes := make(map[string]struct{}, len(parsedManifest.Files))
		for _, fileName := range sortedManifestFiles(parsedManifest.Files) {
			fileHash := strings.TrimSpace(parsedManifest.Files[fileName])
			if fileHash == "" {
				continue
			}
			if _, dup := seenHashes[fileHash]; dup {
				continue
			}
			seenHashes[fileHash] = struct{}{}
			if cerr := s.model.EnsureContentObjectTx(ctx, tx, fileHash, req.ArtifactProvider, artifactObjectKey(req.ArtifactKey, fileName), now); cerr != nil {
				return cerr
			}
			if cerr := s.model.CreateArtifactObjectTx(ctx, tx, &artifactmodel.PageArtifactObjectEntity{
				ArtifactID:  entity.ID,
				ContentHash: fileHash,
			}); cerr != nil {
				return cerr
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errArtifactIdentityConflict) {

			if existing, exists, readErr := s.findByHash(ctx, req.PageID, req.ArtifactHash); readErr != nil {
				return nil, readErr
			} else if exists {
				return toResp(existing), nil
			}
			return nil, artifactenums.Err(artifactenums.ErrArtifactMismatch)
		}
		return nil, mapPersistenceError(err)
	}
	return toResp(entity), nil
}

var errArtifactIdentityConflict = errors.New("artifact identity conflict")

func (s *Service) Detail(ctx context.Context, req *artifactdto.DetailReq) (res *artifactdto.ArtifactResp, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" || strings.TrimSpace(req.Hash) == "" {
		return nil, artifactenums.Err(artifactenums.ErrInvalidParam)
	}
	entity, exists, err := s.findByHash(ctx, req.PageID, req.Hash)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, artifactenums.Err(artifactenums.ErrArtifactNotFound)
	}
	return toResp(entity), nil
}

func (s *Service) DetailByID(ctx context.Context, req *artifactdto.DetailByIDReq) (res *artifactdto.ArtifactResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, artifactenums.Err(artifactenums.ErrInvalidParam)
	}
	entity, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, artifactenums.Err(artifactenums.ErrArtifactNotFound)
	}
	if err != nil {
		return nil, err
	}
	return toResp(entity), nil
}

func (s *Service) ListStalePageIDs(ctx context.Context, current string, artifactIDs []string) (ids []string, err error) {
	if strings.TrimSpace(current) == "" || len(artifactIDs) == 0 {
		return nil, nil
	}
	return s.model.ListStalePageIDs(ctx, current, artifactIDs)
}

func (s *Service) findByHash(ctx context.Context, pageID, hash string) (e *artifactmodel.PageArtifactEntity, exists bool, err error) {
	e, err = s.model.GetByHash(ctx, pageID, hash)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return e, true, nil
}

func (s *Service) EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (*artifactdto.ArtifactResp, error) {
	return s.Record(ctx, req)
}

func sortedManifestFiles(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func artifactObjectKey(artifactKey, fileName string) string {
	base := strings.TrimSuffix(strings.TrimSpace(artifactKey), "/")
	name := strings.TrimPrefix(strings.TrimSpace(fileName), "/")
	if name == "" {
		return base
	}
	return base + "/" + name
}

func defaultCreator(createdBy string) string {
	if strings.TrimSpace(createdBy) == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return createdBy
}

func mapPersistenceError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return artifactenums.Err(artifactenums.ErrArtifactMismatch)
	}
	return err
}

func toResp(e *artifactmodel.PageArtifactEntity) *artifactdto.ArtifactResp {
	var meta struct {
		CanonicalPath string `json:"canonicalPath"`
	}
	if err := json.Unmarshal(e.Manifest, &meta); err != nil {
		logger.Scene("artifact").With("err", err).Warn("产物 manifest 解析失败")
	}
	return &artifactdto.ArtifactResp{
		ID:               e.ID,
		PageID:           e.PageID,
		Version:          e.Version,
		Lang:             e.Lang,
		SourceDocument:   e.SourceDocument,
		SourceHash:       e.SourceHash,
		BuildInputHash:   e.BuildInputHash,
		ArtifactProvider: e.ArtifactProvider,
		ArtifactKey:      e.ArtifactKey,
		ArtifactHash:     e.ArtifactHash,
		CanonicalPath:    meta.CanonicalPath,
		CompilerVersion:  e.CompilerVersion,
		RegistryVersion:  e.RegistryVersion,
		Manifest:         e.Manifest,
		PayloadState:     e.PayloadState,
		CreatedBy:        e.CreatedBy,
		CreatedAt:        utils.NewJSONTime(e.CreatedAt),
	}
}

func (s *Service) ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []artifactdto.GCCandidateResp, err error) {
	rows, err := s.model.ListGCCandidates(ctx, before, excludeIDs)
	if err != nil {
		return nil, err
	}
	list = make([]artifactdto.GCCandidateResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, artifactdto.GCCandidateResp{
			ID: r.ID, PageID: r.PageID, Version: r.Version, Lang: r.Lang,
			ArtifactHash: r.ArtifactHash, ArtifactKey: r.ArtifactKey, CreatedAt: utils.NewJSONTime(r.CreatedAt),
		})
	}
	return list, nil
}

func (s *Service) CountOtherAvailableByHash(ctx context.Context, hash, excludeID string) (n int64, err error) {
	return s.model.CountOtherAvailableByHash(ctx, hash, excludeID)
}

func (s *Service) MarkPayloadState(ctx context.Context, ids []string, state string) (n int64, err error) {
	return s.model.MarkPayloadState(ctx, ids, state, time.Now().UTC())
}

const (
	defaultContentObjectRetentionDays = 30

	contentObjectGCBatch = 2000
)

type ExternalContentRefs func(ctx context.Context, hashes []string) (referenced []string, err error)

func (s *Service) SetExternalContentRefs(fn ExternalContentRefs) {
	if s == nil {
		return
	}
	s.externalRefs = fn
}

const (
	objectActionWouldDelete   = "would_delete"
	objectActionDeleted       = "deleted"
	objectActionKeptExternal  = "kept_external"
	objectActionKeptReclaimed = "kept_reclaimed"
	objectActionDeleteFailed  = "delete_failed"
)

func (s *Service) GarbageCollectContentObjects(ctx context.Context, req *artifactdto.ContentObjectGCReq) (res *artifactdto.ContentObjectGCResp, err error) {
	retention, dryRun, limit := defaultContentObjectRetentionDays, true, contentObjectGCBatch
	if req != nil {
		if req.RetentionDays > 0 {
			retention = req.RetentionDays
		}
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	before := time.Now().UTC().AddDate(0, 0, -retention)
	res = &artifactdto.ContentObjectGCResp{
		RetentionDays: retention, DryRun: dryRun, Items: []artifactdto.OrphanContentObjectResp{},
	}

	orphans, err := s.model.CountOrphanContentObjects(ctx, before)
	if err != nil {
		return nil, err
	}
	res.Orphans = orphans
	if orphans == 0 {
		return res, nil
	}
	rows, err := s.model.ListOrphanContentObjects(ctx, before, limit)
	if err != nil {
		return nil, err
	}
	res.Scanned = len(rows)
	if len(rows) == 0 {
		return res, nil
	}

	hashes := make([]string, 0, len(rows))
	for _, row := range rows {
		hashes = append(hashes, row.ContentHash)
	}
	keptExternal, eerr := s.externalKept(ctx, hashes)
	if eerr != nil {
		return nil, eerr
	}

	if dryRun {
		for _, row := range rows {
			item := orphanItem(row)
			if keptExternal[row.ContentHash] {
				item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
				res.SkippedExtern++
			} else {
				item.Action = objectActionWouldDelete
			}
			res.Items = append(res.Items, item)
		}
		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("skippedExternal", res.SkippedExtern).Info("孤儿内容对象预演完成（未删除）")
		return res, nil
	}

	deletable := make([]string, 0, len(rows))
	for _, row := range rows {
		if !keptExternal[row.ContentHash] {
			deletable = append(deletable, row.ContentHash)
		}
	}
	deletedSet, derr := s.deleteOrphans(ctx, deletable, before)
	if derr != nil {
		for _, row := range rows {
			item := orphanItem(row)
			if keptExternal[row.ContentHash] {
				item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
				res.SkippedExtern++
			} else {
				item.Action, item.Reason = objectActionDeleteFailed, derr.Error()
				res.Failed++
			}
			res.Items = append(res.Items, item)
		}
		res.FailedRate = failedRate(res.Failed, res.Scanned)
		logger.Scene("artifact").With("failed", res.Failed).With("failedRate", res.FailedRate).
			Error(derr, "孤儿内容对象回收失败")
		return res, nil
	}

	stillOrphan, rerr := s.stillOrphanAfterDelete(ctx, deletable, deletedSet, before)
	if rerr != nil {

		logger.Scene("artifact").With("undecided", len(deletable)-len(deletedSet)).
			Error(rerr, "删除后复查失败：本轮无法区分并发认领与删除未生效")
	}

	for _, row := range rows {
		item := orphanItem(row)
		switch {
		case keptExternal[row.ContentHash]:
			item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
			res.SkippedExtern++
		case deletedSet[row.ContentHash]:
			item.Action = objectActionDeleted
			res.Deleted++
		case stillOrphan[row.ContentHash]:

			item.Action, item.Reason = objectActionDeleteFailed, "删除未生效：复查确认该行仍存在且仍无引用"
			res.Failed++
		default:

			item.Action, item.Reason = objectActionKeptReclaimed, "删除前已被新产物引用，本轮保留"
		}
		res.Items = append(res.Items, item)
	}
	res.FailedRate = failedRate(res.Failed, res.Scanned)
	if res.Failed > 0 {

		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("deleted", res.Deleted).With("skippedExternal", res.SkippedExtern).
			With("failed", res.Failed).With("failedRate", res.FailedRate).
			Error(fmt.Errorf("%s: %d/%d", artifactenums.ErrContentObjectDeleteNotApplied, res.Failed, res.Scanned),
				"内容对象回收存在失败（失败率告警）")
	} else if res.Deleted > 0 {
		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("deleted", res.Deleted).With("skippedExternal", res.SkippedExtern).
			Info("孤儿内容对象回收完成")
	}
	return res, nil
}

func (s *Service) stillOrphanAfterDelete(ctx context.Context, deletable []string, deleted map[string]bool, before time.Time) (set map[string]bool, err error) {
	set = map[string]bool{}
	if len(deleted) >= len(deletable) {
		return set, nil
	}
	remaining := make([]string, 0, len(deletable)-len(deleted))
	for _, h := range deletable {
		if !deleted[h] {
			remaining = append(remaining, h)
		}
	}
	if len(remaining) == 0 {
		return set, nil
	}
	still, serr := s.model.ListStillOrphanHashes(ctx, remaining, before)
	if serr != nil {
		return set, serr
	}
	for _, h := range still {
		set[h] = true
	}
	return set, nil
}

func failedRate(failed, scanned int) float64 {
	if scanned <= 0 {
		return 0
	}
	return float64(failed) / float64(scanned)
}

func (s *Service) externalKept(ctx context.Context, hashes []string) (kept map[string]bool, err error) {
	kept = map[string]bool{}
	if s.externalRefs == nil || len(hashes) == 0 {
		return kept, nil
	}
	referenced, err := s.externalRefs(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", artifactenums.ErrContentRefQueryFailed, err)
	}
	for _, h := range referenced {
		kept[h] = true
	}
	return kept, nil
}

func (s *Service) deleteOrphans(ctx context.Context, hashes []string, before time.Time) (deleted map[string]bool, err error) {
	deleted = map[string]bool{}
	if len(hashes) == 0 {
		return deleted, nil
	}
	rows, err := s.model.DeleteOrphanContentObjects(ctx, hashes, before)
	if err != nil {
		return nil, err
	}
	for _, h := range rows {
		deleted[h] = true
	}
	return deleted, nil
}

func orphanItem(row artifactmodel.ContentObjectEntity) artifactdto.OrphanContentObjectResp {
	return artifactdto.OrphanContentObjectResp{
		ContentHash: row.ContentHash, Provider: row.Provider, ObjectKey: row.ObjectKey,
		ByteSize: row.ByteSize, CreatedAt: utils.NewJSONTime(row.CreatedAt),
	}
}
