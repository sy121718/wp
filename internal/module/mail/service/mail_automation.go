package mailservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	json "go_wp/pkg/jsonx"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/queue"
	"go_wp/pkg/ssrf"
	"go_wp/pkg/utils"
)

func (s *Service) SaveAutomation(ctx context.Context, req *maildto.SaveAutomationReq) (res *maildto.AutomationItem, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	if !validTrigger(req.TriggerType) {
		return nil, mailenums.Err(mailenums.ErrAutomationTriggerInvalid)
	}
	var raw map[string]any
	if len(req.Definition) > 0 {
		if uerr := json.Unmarshal(req.Definition, &raw); uerr != nil {
			return nil, graphInvalidRequestError(uerr.Error())
		}
	}

	def, err := ParseDefinition(raw)
	if err != nil {
		return nil, graphInvalidError(err)
	}
	norm, merr := json.Marshal(def)
	if merr != nil {
		return nil, merr
	}
	var definition mailmodel.JSONMap
	if uerr := json.Unmarshal(norm, &definition); uerr != nil {
		return nil, uerr
	}
	trigger := mailmodel.JSONMap{}
	for k, v := range req.TriggerParams {
		trigger[k] = v
	}

	now := time.Now()
	if req.ID > 0 {
		old, gerr := s.m.GetAutomation(ctx, req.ID)
		if gerr != nil {
			return nil, mailenums.Err(mailenums.ErrAutomationNotFound)
		}
		fields := map[string]any{
			"name":         strings.TrimSpace(req.Name),
			"description":  nullable(req.Description),
			"trigger_type": req.TriggerType,

			"definition": definition,

			"version":     old.Version + 1,
			"update_time": now,
		}
		if req.TriggerParams != nil {
			fields["trigger_params"] = trigger
		}

		if f := strings.TrimSpace(req.TriggerFrequency); f != "" {
			fields["trigger_frequency"] = f
		}
		if req.ExitOnReply != nil {
			fields["exit_on_reply"] = *req.ExitOnReply
		}
		if err = s.m.UpdateAutomationFields(ctx, req.ID, fields); err != nil {
			return nil, err
		}
	} else {
		frequency := strings.TrimSpace(req.TriggerFrequency)
		if frequency == "" {
			frequency = mailmodel.TriggerFrequencyEveryTime
		}
		e := &mailmodel.MailAutomationEntity{
			Name:             strings.TrimSpace(req.Name),
			TriggerType:      req.TriggerType,
			TriggerParams:    trigger,
			TriggerFrequency: frequency,
			Definition:       definition,
			Status:           mailmodel.AutomationStatusDraft,
			Version:          1,
			CreateBy:         req.OperatorID,
		}
		if req.ExitOnReply != nil {
			e.ExitOnReply = *req.ExitOnReply
		}
		if v := strings.TrimSpace(req.Description); v != "" {
			e.Description = &v
		}
		if err = s.m.CreateAutomation(ctx, e); err != nil {
			return nil, err
		}
		req.ID = e.ID
	}
	row, err := s.m.GetAutomation(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return automationItemOf(row), nil
}

func (s *Service) SaveAutomationLayout(ctx context.Context, req *maildto.SaveAutomationLayoutReq) (err error) {
	if req == nil || req.ID == 0 || len(req.Positions) == 0 {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	row, gerr := s.m.GetAutomation(ctx, req.ID)
	if gerr != nil {
		return mailenums.Err(mailenums.ErrAutomationNotFound)
	}
	def, derr := ParseDefinition(row.Definition)
	if derr != nil {
		return graphInvalidError(derr)
	}
	changed := false
	for i := range def.Nodes {
		p, ok := req.Positions[def.Nodes[i].Key]
		if !ok {
			continue
		}
		if def.Nodes[i].X == p.X && def.Nodes[i].Y == p.Y {
			continue
		}
		def.Nodes[i].X = p.X
		def.Nodes[i].Y = p.Y
		changed = true
	}
	if !changed {
		return nil
	}
	raw, merr := json.Marshal(def)
	if merr != nil {
		return merr
	}
	var definition mailmodel.JSONMap
	if uerr := json.Unmarshal(raw, &definition); uerr != nil {
		return uerr
	}
	return s.m.UpdateAutomationFields(ctx, req.ID, map[string]any{
		"definition": definition, "update_time": time.Now(),
	})
}

func (s *Service) ListAutomations(ctx context.Context, req *maildto.AutomationListReq) (res *maildto.AutomationListResp, err error) {
	if req == nil {
		req = &maildto.AutomationListReq{}
	}
	page, size := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	list, total, err := s.m.ListAutomations(ctx, req.Status, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	res = &maildto.AutomationListResp{Items: make([]maildto.AutomationItem, 0, len(list)), Total: total}
	for _, e := range list {
		res.Items = append(res.Items, *automationItemOf(e))
	}
	return res, nil
}

func (s *Service) GetAutomation(ctx context.Context, id uint64) (res *maildto.AutomationItem, err error) {
	row, err := s.m.GetAutomation(ctx, id)
	if err != nil {
		return nil, mailenums.Err(mailenums.ErrAutomationNotFound)
	}
	return automationItemOf(row), nil
}

func (s *Service) SetAutomationStatus(ctx context.Context, req *maildto.SetAutomationStatusReq) (err error) {
	if req == nil || req.ID == 0 {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	switch req.Status {
	case mailmodel.AutomationStatusDraft, mailmodel.AutomationStatusActive, mailmodel.AutomationStatusPaused:
	default:
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	row, gerr := s.m.GetAutomation(ctx, req.ID)
	if gerr != nil {
		return mailenums.Err(mailenums.ErrAutomationNotFound)
	}

	if req.Status == mailmodel.AutomationStatusActive {
		var raw map[string]any
		b, _ := json.Marshal(row.Definition)
		_ = json.Unmarshal(b, &raw)
		if _, verr := ParseDefinition(raw); verr != nil {
			return graphInvalidError(verr)
		}
	}
	return s.m.UpdateAutomationFields(ctx, req.ID, map[string]any{
		"status": req.Status, "update_time": time.Now(),
	})
}

func (s *Service) DeleteAutomation(ctx context.Context, id uint64) (err error) {
	_, err = s.DeleteAutomations(ctx, &maildto.DeleteIDsReq{IDs: []uint64{id}})
	return err
}

// DeleteAutomations 整批原子删除自动化：同一事务内先停运行中的实例再删流程；
// 任一条不存在则整批拒绝，逐条原因进 data.items。
func (s *Service) DeleteAutomations(ctx context.Context, req *maildto.DeleteIDsReq) (deleted int64, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	ids, ok := dedupIDs(req.IDs)
	if !ok || len(ids) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	now := time.Now()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		rows, lerr := s.m.AutomationsByIDsTx(ctx, tx, ids)
		if lerr != nil {
			return lerr
		}
		if missing := missingIDs(ids, rows, func(a *mailmodel.MailAutomationEntity) uint64 { return a.ID }); len(missing) > 0 {
			return rejectBatch(len(ids), blockedIDs(missing, mailenums.ErrAutomationNotFound))
		}
		for _, id := range ids {
			if serr := s.m.StopRunsOfAutomationTx(ctx, tx, id, now); serr != nil {
				return serr
			}
		}
		var derr error
		deleted, derr = s.m.DeleteAutomationsByIDsTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if deleted != int64(len(ids)) {
			return rejectBatch(len(ids), blockedIDs(ids, mailenums.ErrAutomationNotFound))
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func validTrigger(t string) bool {
	switch t {
	case mailmodel.TriggerManual, mailmodel.TriggerContactCreated, mailmodel.TriggerContactSubscribed,
		mailmodel.TriggerEmailOpened, mailmodel.TriggerEmailClicked, mailmodel.TriggerTagAdded:
		return true
	}
	return false
}

func automationItemOf(e *mailmodel.MailAutomationEntity) *maildto.AutomationItem {
	item := &maildto.AutomationItem{
		ID:               e.ID,
		Name:             e.Name,
		TriggerType:      e.TriggerType,
		TriggerParams:    map[string]any{},
		TriggerFrequency: e.TriggerFrequency,
		ExitOnReply:      e.ExitOnReply,
		Status:           e.Status,
		Version:          e.Version,
	}
	if e.Description != nil {
		item.Description = *e.Description
	}
	for k, v := range e.TriggerParams {
		item.TriggerParams[k] = v
	}
	def := map[string]any{}
	for k, v := range e.Definition {
		def[k] = v
	}
	if b, err := json.Marshal(def); err == nil {
		item.Definition = b
	}
	if parsed, err := ParseDefinition(e.Definition); err == nil {
		item.Entry = parsed.Entry
		for _, n := range parsed.Nodes {
			item.Nodes = append(item.Nodes, maildto.AutomationNodeItem{
				Key: n.Key,

				Type:   maildto.LegacyNodeType(n.Type),
				Params: n.Params,

				Next: n.outlet(outletNext), Yes: n.outlet(outletYes), No: n.outlet(outletNo),
				X: n.X, Y: n.Y,
			})
		}
	}
	if e.CreateTime != nil {
		item.CreateTime = e.CreateTime.Format(time.RFC3339)
	}
	return item
}

const (
	NodeTypeTrigger = "trigger"
	NodeTypeDelay   = "delay"
	NodeTypeEmail   = "email"

	NodeTypeBranch           = "branch"
	NodeTypeFilter           = "filter"
	NodeTypeListStatusBranch = "list_status_branch"
	NodeTypeABTest           = "ab_test"
	NodeTypeWebhook          = "webhook"
	NodeTypeTag              = "tag"
	NodeTypeEnd              = "end"
)

const (
	ConditionModeAll = "all"

	ConditionModeAny = "any"
)

type AutomationDefinition struct {
	Schema int              `json:"schema"`
	Entry  string           `json:"entry"`
	Nodes  []AutomationNode `json:"nodes"`
}

type AutomationNode struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`

	Out map[string]string `json:"out,omitempty"`

	Next string `json:"-"`
	Yes  string `json:"-"`
	No   string `json:"-"`

	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

func (n AutomationNode) outgoing() []string {
	filled := n.allOutlets()
	if len(filled) == 0 {
		return nil
	}
	names := make([]string, 0, len(filled))
	for name := range filled {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, filled[name])
	}
	return out
}

func ParseDefinition(raw map[string]any) (*AutomationDefinition, error) {
	if len(raw) == 0 {
		return nil, graphErr(mailenums.DetailGraphEmptyDefinition, "流程定义不能为空")
	}
	normalized, _ := normalizeDefinitionVersioned(raw)
	b, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	var def AutomationDefinition
	if err := json.Unmarshal(b, &def); err != nil {
		return nil, graphErr(mailenums.DetailGraphNotGraph,
			"流程定义不是合法的图结构: {reason}", "reason", err.Error())
	}
	def.Schema = automationSchemaV2
	if err := ValidateDefinition(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

func ValidateDefinition(def *AutomationDefinition) error {
	if def == nil || len(def.Nodes) == 0 {
		return graphErr(mailenums.DetailGraphNoNode, "流程里至少要有一个节点")
	}

	byKey := make(map[string]AutomationNode, len(def.Nodes))
	order := make([]string, 0, len(def.Nodes))
	for _, n := range def.Nodes {
		key := strings.TrimSpace(n.Key)
		if key == "" {
			return graphErr(mailenums.DetailGraphNodeKeyMissing, "存在没有 key 的节点")
		}
		if _, dup := byKey[key]; dup {
			return graphErr(mailenums.DetailGraphNodeKeyDup, "节点 key 重复: {node}", "node", key)
		}
		if _, err := nodeArity(n); err != nil {

			return err
		}
		byKey[key] = n
		order = append(order, key)
	}

	entry := strings.TrimSpace(def.Entry)
	if entry == "" {
		return graphErr(mailenums.DetailGraphEntryMissing, "没有指定入口节点")
	}
	if _, ok := byKey[entry]; !ok {
		return graphErr(mailenums.DetailGraphEntryNotExist, "入口节点不存在: {node}", "node", entry)
	}

	for _, n := range def.Nodes {
		for _, to := range n.outgoing() {
			if _, ok := byKey[to]; !ok {
				return graphErr(mailenums.DetailGraphEdgeTargetMissing,
					"节点 {from} 指向了不存在的节点 {to}", "from", n.Key, "to", to)
			}
		}
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(byKey))
	var visit func(key string, path []string) error
	visit = func(key string, path []string) error {
		switch color[key] {
		case gray:
			return graphErr(mailenums.DetailGraphCycle,
				"流程里有环: {path}", "path", strings.Join(append(path, key), " → "))
		case black:
			return nil
		}
		color[key] = gray
		n := byKey[key]
		for _, to := range n.outgoing() {
			if err := visit(to, append(path, key)); err != nil {
				return err
			}
		}
		color[key] = black
		return nil
	}
	if err := visit(entry, nil); err != nil {
		return err
	}

	var unreachable []string
	for _, key := range order {
		if color[key] == white {
			unreachable = append(unreachable, key)
		}
	}
	if len(unreachable) > 0 {
		return graphErr(mailenums.DetailGraphUnreachable,
			"有节点从入口走不到: {nodes}", "nodes", strings.Join(unreachable, ", "))
	}
	return nil
}

func nodeArity(n AutomationNode) (int, error) {
	key := strings.TrimSpace(n.Key)
	if unknown := unknownOutlets(n); len(unknown) > 0 {
		return 0, graphErrUnknownOutlet(key, unknown[0])
	}
	switch n.Type {
	case NodeTypeTrigger:

		return 1, nil
	case NodeTypeDelay:
		mins, _ := toInt(n.Params["minutes"])
		if mins <= 0 {
			return 0, graphErr(mailenums.DetailGraphNeedMinutes,
				"节点 {node}: 等待节点需要正数的 minutes", "node", key)
		}
		return 1, nil
	case NodeTypeEmail:
		if strings.TrimSpace(toString(n.Params["template_key"])) == "" {
			return 0, graphErr(mailenums.DetailGraphNeedTemplate,
				"节点 {node}: 发信节点需要 template_key", "node", key)
		}
		return 1, nil
	case NodeTypeFilter:
		filled := n.allOutlets()
		if filled[outletYes] == "" || filled[outletNo] == "" {
			return 0, graphErr(mailenums.DetailGraphNeedTwoArms,
				"节点 {node}: 条件分支需要 yes 与 no 两条出边", "node", key)
		}
		if len(toStringSlice(n.Params["conditions"])) == 0 {
			return 0, graphErr(mailenums.DetailGraphNeedCondition,
				"节点 {node}: 条件分支需要至少一个条件", "node", key)
		}

		if !conditionModeValid(n.Params) {
			return 0, graphErr(mailenums.DetailGraphBadCondition,
				"节点 {node}: 模式只能是 all 或 any", "node", key,
				"reason", "mode="+toString(n.Params["mode"]))
		}
		return 2, nil
	case NodeTypeTag:
		add := toStringSlice(n.Params["add"])
		remove := toStringSlice(n.Params["remove"])
		if len(add) == 0 && len(remove) == 0 {
			return 0, graphErr(mailenums.DetailGraphNeedTagAction,
				"节点 {node}: 标签节点需要 add 或 remove", "node", key)
		}
		return 1, nil
	case NodeTypeListStatusBranch:

		if err := requireAllOutlets(n, key); err != nil {
			return 0, err
		}
		return len(nodeOutlets(NodeTypeListStatusBranch)), nil
	case NodeTypeABTest:
		return checkABTest(n, key)
	case NodeTypeWebhook:
		raw := strings.TrimSpace(toString(n.Params["url"]))
		if raw == "" {
			return 0, graphErr(mailenums.DetailGraphNeedWebhookURL,
				"节点 {node}: 回调节点需要 https 地址", "node", key)
		}

		if uerr := ssrf.Check(context.Background(), raw, "https"); uerr != nil {
			return 0, graphErr(mailenums.DetailGraphWebhookDenied,
				"节点 {node}: 回调地址不允许指向内网", "node", key)
		}
		return 1, nil
	case NodeTypeEnd:
		return 0, nil
	default:
		return 0, graphErr(mailenums.DetailGraphUnknownNodeTyp,
			"节点 {node}: 未知节点类型: {type}", "node", key, "type", n.Type)
	}
}

func requireAllOutlets(n AutomationNode, key string) error {
	required := nodeOutlets(n.Type)
	filled := n.allOutlets()
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if filled[name] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return graphErr(mailenums.DetailGraphOutletMissing,
		"节点 {node}: 缺少出口 {outlet}", "node", key, "outlet", strings.Join(missing, ", "))
}

func checkABTest(n AutomationNode, key string) (int, error) {
	variants := abVariants(n.Params)
	if len(variants) < 2 || len(variants) > 5 {
		return 0, graphErr(mailenums.DetailGraphNeedVariants,
			"节点 {node}: 分流转需要 2–5 个变体", "node", key)
	}
	seen := make(map[string]bool, len(variants))
	total := 0
	for _, v := range variants {
		if seen[v.ID] {
			return 0, graphErr(mailenums.DetailGraphVariantDup,
				"节点 {node}: 变体 id 重复: {variant}", "node", key, "variant", v.ID)
		}
		seen[v.ID] = true
		total += v.Weight
	}

	if total != 100 {
		return 0, graphErr(mailenums.DetailGraphVariantWeightBad,
			"节点 {node}: 变体权重之和必须是 100（当前 {weight}）", "node", key,
			"weight", strconv.Itoa(total))
	}

	filled := n.allOutlets()
	missing := make([]string, 0, len(variants))
	for _, v := range variants {
		if filled[v.ID] == "" {
			missing = append(missing, v.ID)
		}
	}
	if len(missing) > 0 {
		return 0, graphErr(mailenums.DetailGraphOutletMissing,
			"节点 {node}: 缺少出口 {outlet}", "node", key, "outlet", strings.Join(missing, ", "))
	}
	return len(variants), nil
}

type abVariant struct {
	ID     string
	Name   string
	Weight int
}

func abVariants(params map[string]any) []abVariant {
	if params == nil {
		return nil
	}
	list, ok := params["variants"].([]any)
	if !ok {
		return nil
	}
	out := make([]abVariant, 0, len(list))
	for _, item := range list {
		v, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		id := strings.TrimSpace(toString(v["id"]))
		if id == "" {
			continue
		}
		weight, _ := toInt(v["weight"])
		out = append(out, abVariant{ID: id, Name: toString(v["name"]), Weight: weight})
	}
	return out
}

func conditionModeValid(params map[string]any) bool {
	switch strings.TrimSpace(toString(params["mode"])) {
	case "", ConditionModeAll, ConditionModeAny:
		return true
	}
	return false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toStringSlice(v any) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (s *Service) ListAutomationRuns(ctx context.Context, req *maildto.AutomationRunListReq) (res *maildto.AutomationRunListResp, err error) {
	if req == nil {
		req = &maildto.AutomationRunListReq{}
	}
	page, size := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	rows, total, err := s.m.ListRunRows(ctx, req.AutomationID, req.Status, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	res = &maildto.AutomationRunListResp{Items: make([]maildto.AutomationRunItem, 0, len(rows)), Total: total}
	for _, row := range rows {
		res.Items = append(res.Items, runItemOf(row))
	}
	res.Counts, _ = s.m.CountRunsByStatus(ctx, req.AutomationID)
	return res, nil
}

func (s *Service) AutomationRunDetail(ctx context.Context, runID uint64) (res *maildto.AutomationRunDetailResp, err error) {
	if runID == 0 {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	row, err := s.m.RunRowByID(ctx, runID)
	if err != nil || row.ID == 0 {
		return nil, mailenums.Err(mailenums.ErrAutomationRunNotFound)
	}
	res = &maildto.AutomationRunDetailResp{
		Run:            runItemOf(row),
		AutomationID:   row.AutomationID,
		AutomationName: row.AutomationName,
		Email:          row.Email,
	}
	if row.Name != nil {
		res.Name = *row.Name
	}

	logs, lerr := s.m.ListNodeLogs(ctx, runID, 200)
	if lerr == nil {
		for _, l := range logs {
			item := maildto.AutomationNodeLogItem{
				NodeKey: l.NodeKey, NodeType: l.NodeType, Status: l.Status,
			}
			if l.Detail != nil {
				item.Detail = *l.Detail
			}
			if l.CreateTime != nil {
				item.CreateTime = l.CreateTime.Format(time.RFC3339)
			}
			res.Timeline = append(res.Timeline, item)
			if l.Status == mailmodel.NodeStatusOK {
				res.DoneNodes++
			}
		}
	}

	if automation, aerr := s.m.GetAutomation(ctx, row.AutomationID); aerr == nil {
		if def, derr := ParseDefinition(automation.Definition); derr == nil {
			res.TotalNodes = len(def.Nodes)
		}
	}

	res.Explain = explainRun(row, res)
	return res, nil
}

func (s *Service) ListContactRuns(ctx context.Context, contactID uint64, limit int) (items []maildto.AutomationRunItem, err error) {
	if contactID == 0 {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	rows, err := s.m.ListRunsOfContact(ctx, contactID, limit)
	if err != nil {
		return nil, err
	}
	items = make([]maildto.AutomationRunItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, runItemOf(row))
	}
	return items, nil
}

func explainRun(row mailmodel.RunRow, res *maildto.AutomationRunDetailResp) string {

	at := mailenums.EncodeRunText(mailenums.RunKeyEntryNode, nil)
	if row.CurrentNode != nil && *row.CurrentNode != "" {
		at = *row.CurrentNode
	}
	switch row.Status {
	case mailmodel.RunStatusWaiting:
		when := mailenums.EncodeRunText(mailenums.RunKeyWhenUnset, nil)
		if row.NextRunAt != nil {
			when = row.NextRunAt.Format("2006-01-02 15:04")
		}
		return mailenums.EncodeRunText(mailenums.RunKeyWaiting, map[string]string{
			mailenums.RunArgWhen: when, mailenums.RunArgNode: at})
	case mailmodel.RunStatusRunning:
		return mailenums.EncodeRunText(mailenums.RunKeyRunning, map[string]string{mailenums.RunArgNode: at})
	case mailmodel.RunStatusCompleted:
		return mailenums.EncodeRunText(mailenums.RunKeyCompleted,
			map[string]string{mailenums.RunArgCount: strconv.Itoa(res.DoneNodes)})
	case mailmodel.RunStatusStopped:

		if row.ErrorMessage != nil && strings.TrimSpace(*row.ErrorMessage) != "" {
			return mailenums.EncodeRunText(mailenums.RunKeyStoppedWhy,
				map[string]string{mailenums.RunArgReason: *row.ErrorMessage})
		}
		return mailenums.EncodeRunText(mailenums.RunKeyStopped, nil)
	case mailmodel.RunStatusFailed:
		reason := mailenums.EncodeRunText(mailenums.RunKeyReasonUnset, nil)
		if row.ErrorMessage != nil && *row.ErrorMessage != "" {
			reason = *row.ErrorMessage
		}
		return mailenums.EncodeRunText(mailenums.RunKeyFailed, map[string]string{
			mailenums.RunArgNode: at, mailenums.RunArgReason: reason})
	}
	return mailenums.EncodeRunText(mailenums.RunKeyStatusUnkwn,
		map[string]string{mailenums.RunArgStatus: row.Status})
}

func runItemOf(row mailmodel.RunRow) maildto.AutomationRunItem {
	item := maildto.AutomationRunItem{
		ID:           row.ID,
		AutomationID: row.AutomationID,
		ContactID:    row.ContactID,
		Email:        row.Email,
		Status:       row.Status,
	}
	if row.CurrentNode != nil {
		item.CurrentNode = *row.CurrentNode
	}
	if row.ErrorMessage != nil {
		item.ErrorMessage = *row.ErrorMessage
	}
	if row.TriggerEvent != nil {
		item.TriggerEvent = *row.TriggerEvent
	}
	if row.NextRunAt != nil {
		item.NextRunAt = row.NextRunAt.Format(time.RFC3339)
	}
	if row.StartedAt != nil {
		item.StartedAt = row.StartedAt.Format(time.RFC3339)
	}
	if row.FinishedAt != nil {
		item.FinishedAt = row.FinishedAt.Format(time.RFC3339)
	}
	return item
}

const maxStepsPerExecution = 100

func (s *Service) RunAutomation(ctx context.Context, runID uint64) (err error) {
	if runID == 0 {
		return mailenums.Err(mailenums.ErrInvalidParam)
	}
	run, err := s.m.GetRun(ctx, runID)
	if err != nil {

		return nil
	}

	switch run.Status {
	case mailmodel.RunStatusCompleted, mailmodel.RunStatusStopped, mailmodel.RunStatusFailed:
		return nil
	}

	automation, err := s.m.GetAutomation(ctx, run.AutomationID)
	if err != nil {
		return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyAutomationGone, nil))
	}
	def, derr := ParseDefinition(automation.Definition)
	if derr != nil {

		return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyDefInvalid, map[string]string{mailenums.RunArgReason: derr.Error()}))
	}
	byKey := make(map[string]AutomationNode, len(def.Nodes))
	for _, n := range def.Nodes {
		byKey[n.Key] = n
	}

	contact, err := s.m.GetContact(ctx, run.ContactID)
	if err != nil {

		return s.stopRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyContactGone, nil))
	}

	current := ""
	if run.CurrentNode != nil {
		current = strings.TrimSpace(*run.CurrentNode)
	}
	if current == "" {
		current = def.Entry
	}

	for step := 0; step < maxStepsPerExecution; step++ {
		node, ok := byKey[current]
		if !ok {
			return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyNodeGone, map[string]string{mailenums.RunArgNode: current}))
		}

		done, lerr := s.m.NodeLogExists(ctx, run.ID, node.Key)
		if lerr != nil {
			return lerr
		}

		switch node.Type {
		case NodeTypeTrigger:

			if !done {
				s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, mailenums.EncodeRunText(mailenums.RunKeyTriggerEntered, nil))
			}
			current = node.outlet(outletNext)
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeDelay:
			if done {
				current = node.outlet(outletNext)
				if err = s.advance(ctx, run.ID, current); err != nil {
					return err
				}
				if current == "" {
					return s.completeRun(ctx, run.ID)
				}
				continue
			}
			mins, _ := toInt(node.Params["minutes"])
			due := time.Now().Add(time.Duration(mins) * time.Minute)
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK,
				mailenums.EncodeRunText(mailenums.RunKeyDelayContinue,
					map[string]string{mailenums.RunArgMinutes: strconv.Itoa(mins)}))

			if err = s.m.UpdateRunFields(ctx, run.ID, map[string]any{
				"status":       mailmodel.RunStatusWaiting,
				"current_node": nullable(node.outlet(outletNext)),
				"next_run_at":  due,
				"update_time":  time.Now(),
			}); err != nil {
				return err
			}

			enqueueAutomationRun(run.ID, due)
			return nil

		case NodeTypeEmail:
			if done {
				current = node.outlet(outletNext)
				if err = s.advance(ctx, run.ID, current); err != nil {
					return err
				}
				if current == "" {
					return s.completeRun(ctx, run.ID)
				}
				continue
			}
			tplKey := strings.TrimSpace(toString(node.Params["template_key"]))
			locale := strings.TrimSpace(toString(node.Params["locale"]))
			subject := strings.TrimSpace(toString(node.Params["subject"]))
			vars := s.automationVars(ctx, contact, node.Params)
			res, serr := s.SendTemplate(ctx, &maildto.SendTemplateReq{
				TemplateKey: tplKey,
				Locale:      locale,
				To:          contact.Email,
				Vars:        vars,
				ContactID:   contact.ID,
			})
			if serr != nil {

				s.logNode(ctx, run.ID, node, mailmodel.NodeStatusFailed,
					mailenums.EncodeRunText(mailenums.RunKeySendFailed,
						map[string]string{mailenums.RunArgReason: serr.Error()}))
				_ = s.m.UpdateRunFields(ctx, run.ID, map[string]any{
					"error_message": serr.Error(), "update_time": time.Now(),
				})
				return serr
			}

			suppressed := res != nil && res.Suppressed
			var detail string
			switch {
			case suppressed && subject != "":
				detail = mailenums.EncodeRunText(mailenums.RunKeyEmailSuppSubj,
					map[string]string{mailenums.RunArgSubject: subject})
			case suppressed:
				detail = mailenums.EncodeRunText(mailenums.RunKeyEmailSuppress, nil)
			case subject != "":
				detail = mailenums.EncodeRunText(mailenums.RunKeyEmailSentSubj,
					map[string]string{mailenums.RunArgSubject: subject})
			default:
				detail = mailenums.EncodeRunText(mailenums.RunKeyEmailSent, nil)
			}
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, detail)
			contact = refreshContact(ctx, s, contact)
			current = node.outlet(outletNext)
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeFilter:
			matched, berr := s.evalConditions(ctx, contact, node.Params)
			if berr != nil {

				return s.failRun(ctx, run.ID, berr.Error())
			}
			branch := node.outlet(outletNo)
			tag := mailenums.EncodeRunText(mailenums.RunKeyBranchNo, nil)
			if matched {
				branch = node.outlet(outletYes)
				tag = mailenums.EncodeRunText(mailenums.RunKeyBranchYes, nil)
			}
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, tag)
			current = branch
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeTag:
			if done {
				current = node.outlet(outletNext)
				if err = s.advance(ctx, run.ID, current); err != nil {
					return err
				}
				if current == "" {
					return s.completeRun(ctx, run.ID)
				}
				continue
			}
			added, removed, terr := s.applyTags(ctx, contact.ID, node.Params)
			if terr != nil {
				return s.failRun(ctx, run.ID, terr.Error())
			}
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK,
				mailenums.EncodeRunText(mailenums.RunKeyTagsApplied, map[string]string{
					mailenums.RunArgAdded:   tagListText(added),
					mailenums.RunArgRemoved: tagListText(removed),
				}))
			contact = refreshContact(ctx, s, contact)
			current = node.outlet(outletNext)
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeListStatusBranch:
			outlet := listStatusOutlet(contact.Status)
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK,
				mailenums.EncodeRunText(mailenums.RunKeyBranchOut, map[string]string{
					mailenums.RunArgStatus: outlet,
				}))
			current = node.outlet(outlet)
			if current == "" {

				return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyOutletMissing,
					map[string]string{mailenums.RunArgNode: node.Key, mailenums.RunArgOutlet: outlet}))
			}
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}

		case NodeTypeABTest:
			outlet, berr := abOutletOf(run.ID, node)
			if berr != nil {
				return s.failRun(ctx, run.ID, berr.Error())
			}
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK,
				mailenums.EncodeRunText(mailenums.RunKeyABVariant,
					map[string]string{mailenums.RunArgVariant: outlet}))
			current = node.outlet(outlet)
			if current == "" {
				return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyOutletMissing,
					map[string]string{mailenums.RunArgNode: node.Key, mailenums.RunArgOutlet: outlet}))
			}
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}

		case NodeTypeWebhook:
			if done {
				current = node.outlet(outletNext)
				if err = s.advance(ctx, run.ID, current); err != nil {
					return err
				}
				if current == "" {
					return s.completeRun(ctx, run.ID)
				}
				continue
			}

			status, werr := s.postWebhook(ctx, node)
			logStatus := mailmodel.NodeStatusOK
			if werr != nil {
				logStatus = mailmodel.NodeStatusFailed
			}
			detail := mailenums.EncodeRunText(mailenums.RunKeyWebhookOK,
				map[string]string{mailenums.RunArgStatus: strconv.Itoa(status)})
			if werr != nil {
				detail = mailenums.EncodeRunText(mailenums.RunKeyWebhookFailed,
					map[string]string{mailenums.RunArgReason: werr.Error()})
			}
			s.logNode(ctx, run.ID, node, logStatus, detail)
			current = node.outlet(outletNext)
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeEnd:
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, mailenums.EncodeRunText(mailenums.RunKeyEnded, nil))
			return s.completeRun(ctx, run.ID)

		default:
			return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyUnknownNode, map[string]string{mailenums.RunArgType: node.Type}))
		}
	}

	return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyStepsExceeded, nil))
}

func (s *Service) advance(ctx context.Context, runID uint64, next string) error {
	return s.m.UpdateRunFields(ctx, runID, map[string]any{
		"current_node": nullable(next),
		"status":       mailmodel.RunStatusRunning,
		"update_time":  time.Now(),
	})
}

func (s *Service) completeRun(ctx context.Context, runID uint64) error {
	now := time.Now()
	return s.m.UpdateRunFields(ctx, runID, map[string]any{
		"status":      mailmodel.RunStatusCompleted,
		"finished_at": now,
		"update_time": now,
		"next_run_at": nil,
	})
}

func (s *Service) failRun(ctx context.Context, runID uint64, reason string) error {
	now := time.Now()
	_ = s.m.UpdateRunFields(ctx, runID, map[string]any{
		"status":        mailmodel.RunStatusFailed,
		"error_message": reason,
		"finished_at":   now,
		"update_time":   now,
		"next_run_at":   nil,
	})
	return nil
}

func (s *Service) stopRun(ctx context.Context, runID uint64, reason string) error {
	now := time.Now()
	return s.m.UpdateRunFields(ctx, runID, map[string]any{
		"status":        mailmodel.RunStatusStopped,
		"error_message": reason,
		"finished_at":   now,
		"update_time":   now,
		"next_run_at":   nil,
	})
}

func (s *Service) logNode(ctx context.Context, runID uint64, node AutomationNode, status, detail string) {
	e := &mailmodel.MailAutomationNodeLogEntity{
		RunID:    runID,
		NodeKey:  node.Key,
		NodeType: node.Type,
		Status:   status,
	}
	if strings.TrimSpace(detail) != "" {
		e.Detail = &detail
	}
	_ = s.m.CreateNodeLog(ctx, e)
}

func (s *Service) applyTags(ctx context.Context, contactID uint64, params map[string]any) (added, removed []string, err error) {
	add := toStringSlice(params["add"])
	remove := toStringSlice(params["remove"])
	if len(add) == 0 && len(remove) == 0 {
		return nil, nil, nil
	}
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		current, lerr := s.m.LockContactTagsTx(ctx, tx, contactID)
		if lerr != nil {
			return lerr
		}
		present := make(map[string]bool, len(current))
		for _, t := range current {
			present[t] = true
		}
		removeSet := make(map[string]bool, len(remove))
		for _, t := range remove {
			removeSet[t] = true
		}
		nextAdded := make([]string, 0, len(add))
		nextRemoved := make([]string, 0, len(remove))
		next := make([]string, 0, len(current)+len(add))
		for _, t := range current {
			if removeSet[t] {
				nextRemoved = append(nextRemoved, t)
				continue
			}
			next = append(next, t)
		}
		for _, t := range add {
			if present[t] {
				continue
			}
			next = append(next, t)
			nextAdded = append(nextAdded, t)
		}
		if len(nextAdded) == 0 && len(nextRemoved) == 0 {
			return nil
		}
		if uerr := s.m.UpdateContactFieldsTx(ctx, tx, contactID, map[string]any{
			"tags": mailmodel.StringArray(next), "update_time": time.Now(),
		}); uerr != nil {
			return uerr
		}
		added, removed = nextAdded, nextRemoved
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return added, removed, nil
}

func (s *Service) automationVars(ctx context.Context, contact *mailmodel.MailContactEntity, params map[string]any) map[string]any {
	vars := map[string]any{
		"email": contact.Email,
		"name":  displayNameOf(contact),
	}
	if contact.Name != nil {
		vars["name"] = *contact.Name
	}
	if extra, ok := params["vars"].(map[string]any); ok {
		for k, v := range extra {
			vars[k] = v
		}
	}
	return vars
}

func displayNameOf(c *mailmodel.MailContactEntity) string {
	if c.Name != nil && strings.TrimSpace(*c.Name) != "" {
		return strings.TrimSpace(*c.Name)
	}
	at := strings.Index(c.Email, "@")
	if at > 0 {
		return c.Email[:at]
	}
	return c.Email
}

type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

const (
	condOpEq     = "eq"
	condOpNeq    = "neq"
	condOpHas    = "has"
	condOpNotHas = "not_has"
	condOpGt     = "gt"
	condOpGte    = "gte"
	condOpLt     = "lt"
	condOpLte    = "lte"
)

func (s *Service) evalConditions(ctx context.Context, contact *mailmodel.MailContactEntity, params map[string]any) (bool, error) {
	raw, _ := params["conditions"].([]any)
	if len(raw) == 0 {
		return false, errors.New(mailenums.EncodeRunText(mailenums.RunKeyCondMissing, nil))
	}
	anyMode := strings.TrimSpace(toString(params["mode"])) == ConditionModeAny

	seen := 0
	for _, item := range raw {
		ok, err := s.evalOneCondition(ctx, contact, item)
		if err != nil {
			return false, err
		}
		if ok {
			seen++
		}

		if anyMode && ok {
			return true, nil
		}
		if !anyMode && !ok {
			return false, nil
		}
	}
	if seen == 0 && !anyMode {
		return false, nil
	}
	return !anyMode || seen > 0, nil
}

func (s *Service) evalOneCondition(ctx context.Context, contact *mailmodel.MailContactEntity, item any) (bool, error) {
	switch v := item.(type) {
	case string:
		return s.evalPrefixCondition(ctx, contact, strings.TrimSpace(v))
	case map[string]any:
		return s.evalStructuredCondition(ctx, contact, v)
	}
	return false, errors.New(mailenums.EncodeRunText(mailenums.RunKeyCondUnknown,
		map[string]string{mailenums.RunArgCondition: fmt.Sprintf("%v", item)}))
}

func (s *Service) evalPrefixCondition(ctx context.Context, contact *mailmodel.MailContactEntity, cond string) (bool, error) {
	switch {
	case cond == "":

		return false, nil
	case cond == "opened":
		return s.m.ContactHasEvent(ctx, contact.ID, mailmodel.EventTypeOpen)
	case cond == "clicked":
		return s.m.ContactHasEvent(ctx, contact.ID, mailmodel.EventTypeClick)
	case cond == "subscribed":
		return contact.Status == mailmodel.ContactStatusSubscribed, nil
	case strings.HasPrefix(cond, "has_tag:"):
		tag := strings.TrimSpace(strings.TrimPrefix(cond, "has_tag:"))
		if tag == "" {
			return false, errors.New(mailenums.EncodeRunText(mailenums.RunKeyCondTagMissing, nil))
		}
		tags, err := s.m.GetContactTags(ctx, contact.ID)
		if err != nil {
			return false, err
		}
		for _, t := range tags {
			if t == tag {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, errors.New(mailenums.EncodeRunText(mailenums.RunKeyCondUnknown,
			map[string]string{mailenums.RunArgCondition: cond}))
	}
}

func (s *Service) evalStructuredCondition(ctx context.Context, contact *mailmodel.MailContactEntity, raw map[string]any) (bool, error) {
	var cond Condition
	cond.Field = strings.TrimSpace(toString(raw["field"]))
	cond.Op = strings.TrimSpace(toString(raw["op"]))
	cond.Value = raw["value"]
	if cond.Field == "" || cond.Op == "" {
		return false, conditionErr(cond, "field 与 op 都必须填")
	}

	switch cond.Field {
	case "status":
		return compareString(string(contact.Status), cond)
	case "email":
		return compareString(contact.Email, cond)
	case "name":
		name := ""
		if contact.Name != nil {
			name = *contact.Name
		}
		return compareString(name, cond)
	case "source":
		return compareString(contact.Source, cond)
	case "last_activity_at":
		if contact.LastActivityAt == nil {

			return false, nil
		}
		return compareTime(*contact.LastActivityAt, cond)
	case "tags":
		tags, err := s.m.GetContactTags(ctx, contact.ID)
		if err != nil {
			return false, err
		}
		return compareTags(tags, cond)
	default:
		return false, conditionErr(cond, "未知字段")
	}
}

func conditionErr(cond Condition, reason string) error {
	return errors.New(mailenums.EncodeRunText(mailenums.RunKeyCondInvalid, map[string]string{
		mailenums.RunArgCondition: fmt.Sprintf("%s %s %v（%s）", cond.Field, cond.Op, cond.Value, reason),
	}))
}

func compareString(actual string, cond Condition) (bool, error) {
	want := strings.TrimSpace(toString(cond.Value))
	switch cond.Op {
	case condOpEq:
		return actual == want, nil
	case condOpNeq:
		return actual != want, nil
	}
	return false, conditionErr(cond, "该字段只支持 eq / neq")
}

func compareTime(actual time.Time, cond Condition) (bool, error) {
	var want time.Time
	text := strings.TrimSpace(toString(cond.Value))
	if days, ok := toInt(cond.Value); ok {
		want = time.Now().Add(time.Duration(days) * 24 * time.Hour)
	} else {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return false, conditionErr(cond, "时间要填 RFC3339 或天数")
		}
		want = parsed
	}
	switch cond.Op {
	case condOpGt:
		return actual.After(want), nil
	case condOpGte:
		return actual.After(want) || actual.Equal(want), nil
	case condOpLt:
		return actual.Before(want), nil
	case condOpLte:
		return actual.Before(want) || actual.Equal(want), nil
	}
	return false, conditionErr(cond, "时间字段只支持 gt / gte / lt / lte")
}

func compareTags(tags []string, cond Condition) (bool, error) {
	want := strings.TrimSpace(toString(cond.Value))
	if want == "" {
		return false, conditionErr(cond, "标签名不能为空")
	}
	found := false
	for _, t := range tags {
		if t == want {
			found = true
			break
		}
	}
	switch cond.Op {
	case condOpHas:
		return found, nil
	case condOpNotHas:
		return !found, nil
	}
	return false, conditionErr(cond, "标签字段只支持 has / not_has")
}

func tagListText(tags []string) string {
	if len(tags) == 0 {
		return "—"
	}
	return strings.Join(tags, ", ")
}

func refreshContact(ctx context.Context, s *Service, c *mailmodel.MailContactEntity) *mailmodel.MailContactEntity {
	row, err := s.m.GetContact(ctx, c.ID)
	if err != nil {
		return c
	}
	return row
}

const (
	mailAutomationTickInterval = time.Minute

	mailAutomationTickLimit = 500

	mailAutomationTickTimeout = 30 * time.Second
)

func (s *Service) EnqueueDueRuns(ctx context.Context, limit int) (queued int, err error) {
	now := time.Now()
	runs, err := s.m.DueRuns(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	for _, run := range runs {
		enqueueAutomationRun(run.ID, time.Time{})
		queued++
	}
	return queued, nil
}

func (s *Service) TickDueRuns(ctx context.Context) (int, error) {
	return s.EnqueueDueRuns(ctx, mailAutomationTickLimit)
}

func StartMailAutomationScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	startMailAutomationScheduler(svc, mailAutomationTickInterval)
}

func StartMailAutomationSchedulerWithInterval(svc *Service, interval time.Duration) {
	startMailAutomationScheduler(svc, interval)
}

func startMailAutomationScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runMailAutomationTickLoop(func() {

		defer func() {
			if r := recover(); r != nil {
				logger.Scene("mail").Error(fmt.Errorf("panic: %v", r),
					"自动化延时兜底扫描单轮 panic（已收敛，下一轮照常；不影响进程）")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), mailAutomationTickTimeout)
		defer cancel()
		queued, err := svc.TickDueRuns(ctx)
		if err != nil {
			logger.Scene("mail").Error(err, "自动化延时兜底扫描失败（下一轮重试）")
			return
		}
		if queued > 0 {
			logger.Scene("mail").With("queued", queued).
				Info("自动化延时兜底：已重新投递到点的等待实例")
		}
	}, interval)
}

func runMailAutomationTickLoop(run func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = mailAutomationTickInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				run()
			case <-done:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func (s *Service) StartRun(ctx context.Context, automationID, contactID uint64, triggerEvent string) (newRun bool, err error) {
	if automationID == 0 || contactID == 0 {
		return false, mailenums.Err(mailenums.ErrInvalidParam)
	}
	automation, err := s.m.GetAutomation(ctx, automationID)
	if err != nil {
		return false, mailenums.Err(mailenums.ErrAutomationNotFound)
	}

	if automation.Status != mailmodel.AutomationStatusActive {
		return false, nil
	}
	return s.startRunWithAutomation(ctx, automation, contactID, triggerEvent)
}

func (s *Service) startRunWithAutomation(ctx context.Context, automation *mailmodel.MailAutomationEntity,
	contactID uint64, triggerEvent string) (newRun bool, err error) {
	if automation == nil || automation.ID == 0 || contactID == 0 {
		return false, mailenums.Err(mailenums.ErrInvalidParam)
	}
	if _, cerr := s.m.GetContact(ctx, contactID); cerr != nil {
		return false, mailenums.Err(mailenums.ErrContactNotFound)
	}
	def, derr := ParseDefinition(automation.Definition)
	if derr != nil {
		return false, graphInvalidError(derr)
	}

	if _, aerr := s.m.ActiveRun(ctx, automation.ID, contactID); aerr == nil {

		return false, nil
	}

	if automation.TriggerFrequency == mailmodel.TriggerFrequencyOnce {
		seen, herr := s.m.HasAnyRun(ctx, automation.ID, contactID)
		if herr != nil {
			return false, herr
		}
		if seen {
			logger.Scene("mail").With("automation_id", automation.ID).With("contact_id", contactID).
				Info("流程配置为「只跑一次」，该联系人已有实例，本次触发跳过")
			return false, nil
		}
	}

	now := time.Now()
	entry := def.Entry
	e := &mailmodel.MailAutomationRunEntity{
		AutomationID:      automation.ID,
		AutomationVersion: automation.Version,
		ContactID:         contactID,
		Status:            mailmodel.RunStatusRunning,
		StartedAt:         &now,
	}
	if entry != "" {
		e.CurrentNode = &entry
	}
	if ev := strings.TrimSpace(triggerEvent); ev != "" {
		e.TriggerEvent = &ev
	}
	if cerr := s.m.CreateRun(ctx, e); cerr != nil {

		return false, nil
	}
	enqueueAutomationRun(e.ID, time.Time{})
	return true, nil
}

func (s *Service) StartRunsByTrigger(ctx context.Context, triggerType string, contactID uint64, extra map[string]any) (started int, err error) {
	autos, lerr := s.activeAutomationsFor(ctx, triggerType, extra)
	if lerr != nil {
		return 0, lerr
	}
	ok, serr := s.startRunsWithAutomations(ctx, autos, []uint64{contactID}, triggerType)
	return ok, serr
}

func (s *Service) activeAutomations(ctx context.Context) []*mailmodel.MailAutomationEntity {
	list, err := s.m.ListActiveAutomations(ctx)
	if err != nil {
		logger.Scene("mail").Error(err, "读取启用中的自动化流程失败，本次不触发（业务动作本身不受影响）")
		return nil
	}
	return list
}

func (s *Service) activeAutomationsFor(ctx context.Context, triggerType string, extra map[string]any) ([]*mailmodel.MailAutomationEntity, error) {
	list, err := s.m.ListActiveAutomations(ctx)
	if err != nil {
		return nil, err
	}
	return automationsForTrigger(list, triggerType, extra), nil
}

func automationsForTrigger(list []*mailmodel.MailAutomationEntity, triggerType string, extra map[string]any) []*mailmodel.MailAutomationEntity {
	out := make([]*mailmodel.MailAutomationEntity, 0, len(list))
	for _, a := range list {
		if a == nil || a.TriggerType != triggerType {
			continue
		}
		if !triggerParamsMatch(a.TriggerParams, extra) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func hasTriggerType(list []*mailmodel.MailAutomationEntity, triggerType string) bool {
	for _, a := range list {
		if a != nil && a.TriggerType == triggerType {
			return true
		}
	}
	return false
}

func (s *Service) startRunsWithAutomations(ctx context.Context, autos []*mailmodel.MailAutomationEntity,
	contactIDs []uint64, triggerEvent string) (started int, err error) {
	if len(autos) == 0 || len(contactIDs) == 0 {
		return 0, nil
	}
	seen := make(map[uint64]bool, len(contactIDs))
	var firstErr error
	for _, contactID := range contactIDs {
		if contactID == 0 || seen[contactID] {
			continue
		}
		seen[contactID] = true
		if ctx.Err() != nil {
			break
		}
		for _, a := range autos {
			ok, serr := s.startRunWithAutomation(ctx, a, contactID, triggerEvent)
			if serr != nil {

				if firstErr == nil {
					firstErr = serr
				}
				continue
			}
			if ok {
				started++
			}
		}
	}
	return started, firstErr
}

func (s *Service) StartRunsByTriggerBatch(ctx context.Context, triggerType string, contactIDs []uint64, extra map[string]any) (started int, err error) {
	autos, lerr := s.activeAutomationsFor(ctx, triggerType, extra)
	if lerr != nil {
		return 0, lerr
	}
	return s.startRunsWithAutomations(ctx, autos, contactIDs, triggerType)
}

func (s *Service) OnContactCreated(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactCreated, contactID, nil)
}

func (s *Service) OnContactsCreated(ctx context.Context, contactIDs []uint64) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTriggerBatch(ctx, mailmodel.TriggerContactCreated, contactIDs, nil)
}

func (s *Service) OnContactSubscribed(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactSubscribed, contactID, nil)
}

func (s *Service) OnTagsAdded(ctx context.Context, contactID uint64, tags []string) {
	for _, tag := range tags {
		s.fireTrigger(ctx, mailmodel.TriggerTagAdded, contactID, map[string]any{"tag": tag})
	}
}

func (s *Service) OnTagsAddedBatch(ctx context.Context, tag string, contactIDs []uint64) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTriggerBatch(ctx, mailmodel.TriggerTagAdded, contactIDs, map[string]any{"tag": tag})
}

func (s *Service) OnEmailOpened(ctx context.Context, contactID uint64) {
	if contactID > 0 {
		s.fireTrigger(ctx, mailmodel.TriggerEmailOpened, contactID, nil)
	}
}

func (s *Service) OnEmailReplied(ctx context.Context, contactID uint64) {
	if contactID == 0 {
		return
	}

	defer func() { _ = recover() }()
	if err := s.StopRunsOnReply(ctx, contactID); err != nil {
		logger.Scene("mail").With("contact_id", contactID).
			Error(err, "回信退出：停止流程实例失败（不影响回信本身）")
	}
}

func (s *Service) StopRunsOnReply(ctx context.Context, contactID uint64) error {
	runs, err := s.m.ListActiveRunsOfContact(ctx, contactID)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		return nil
	}
	now := time.Now()
	for _, run := range runs {
		automation, aerr := s.m.GetAutomation(ctx, run.AutomationID)
		if aerr != nil || !automation.ExitOnReply {
			continue
		}
		if serr := s.m.UpdateRunFields(ctx, run.ID, map[string]any{
			"status":        mailmodel.RunStatusStopped,
			"error_message": mailenums.EncodeRunText(mailenums.RunKeyStoppedByReply, nil),
			"finished_at":   now,
			"update_time":   now,
			"next_run_at":   nil,
		}); serr != nil {
			return serr
		}
	}
	return nil
}

func (s *Service) OnEmailClicked(ctx context.Context, contactID uint64) {
	if contactID > 0 {
		s.fireTrigger(ctx, mailmodel.TriggerEmailClicked, contactID, nil)
	}
}

func (s *Service) fireTrigger(ctx context.Context, triggerType string, contactID uint64, extra map[string]any) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTrigger(ctx, triggerType, contactID, extra)
}

func triggerParamsMatch(params map[string]any, extra map[string]any) bool {
	if len(params) == 0 {
		return true
	}
	wantTag := strings.TrimSpace(toString(params["tag"]))
	if wantTag == "" {
		return true
	}
	gotTag := ""
	if extra != nil {
		gotTag = strings.TrimSpace(toString(extra["tag"]))
	}
	return wantTag == gotTag
}

const TaskMailAutomationRun = "mail:automation_run"

type AutomationRunPayload struct {
	RunID uint64 `json:"run_id"`
}

var automationRunTask = queue.NewTask(TaskMailAutomationRun, queue.WithQueue("default"), queue.WithMaxRetry(3))

func enqueueAutomationRun(runID uint64, at time.Time) {
	if !queue.IsInited() || runID == 0 {
		return
	}
	p := AutomationRunPayload{RunID: runID}
	if at.IsZero() {
		_ = automationRunTask.Enqueue(p)
		return
	}
	_ = automationRunTask.EnqueueAt(at, p)
}

func RegisterMailAutomationTaskHandler(db *gorm.DB, cipherSecret string) {
	queue.Register(TaskMailAutomationRun, handleAutomationRun(db, cipherSecret))
}

func handleAutomationRun(db *gorm.DB, cipherSecret string) queue.Handler {
	return func(ctx context.Context, raw []byte) error {
		var p AutomationRunPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.RunID == 0 {
			return errors.New("推进任务载荷缺少 run_id")
		}
		svc := NewService(mailmodel.NewMailModel(db))
		svc.SetCipherSecret(cipherSecret)
		return svc.RunAutomation(ctx, p.RunID)
	}
}

type GraphError struct {
	key  string
	args []string
	text string
}

func (e *GraphError) Error() string {
	if e == nil {
		return ""
	}
	return e.text
}

func (e *GraphError) Detail() string {
	if e == nil {
		return ""
	}
	return i18n.ErrorDetail(e.key, e.args...)
}

func graphErr(key, tmpl string, kv ...string) *GraphError {
	params := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i]] = kv[i+1]
	}
	text, ok := i18n.FillNamedPlaceholders(tmpl, params)
	if !ok {

		text = tmpl
	}
	return &GraphError{key: key, args: append([]string(nil), kv...), text: text}
}

func graphInvalidError(err error) error {
	var ge *GraphError
	if errors.As(err, &ge) {
		return errors.New(mailenums.ErrAutomationGraphInvalid + ": " + ge.Detail())
	}
	return mailenums.Err(mailenums.ErrAutomationGraphInvalid)
}

func graphInvalidRequestError(reason string) error {
	return errors.New(mailenums.ErrAutomationGraphInvalid + ": " +
		i18n.ErrorDetail(mailenums.DetailGraphRequestNotJSON, "reason", reason))
}

func listStatusOutlet(status string) string {
	switch status {
	case mailmodel.ContactStatusSubscribed:
		return outletActive
	case mailmodel.ContactStatusPending:
		return outletNonActive
	default:
		return outletNotInList
	}
}

func abOutletOf(runID uint64, node AutomationNode) (string, error) {
	variants := abVariants(node.Params)
	if len(variants) == 0 {
		return "", errors.New(mailenums.EncodeRunText(mailenums.RunKeyOutletMissing,
			map[string]string{mailenums.RunArgNode: node.Key, mailenums.RunArgOutlet: "variants"}))
	}
	bucket := int(bucketOf(runID, node.Key) % 100)
	acc := 0
	for _, v := range variants {
		acc += v.Weight
		if bucket < acc {
			if node.outlet(v.ID) == "" {
				return "", errors.New(mailenums.EncodeRunText(mailenums.RunKeyOutletMissing,
					map[string]string{mailenums.RunArgNode: node.Key, mailenums.RunArgOutlet: v.ID}))
			}
			return v.ID, nil
		}
	}

	return "", errors.New(mailenums.EncodeRunText(mailenums.RunKeyOutletMissing,
		map[string]string{mailenums.RunArgNode: node.Key, mailenums.RunArgOutlet: "weight"}))
}

func bucketOf(runID uint64, nodeKey string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < 8; i++ {
		h ^= (runID >> (8 * i)) & 0xff
		h *= prime64
	}
	for i := 0; i < len(nodeKey); i++ {
		h ^= uint64(nodeKey[i])
		h *= prime64
	}
	return h
}

func (s *Service) postWebhook(ctx context.Context, node AutomationNode) (int, error) {
	raw := strings.TrimSpace(toString(node.Params["url"]))
	if raw == "" {
		return 0, errors.New(mailenums.EncodeRunText(mailenums.RunKeyWebhookNoURL, nil))
	}

	if err := ssrf.Check(ctx, raw, "https"); err != nil {
		return 0, errors.New(mailenums.EncodeRunText(mailenums.RunKeyWebhookDenied, nil))
	}
	body, _ := json.Marshal(map[string]any{"node": node.Key, "params": node.Params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := s.webhookClient
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,

			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("webhook 返回 %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}
