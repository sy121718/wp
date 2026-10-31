package purchaseservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	masterdataenums "go_wp/internal/module/masterdata/enums"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasemodel "go_wp/internal/module/purchase/model"

	"go_wp/pkg/rls"
)

const (
	maxSourceCodeLen = 16

	maxSourceConfigBytes = 64 * 1024

	defaultSourcePageSize = 50
	maxSourcePageSize     = 200
)

func (s *Service) CreateSource(ctx context.Context, req *purchasedto.CreateSourceReq) (res *purchasedto.SourceResp, err error) {
	if req == nil {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceNameRequired)
	}
	code, err := normalizeSourceCode(req.Code)
	if err != nil {
		return nil, err
	}
	sourceType, err := normalizeSourceType(req.Type)
	if err != nil {
		return nil, err
	}
	status, err := normalizeSourceStatus(req.Status)
	if err != nil {
		return nil, err
	}
	related, err := resolveSourceRelatedParty(sourceType, false, req.RelatedParty)
	if err != nil {
		return nil, err
	}
	settle, err := normalizeSettlePrice(sourceType, req.SettlePrice)
	if err != nil {
		return nil, err
	}
	config, err := normalizeSourceConfig(req.Config)
	if err != nil {
		return nil, err
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if taken, cerr := s.m.SourceCodeExists(ctx, projectID, code, ""); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceCodeTaken)
	}
	now := time.Now().UTC()
	e := &purchasemodel.SourceEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Code: code, Name: name, Type: sourceType,
		RelatedParty: related, SettlePrice: settle, Status: status,
		Config: config, Sort: req.Sort,
		Metadata:  orJSON(req.Metadata, "{}"),
		CreatedAt: now, UpdatedAt: now,
	}

	change := sourceChangeInput(e, masterdataenums.ActionCreate, req.OperatorID, nil, sourceChangeSnapshot(e))
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateSourceTx(ctx, tx, e); cerr != nil {
			return cerr
		}
		return s.recordChangesTx(ctx, tx, change)
	}); err != nil {
		return nil, err
	}
	return toSourceResp(e), nil
}

func (s *Service) UpdateSource(ctx context.Context, req *purchasedto.UpdateSourceReq) (res *purchasedto.SourceResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	var newCode string
	if req.Code != nil {
		code, cerr := normalizeSourceCode(*req.Code)
		if cerr != nil {
			return nil, cerr
		}
		if taken, xerr := s.m.SourceCodeExists(ctx, projectID, code, req.ID); xerr != nil {
			return nil, xerr
		} else if taken {
			return nil, purchaseenums.Err(purchaseenums.ErrSourceCodeTaken)
		}
		newCode = code
	}

	var updated *purchasemodel.SourceEntity

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.m.LockSourceTx(ctx, tx, req.ID, projectID)
		if lerr != nil {
			return mapSourceNotFound(lerr)
		}

		before := sourceChangeSnapshot(e)
		if req.Code != nil {
			e.Code = newCode
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return purchaseenums.Err(purchaseenums.ErrSourceNameRequired)
			}
			e.Name = name
		}

		sourceType := e.Type
		if req.Type != nil {
			st, terr := normalizeSourceType(*req.Type)
			if terr != nil {
				return terr
			}
			sourceType = st
		}
		related, rerr := resolveSourceRelatedParty(sourceType, e.RelatedParty, req.RelatedParty)
		if rerr != nil {
			return rerr
		}

		if req.ClearSettlePrice {
			e.SettlePrice = nil
		}
		if req.SettlePrice != nil {
			if sourceType != purchaseenums.SourceTypeInternal {
				return purchaseenums.Err(purchaseenums.ErrSourceSettleNotInternal)
			}
			if *req.SettlePrice < 0 {
				return purchaseenums.Err(purchaseenums.ErrSourceSettleInvalid)
			}
			e.SettlePrice = req.SettlePrice
		}
		if sourceType != purchaseenums.SourceTypeInternal && e.SettlePrice != nil {
			return purchaseenums.Err(purchaseenums.ErrSourceSettleNotInternal)
		}
		if req.Status != nil {
			status, serr := normalizeSourceStatus(*req.Status)
			if serr != nil {
				return serr
			}
			e.Status = status
		}
		if req.Config != nil {
			config, cerr := normalizeSourceConfig(req.Config)
			if cerr != nil {
				return cerr
			}
			e.Config = config
		}
		if req.Sort != nil {
			e.Sort = *req.Sort
		}
		if req.Metadata != nil {
			e.Metadata = orJSON(req.Metadata, "{}")
		}
		e.Type = sourceType
		e.RelatedParty = related
		e.UpdatedAt = time.Now().UTC()
		if uerr := s.m.UpdateSourceTx(ctx, tx, e); uerr != nil {
			return uerr
		}

		if cerr := s.recordChangesTx(ctx, tx, sourceChangeInput(e, masterdataenums.ActionUpdate,
			req.OperatorID, before, sourceChangeSnapshot(e))); cerr != nil {
			return cerr
		}
		updated = e
		return nil
	})
	if err != nil {
		return nil, err
	}
	return toSourceResp(updated), nil
}

