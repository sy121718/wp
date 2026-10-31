package blueprintservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	blueprintenums "go_wp/internal/module/blueprint/enums"
	blueprintmodel "go_wp/internal/module/blueprint/model"
)

type Service struct {
	m *blueprintmodel.Model
}

func NewService(m *blueprintmodel.Model) *Service { return &Service{m: m} }

var _ blueprintcontract.BlueprintService = (*Service)(nil)

func (s *Service) Create(ctx context.Context, req *blueprintdto.CreateReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.Name == "" {
		return nil, blueprintenums.Err(blueprintenums.ErrInvalidParam)
	}
	if !blueprintmodel.IsValidKind(req.Kind) {
		return nil, blueprintenums.Err(blueprintenums.ErrInvalidKind)
	}

	doc, err := validateDocument(req.DraftDocument)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &blueprintmodel.BlueprintEntity{
		ID: uuid.NewString(), Name: req.Name, Kind: req.Kind,
		DraftDocument: doc, DraftVersion: 1, CreatedAt: now, UpdatedAt: now,
	}

	if err = s.m.CreateWithVersion(ctx, e, &blueprintmodel.VersionEntity{
		ID: uuid.NewString(), BlueprintID: e.ID, Version: 1, Document: doc, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) Update(ctx context.Context, req *blueprintdto.UpdateReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.ID == "" {
		return nil, blueprintenums.Err(blueprintenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, blueprintenums.Err(blueprintenums.ErrNotFound)
		}
		return nil, err
	}
	doc, err := validateDocument(req.DraftDocument)
	if err != nil {
		return nil, err
	}
	e.DraftDocument = doc
	e.DraftVersion++
	e.UpdatedAt = time.Now().UTC()

	if err = s.m.SaveWithVersion(ctx, e, &blueprintmodel.VersionEntity{
		ID: uuid.NewString(), BlueprintID: e.ID, Version: e.DraftVersion, Document: doc, CreatedAt: e.UpdatedAt,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) Publish(ctx context.Context, req *blueprintdto.PublishReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.ID == "" {
		return nil, blueprintenums.Err(blueprintenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, blueprintenums.Err(blueprintenums.ErrNotFound)
		}
		return nil, err
	}

	if _, err = s.m.GetVersion(ctx, e.ID, e.DraftVersion); err == nil {
		return toResp(e), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	now := time.Now().UTC()
	if err = s.m.CreateVersion(ctx, &blueprintmodel.VersionEntity{
		ID: uuid.NewString(), BlueprintID: e.ID, Version: e.DraftVersion, Document: e.DraftDocument, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) Get(ctx context.Context, req *blueprintdto.GetReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.ID == "" {
		return nil, blueprintenums.Err(blueprintenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, blueprintenums.Err(blueprintenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

func (s *Service) List(ctx context.Context, req *blueprintdto.ListReq) (list []*blueprintdto.BlueprintResp, err error) {
	if req == nil {
		req = &blueprintdto.ListReq{}
	}
	if req.Kind != "" && !blueprintmodel.IsValidKind(req.Kind) {
		return nil, blueprintenums.Err(blueprintenums.ErrInvalidKind)
	}
	rows, err := s.m.List(ctx, req.Kind)
	if err != nil {
		return nil, err
	}
	out := make([]*blueprintdto.BlueprintResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

func (s *Service) Delete(ctx context.Context, req *blueprintdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return blueprintenums.Err(blueprintenums.ErrInvalidParam)
	}
	if _, err = s.m.Get(ctx, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return blueprintenums.Err(blueprintenums.ErrNotFound)
		}
		return err
	}
	return s.m.Delete(ctx, req.ID)
}

func (s *Service) InitPageDocument(ctx context.Context, blueprintID string) (doc json.RawMessage, err error) {
	if strings.TrimSpace(blueprintID) == "" {
		return nil, blueprintenums.Err(blueprintenums.ErrInvalidParam)
	}
	ver, err := s.m.LatestVersion(ctx, blueprintID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, blueprintenums.Err(blueprintenums.ErrNotFound)
		}
		return nil, err
	}
	page, err := builder.ParsePage(ver.Document)
	if err != nil {
		return nil, blueprintenums.Err(blueprintenums.ErrDataInvalid)
	}

	cloned := builder.ClonePageWithNewIDs(page)
	out, err := json.Marshal(cloned)
	if err != nil {
		return nil, blueprintenums.Err(blueprintenums.ErrDataInvalid)
	}
	return out, nil
}

func validateDocument(raw json.RawMessage) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, blueprintenums.Err(blueprintenums.ErrDataInvalid)
	}
	if err = builder.ValidatePage(page); err != nil {
		return nil, blueprintenums.Err(blueprintenums.ErrDataInvalid)
	}

	doc, err := json.Marshal(page)
	if err != nil {
		return nil, blueprintenums.Err(blueprintenums.ErrDataInvalid)
	}
	return doc, nil
}

func toResp(e *blueprintmodel.BlueprintEntity) *blueprintdto.BlueprintResp {
	return &blueprintdto.BlueprintResp{
		ID:            e.ID,
		Name:          e.Name,
		Kind:          e.Kind,
		DraftVersion:  e.DraftVersion,
		DraftDocument: e.DraftDocument,
		UpdatedAt:     e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
