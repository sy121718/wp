package inventoryservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/rls"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	"go_wp/pkg/crypto"
)

func (s *Service) CreateWarehouse(ctx context.Context, req *inventorydto.CreateWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseNameRequired)
	}
	code, err := normalizeCode(req.Code)
	if err != nil {
		return nil, err
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		return nil, err
	}
	typ, err := normalizeWarehouseType(req.Type)
	if err != nil {
		return nil, err
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if taken, cerr := s.m.CodeExists(ctx, projectID, code, ""); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseCodeTaken)
	}

	existing, lerr := s.m.ListWarehouses(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	asDefault := req.IsDefault || len(existing) == 0

	if asDefault && typ == inventoryenums.WarehouseTypeVirtual {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseTypeVirtualDefault)
	}
	config, err := s.applyThirdPartyConfig(typ, req.ThirdParty, nil)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	priority, perr := normalizeDeductPriority(req.DeductPriority)
	if perr != nil {
		return nil, perr
	}
	e := &inventorymodel.WarehouseEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Code: code, Name: name, Type: typ, Status: status,
		IsDefault: asDefault, Sort: req.Sort, DeductPriority: priority,
		Config:    config,
		Metadata:  orJSON(req.Metadata, "{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateWarehouse(ctx, e, asDefault); err != nil {
		return nil, err
	}
	return toWarehouseResp(e), nil
}

const defaultDeductPriority = 100

func normalizeDeductPriority(raw int) (int, error) {
	if raw < 0 {
		return 0, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	return raw, nil
}

func (s *Service) UpdateWarehouse(ctx context.Context, req *inventorydto.UpdateWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetWarehouse(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapWarehouseNotFound(err)
	}
	if req.Code != nil {
		code, cerr := normalizeCode(*req.Code)
		if cerr != nil {
			return nil, cerr
		}
		if taken, xerr := s.m.CodeExists(ctx, e.ProjectID, code, e.ID); xerr != nil {
			return nil, xerr
		} else if taken {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseCodeTaken)
		}
		e.Code = code
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseNameRequired)
		}
		e.Name = name
	}
	if req.DeductPriority != nil {
		priority, perr := normalizeDeductPriority(*req.DeductPriority)
		if perr != nil {
			return nil, perr
		}
		e.DeductPriority = priority
	}
	if req.Status != nil {
		status, serr := normalizeStatus(*req.Status)
		if serr != nil {
			return nil, serr
		}

		if e.IsDefault && status != inventoryenums.StatusActive {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseIsDefault)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	if req.Type != nil {
		typ, terr := normalizeWarehouseType(*req.Type)
		if terr != nil {
			return nil, terr
		}

		if e.IsDefault && typ == inventoryenums.WarehouseTypeVirtual {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseTypeVirtualDefault)
		}
		e.Type = typ
	}

	config, cerr := s.applyThirdPartyConfig(e.Type, req.ThirdParty, e.Config)
	if cerr != nil {
		return nil, cerr
	}
	e.Config = config
	if req.Metadata != nil {
		e.Metadata = orJSON(req.Metadata, "{}")
	}

	if req.IsDefault != nil && !*req.IsDefault && e.IsDefault {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseIsDefault)
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateWarehouse(ctx, e); err != nil {
		return nil, err
	}
	if req.IsDefault != nil && *req.IsDefault && !e.IsDefault {
		if err = s.m.SetDefaultWarehouse(ctx, e.ProjectID, e.ID); err != nil {
			return nil, err
		}
		e.IsDefault = true
	}
	return toWarehouseResp(e), nil
}

func (s *Service) GetWarehouse(ctx context.Context, req *inventorydto.GetWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetWarehouse(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapWarehouseNotFound(err)
	}
	return toWarehouseResp(e), nil
}

