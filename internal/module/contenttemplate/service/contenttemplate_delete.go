package contenttemplateservice

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// deleteBlockedError 是整批删除被拒绝的事实：主消息只取一句
// （单条取该条的具体原因，多项取公共整批文案），逐条原因交给共享失败出口。
type deleteBlockedError struct {
	key   string
	items []facing.BlockedItem
}

func (e *deleteBlockedError) Error() string { return e.key }

func (e *deleteBlockedError) Facing() (string, string) { return e.key, "" }

func (e *deleteBlockedError) HTTPStatus() int { return deleteBlockedStatus(e.items, e.key) }

func (e *deleteBlockedError) BlockedItems() []facing.BlockedItem { return e.items }

func deleteBlockedStatus(items []facing.BlockedItem, key string) int {
	if key == contenttemplateenums.ErrReferenceUnavailable {
		return http.StatusInternalServerError
	}
	status := http.StatusBadRequest
	for _, item := range items {
		switch item.ReasonKey {
		case contenttemplateenums.ErrTemplateInUse, contenttemplateenums.ErrStructureTemplateInUse:
			return http.StatusConflict
		case contenttemplateenums.ErrNotFound:
			status = http.StatusNotFound
		}
	}
	return status
}

// deleteRequested 表示本次请求的目标数；单条请求取该条的具体原因作主消息。
func deleteReject(requested int, items []facing.BlockedItem) error {
	if len(items) == 0 {
		return nil
	}
	key := contenttemplateenums.ErrBulkRejected
	if requested == 1 && len(items) == 1 {
		key = items[0].ReasonKey
	}
	return &deleteBlockedError{key: key, items: items}
}

// SetReferencePort 注入跨模块引用反查端口；未注入时删除 fail-closed。
func (s *Service) SetReferencePort(p contenttemplatecontract.TemplateReferenceTxReader) {
	s.refs = p
}

var _ contenttemplatecontract.TemplateExistenceTxReader = (*Service)(nil)

