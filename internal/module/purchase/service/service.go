package purchaseservice

import (
	"context"
	"strings"

	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasemodel "go_wp/internal/module/purchase/model"
)

type Service struct {
	m       *purchasemodel.Model
	project projectcontract.ProjectService

	stock purchasecontract.StockPort

	variantCost productcontract.VariantCostPort

	changes masterdatacontract.MasterDataService
}

func NewService(m *purchasemodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

func (s *Service) SetStock(port purchasecontract.StockPort) { s.stock = port }

func (s *Service) SetVariantCost(port productcontract.VariantCostPort) { s.variantCost = port }

func (s *Service) SetMasterDataChanges(port masterdatacontract.MasterDataService) { s.changes = port }

var _ purchasecontract.PurchaseService = (*Service)(nil)

func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if strings.TrimSpace(projectID) != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}

func sourceChangeSnapshot(e *purchasemodel.SourceEntity) masterdatacontract.FieldSnapshot {
	if e == nil {
		return nil
	}
	return masterdatacontract.NewSnapshot(
		"code", e.Code,
		"name", e.Name,
		"type", e.Type,
		"related_party", masterdatacontract.FormatBool(e.RelatedParty),
		"settle_price", masterdatacontract.FormatPricePtr(e.SettlePrice),
		"status", e.Status,
		"config", masterdatacontract.FormatJSON(e.Config),
		"sort", masterdatacontract.FormatInt(e.Sort),
	)
}

func sourceChangeInput(e *purchasemodel.SourceEntity, action, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: e.ProjectID, EntityType: masterdataenums.EntityInventorySource,
		EntityID: e.ID, EntityLabel: e.Name,
		Action: action, Origin: masterdataenums.OriginSource, OperatorID: operator,
		Before: before, After: after,
	}
}

func (s *Service) recordChangesTx(ctx context.Context, tx *gorm.DB, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChangesTx(ctx, tx, inputs)
}

func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

const skuSeparator = "_"

func stripWarehousePrefix(code, warehouseCode string) string {
	trimmed := strings.TrimSpace(code)
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return trimmed
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), prefix+skuSeparator) {
		return trimmed[len(prefix)+len(skuSeparator):]
	}
	return trimmed
}

func normalizeStockSKU(code, warehouseCode string) (bare string, err error) {
	bare = stripWarehousePrefix(code, warehouseCode)
	if bare == "" {
		return "", purchaseenums.Err(purchaseenums.ErrStockSKURequired)
	}
	return bare, nil
}
