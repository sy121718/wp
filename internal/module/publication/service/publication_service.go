package pubservice

import (
	"strings"

	"go_wp/internal/module/publication/contract"
	"go_wp/internal/module/publication/dto"
	"go_wp/internal/module/publication/enums"
	"go_wp/internal/module/publication/model"
	"go_wp/pkg/pathkit"
	"go_wp/pkg/utils"
)

var _ pubcontract.PublicationService = (*Service)(nil)

type Service struct {
	model *pubmodel.Model
}

func NewService(model *pubmodel.Model) *Service {
	return &Service{model: model}
}

func (s *Service) Model() *pubmodel.Model {
	return s.model
}

func normalizePath(raw string) (string, error) {
	p, err := pathkit.NormalizeRoutePath(raw)
	if err != nil {
		return "", pubenums.Err(pubenums.ErrInvalidParam)
	}
	return p, nil
}

func strPtr(s string) *string { return &s }

func routeResp(e *pubmodel.RouteEntity) *pubdto.RouteResp {
	return &pubdto.RouteResp{
		ProjectID: e.ProjectID, Path: e.Path, PageID: e.PageID,
		PresentationID: e.PresentationID,
		RouteKind:      e.RouteKind, ArtifactID: e.ArtifactID, UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
}

type routeOwner struct {
	pageID         string
	presentationID string

	projectID string
}

func parseRouteOwner(pageID, presentationID string) (routeOwner, error) {
	o := routeOwner{
		pageID:         strings.TrimSpace(pageID),
		presentationID: strings.TrimSpace(presentationID),
	}
	if (o.pageID == "") == (o.presentationID == "") {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	return o, nil
}

func (o routeOwner) sourceType() string {
	if o.projectID != "" {
		return pubdto.ReceiptSourceProject
	}
	if o.presentationID != "" {
		return pubdto.ReceiptSourcePresentation
	}
	return pubdto.ReceiptSourcePage
}

func (o routeOwner) sourceID() string {
	if o.projectID != "" {
		return o.projectID
	}
	if o.presentationID != "" {
		return o.presentationID
	}
	return o.pageID
}

func (o routeOwner) pageIDPtr() *string {
	if o.pageID == "" {
		return nil
	}
	return strPtr(o.pageID)
}

func (o routeOwner) presentationIDPtr() *string {
	if o.presentationID == "" {
		return nil
	}
	return strPtr(o.presentationID)
}

func (o routeOwner) match() (string, []any) {
	if o.presentationID != "" {
		return "presentation_id = ?", []any{o.presentationID}
	}
	return "page_id = ?", []any{o.pageID}
}