func (s *Service) ListWarehouses(ctx context.Context, req *inventorydto.ListWarehouseReq) (list []*inventorydto.WarehouseResp, err error) {
	projectID := ""
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
	}
	if projectID == "" {
		if projectID, err = s.resolveProjectID(ctx, ""); err != nil {
			return nil, err
		}
	}
	rows, err := s.m.ListWarehouses(ctx, projectID)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.WarehouseResp, 0, len(rows))
	for _, e := range rows {
		list = append(list, toWarehouseResp(e))
	}
	return list, nil
}

func (s *Service) DeleteWarehouse(ctx context.Context, req *inventorydto.DeleteWarehouseReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	e, err := s.m.GetWarehouse(ctx, req.ID, projectID)
	if err != nil {
		return mapWarehouseNotFound(err)
	}
	if e.IsDefault {
		return inventoryenums.Err(inventoryenums.ErrWarehouseIsDefault)
	}

	n, cerr := s.m.CountNonZeroStocks(ctx, e.ID, e.ProjectID)
	if cerr != nil {
		return cerr
	}
	if n > 0 {
		return inventoryenums.Err(inventoryenums.ErrWarehouseHasStock)
	}
	return s.m.DeleteWarehouse(ctx, e.ID, e.ProjectID)
}

func (s *Service) resolveWarehouse(ctx context.Context, projectID, warehouseID string) (e *inventorymodel.WarehouseEntity, err error) {
	id := strings.TrimSpace(warehouseID)
	if id != "" {

		pid, perr := s.resolveProjectID(ctx, projectID)
		if perr != nil {
			return nil, perr
		}
		e, err = s.m.GetWarehouse(ctx, id, pid)
		if err != nil {
			return nil, mapWarehouseNotFound(err)
		}
		if e.ProjectID != pid {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseProjectMismatch)
		}
		if e.Status != inventoryenums.StatusActive {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseDisabled)
		}
		return e, nil
	}
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	e, err = s.m.GetDefaultWarehouse(ctx, pid)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseDefaultMissing)
		}
		return nil, err
	}
	if e.Status != inventoryenums.StatusActive {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseDisabled)
	}
	return e, nil
}

func normalizeCode(code string) (out string, err error) {
	out = strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", inventoryenums.Err(inventoryenums.ErrWarehouseCodeRequired)
	}
	if len(out) > maxWarehouseCodeLen {
		return "", inventoryenums.Err(inventoryenums.ErrWarehouseCodeInvalid)
	}
	for _, r := range out {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return "", inventoryenums.Err(inventoryenums.ErrWarehouseCodeInvalid)
	}
	return out, nil
}

func normalizeStatus(status string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return inventoryenums.StatusActive, nil
	case inventoryenums.StatusActive:
		return inventoryenums.StatusActive, nil
	case inventoryenums.StatusDisabled:
		return inventoryenums.StatusDisabled, nil
	default:
		return "", inventoryenums.Err(inventoryenums.ErrWarehouseStatusInvalid)
	}
}

func mapWarehouseNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return inventoryenums.Err(inventoryenums.ErrWarehouseNotFound)
	}
	return err
}

func toWarehouseResp(e *inventorymodel.WarehouseEntity) *inventorydto.WarehouseResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.WarehouseResp{
		ID: e.ID, ProjectID: e.ProjectID, Code: e.Code, Name: e.Name,
		Type: e.Type, Status: e.Status, IsDefault: e.IsDefault, Sort: e.Sort,
		DeductPriority: e.DeductPriority,
		CreatedAt:      e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}

	if e.Type == inventoryenums.WarehouseTypeThirdParty {
		resp.ThirdParty = warehouseThirdPartyResp(e.Config)
	}
	return resp
}

func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

const maxCredentialLen = 512

func normalizeWarehouseType(raw string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return inventoryenums.WarehouseTypeSelf, nil
	case inventoryenums.WarehouseTypeSelf:
		return inventoryenums.WarehouseTypeSelf, nil
	case inventoryenums.WarehouseTypeThirdParty:
		return inventoryenums.WarehouseTypeThirdParty, nil
	case inventoryenums.WarehouseTypeVirtual:
		return inventoryenums.WarehouseTypeVirtual, nil
	default:
		return "", inventoryenums.Err(inventoryenums.ErrWarehouseTypeInvalid)
	}
}