// MissingTemplateIDsTx 在调用方事务内复核这些模板仍存在，返回缺失的 id。
func (s *Service) MissingTemplateIDsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) ([]string, error) {
	ids := cleanIDList(templateIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	return s.m.MissingTemplateIDsTx(ctx, tx, projectID, ids)
}

// MissingTemplateVersionIDsTx 在调用方事务内复核这些模板版本仍存在。
func (s *Service) MissingTemplateVersionIDsTx(ctx context.Context, tx *gorm.DB, projectID string, versionIDs []string) ([]string, error) {
	ids := cleanIDList(versionIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	found, err := s.m.ExistingVersionIDsTx(ctx, tx, projectID, ids)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(found))
	for _, id := range found {
		have[id] = true
	}
	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// errDeleteRollback 把「已收集到拒绝原因」带回事务外，让整批写入回滚。
var errDeleteRollback = errors.New("contenttemplate: 批量删除被拒绝")

// BulkDelete 在同一事务里删除整批模板：先按稳定顺序取完所有工程内容写锁，
// 再按 id 升序逐工程锁行、锁内重验引用，全部通过后才整批写入；
// 任一目标缺失 / 非法 / 仍被引用都整批拒绝，不静默跳过。
func (s *Service) BulkDelete(ctx context.Context, req *contenttemplatedto.BulkDeleteReq) error {
	if req == nil {
		return contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	ids, err := normalizeDeleteIDs(req.IDs)
	if err != nil {
		return err
	}
	projectOf, err := s.locateDeleteTargets(ctx, ids)
	if err != nil {
		return err
	}
	if s.refs == nil {
		logger.Scene("contenttemplate").Warn("模板引用反查端口未装配：删除已拒绝")
		return &deleteBlockedError{key: contenttemplateenums.ErrReferenceUnavailable}
	}

	projectIDs := make([]string, 0, len(projectOf))
	for _, projectID := range projectOf {
		projectIDs = append(projectIDs, projectID)
	}
	ordered := contenttemplatemodel.SortedProjectIDs(projectIDs)
	byProject := make(map[string][]string, len(ordered))
	for _, id := range ids {
		pid := projectOf[id]
		byProject[pid] = append(byProject[pid], id)
	}

	var blocked []facing.BlockedItem
	err = s.m.TransactionAny(ctx, func(tx *gorm.DB) error {
		for _, projectID := range ordered {
			if serr := s.m.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			if lerr := i18n.LockProjectContentWrite(ctx, tx, projectID); lerr != nil {
				return lerr
			}
		}
		for _, projectID := range ordered {
			items, verr := s.verifyDeleteProjectTx(ctx, tx, projectID, byProject[projectID])
			if verr != nil {
				return verr
			}
			blocked = append(blocked, items...)
		}
		if len(blocked) > 0 {
			return errDeleteRollback
		}
		for _, projectID := range ordered {
			if serr := s.m.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			for _, id := range byProject[projectID] {
				if derr := s.m.DeleteTemplateTx(ctx, tx, projectID, id); derr != nil {
					return derr
				}
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errDeleteRollback) {
			return deleteReject(len(ids), blocked)
		}
		logger.Scene("contenttemplate").Error(err, "批量删除内容模板失败")
		return err
	}
	for _, id := range ids {
		s.notifyTemplateChanged(ctx, id)
	}
	return nil
}

// locateDeleteTargets 逐工程定位每个 id 的归属；任一缺失整批拒绝。
func (s *Service) locateDeleteTargets(ctx context.Context, ids []string) (map[string]string, error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(projectIDs)
	projectOf := make(map[string]string, len(ids))
	missing := make([]facing.BlockedItem, 0)
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pending := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, ok := projectOf[id]; !ok {
				pending = append(pending, id)
			}
		}
		if len(pending) == 0 {
			break
		}
		rows, lerr := s.m.ListByIDs(ctx, projectID, pending)
		if lerr != nil {
			return nil, lerr
		}
		for i := range rows {
			projectOf[rows[i].ID] = projectID
		}
	}
	for _, id := range ids {
		if _, ok := projectOf[id]; !ok {
			missing = append(missing, facing.BlockedItem{ID: id, ReasonKey: contenttemplateenums.ErrNotFound})
		}
	}
	if len(missing) > 0 {
		return nil, deleteReject(len(ids), missing)
	}
	return projectOf, nil
}

// verifyDeleteProjectTx 在已持锁的事务内重验目标存在性与引用，返回拒绝明细。
func (s *Service) verifyDeleteProjectTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) ([]facing.BlockedItem, error) {
	rows, err := s.m.LockByIDsTx(ctx, tx, projectID, ids)
	if err != nil {
		return nil, err
	}
	found := make(map[string]bool, len(rows))
	names := make(map[string]string, len(rows))
	for i := range rows {
		found[rows[i].ID] = true
		names[rows[i].ID] = rows[i].Name
	}
	var items []facing.BlockedItem
	for _, id := range ids {
		if !found[id] {
			items = append(items, facing.BlockedItem{ID: id, ReasonKey: contenttemplateenums.ErrNotFound})
		}
	}
	if len(items) > 0 {
		return items, nil
	}

	structureRefs, err := s.structureRefsByTargetTx(ctx, tx, projectID, ids)
	if err != nil {
		return nil, err
	}
	versions, err := s.m.VersionIDsOfTx(ctx, tx, projectID, ids)
	if err != nil {
		return nil, err
	}
	external, err := s.refs.ListTemplateReferencesTx(ctx, tx, projectID, &contenttemplatecontract.TemplateRefQuery{
		TemplateIDs: ids, VersionIDs: versions,
	})
	if err != nil {
		return nil, err
	}
	externalRefs := make(map[string][]contenttemplatecontract.TemplateReferenceRow, len(ids))
	for _, row := range external {
		externalRefs[row.TemplateID] = append(externalRefs[row.TemplateID], row)
	}

	for _, id := range ids {
		target := facing.BlockedItem{ID: id, Name: names[id]}
		if refs := structureRefs[id]; len(refs) > 0 {
			sort.Strings(refs)
			target.ReasonKey = contenttemplateenums.ErrStructureTemplateInUse
			target.Fallback = strings.Join(refs, "；")
			items = append(items, target)
			continue
		}
		if refs := externalRefs[id]; len(refs) > 0 {
			target.ReasonKey = contenttemplateenums.ErrTemplateInUse
			target.Fallback = strings.Join(templateRefDetails(refs), "；")
			items = append(items, target)
			continue
		}
	}
	return items, nil
}

// structureRefsByTargetTx 反查其它模板文档仍把目标绑定为结构槽，并按目标分组。
func (s *Service) structureRefsByTargetTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) (map[string][]string, error) {
	rows, err := s.m.StructureBindingRefsTx(ctx, tx, projectID, ids)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}
	out := make(map[string][]string, len(ids))
	for i := range rows {
		bindings, perr := pipeline.ParseStructureBindings(rows[i].DraftDocument)
		if perr != nil {
			logger.Scene("contenttemplate").With("templateId", rows[i].ID).Warn("结构绑定文档无法解析，按全部目标可能被引用处理")
			for _, id := range ids {
				out[id] = append(out[id], "模板《"+rows[i].Name+"》（文档无法解析，无法判定槽位）")
			}
			continue
		}
		slots := make(map[string][]string)
		for slot, templateID := range bindings.TemplateBindings() {
			if known[templateID] {
				slots[templateID] = append(slots[templateID], structureSlotLabel(slot))
			}
		}
		for templateID, labels := range slots {
			sort.Strings(labels)
			out[templateID] = append(out[templateID], "模板《"+rows[i].Name+"》的"+strings.Join(labels, " / "))
		}
	}
	return out, nil
}

func templateRefDetails(rows []contenttemplatecontract.TemplateReferenceRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		label := strings.TrimSpace(row.Label)
		if label == "" {
			label = row.EntityID
		}
		detail := strings.TrimSpace(row.Detail)
		if detail == "" {
			out = append(out, label)
			continue
		}
		out = append(out, label+"（"+detail+"）")
	}
	sort.Strings(out)
	return out
}

// normalizeDeleteIDs 做完整批校验：空项、非法 UUID 都整批拒绝，
// 合法项按规范化 UUID 去重后保序，超限返回共享上限事实。
func normalizeDeleteIDs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	seen := make(map[string]bool, len(raw))
	ids := make([]string, 0, len(raw))
	invalid := make([]facing.BlockedItem, 0)
	for _, item := range raw {
		text := strings.TrimSpace(item)
		if text == "" {
			invalid = append(invalid, facing.BlockedItem{ReasonKey: contenttemplateenums.ErrInvalidParam})
			continue
		}
		parsed, err := uuid.Parse(text)
		if err != nil {
			invalid = append(invalid, facing.BlockedItem{ID: text, ReasonKey: contenttemplateenums.ErrInvalidParam})
			continue
		}
		canonical := parsed.String()
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		ids = append(ids, canonical)
	}
	if len(invalid) > 0 {
		return nil, deleteReject(len(raw), invalid)
	}
	if len(ids) == 0 {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrInvalidParam)
	}
	if len(ids) > bulkids.MaxIDs {
		return nil, &bulkids.IDsError{Count: len(ids), Max: bulkids.MaxIDs}
	}
	return ids, nil
}

func cleanIDList(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		text := strings.TrimSpace(item)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	return out
}
