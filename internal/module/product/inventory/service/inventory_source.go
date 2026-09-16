// inventory_source.go — 货源读写（issue #17 验收 1/2/4）。
//
// 「一张表承载所有进货来源」是本文件维护的口径：外部供应商、集团内关联公司、自家工厂
// 登记在同一处，靠 type + related_party 两个结构化字段区分，而不是靠命名约定或备注文字。
//
// 三条服务端不变量（数据库 CHECK 兜底，服务层给可读的业务错误）：
//
//	· 内部货源恒为关联方 —— 内部交易必须能被关联方报表捕获，不允许出现「内部但非关联」；
//	  显式取消被拒绝（ErrSourceInternalNotRelated），而不是静默改回去。
//	· 结算价只属于内部货源 —— 外部供应商的成本口径是采购价（采购单行上的历史快照），
//	  给它一个结算价会让两套成本口径打架；改类型为外部时必须显式清空（ClearSettlePrice）。
//	· config 必须是 JSON 对象 —— 它承载异构对接扩展信息，插件/适配器按键读取，
//	  标量与数组都没有键可读，等于写进去读不出来。
package inventoryservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	masterdataenums "go_wp/internal/module/masterdata/enums"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
)

const (
	// maxSourceCodeLen 货源编码长度上限。
	maxSourceCodeLen = 16
	// maxSourceConfigBytes 对接配置（config）体积上限：扩展信息不是数据仓库。
	maxSourceConfigBytes = 64 * 1024
	// 货源列表分页。
	defaultSourcePageSize = 50
	maxSourcePageSize     = 200
)

// CreateSource 新建货源（验收 1/2）。
func (s *Service) CreateSource(ctx context.Context, req *inventorydto.CreateSourceReq) (res *inventorydto.SourceResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(inventoryenums.ErrSourceNameRequired)
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
		return nil, errors.New(inventoryenums.ErrSourceCodeTaken)
	}
	now := time.Now().UTC()
	e := &inventorymodel.SourceEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Code: code, Name: name, Type: sourceType,
		RelatedParty: related, SettlePrice: settle, Status: status,
		Config: config, Sort: req.Sort,
		Metadata:  orJSON(req.Metadata, "{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateSource(ctx, e); err != nil {
		return nil, err
	}
	// issue #19：新增货源 → 变更记录（编码 / 名称 / 类型 / 关联方 / 结算价 / 状态 / 对接配置）。
	if err = s.recordChanges(ctx, sourceChangeInput(e, masterdataenums.ActionCreate,
		req.OperatorID, nil, sourceChangeSnapshot(e))); err != nil {
		return nil, err
	}
	return toSourceResp(e), nil
}

