package inventorydto

import (
	"strconv"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

func formBool(v string) bool { return strings.TrimSpace(v) != "" }

func formInt(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

const DefaultDeductPriority = 100

type WarehouseFormReq struct {
	ProjectID string `form:"projectId"`
	ID        string `form:"id"`
	Code      string `form:"code"`
	Name      string `form:"name"`
	Sort      string `form:"sort"`
	Status    string `form:"status"`
	Type      string `form:"type"`
	IsDefault string `form:"isDefault"`

	DeductPriority string `form:"deductPriority"`

	Provider        string `form:"provider"`
	ExternalCode    string `form:"externalCode"`
	Address         string `form:"address"`
	Contact         string `form:"contact"`
	AllowsShipping  string `form:"allowsShipping"`
	APICredential   string `form:"apiCredential"`
	ClearCredential string `form:"clearCredential"`
	SecretRef       string `form:"secretRef"`
}

func (r *WarehouseFormReq) ThirdParty(typ string) *WarehouseThirdPartyReq {
	if typ != inventoryenums.WarehouseTypeThirdParty {
		return nil
	}
	return &WarehouseThirdPartyReq{
		Provider:        strings.TrimSpace(r.Provider),
		ExternalCode:    strings.TrimSpace(r.ExternalCode),
		Address:         strings.TrimSpace(r.Address),
		Contact:         strings.TrimSpace(r.Contact),
		AllowsShipping:  formBool(r.AllowsShipping),
		APICredential:   strings.TrimSpace(r.APICredential),
		ClearCredential: formBool(r.ClearCredential),
		SecretRef:       strings.TrimSpace(r.SecretRef),
	}
}

func (r *WarehouseFormReq) CreateReq() *CreateWarehouseReq {
	typ := strings.TrimSpace(r.Type)
	return &CreateWarehouseReq{
		ProjectID:      strings.TrimSpace(r.ProjectID),
		Code:           strings.TrimSpace(r.Code),
		Name:           strings.TrimSpace(r.Name),
		Type:           typ,
		IsDefault:      formBool(r.IsDefault),
		Sort:           formInt(r.Sort, 0),
		DeductPriority: formInt(r.DeductPriority, DefaultDeductPriority),
		ThirdParty:     r.ThirdParty(typ),
	}
}

func (r *WarehouseFormReq) UpdateReq() *UpdateWarehouseReq {
	code, name := strings.TrimSpace(r.Code), strings.TrimSpace(r.Name)
	status, typ := strings.TrimSpace(r.Status), strings.TrimSpace(r.Type)
	if status == "" {
		status = inventoryenums.StatusActive
	}
	if typ == "" {
		typ = inventoryenums.WarehouseTypeSelf
	}
	sort := formInt(r.Sort, 0)
	priority := formInt(r.DeductPriority, DefaultDeductPriority)
	return &UpdateWarehouseReq{
		ID:             strings.TrimSpace(r.ID),
		Code:           &code,
		Name:           &name,
		Status:         &status,
		Type:           &typ,
		Sort:           &sort,
		DeductPriority: &priority,
		ThirdParty:     r.ThirdParty(typ),
	}
}

type WarehouseDefaultFormReq struct {
	ID string `form:"id"`
}

type WarehouseDeleteFormReq struct {
	ID        string `form:"id"`
	ProjectID string `form:"projectId"`
}

type WarehouseBulkFormReq struct {
	ProjectID string   `form:"projectId"`
	IDs       []string `form:"ids"`
}