func (s *Service) GetSource(ctx context.Context, req *purchasedto.GetSourceReq) (res *purchasedto.SourceResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetSource(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapSourceNotFound(err)
	}
	return toSourceResp(e), nil
}

func (s *Service) DeleteSource(ctx context.Context, req *purchasedto.DeleteSourceReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}

	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		locked, lerr := s.m.LockSourceTx(ctx, tx, req.ID, projectID)
		if lerr != nil {
			return mapSourceNotFound(lerr)
		}

		before := sourceChangeSnapshot(locked)
		if derr := s.m.DeleteSourceTx(ctx, tx, locked.ID); derr != nil {
			return derr
		}
		return s.recordChangesTx(ctx, tx, sourceChangeInput(locked, masterdataenums.ActionDelete,
			req.OperatorID, before, nil))
	})
}

func (s *Service) ListSources(ctx context.Context, req *purchasedto.ListSourceReq) (list []*purchasedto.SourceResp, err error) {
	filter, page, size, err := s.sourceFilter(ctx, req)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListSources(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*purchasedto.SourceResp, 0, len(rows))
	for _, e := range rows {
		list = append(list, toSourceResp(e))
	}
	return list, nil
}

func (s *Service) CountSources(ctx context.Context, req *purchasedto.ListSourceReq) (n int64, err error) {
	filter, _, _, err := s.sourceFilter(ctx, req)
	if err != nil {
		return 0, err
	}
	return s.m.CountSources(ctx, filter)
}

func (s *Service) sourceFilter(ctx context.Context, req *purchasedto.ListSourceReq) (filter purchasemodel.SourceFilter, page, size int, err error) {
	page, size = 1, defaultSourcePageSize
	if req != nil {
		filter.Keyword = strings.TrimSpace(req.Keyword)
		if strings.TrimSpace(req.Type) != "" {
			sourceType, terr := normalizeSourceType(req.Type)
			if terr != nil {
				return filter, page, size, terr
			}
			filter.Type = sourceType
		}
		related, rerr := parseRelatedPartyFilter(req.RelatedParty)
		if rerr != nil {
			return filter, page, size, rerr
		}
		filter.RelatedParty = related
		if strings.TrimSpace(req.Status) != "" {
			status, serr := normalizeSourceStatus(req.Status)
			if serr != nil {
				return filter, page, size, serr
			}
			filter.Status = status
		} else if !req.IncludeDisabled {
			filter.Status = purchaseenums.SourceStatusActive
		}
		if req.Page > 0 {
			page = req.Page
		}
		if req.Size > 0 {
			size = req.Size
			if size > maxSourcePageSize {
				size = maxSourcePageSize
			}
		}
	} else {
		filter.Status = purchaseenums.SourceStatusActive
	}
	projectID, perr := s.resolveProjectID(ctx, projectIDOf(req))
	if perr != nil {
		return filter, page, size, perr
	}
	filter.ProjectID = projectID
	return filter, page, size, nil
}