// UpdateSource 修改货源（逐字段可选；nil = 本次不改）。
func (s *Service) UpdateSource(ctx context.Context, req *inventorydto.UpdateSourceReq) (res *inventorydto.SourceResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetSource(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapSourceNotFound(err)
	}
	// issue #19：改前快照必须在任何赋值之前取（之后的字段级 diff 以它为基准）。
	before := sourceChangeSnapshot(e)
	if req.Code != nil {
		code, cerr := normalizeSourceCode(*req.Code)
		if cerr != nil {
			return nil, cerr
		}
		if taken, xerr := s.m.SourceCodeExists(ctx, e.ProjectID, code, e.ID); xerr != nil {
			return nil, xerr
		} else if taken {
			return nil, errors.New(inventoryenums.ErrSourceCodeTaken)
		}
		e.Code = code
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(inventoryenums.ErrSourceNameRequired)
		}
		e.Name = name
	}
	// 类型可能变：先算出最终类型，再据此校验关联方与结算价（顺序不能反）。
	sourceType := e.Type
	if req.Type != nil {
		if sourceType, err = normalizeSourceType(*req.Type); err != nil {
			return nil, err
		}
	}
	related, err := resolveSourceRelatedParty(sourceType, e.RelatedParty, req.RelatedParty)
	if err != nil {
		return nil, err
	}
	// 结算价：显式清空优先；显式赋值必须与最终类型相容；改类型为外部时必须显式清空。
	if req.ClearSettlePrice {
		e.SettlePrice = nil
	}
	if req.SettlePrice != nil {
		if sourceType != inventoryenums.SourceTypeInternal {
			return nil, errors.New(inventoryenums.ErrSourceSettleNotInternal)
		}
		if *req.SettlePrice < 0 {
			return nil, errors.New(inventoryenums.ErrSourceSettleInvalid)
		}
		e.SettlePrice = req.SettlePrice
	}
	if sourceType != inventoryenums.SourceTypeInternal && e.SettlePrice != nil {
		return nil, errors.New(inventoryenums.ErrSourceSettleNotInternal)
	}
	if req.Status != nil {
		status, serr := normalizeSourceStatus(*req.Status)
		if serr != nil {
			return nil, serr
		}
		e.Status = status
	}
	if req.Config != nil {
		config, cerr := normalizeSourceConfig(req.Config)
		if cerr != nil {
			return nil, cerr
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
	if err = s.m.UpdateSource(ctx, e); err != nil {
		return nil, err
	}
	// issue #19：字段级变更留痕（类型 / 关联方 / 结算价 / 状态 / 对接配置 / 编码 / 名称）。
	// 只写真正变化的字段 —— 打开表单什么都没改就保存，审计里不留痕迹。
	if err = s.recordChanges(ctx, sourceChangeInput(e, masterdataenums.ActionUpdate,
		req.OperatorID, before, sourceChangeSnapshot(e))); err != nil {
		return nil, err
	}
	return toSourceResp(e), nil
}

// GetSource 货源详情。
func (s *Service) GetSource(ctx context.Context, req *inventorydto.GetSourceReq) (res *inventorydto.SourceResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
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

// DeleteSource 删除货源（硬删除；被采购单引用的守卫由 issue #18 在服务层补）。
func (s *Service) DeleteSource(ctx context.Context, req *inventorydto.DeleteSourceReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	e, err := s.m.GetSource(ctx, req.ID, projectID)
	if err != nil {
		return mapSourceNotFound(err)
	}
	// issue #19：删除前取快照（删完之后连名称都查不到了），删成功后落 delete 记录。
	before := sourceChangeSnapshot(e)
	if err = s.m.DeleteSource(ctx, e.ID); err != nil {
		return err
	}
	return s.recordChanges(ctx, sourceChangeInput(e, masterdataenums.ActionDelete, req.OperatorID, before, nil))
}

// ListSources 货源列表（验收 4：类型 / 关联方 / 状态 / 关键词都是可组合的筛选维度）。
//
// 缺省只列启用中的货源（停用是「不再选用」的标记）；IncludeDisabled 显式打开才列停用的。
func (s *Service) ListSources(ctx context.Context, req *inventorydto.ListSourceReq) (list []*inventorydto.SourceResp, err error) {
	filter := inventorymodel.SourceFilter{}
	page, size := 1, defaultSourcePageSize
	if req != nil {
		filter.Keyword = strings.TrimSpace(req.Keyword)
		if strings.TrimSpace(req.Type) != "" {
			sourceType, terr := normalizeSourceType(req.Type)
			if terr != nil {
				return nil, terr
			}
			filter.Type = sourceType
		}
		related, rerr := parseRelatedPartyFilter(req.RelatedParty)
		if rerr != nil {
			return nil, rerr
		}
		filter.RelatedParty = related
		if strings.TrimSpace(req.Status) != "" {
			status, serr := normalizeSourceStatus(req.Status)
			if serr != nil {
				return nil, serr
			}
			filter.Status = status
		} else if !req.IncludeDisabled {
			filter.Status = inventoryenums.SourceStatusActive
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
		filter.Status = inventoryenums.SourceStatusActive
	}
	projectID, err := s.resolveProjectID(ctx, projectIDOf(req))
	if err != nil {
		return nil, err
	}
	filter.ProjectID = projectID
	rows, err := s.m.ListSources(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.SourceResp, 0, len(rows))
	for _, e := range rows {
		list = append(list, toSourceResp(e))
	}
	return list, nil
}

// SourceSummary 货源关联方统计（验收 4「关联方标志可用于报表区分」的数据出口）。
//
// 口径：统计**不带状态过滤** —— 停用只是「不再选用」，历史关联交易的口径不该随停用而变。
// 两组数字交叉给出报表区分能力：类型（内部 / 外部）× 关联方（是 / 否）。
func (s *Service) SourceSummary(ctx context.Context, req *inventorydto.SourceSummaryReq) (res *inventorydto.SourceSummaryResp, err error) {
	projectID, err := s.resolveProjectID(ctx, projectIDOfSummary(req))
	if err != nil {
		return nil, err
	}
	rows, err := s.m.SummarySources(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = &inventorydto.SourceSummaryResp{
		ProjectID: projectID,
		Groups:    make([]*inventorydto.SourceSummaryGroupResp, 0, len(rows)),
	}
	for _, row := range rows {
		res.Total += row.Count
		switch row.Type {
		case inventoryenums.SourceTypeInternal:
			res.Internal += row.Count
		case inventoryenums.SourceTypeExternal:
			res.External += row.Count
		}
		if row.RelatedParty {
			res.RelatedParty += row.Count
		} else {
			res.Unrelated += row.Count
		}
		res.Groups = append(res.Groups, &inventorydto.SourceSummaryGroupResp{
			Type: row.Type, RelatedParty: row.RelatedParty, Count: row.Count,
		})
	}
	yes := true
	settle, cerr := s.m.CountSources(ctx, inventorymodel.SourceFilter{
		ProjectID: projectID, HasSettlePrice: &yes,
	})
	if cerr != nil {
		return nil, cerr
	}
	res.SettlePriced = settle
	return res, nil
}

// projectIDOf / projectIDOfSummary 从可空入参里取工程（nil 安全）。
func projectIDOf(req *inventorydto.ListSourceReq) string {
	if req == nil {
		return ""
	}
	return req.ProjectID
}

func projectIDOfSummary(req *inventorydto.SourceSummaryReq) string {
	if req == nil {
		return ""
	}
	return req.ProjectID
}

// normalizeSourceCode 归一并校验货源编码：大写字母 / 数字 / 下划线，1..maxSourceCodeLen。
func normalizeSourceCode(code string) (out string, err error) {
	out = strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", errors.New(inventoryenums.ErrSourceCodeRequired)
	}
	if len(out) > maxSourceCodeLen {
		return "", errors.New(inventoryenums.ErrSourceCodeInvalid)
	}
	for _, r := range out {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", errors.New(inventoryenums.ErrSourceCodeInvalid)
	}
	return out, nil
}

// normalizeSourceType 归一货源类型：空串取默认「外部供应商」，其余必须是内置取值。
func normalizeSourceType(sourceType string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "":
		return inventoryenums.SourceTypeExternal, nil
	case inventoryenums.SourceTypeExternal:
		return inventoryenums.SourceTypeExternal, nil
	case inventoryenums.SourceTypeInternal:
		return inventoryenums.SourceTypeInternal, nil
	default:
		return "", errors.New(inventoryenums.ErrSourceTypeInvalid)
	}
}

// normalizeSourceStatus 归一货源状态：空串取默认启用，其余必须是内置取值。
func normalizeSourceStatus(status string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return inventoryenums.SourceStatusActive, nil
	case inventoryenums.SourceStatusActive:
		return inventoryenums.SourceStatusActive, nil
	case inventoryenums.SourceStatusDisabled:
		return inventoryenums.SourceStatusDisabled, nil
	default:
		return "", errors.New(inventoryenums.ErrSourceStatusInvalid)
	}
}

// resolveSourceRelatedParty 解析关联方标志。
//
// 内部货源恒为关联方（显式取消被拒绝）；外部货源按入参，未给入参时保持当前值
// （新建时的「当前值」是 false）。
func resolveSourceRelatedParty(sourceType string, current bool, requested *bool) (out bool, err error) {
	if sourceType == inventoryenums.SourceTypeInternal {
		if requested != nil && !*requested {
			return false, errors.New(inventoryenums.ErrSourceInternalNotRelated)
		}
		return true, nil
	}
	if requested == nil {
		return current, nil
	}
	return *requested, nil
}

// normalizeSettlePrice 校验内部结算价：只属于内部货源，且非负。
func normalizeSettlePrice(sourceType string, price *float64) (out *float64, err error) {
	if price == nil {
		return nil, nil
	}
	if sourceType != inventoryenums.SourceTypeInternal {
		return nil, errors.New(inventoryenums.ErrSourceSettleNotInternal)
	}
	if *price < 0 {
		return nil, errors.New(inventoryenums.ErrSourceSettleInvalid)
	}
	return price, nil
}

// normalizeSourceConfig 归一对接配置：必须是 JSON 对象（空即 {}），并压缩重写。
func normalizeSourceConfig(raw json.RawMessage) (out json.RawMessage, err error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	if len(raw) > maxSourceConfigBytes {
		return nil, errors.New(inventoryenums.ErrSourceConfigInvalid)
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New(inventoryenums.ErrSourceConfigInvalid)
	}
	compact, merr := json.Marshal(obj)
	if merr != nil {
		return nil, errors.New(inventoryenums.ErrSourceConfigInvalid)
	}
	return json.RawMessage(compact), nil
}

// parseRelatedPartyFilter 解析报表筛选里的关联方三态："" 全部 / true 仅关联方 / false 仅非关联方。
//
// 参数缺失与参数为空必须是同一件事（不过滤）；写错的取值一律拒绝，
// 绝不把「拼错的筛选条件」静默当成「不过滤」——那会让报表悄悄给出全量数字。
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
		return nil, errors.New(inventoryenums.ErrSourceFilterInvalid)
	}
}

// mapSourceNotFound 行不存在 → 业务错误，其余原样透出。
func mapSourceNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrSourceNotFound)
	}
	return err
}

// toSourceResp 实体 → 响应。
func toSourceResp(e *inventorymodel.SourceEntity) *inventorydto.SourceResp {
	if e == nil {
		return nil
	}
	return &inventorydto.SourceResp{
		ID: e.ID, ProjectID: e.ProjectID, Code: e.Code, Name: e.Name,
		Type: e.Type, RelatedParty: e.RelatedParty, SettlePrice: e.SettlePrice,
		Status: e.Status, Config: orJSON(e.Config, "{}"), Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