var knownConfigKeys = map[string]bool{
	inventoryenums.WarehouseConfigKeyProvider:       true,
	inventoryenums.WarehouseConfigKeyExternalCode:   true,
	inventoryenums.WarehouseConfigKeyAddress:        true,
	inventoryenums.WarehouseConfigKeyContact:        true,
	inventoryenums.WarehouseConfigKeyAllowsShipping: true,
	inventoryenums.WarehouseConfigKeyAPICipher:      true,
	inventoryenums.WarehouseConfigKeySecretRef:      true,
}

var sensitiveConfigKeys = map[string]bool{
	"apikeycipher": true, "apikey": true, "api_key": true, "apisecret": true,
	"credential": true, "credentials": true, "password": true, "passwd": true,
	"secret": true, "token": true, "accesstoken": true, "access_token": true,
}

func isSensitiveConfigKey(key string) bool {
	return sensitiveConfigKeys[strings.ToLower(strings.TrimSpace(key))]
}

func decodeConfigObject(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return out
	}
	obj, ok := probe.(map[string]any)
	if !ok {
		return out
	}
	return obj
}

func encodeConfigObject(cfg map[string]any) (json.RawMessage, error) {
	if len(cfg) == 0 {
		return json.RawMessage("{}"), nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return json.RawMessage("{}"), nil
	}
	return raw, nil
}

func hasThirdPartyFields(in *inventorydto.WarehouseThirdPartyReq) bool {
	if in == nil {
		return false
	}
	return strings.TrimSpace(in.Provider) != "" || strings.TrimSpace(in.ExternalCode) != "" ||
		strings.TrimSpace(in.Address) != "" || strings.TrimSpace(in.Contact) != "" ||
		strings.TrimSpace(in.APICredential) != "" || strings.TrimSpace(in.SecretRef) != "" ||
		in.AllowsShipping || in.ClearCredential
}

func (s *Service) applyThirdPartyConfig(typ string, in *inventorydto.WarehouseThirdPartyReq,
	old json.RawMessage) (out json.RawMessage, err error) {
	cfg := decodeConfigObject(old)
	if in == nil {
		return encodeConfigObject(cfg)
	}
	if typ != inventoryenums.WarehouseTypeThirdParty {

		if hasThirdPartyFields(in) {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseConfigInvalid)
		}
		return encodeConfigObject(cfg)
	}
	cfg[inventoryenums.WarehouseConfigKeyProvider] = strings.TrimSpace(in.Provider)
	cfg[inventoryenums.WarehouseConfigKeyExternalCode] = strings.TrimSpace(in.ExternalCode)
	cfg[inventoryenums.WarehouseConfigKeyAddress] = strings.TrimSpace(in.Address)
	cfg[inventoryenums.WarehouseConfigKeyContact] = strings.TrimSpace(in.Contact)
	cfg[inventoryenums.WarehouseConfigKeyAllowsShipping] = in.AllowsShipping
	if err = s.applyCredential(cfg, in); err != nil {
		return nil, err
	}
	return encodeConfigObject(cfg)
}

