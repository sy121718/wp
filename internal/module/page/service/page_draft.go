package pageservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go_wp/internal/builder"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	"go_wp/internal/pipeline"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Create 创建 Page、初始 Draft 与 Revision，并原子保留路径。
func (s *Service) Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error) {
	if req == nil {
		return nil, errors.New(pageenums.ErrInvalidDocument)
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	if err = validateKind(req.Kind, req.ContentTargetType, req.ContentTargetID); err != nil {
		return nil, err
	}
	path, doc, err := validateDraft(req.DraftPath, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	page := &pagemodel.PageEntity{
		ID: uuid.NewString(), ProjectID: req.ProjectID, Kind: req.Kind,
		ContentTargetType: req.ContentTargetType, ContentTargetID: req.ContentTargetID,
		DraftPath: path, DraftDocument: doc, DraftVersion: 1, Stale: true,
		CreatedAt: now, UpdatedAt: now,
	}
	revision := &pagemodel.RevisionEntity{
		ID: uuid.NewString(), PageID: page.ID, Version: page.DraftVersion,
		DraftPath: path, DraftDocument: doc, SourceHash: hash(doc), CreatedAt: now,
	}
	if err = s.model.CreateWithRevisionAndRoute(ctx, page, revision, path); err != nil {
		return nil, mapPersistenceError(err)
	}
	return pageResp(page), nil
}

// Detail 查询当前 Page Draft。
func (s *Service) Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	page, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	if err != nil {
		return nil, err
	}
	return pageResp(page), nil
}

// SaveDraft 使用 draftVersion 乐观锁保存完整 Draft AST，并追加不可变 Revision。
// 本方法不会修改 active/staged Artifact；任何 URL 新旧占用变化与 Revision 在同一事务提交。
func (s *Service) SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	path, doc, err := validateDraft(req.DraftPath, req.DraftDocument)
	if err != nil {
		return nil, err
	}
	page, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion != page.DraftVersion {
		return nil, errors.New(pageenums.ErrDraftVersionConflict)
	}

	nextVersion := page.DraftVersion + 1
	now := time.Now().UTC()
	revision := &pagemodel.RevisionEntity{
		ID: uuid.NewString(), PageID: page.ID, Version: nextVersion,
		DraftPath: path, DraftDocument: doc, SourceHash: hash(doc), CreatedAt: now,
	}
	changedPath := page.DraftPath != path
	if err = s.model.SaveDraftWithRevision(ctx, page.ID, page.ProjectID, page.DraftVersion,
		page.DraftPath, path, doc, nextVersion, now, revision, changedPath); err != nil {
		return nil, mapPersistenceError(err)
	}
	page.DraftPath = path
	page.DraftDocument = doc
	page.DraftVersion = nextVersion
	page.Stale = true
	page.UpdatedAt = now
	return pageResp(page), nil
}

// ListRevisions 查询 Page 历史草稿快照（最新在前）。
func (s *Service) ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	if _, err = s.model.GetByID(ctx, req.PageID); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(pageenums.ErrPageNotFound)
	} else if err != nil {
		return nil, err
	}
	list, err := s.model.ListRevisions(ctx, req.PageID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.RevisionResp, 0, len(list))
	for _, r := range list {
		res = append(res, pagedto.RevisionResp{
			ID: r.ID, PageID: r.PageID, Version: r.Version, DraftPath: r.DraftPath,
			DraftDocument: r.DraftDocument, SourceHash: r.SourceHash, CreatedAt: r.CreatedAt,
		})
	}
	return res, nil
}

func (s *Service) requireProject(ctx context.Context, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New(pageenums.ErrProjectNotFound)
	}
	exists, err := s.project.Exists(ctx, projectID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(pageenums.ErrProjectNotFound)
	}
	return nil
}

// validateKind 执行 pages 表同等领域约束，防止无效 Kind/ContentTarget 入库。
func validateKind(kind, targetType string, targetID *string) error {
	if targetID != nil && strings.TrimSpace(*targetID) == "" {
		return errors.New(pageenums.ErrInvalidKind)
	}
	noTarget := func() bool { return targetType == "none" && targetID == nil }
	target := func(expected string) bool { return targetType == expected && targetID != nil }
	switch kind {
	case "home", "archive", "search", "notFound":
		if noTarget() {
			return nil
		}
	case "page", "article", "product", "category", "tag":
		if target(kind) {
			return nil
		}
	}
	return errors.New(pageenums.ErrInvalidKind)
}

// validateDraft 解析并验证 Page Document，再规范化 URL。
func validateDraft(rawPath string, rawDoc json.RawMessage) (path string, doc json.RawMessage, err error) {
	path, err = normalizePagePath(rawPath)
	if err != nil {
		return "", nil, err
	}
	page, err := builder.ParsePage(rawDoc)
	if err != nil {
		return "", nil, errors.New(pageenums.ErrInvalidDocument)
	}
	if err = builder.ValidatePage(page); err != nil {
		return "", nil, fmt.Errorf("%s: %w", pageenums.ErrInvalidDocument, err)
	}
	// 重新编码保证存储 JSON 的规范格式；Document 不接受任意散乱字节。
	doc, err = json.Marshal(page)
	if err != nil {
		return "", nil, errors.New(pageenums.ErrInvalidDocument)
	}
	return path, doc, nil
}

func normalizePagePath(raw string) (string, error) {
	path, err := pipeline.NormalizeURL(raw)
	if err != nil {
		return "", errors.New(pageenums.ErrInvalidPath)
	}
	return path, nil
}

func mapPersistenceError(err error) error {
	if errors.Is(err, pagemodel.ErrDraftVersionConflict) {
		return errors.New(pageenums.ErrDraftVersionConflict)
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate key") || strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
		return errors.New(pageenums.ErrPathOccupied)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(pageenums.ErrPageNotFound)
	}
	return err
}

func pageResp(page *pagemodel.PageEntity) *pagedto.PageResp {
	return &pagedto.PageResp{
		ID: page.ID, ProjectID: page.ProjectID, Kind: page.Kind, ContentTargetType: page.ContentTargetType,
		ContentTargetID: page.ContentTargetID, DraftPath: page.DraftPath, ActivePath: page.ActivePath,
		StagedArtifactID: page.StagedArtifactID, ActiveArtifactID: page.ActiveArtifactID,
		DraftDocument: page.DraftDocument, DraftVersion: page.DraftVersion, Stale: page.Stale,
		CreatedAt: page.CreatedAt, UpdatedAt: page.UpdatedAt,
	}
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// 保证编译期使用 page 模块对外契约。
var _ pagecontract.PageService = (*Service)(nil)
