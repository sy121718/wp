package inventoryservice

import (
	"context"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/utils"
)

const (
	defaultPageSize = 20
	maxPageSize     = 200

	maxWarehouseCodeLen = 8
)

type Service struct {
	m       *inventorymodel.Model
	project projectcontract.ProjectService

	cipherSecret string

	refs productcontract.VariantWarehouseStockPort

	attachments inventorycontract.AttachmentResolver
}

func NewService(m *inventorymodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

func (s *Service) SetAttachmentResolver(resolver inventorycontract.AttachmentResolver) {
	if s == nil {
		return
	}
	s.attachments = resolver
}

func (s *Service) SetVariantWarehouseStocks(refs productcontract.VariantWarehouseStockPort) {
	if s == nil {
		return
	}
	s.refs = refs
}

var (
	_ inventorycontract.InventoryService = (*Service)(nil)

	_ productcontract.VariantAvailabilityPort = (*Service)(nil)
)

func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if strings.TrimSpace(projectID) != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}

func pageArgs(req *inventorydto.ListStockReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultPageSize, maxPageSize)
	return paging.Page, paging.Size
}