func (s *Service) applyCredential(cfg map[string]any, in *inventorydto.WarehouseThirdPartyReq) (err error) {
	if in.ClearCredential {
		delete(cfg, inventoryenums.WarehouseConfigKeyAPICipher)
		delete(cfg, inventoryenums.WarehouseConfigKeySecretRef)
		return nil
	}
	plain := strings.TrimSpace(in.APICredential)

	if plain != "" && plain != inventoryenums.CredentialMask {
		if verr := validateCredential(plain); verr != nil {
			return verr
		}
		if strings.TrimSpace(s.cipherSecret) == "" {

			return inventoryenums.Err(inventoryenums.ErrWarehouseCredentialKeyMissing)
		}
		cipherText, cerr := crypto.Encrypt(plain, s.cipherSecret)
		if cerr != nil {
			return inventoryenums.Err(inventoryenums.ErrWarehouseCredentialInvalid)
		}
		cfg[inventoryenums.WarehouseConfigKeyAPICipher] = cipherText
		delete(cfg, inventoryenums.WarehouseConfigKeySecretRef)
		return nil
	}
	if ref := strings.TrimSpace(in.SecretRef); ref != "" {
		if strings.ContainsRune(ref, 0) || len(ref) > maxCredentialLen {
			return inventoryenums.Err(inventoryenums.ErrWarehouseCredentialInvalid)
		}
		cfg[inventoryenums.WarehouseConfigKeySecretRef] = ref
		delete(cfg, inventoryenums.WarehouseConfigKeyAPICipher)
		return nil
	}

	return nil
}

func validateCredential(plain string) error {
	if len(plain) > maxCredentialLen {
		return inventoryenums.Err(inventoryenums.ErrWarehouseCredentialInvalid)
	}
	for _, r := range plain {
		if r == 0 || (unicode.IsControl(r) && r != '\t') {
			return inventoryenums.Err(inventoryenums.ErrWarehouseCredentialInvalid)
		}
	}
	return nil
}

func warehouseThirdPartyResp(raw json.RawMessage) *inventorydto.WarehouseThirdPartyResp {
	cfg := decodeConfigObject(raw)
	resp := &inventorydto.WarehouseThirdPartyResp{
		Provider:       stringOf(cfg[inventoryenums.WarehouseConfigKeyProvider]),
		ExternalCode:   stringOf(cfg[inventoryenums.WarehouseConfigKeyExternalCode]),
		Address:        stringOf(cfg[inventoryenums.WarehouseConfigKeyAddress]),
		Contact:        stringOf(cfg[inventoryenums.WarehouseConfigKeyContact]),
		AllowsShipping: boolOf(cfg[inventoryenums.WarehouseConfigKeyAllowsShipping]),
		SecretRef:      stringOf(cfg[inventoryenums.WarehouseConfigKeySecretRef]),
	}
	extras := map[string]any{}
	for key, value := range cfg {
		lower := strings.ToLower(strings.TrimSpace(key))
		if knownConfigKeys[lower] {
			if lower == inventoryenums.WarehouseConfigKeyAPICipher {
				resp.HasCredential = true
			}
			continue
		}

		if isSensitiveConfigKey(lower) {
			resp.HasCredential = true
			continue
		}
		extras[key] = value
	}
	if resp.SecretRef != "" {
		resp.HasCredential = true
	}
	if resp.HasCredential {
		resp.CredentialMasked = inventoryenums.CredentialMask
	}
	if len(extras) > 0 {
		resp.Extras = extras
	}
	return resp
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

// BulkDeleteWarehouses 在一个事务里删除多个仓库：任一失败整批回滚。
//
// 前置校验（默认仓、有库存）在事务内逐条执行；失败时整批不落库。
func (s *Service) BulkDeleteWarehouses(ctx context.Context, req *inventorydto.BulkDeleteWarehouseReq) error {
	if req == nil || len(req.IDs) == 0 {
		return inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	pid, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, pid); serr != nil {
			return serr
		}
		for _, raw := range req.IDs {
			e, gerr := s.m.GetWarehouseTx(ctx, tx, raw, pid)
			if gerr != nil {
				return mapWarehouseNotFound(gerr)
			}
			if e.IsDefault {
				return inventoryenums.Err(inventoryenums.ErrWarehouseIsDefault)
			}
			n, cerr := s.m.CountNonZeroStocksTx(ctx, tx, e.ID, pid)
			if cerr != nil {
				return cerr
			}
			if n > 0 {
				return inventoryenums.Err(inventoryenums.ErrWarehouseHasStock)
			}
			if derr := s.m.DeleteWarehouseTx(ctx, tx, e.ID); derr != nil {
				return derr
			}
		}
		return nil
	})
}