func (s *Service) SourceSummary(ctx context.Context, req *purchasedto.SourceSummaryReq) (res *purchasedto.SourceSummaryResp, err error) {
	projectID, err := s.resolveProjectID(ctx, projectIDOfSummary(req))
	if err != nil {
		return nil, err
	}
	rows, err := s.m.SummarySources(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = &purchasedto.SourceSummaryResp{
		ProjectID: projectID,
		Groups:    make([]*purchasedto.SourceSummaryGroupResp, 0, len(rows)),
	}
	for _, row := range rows {
		res.Total += row.Count
		switch row.Type {
		case purchaseenums.SourceTypeInternal:
			res.Internal += row.Count
		case purchaseenums.SourceTypeExternal:
			res.External += row.Count
		}
		if row.RelatedParty {
			res.RelatedParty += row.Count
		} else {
			res.Unrelated += row.Count
		}
		res.Groups = append(res.Groups, &purchasedto.SourceSummaryGroupResp{
			Type: row.Type, RelatedParty: row.RelatedParty, Count: row.Count,
		})
	}
	yes := true
	settle, cerr := s.m.CountSources(ctx, purchasemodel.SourceFilter{
		ProjectID: projectID, HasSettlePrice: &yes,
	})
	if cerr != nil {
		return nil, cerr
	}
	res.SettlePriced = settle
	return res, nil
}

func projectIDOf(req *purchasedto.ListSourceReq) string {
	if req == nil {
		return ""
	}
	return req.ProjectID
}

func projectIDOfSummary(req *purchasedto.SourceSummaryReq) string {
	if req == nil {
		return ""
	}
	return req.ProjectID
}

func normalizeSourceCode(code string) (out string, err error) {
	out = strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", purchaseenums.Err(purchaseenums.ErrSourceCodeRequired)
	}
	if len(out) > maxSourceCodeLen {
		return "", purchaseenums.Err(purchaseenums.ErrSourceCodeInvalid)
	}
	for _, r := range out {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", purchaseenums.Err(purchaseenums.ErrSourceCodeInvalid)
	}
	return out, nil
}

func normalizeSourceType(sourceType string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "":
		return purchaseenums.SourceTypeExternal, nil
	case purchaseenums.SourceTypeExternal:
		return purchaseenums.SourceTypeExternal, nil
	case purchaseenums.SourceTypeInternal:
		return purchaseenums.SourceTypeInternal, nil
	default:
		return "", purchaseenums.Err(purchaseenums.ErrSourceTypeInvalid)
	}
}

func normalizeSourceStatus(status string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return purchaseenums.SourceStatusActive, nil
	case purchaseenums.SourceStatusActive:
		return purchaseenums.SourceStatusActive, nil
	case purchaseenums.SourceStatusDisabled:
		return purchaseenums.SourceStatusDisabled, nil
	default:
		return "", purchaseenums.Err(purchaseenums.ErrSourceStatusInvalid)
	}
}

func resolveSourceRelatedParty(sourceType string, current bool, requested *bool) (out bool, err error) {
	if sourceType == purchaseenums.SourceTypeInternal {
		if requested != nil && !*requested {
			return false, purchaseenums.Err(purchaseenums.ErrSourceInternalNotRelated)
		}
		return true, nil
	}
	if requested == nil {
		return current, nil
	}
	return *requested, nil
}

func normalizeSettlePrice(sourceType string, price *float64) (out *float64, err error) {
	if price == nil {
		return nil, nil
	}
	if sourceType != purchaseenums.SourceTypeInternal {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceSettleNotInternal)
	}
	if *price < 0 {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceSettleInvalid)
	}
	return price, nil
}

func normalizeSourceConfig(raw json.RawMessage) (out json.RawMessage, err error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	if len(raw) > maxSourceConfigBytes {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceConfigInvalid)
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceConfigInvalid)
	}
	compact, merr := json.Marshal(obj)
	if merr != nil {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceConfigInvalid)
	}
	return json.RawMessage(compact), nil
}

func parseRelatedPartyFilter(value string) (out *bool, err error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return nil, nil
	case "true", "1":
		yes := true
		return &yes, nil
	case "false", "0":
		no := false
		return &no, nil
	default:
		return nil, purchaseenums.Err(purchaseenums.ErrSourceFilterInvalid)
	}
}

func mapSourceNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return purchaseenums.Err(purchaseenums.ErrSourceNotFound)
	}
	return err
}

func toSourceResp(e *purchasemodel.SourceEntity) *purchasedto.SourceResp {
	if e == nil {
		return nil
	}
	return &purchasedto.SourceResp{
		ID: e.ID, ProjectID: e.ProjectID, Code: e.Code, Name: e.Name,
		Type: e.Type, RelatedParty: e.RelatedParty, SettlePrice: e.SettlePrice,
		Status: e.Status, Config: orJSON(e.Config, "{}"), Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
