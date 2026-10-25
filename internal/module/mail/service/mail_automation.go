package mailservice

// 定义的**读写都是整体**：编辑器一次保存整张图，所以没有「改一个节点」这种接口。
// 版本号在每次保存定义时递增 —— 运行中的实例记着自己启动时的版本，
// 改图不会让它们执行到不存在的节点。

// 图校验是引擎的地基：一条指向不存在节点的边、一个死循环、一个永远走不到的节点，
// 在运行期表现为「某个人莫名其妙卡住了」，很难查。所以在**保存时**就拒绝。
//
// 五条校验：
//   1. 必须有 entry 且指向存在的节点；
//   2. 节点 key 非空且唯一；
//   3. 类型的出边形状正确（email / delay / tag 是单出边，branch 是两条，end 没有）；
//   4. 所有出边指向存在的节点；
//   5. **无环**，且从 entry 可达所有节点。
//
// 第 5 条为什么必须有：有环的图在运行期就是无限循环 —— 每一轮都会给同一个人发一次邮件，
// 直到把发信额度烧完或被收件方投诉。可达性是另一半：不可达的节点是作者写错了，
// 静默留着会让人以为「配好了」。

// 排障要回答的问题只有一个：**这个人卡在哪一步、为什么**。
// 光给 status 字段（running / waiting / failed）没有用 —— 运营看不懂，
// 也无法判断「该不该管」。所以每个实例都带一句 Explain。

// ## 执行模型
//
// 一个实例（run）由 current_node 指向「下一个要执行的节点」。执行器从那里开始一路往前走，
// 直到遇到**等待节点**（挂起，等调度唤醒）或**结束节点**（完成）。每一步都写一条节点日志。
//
// ## 幂等靠节点日志，不靠状态
//
// 队列会重试、任务会重复投递、人也可能手工重跑。判断「这一步做过没有」的唯一可靠依据是
// **节点日志里有没有这条 ok 记录**（NodeLogExists），而不是 run 的状态 —— 状态只表示
// 「实例整体在哪个阶段」，同一个阶段里可能有多个节点。没有这层判断，重试就等于
// 「同一个人再收一封一模一样的邮件」。
//
// ## 等待节点为什么先把游标推进再挂起
//
// delay 执行完就把 current_node 指到它的下一步，然后状态置 waiting、记 next_run_at、投递延时任务。
// 这样唤醒时直接从下一步继续，不必再判断「这个等待是不是已经等过了」——
// 游标本身就是那个判断。
//
// ## 步数上限是防呆，不是主防线
//
// 主防线是保存时的环检测。这里再设一个上限，是为了防止「校验之后图被改坏」
// （直接改库、并发写）导致执行器空转把 worker 占死。

// 等待节点的**主路径**是队列的延时任务（EnqueueAt）。但主路径可能失效：
//   · 队列重启时任务丢失（asynq 的延时任务在 Redis 里，Redis 掉数据就没了）；
//   · 队列当时未启用（任务被静默跳过）；
//   · worker 崩溃在入队与执行之间。
//
// 所以需要一个扫描器：把「已到点但仍挂着」的实例重新投递。它是**幂等安全**的 ——
// 重复投递最多让执行器多跑一次，而执行器靠节点日志幂等（同一步不会做两次）。
//
// 扫描本身只做「扫一轮」（EnqueueDueRuns / TickDueRuns），周期由本文件的
// StartMailAutomationScheduler 提供；运维也仍可手工触发一轮（排障时很有用）。

// 幂等由**部分唯一索引**兜底：同一人同流程只允许一个进行中的实例。
// 应用层先查一次是为了给出「已在流程中」这种可读结果，但真正的保证在数据库 ——
// 并发触发（两个事件同时到达）时应用层的「先查再插」必然漏，唯一索引才拦得住。

// 两个入口都会推进实例：
//   · 事件触发时**立即**投递一次（RunNow）；
//   · 等待节点挂起时按 next_run_at **延时投递**（EnqueueAt）。
//
// 延时用队列而不是自建调度器：队列的延时是持久化的（重启不丢），
// 且失败重试、超时、可视化都在队列侧已解决。

// 两条协议在这里汇合（见 pkg/i18n/errdetail.go 与 mailenums/mail_err_detail.go）：
//
//	① 图校验器（mail_automation.go）不再直接产出中文 error，而是 GraphError ——
//	   它的 Error() 仍是那句中文（日志与 mail_automation_graph_test.go 的断言看的都是它），
//	   同时 Detail() 给出 i18n.ErrorDetail 编码（词条 key + 具名参数）；
//	② service 包装点用 graphInvalidError 把「业务 key：明细编码」拼成业务错误，
//	   读侧（mail_err.go 的 translateMailFacing）据此按当前语言取词。
//
// 为什么不让校验器直接产出编码串：图校验的文本是**运维日志**与**单测**的判据
//（「流程里有环」「有节点从入口走不到」），把它们换成控制字符 + key 会让日志不可读、
// 单测断言全部失效。两份文本各有判据，所以都用**同一个模板**生成，不各写一遍。

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

// SaveAutomation 新建或更新流程（保存前校验图）。
func (s *Service) SaveAutomation(ctx context.Context, req *maildto.SaveAutomationReq) (res *maildto.AutomationItem, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	if !validTrigger(req.TriggerType) {
		return nil, errors.New(mailenums.ErrAutomationTriggerInvalid)
	}
	var raw map[string]any
	if len(req.Definition) > 0 {
		if uerr := json.Unmarshal(req.Definition, &raw); uerr != nil {
			return nil, graphInvalidRequestError(uerr.Error())
		}
	}
	// 保存时校验：指向不存在的节点 / 有环 / 不可达，都在这里被拒绝（见 graph 文件头说明）。
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
			return nil, errors.New(mailenums.ErrAutomationNotFound)
		}
		fields := map[string]any{
			"name":         strings.TrimSpace(req.Name),
			"description":  nullable(req.Description),
			"trigger_type": req.TriggerType,
			// TriggerParams 只在**调用方真的传了**时才改写。
			//
			// 为什么不能无条件写：画布（唯一的编辑器）只提交图与名称，不带触发条件 ——
			// 无条件写会把一条 tag_added 流程的「只对该标签触发」清成空 map，而
			// triggerParamsMatch 对空条件恒为 true：该流程此后对**任意**标签都触发。
			// 触发条件不是图的一部分（它是流程级的配置），改图不该顺手抹掉它。
			// 「想清空条件」的调用方传一个空 map 即可 —— 那是显式意图，与「没传」可区分。
			"definition": definition,
			// 定义变了就推进版本：运行中的实例继续用它们启动时的版本。
			"version":     old.Version + 1,
			"update_time": now,
		}
		if req.TriggerParams != nil {
			fields["trigger_params"] = trigger
		}
		// 频率与「回信即退出」同样只在调用方真的传了时才改写（画布不提交它们）。
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

// SaveAutomationLayout 保存画布位置（P4）。
//
// 三个刻意的决定：
//
//  1. **不动版本号**：位置不是流程语义。挪一下节点就让所有在跑的实例「版本落后」，
//     排障页面会给出误导信息。
//  2. **只改位置，不改结构**：从库里的定义读出来、只覆盖 x/y、再写回去。
//     这样即使编辑器传来的数据不完整，也不会把节点或连线弄丢。
//  3. **不认识的 key 直接忽略**：可能来自另一个标签页的旧画布。
func (s *Service) SaveAutomationLayout(ctx context.Context, req *maildto.SaveAutomationLayoutReq) (err error) {
	if req == nil || req.ID == 0 || len(req.Positions) == 0 {
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, gerr := s.m.GetAutomation(ctx, req.ID)
	if gerr != nil {
		return errors.New(mailenums.ErrAutomationNotFound)
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

// ListAutomations 流程列表。
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

// GetAutomation 流程详情。
func (s *Service) GetAutomation(ctx context.Context, id uint64) (res *maildto.AutomationItem, err error) {
	row, err := s.m.GetAutomation(ctx, id)
	if err != nil {
		return nil, errors.New(mailenums.ErrAutomationNotFound)
	}
	return automationItemOf(row), nil
}

// SetAutomationStatus 启用 / 暂停 / 退回草稿。
//
// 暂停**不影响已运行的实例**：它们已经在流程里了，半路停掉会让用户收到一半的流程
// （比如只收到欢迎邮件却没有后续的优惠券）。要阻止新实例进入才是暂停的语义。
func (s *Service) SetAutomationStatus(ctx context.Context, req *maildto.SetAutomationStatusReq) (err error) {
	if req == nil || req.ID == 0 {
		return errors.New(mailenums.ErrInvalidParam)
	}
	switch req.Status {
	case mailmodel.AutomationStatusDraft, mailmodel.AutomationStatusActive, mailmodel.AutomationStatusPaused:
	default:
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, gerr := s.m.GetAutomation(ctx, req.ID)
	if gerr != nil {
		return errors.New(mailenums.ErrAutomationNotFound)
	}
	// 启用前再校验一遍图：草稿期间如果被外部改动过（直接改库），启用就是最后一道关。
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

// DeleteAutomation 删除流程（进行中的实例先停掉，避免留下孤儿实例）。
//
// 「停实例 + 删流程」**同事务**：分开提交时第二步失败会留下「实例停了、流程还在」——
// 运营以为删掉了，刷新后它又出现，而它的实例再也不会推进（AGENTS.md「写操作的事务与回滚」）。
func (s *Service) DeleteAutomation(ctx context.Context, id uint64) (err error) {
	if _, gerr := s.m.GetAutomation(ctx, id); gerr != nil {
		return errors.New(mailenums.ErrAutomationNotFound)
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := s.m.StopRunsOfAutomationTx(ctx, tx, id, time.Now()); serr != nil {
			return serr
		}
		return s.m.DeleteAutomationTx(ctx, tx, id)
	})
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
				// 投影回 v1 的类型名：画布 JS / 表单还原 / MCP 工具都按 v1 取值，
				// 翻译只发生在这一处（见 dto/automation_node_type.go）。
				Type:   maildto.LegacyNodeType(n.Type),
				Params: n.Params,
				// 投影出边的三个旧字段（dto 形状在本次改造里不动）：画布与表单页
				// 读的都是它们。值来自 Out —— 出口名表的唯一真源。
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

// 节点类型（v2）。
//
// 出口名表在 automation_definition.go 的 nodeOutlets：新增类型只改那张表，
// 不要在这里加一个「只有本文件认识」的分支。
const (
	NodeTypeTrigger = "trigger"
	NodeTypeDelay   = "delay"
	NodeTypeEmail   = "email"
	// NodeTypeBranch 是 **v1 的类型名**：读取时被 normalizeDefinition 改写成 filter
	//（出口与条件都相同，差别只是 v2 多了 mode）。保留常量是为了让迁移前的历史行与
	// 既有单测还能引用它 —— 新代码一律用 NodeTypeFilter。
	NodeTypeBranch           = "branch"
	NodeTypeFilter           = "filter"
	NodeTypeListStatusBranch = "list_status_branch"
	NodeTypeABTest           = "ab_test"
	NodeTypeWebhook          = "webhook"
	NodeTypeTag              = "tag"
	NodeTypeEnd              = "end"
)

// 条件求值模式。
const (
	// ConditionModeAll 全部满足才走 yes（v1 的 AND 语义，也是缺省值）。
	ConditionModeAll = "all"
	// ConditionModeAny 任一满足即走 yes。
	ConditionModeAny = "any"
)

// AutomationDefinition 图定义。
type AutomationDefinition struct {
	// Schema 图定义版本。读到的 v1 会被升级成 automationSchemaV2，
	// 保存时按这个值落库（库里只有 v2）。
	Schema int              `json:"schema"`
	Entry  string           `json:"entry"`
	Nodes  []AutomationNode `json:"nodes"`
}

// AutomationNode 一个节点。
type AutomationNode struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`

	// Out 出边：出口名 → 目标节点 key。
	//
	// 出口名表由 nodeOutlets 定义（trigger/delay/email/tag/webhook 是 next，filter 是
	// yes/no，list_status_branch 是 not_in_list/active/non_active，ab_test 是变体 id，
	// end 没有出口）。空出口不入 map。
	Out map[string]string `json:"out,omitempty"`

	// Next / Yes / No 是 **v1 的扁平出边字段，已弃用**，读写都走 Out。
	//
	// 保留字段而不删除的原因有二：归一化之后仍有「直接构造结构体」的场景（单测、
	// 迁移前的库行经 outlet 兜底）；标 json:"-" 保证序列化出去的定义里只有 out，
	// v1 与 v2 不会共存于同一个节点的出边表达。
	Next string `json:"-"`
	Yes  string `json:"-"`
	No   string `json:"-"`

	// X / Y 节点在可视化画布上的位置（P4）。
	//
	// **引擎完全忽略它们** —— 它们是编辑器的布局数据，不是流程语义。放同一个 JSONB 里
	// 是因为「节点的位置」与「节点本身」同生命周期（删节点即删位置），拆出去反而要维护两份。
	// 保存位置走单独的接口、**不推进版本号**：挪一下位置不该让正在跑的实例「版本落后」。
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

// outgoing 返回该节点的所有出边（按出口名排序，日志与单测因此稳定）。
//
// 出口名到目标 key 的映射是唯一的真源（Out）；v1 的扁平字段只在 Out 为空时兜底。
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

// ParseDefinition 解析并校验图定义；返回纯 Go 结构供执行器使用。
//
// v1 的定义在这里升级成 v2（normalizeDefinition）：之后的校验、执行、画布、模板都
// 只面对一种形状。升级在**内存里**发生，落库要经 SaveAutomation / 迁移 621。
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

// ValidateDefinition 校验图定义，返回第一条可定位的问题。
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
			// 明细自带 {node} 参数（nodeArity 拿得到节点自身），这里不再包一层「节点 X:」——
			// 包了参数会重复出现两次，译文读起来是「节点 n1: 节点 n1: 等待…」。
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

	// 出边必须指向存在的节点。
	for _, n := range def.Nodes {
		for _, to := range n.outgoing() {
			if _, ok := byKey[to]; !ok {
				return graphErr(mailenums.DetailGraphEdgeTargetMissing,
					"节点 {from} 指向了不存在的节点 {to}", "from", n.Key, "to", to)
			}
		}
	}

	// 环检测 + 可达性：一次 DFS 同时得到两个结论。
	const (
		white = 0 // 未访问
		gray  = 1 // 在当前路径上（撞到即为环）
		black = 2 // 已完成
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

	// 不可达节点：作者写错了，静默留着会让人以为「配好了」。
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

// nodeArity 校验节点类型的出边形状与必需参数。
//
// 出边形状由 automation_definition.go 的 nodeOutlets 定义：本函数只回答「这一份定义配得对不对」，
// 不另写一张出口表。返回值的第二项是「该类型固定出口的个数」（仅用于日志/测试表达，
// 校验本身按出口名逐个查）。
//
// 明细自带 {node} 定位参数（与本函数返回的中文原文同源）：图校验错误要按当前语言
// 展示给作者，参数化的定位比「有一条边配错了」有用得多。调用方（ValidateDefinition）
// 因此不再包一层「节点 X:」。
func nodeArity(n AutomationNode) (int, error) {
	key := strings.TrimSpace(n.Key)
	if unknown := unknownOutlets(n); len(unknown) > 0 {
		return 0, graphErrUnknownOutlet(key, unknown[0])
	}
	switch n.Type {
	case NodeTypeTrigger:
		// 入口节点：有出边就往前走，没有就是「触发即结束」（合法但少用）。
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
		// mode 非法用**独立**的词条：复用 NeedCondition 会出两个不同原因共用一句话，
		// 中文界面上「模式填错了」显示成「需要至少一个条件」，与真正该改的地方无关。
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
		// 三个出口**必须全填**：少一条意味着「某一类状态的人走到这里就没了」，
		// 而执行器找不到目标会把实例判失败 —— 那本该在保存时就拒绝。
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
		// 保存时校验一次（执行时还会再校验一次：库里的地址可能被直接改过）。
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

// requireAllOutlets 校验「出口必须全填」的节点（list_status_branch）。
//
// filter 不走这里：它的两条出边在 v1 就已有专门的错误文案（「需要 yes 与 no 两条出边」），
// 换成通用的「缺少出口 {outlet}」会让既有运维手册与日志检索失效。
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

// checkABTest 校验分流转：2–5 个变体、id 唯一、权重和 = 100、每个变体都要有出口。
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
	// 权重和必须是 100：差一点就会让「按权重落桶」在边界上把最后一段人分不到任何变体。
	if total != 100 {
		return 0, graphErr(mailenums.DetailGraphVariantWeightBad,
			"节点 {node}: 变体权重之和必须是 100（当前 {weight}）", "node", key,
			"weight", strconv.Itoa(total))
	}
	// 每个变体都要有出口（少一条 = 落到这个变体的人走到这里就没了）。
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

// abVariant 一个 A/B 变体（id 是出口名，weight 是落桶权重）。
type abVariant struct {
	ID     string
	Name   string
	Weight int
}

// abVariants 从 params.variants 里读出变体（保持声明顺序 —— 落桶顺序必须是确定的）。
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

// conditionModeValid 条件模式只能是 all / any（空值按 all 处理 = v1 的 AND 语义）。
func conditionModeValid(params map[string]any) bool {
	switch strings.TrimSpace(toString(params["mode"])) {
	case "", ConditionModeAll, ConditionModeAny:
		return true
	}
	return false
}

// ---- 小工具（JSONB 解出来的数字是 float64，取整要小心）----

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

// ListAutomationRuns 实例列表（带联系人邮箱与流程名）。
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

// AutomationRunDetail 实例排障详情：状态 + 一句解释 + 节点时间线。
func (s *Service) AutomationRunDetail(ctx context.Context, runID uint64) (res *maildto.AutomationRunDetailResp, err error) {
	if runID == 0 {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	row, err := s.m.RunRowByID(ctx, runID)
	if err != nil || row.ID == 0 {
		return nil, errors.New(mailenums.ErrAutomationRunNotFound)
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
	// 进度分母：流程当前定义里的节点数（流程被改过时以现状为准 —— 排障看的是「还要走几步」）。
	if automation, aerr := s.m.GetAutomation(ctx, row.AutomationID); aerr == nil {
		if def, derr := ParseDefinition(automation.Definition); derr == nil {
			res.TotalNodes = len(def.Nodes)
		}
	}

	res.Explain = explainRun(row, res)
	return res, nil
}

// ListContactRuns 某联系人的自动化历史（联系人详情页用）。
func (s *Service) ListContactRuns(ctx context.Context, contactID uint64, limit int) (items []maildto.AutomationRunItem, err error) {
	if contactID == 0 {
		return nil, errors.New(mailenums.ErrInvalidParam)
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

// explainRun 把状态翻译成一句人话。
//
// 每种终态/中间态都要给出**下一步动作的线索**：
//   - 等待中 → 等多久（运营看到「还要等 3 天」就不会来问）；
//   - 失败 → 哪一步、什么原因、能不能重试；
//   - 完成 → 走到哪个结束了。
func explainRun(row mailmodel.RunRow, res *maildto.AutomationRunDetailResp) string {
	// 这里只组装「key + 参数」编码（enums.FormatRunText 在出口解码）：
	// 排障页会把 Explain 直接渲染出来，而 service 层拿不到请求语言。
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
		// 原因是**嵌套编码串**（error_message 本身也是运行文案编码）：FormatRunText 递归解码。
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

// maxStepsPerExecution 单次推进的最大步数。
const maxStepsPerExecution = 100

// RunAutomation 推进一个实例，直到挂起或结束。
func (s *Service) RunAutomation(ctx context.Context, runID uint64) (err error) {
	if runID == 0 {
		return errors.New(mailenums.ErrInvalidParam)
	}
	run, err := s.m.GetRun(ctx, runID)
	if err != nil {
		// 实例不存在：任务作废，不再重试。
		return nil
	}
	// 终态不再推进（重试到达时可能是已完成）。
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
		// 落库的是「key + 参数」编码（见 enums/mail_run_text.go）：定义错误的细节仍由
		// graph 校验器给（那一份由页面出口经 mailErrPageText 翻成提示页文案）。
		return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyDefInvalid, map[string]string{mailenums.RunArgReason: derr.Error()}))
	}
	byKey := make(map[string]AutomationNode, len(def.Nodes))
	for _, n := range def.Nodes {
		byKey[n.Key] = n
	}

	contact, err := s.m.GetContact(ctx, run.ContactID)
	if err != nil {
		// 联系人被删了：实例没有意义，标记停止（不是失败 —— 不是流程的问题）。
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

		// 幂等：这一步已经成功执行过就跳过动作，只推进游标。
		done, lerr := s.m.NodeLogExists(ctx, run.ID, node.Key)
		if lerr != nil {
			return lerr
		}

		switch node.Type {
		case NodeTypeTrigger:
			// 入口不做动作，直接放行。
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
			// 先推进游标再挂起：唤醒时直接从下一步开始（见文件头说明）。
			if err = s.m.UpdateRunFields(ctx, run.ID, map[string]any{
				"status":       mailmodel.RunStatusWaiting,
				"current_node": nullable(node.outlet(outletNext)),
				"next_run_at":  due,
				"update_time":  time.Now(),
			}); err != nil {
				return err
			}
			// 投递延时任务；队列不可用则留给调度器扫描（DueRuns）兜底。
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
				// 发信失败：写失败日志并让任务重试（队列自带退避）。
				s.logNode(ctx, run.ID, node, mailmodel.NodeStatusFailed,
					mailenums.EncodeRunText(mailenums.RunKeySendFailed,
						map[string]string{mailenums.RunArgReason: serr.Error()}))
				_ = s.m.UpdateRunFields(ctx, run.ID, map[string]any{
					"error_message": serr.Error(), "update_time": time.Now(),
				})
				return serr
			}
			// 四种组合各有一条词条：拼接（「已发信」+「（主题覆盖: x）」）在取词之后
			// 就再也拆不开，词序也无法按语言调整，所以宁可多三条词条。
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
				// 条件写错不该静默走 no 分支 —— 那会让「配置错误」伪装成「条件不满足」。
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
				// 校验时已要求三条出口全填；走到这里说明库里的图被改过。
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
			// 回调失败**不终止流程**：下游动作还要执行，而「第三方暂时 500」不该吃掉用户
			// 后面那封邮件。失败记一条 failed 日志（排障看得到），然后照常沿 next 前进。
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

	// 步数超限：多半是图被改坏了（保存时的环检测本该拦住）。
	return s.failRun(ctx, run.ID, mailenums.EncodeRunText(mailenums.RunKeyStepsExceeded, nil))
}

// advance 更新实例的游标。
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

// logNode 写一条节点日志（失败不影响主流程）。
//
// detail 一律是 enums 的运行文案编码（key + 参数），**不是**当时的语言文本 ——
// 它会被排障页直接渲染，落中文等于把语言固化进数据（见 enums/mail_run_text.go）。
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

// applyTags 应用标签节点的增删，返回（新增、移除）。幂等：已存在的标签不重复加。
//
// 「读标签 → 算增减 → 写回」是典型的**读-改-写**，所以整段落进一个事务，读用
// SELECT … FOR UPDATE 锁住联系人行（AGENTS.md「读-改-写必须有行锁或原子 SQL」）：
// 不加锁时两个自动化节点、或标签节点与后台手工改标签并发，后写者会拿旧快照覆盖前者的结果
// （标签是整列写回，丢的是别人的整个标签集，不只是自己那一项）。
// 单行按主键加锁，不涉及多行加锁顺序，无死锁面。
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

// automationVars 组装发信节点的模板变量：先放联系人字段，再放节点自己配的。
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

// Condition 结构化条件（图定义 v2 的 `params.conditions` 元素形态之一）。
//
// 另一种元素形态是**字符串**（v1 的前缀语义：opened / clicked / subscribed / has_tag:X）——
// 两种都要支持，因为存量定义里全是字符串，而新画布写的是对象。
//
// 字段名限定在 mail 联系人的列上（status / tags / email / name / source /
// last_activity_at）；未知字段一律报错，不默认 false。
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// 条件操作符（封闭集合）。
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

// evalConditions 求值条件分支。
//
// 语义由 `params.mode` 决定：`all`（缺省，v1 的 AND 语义）与 `any`（任一满足即真）。
// 两个模式都**短路**：all 遇到第一个假就停（省掉后续查询），any 遇到第一个真就停。
//
// 条件元素支持两种形态：
//
//	字符串   opened / clicked / subscribed / has_tag:<标签>（v1 前缀语义，行为不变）
//	对象     {field, op, value}（v2 结构化条件）
//
// **未知条件 / 未知字段 / 未知操作符一律报错**，不静默当 false —— 否则「配置写错」会
// 伪装成「条件不满足」，表现是「这批人怎么都走了 no 分支」，极难查。
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
		// 短路：any 命中即真、all 落空即假。
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

// evalOneCondition 求值单个条件（字符串形态或对象形态）。
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

// evalPrefixCondition v1 的前缀式条件（行为与改造前逐字一致）。
func (s *Service) evalPrefixCondition(ctx context.Context, contact *mailmodel.MailContactEntity, cond string) (bool, error) {
	switch {
	case cond == "":
		// 空串不算一个条件：它既不是真也不是假，跳过（由调用方的计数决定整体结果）。
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

// evalStructuredCondition v2 的结构化条件 {field, op, value}。
//
// 字段与操作符都是封闭集合：**未知的一律报错**（不默认 false）。判据与 v1 的前缀式
// 一致 —— 配置写错必须让人看见，而不是伪装成「条件不满足」。
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
			// 从未活动过：比较类操作一律为假（而不是报错）—— 这是一个真实状态。
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

// conditionErr 结构化条件的可定位错误（带 field / op / value）。
func conditionErr(cond Condition, reason string) error {
	return errors.New(mailenums.EncodeRunText(mailenums.RunKeyCondInvalid, map[string]string{
		mailenums.RunArgCondition: fmt.Sprintf("%s %s %v（%s）", cond.Field, cond.Op, cond.Value, reason),
	}))
}

// compareString 字符串字段的比较（eq / neq / 包含类）。
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

// compareTime 时间字段的比较（gt / gte / lt / lte）。
//
// value 接受 RFC3339 或「相对天数」（如 `-7`）：后者在界面上更容易填（「7 天内活跃过」），
// 而负数代表过去。
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

// compareTags 标签数组的比较（has / not_has）。
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

// tagListText 标签列表 → 展示文本（空列表给「—」，避免词条里出现空的 %s）。
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
	// mailAutomationTickInterval 兜底扫描的间隔。
	//
	// 为什么钉在 1 分钟：等待节点的最小粒度就是 1 分钟（图校验要求 minutes 是正整数，
	// 见 mail_automation.go 的节点校验），扫描间隔取同一粒度意味着
	// 「到点」与「被重新投递」之间的最坏延迟不超过这张图能表达的最短等待时长。
	// 间隔再大就会让「等 1 分钟」的节点在最坏情况下多挂数倍时间，而扫描本身很轻
	//（status='waiting' AND next_run_at <= now，走既有索引，单轮最多 mailAutomationTickLimit 条）。
	mailAutomationTickInterval = time.Minute
	// mailAutomationTickLimit 单轮最多重新投递多少个实例（与 EnqueueDueRuns 的 limit 同义）。
	mailAutomationTickLimit = 500
	// mailAutomationTickTimeout 单轮扫描的超时：一轮卡住不该让 ticker 堆积。
	mailAutomationTickTimeout = 30 * time.Second
)

// EnqueueDueRuns 扫描已到点的等待实例并重新投递，返回投递数量。
//
// 队列未启用时 enqueueAutomationRun 是 no-op：此时投递数仍如实返回
// （它表示「本轮选中了多少个到点实例」），运营据此能看出「兜底在跑但队列是关的」。
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

// TickDueRuns 扫一轮并返回扫到的实例数（由 StartMailAutomationScheduler 周期驱动，
// 也可由运维经后台「补投一轮」手工触发；语义同 EnqueueDueRuns）。
func (s *Service) TickDueRuns(ctx context.Context) (int, error) {
	return s.EnqueueDueRuns(ctx, mailAutomationTickLimit)
}

// StartMailAutomationScheduler 启动延时兜底的周期扫描（进程内 goroutine + ticker）。
//
// 与 StartMailRetentionScheduler 同形：测试进程不启动（首跑会动真实库）、
// 首跑一次再等 ticker、单轮 panic 收敛成日志（一轮的 bug 不该终止进程）。
//
// 为什么必须有人驱动：等待节点的唤醒**原本只有队列延时的主路径**，Redis 掉数据 /
// queue.enabled=false / worker 崩在入队与执行之间时，到点的实例会永远挂着 ——
// 而这条扫描正是 run 文件里写明的兜底保证。
func StartMailAutomationScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	startMailAutomationScheduler(svc, mailAutomationTickInterval)
}

// StartMailAutomationSchedulerWithInterval 同上，但可注入间隔（测试用：
// 远大于用例时长的间隔可证成「首跑确实发生在启动时」，毫秒级间隔可证成「等间隔在驱动」）。
func StartMailAutomationSchedulerWithInterval(svc *Service, interval time.Duration) {
	startMailAutomationScheduler(svc, interval)
}

func startMailAutomationScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	runMailAutomationTickLoop(func() {
		// goroutine 里未 recover 的 panic 会终止整个进程：一轮的 bug 只该让这一轮没有结论。
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

// runMailAutomationTickLoop 调度循环本体：先跑一次，再按 interval 等间隔重复。
//
// 抽成「接受一轮动作」的形状是为了可测（与 media 巡检的 runMediaReconcileLoop 同形）：
// 目标动作要数据库与队列，模块内单测不碰这些依赖，但**调度语义**（首跑 / 等间隔 /
// 单轮 panic 不致命）与动作内容无关 —— 这三件事写反了的表现都是「看起来在跑、其实没动」。
//
// 返回的 stop 关闭后循环退出；生产装配不调用它（进程退出即结束），测试用它收尾。
func runMailAutomationTickLoop(run func(), interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = mailAutomationTickInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		run() // 首跑：与 retention / media 巡检同一节奏（不是先等一个间隔）
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

// StartRun 启动一个实例。返回是否新启动（false 表示该联系人已在此流程中）。
func (s *Service) StartRun(ctx context.Context, automationID, contactID uint64, triggerEvent string) (newRun bool, err error) {
	if automationID == 0 || contactID == 0 {
		return false, errors.New(mailenums.ErrInvalidParam)
	}
	automation, err := s.m.GetAutomation(ctx, automationID)
	if err != nil {
		return false, errors.New(mailenums.ErrAutomationNotFound)
	}
	// 只有启用中的流程接受新实例（暂停/草稿不收）。
	if automation.Status != mailmodel.AutomationStatusActive {
		return false, nil
	}
	return s.startRunWithAutomation(ctx, automation, contactID, triggerEvent)
}

// startRunWithAutomation 用**已取到的**流程实体启动实例。
//
// 与 StartRun 的唯一差异：调用方已经确认过流程存在且启用（批量路径用
// automationsForTrigger 过滤），所以这里不再重查流程行 —— 那次查询正是批量路径要消掉的
// 每人一条。启动规则本身只有这一份实现，StartRun 委托到这里，不存在第二份真源。
func (s *Service) startRunWithAutomation(ctx context.Context, automation *mailmodel.MailAutomationEntity,
	contactID uint64, triggerEvent string) (newRun bool, err error) {
	if automation == nil || automation.ID == 0 || contactID == 0 {
		return false, errors.New(mailenums.ErrInvalidParam)
	}
	if _, cerr := s.m.GetContact(ctx, contactID); cerr != nil {
		return false, errors.New(mailenums.ErrContactNotFound)
	}
	def, derr := ParseDefinition(automation.Definition)
	if derr != nil {
		return false, graphInvalidError(derr)
	}

	if _, aerr := s.m.ActiveRun(ctx, automation.ID, contactID); aerr == nil {
		// 已在流程中：不重复启动。
		return false, nil
	}
	// 频率闸门：`once` 时「有过任何实例」就不再建。
	//
	// 为什么不能只靠 `idx_mail_automation_runs_active`：那个部分唯一索引只管
	// running / waiting，而 completed / failed / stopped 不在里面 —— 「打开邮件」
	// 这类可重复事件会一个接一个地重跑整条欢迎流程。这里的判据必须是**任何**实例，
	// 而不只是「进行中的实例」。
	//
	// 走 (automation_id, contact_id) 索引（迁移 623 加的），不是全表扫描。
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
		// 唯一索引冲突 = 并发下已有人建了同一个实例，按「已在流程中」处理，不是错误。
		return false, nil
	}
	enqueueAutomationRun(e.ID, time.Time{})
	return true, nil
}

// StartRunsByTrigger 按触发方式批量启动（事件进入时调用）。
//
// 匹配规则：流程启用中、trigger_type 相符、trigger_params 的条件也满足。
// 目前只支持 tag_added 的标签匹配；其余触发方式（opened / clicked 等）不带附加条件。
//
// ⚠️ 这个入口**每个联系人查一次流程表**。只处理单个联系人的事件（打开 / 点击 / 订阅）
// 可以用它；**批量场景（导入）必须走下面的批量入口** —— 否则 2000 行导入会发出 2000 条
// 流程查询（护栏测试 TestImportWriteStatementProfile 实测 2006 条语句）。
func (s *Service) StartRunsByTrigger(ctx context.Context, triggerType string, contactID uint64, extra map[string]any) (started int, err error) {
	autos, lerr := s.activeAutomationsFor(ctx, triggerType, extra)
	if lerr != nil {
		return 0, lerr
	}
	ok, serr := s.startRunsWithAutomations(ctx, autos, []uint64{contactID}, triggerType)
	return ok, serr
}

// ActiveAutomations 取一次「启用中的流程全集」（批量路径的预检与触发共用同一份结果）。
//
// 读失败不返回错误：触发是业务动作的附加行为，读不到流程表不该让导入 / 注册失败
// （与 fireTrigger 同一取舍），失败记结构化日志后按「没有流程」处理。
func (s *Service) activeAutomations(ctx context.Context) []*mailmodel.MailAutomationEntity {
	list, err := s.m.ListActiveAutomations(ctx)
	if err != nil {
		logger.Scene("mail").Error(err, "读取启用中的自动化流程失败，本次不触发（业务动作本身不受影响）")
		return nil
	}
	return list
}

// activeAutomationsFor 取启用中的流程里匹配该触发器的那些（**一次查询**）。
func (s *Service) activeAutomationsFor(ctx context.Context, triggerType string, extra map[string]any) ([]*mailmodel.MailAutomationEntity, error) {
	list, err := s.m.ListActiveAutomations(ctx)
	if err != nil {
		return nil, err
	}
	return automationsForTrigger(list, triggerType, extra), nil
}

// automationsForTrigger 从流程全集里挑出匹配的（纯函数，便于单测）。
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

// hasTriggerType 流程全集里是否有该触发器类型的流程（**不判 trigger_params**）。
//
// 用途是「粗判有没有这一类流程」：调用方据此决定要不要做那些「只有存在流程时才有意义」
// 的准备工作（例如导入时读每个联系人的旧标签来算差集）。用带参数匹配的过滤函数做这件事是错的 ——
// extra 为 nil 时带 tag 条件的流程不会被匹配上，粗判会假阴性。
func hasTriggerType(list []*mailmodel.MailAutomationEntity, triggerType string) bool {
	for _, a := range list {
		if a != nil && a.TriggerType == triggerType {
			return true
		}
	}
	return false
}

// startRunsWithAutomations 用**已取到的**流程集合给一批联系人各自建实例。
//
// 这是批量路径的实例层写入主体：流程查询由调用方一次完成，这里每个联系人只剩
// 「联系人存在性 + 幂等检查 + 建实例」三件事。无流程时（len==0）一次查询都不发。
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
				// 单人失败不中断整批（与 StartRunsByTrigger 一致），但要留一个错误出口：
				// 全部失败时调用方至少能看出「这一次触发整体没成」。
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

// StartRunsByTriggerBatch 批量触发：一次流程查询，然后逐联系人 × 匹配流程建实例。
//
// 与 StartRunsByTrigger 的分工就是这个「一次」：批量导入调用它，避免每个联系人各查一次
// 流程表。无匹配流程时**不做任何实例层查询**（零配置站点的批量导入不该为触发付费）。
func (s *Service) StartRunsByTriggerBatch(ctx context.Context, triggerType string, contactIDs []uint64, extra map[string]any) (started int, err error) {
	autos, lerr := s.activeAutomationsFor(ctx, triggerType, extra)
	if lerr != nil {
		return 0, lerr
	}
	return s.startRunsWithAutomations(ctx, autos, contactIDs, triggerType)
}

// ---- 事件触发（issue #38 P3）----
//
// 每个业务动作结束后调用对应方法，把「发生了什么」交给引擎去匹配流程。
// **触发失败绝不影响主流程**：注册 / 导入 / 订阅 / 追踪这些动作本身是用户要的结果，
// 自动化只是附加行为 —— 它出问题不该让注册失败。
//
// 一条边界：**自动化内部的标签节点不再触发 tag_added**。否则「流程 A 加了 tag X」
// 会触发「流程 B」，B 又加 tag Y 触发 A，形成跨流程的递归。标签触发只认**外部**改动
//（后台手工、导入、前台订阅）。

// OnContactCreated 新联系人产生（单条事件源：注册 / 后台手工建号）。
func (s *Service) OnContactCreated(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactCreated, contactID, nil)
}

// OnContactsCreated 一批新联系人产生（导入路径；**一次流程匹配**，不做 N+1）。
//
// 与 OnContactCreated 的关系：同一个触发器、同一个引擎，差别只在「流程表查几次」。
// 导入 2000 行时逐人调用单条入口会发出 2000 条流程查询（护栏测试
// TestImportWriteStatementProfile 守的就是这条），所以批量生产者必须走这里。
func (s *Service) OnContactsCreated(ctx context.Context, contactIDs []uint64) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTriggerBatch(ctx, mailmodel.TriggerContactCreated, contactIDs, nil)
}

// OnContactSubscribed 联系人变为已订阅。
func (s *Service) OnContactSubscribed(ctx context.Context, contactID uint64) {
	s.fireTrigger(ctx, mailmodel.TriggerContactSubscribed, contactID, nil)
}

// OnTagsAdded 联系人被外部加上标签（单条）。
//
// 自动化内部的标签动作不调用它（见上方边界说明）；导入路径**多联系人时**走
// OnTagsAddedBatch（同触发器、同引擎，只是流程表查一次）。
func (s *Service) OnTagsAdded(ctx context.Context, contactID uint64, tags []string) {
	for _, tag := range tags {
		s.fireTrigger(ctx, mailmodel.TriggerTagAdded, contactID, map[string]any{"tag": tag})
	}
}

// OnTagsAddedBatch 一批联系人被打上**同一个**标签（导入路径；一次流程匹配）。
//
// 按标签分组调用：trigger_params 的 tag 条件是按标签匹配的，不同标签要各自匹配一次。
func (s *Service) OnTagsAddedBatch(ctx context.Context, tag string, contactIDs []uint64) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTriggerBatch(ctx, mailmodel.TriggerTagAdded, contactIDs, map[string]any{"tag": tag})
}

// OnEmailOpened / OnEmailClicked 追踪事件（在事件落库之后调用）。
func (s *Service) OnEmailOpened(ctx context.Context, contactID uint64) {
	if contactID > 0 {
		s.fireTrigger(ctx, mailmodel.TriggerEmailOpened, contactID, nil)
	}
}

// OnEmailReplied 收到回信：按 exit_on_reply 停掉该联系人进行中的实例。
//
// ## 为什么这条不等于「触发流程」
//
// 回信是**退出信号**而不是进入信号：给一个刚回信的人继续发自动化营销邮件是最典型的
// 骚扰（而且他会回信正是因为想找人说人话）。所以这里做的只有一件事 —— 停实例。
//
// ## 当前没有生产者
//
// 本仓不接 IMAP / 不解析收件箱，`model.EventTypeReply` 因此没有写入方。这个方法与常量
// 是**接入点**：未来的收信 worker 只需要调 `OnEmailReplied(contactID)`。
// 测试直接调它来验证 exit_on_reply 的语义（见 automation_frequency_test.go）。
func (s *Service) OnEmailReplied(ctx context.Context, contactID uint64) {
	if contactID == 0 {
		return
	}
	// 触发失败绝不影响主流程（与其它 On* 同一约定）：回信记录本身才是用户要的结果。
	defer func() { _ = recover() }()
	if err := s.StopRunsOnReply(ctx, contactID); err != nil {
		logger.Scene("mail").With("contact_id", contactID).
			Error(err, "回信退出：停止流程实例失败（不影响回信本身）")
	}
}

// StopRunsOnReply 把该联系人**进行中的**实例置 stopped（只对 exit_on_reply=true 的流程）。
//
// 逐条按流程配置判断：同一个联系人可能同时在不同的流程里，其中一个配了 exit_on_reply、
// 另一个没配 —— 停错那条就是「配了 A 的开关，B 也跟着停了」。
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

// OnEmailClicked 点击事件触发。
func (s *Service) OnEmailClicked(ctx context.Context, contactID uint64) {
	if contactID > 0 {
		s.fireTrigger(ctx, mailmodel.TriggerEmailClicked, contactID, nil)
	}
}

// fireTrigger 内部入口：吞掉错误（触发不该影响调用方的主流程）。
func (s *Service) fireTrigger(ctx context.Context, triggerType string, contactID uint64, extra map[string]any) {
	defer func() { _ = recover() }()
	_, _ = s.StartRunsByTrigger(ctx, triggerType, contactID, extra)
}

// triggerParamsMatch 判断流程的触发条件是否与本次事件相符。
//
// 目前只认 tag_added 的 tag 字段：不匹配就跳过。其余触发方式不带条件。
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

// TaskMailAutomationRun 实例推进任务类型。
const TaskMailAutomationRun = "mail:automation_run"

// AutomationRunPayload 推进任务载荷。
type AutomationRunPayload struct {
	RunID uint64 `json:"run_id"`
}

var automationRunTask = queue.NewTask(TaskMailAutomationRun, queue.WithQueue("default"), queue.WithMaxRetry(3))

// enqueueAutomationRun 投递推进任务（at 为零表示立即）。
//
// 队列不可用时**不报错也不同步兜底**：自动化是后台行为，没有人等着它返回；
// 挂起的实例会由调度扫描（DueRuns）重新拾起，所以静默跳过是安全的。
// 这一点与「注册验证邮件」不同 —— 那个失败要让用户看到可重发。
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

// RegisterMailAutomationTaskHandler 注册推进 handler（装配期调用）。
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

// GraphError 一条图校验失败（中文原文 + 词条 key + 具名参数）。
type GraphError struct {
	key  string
	args []string // name, value 交替（i18n.ErrorDetail 的入参形态）
	text string   // 中文原文（含已填充的参数）
}

// Error 中文原文：日志与单测断言看这一份。
func (e *GraphError) Error() string {
	if e == nil {
		return ""
	}
	return e.text
}

// Detail i18n.ErrorDetail 编码：写侧拼进业务错误的 tail，读侧据此取词。
func (e *GraphError) Detail() string {
	if e == nil {
		return ""
	}
	return i18n.ErrorDetail(e.key, e.args...)
}

// graphErr 造一条 GraphError。
//
// tmpl 是**中文模板**（`{name}` 占位），kv 是 name, value 交替 —— 参数只写一遍，
// 中文原文由模板填充得到，编码串由同一组参数生成，两边不可能漂移。
func graphErr(key, tmpl string, kv ...string) *GraphError {
	params := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i]] = kv[i+1]
	}
	text, ok := i18n.FillNamedPlaceholders(tmpl, params)
	if !ok {
		// 模板与参数不匹配是**代码缺陷**（少传一个参数）：不让它静默 ——
		// 原文回落模板本身，日志里能一眼看出漏填的 `{name}`。
		text = tmpl
	}
	return &GraphError{key: key, args: append([]string(nil), kv...), text: text}
}

// graphInvalidError 图校验错误 → 业务错误（key + 可翻译明细）。
//
// 不是 GraphError 时只给业务 key、**不留 tail**：那种情况是 JSON / 装配层的原文，
// 词条化不了 —— 按读侧协议「不是词条就丢弃并落日志」，这里直接不编码，
// 免得读侧为它多记一条「形态不符」的日志。
func graphInvalidError(err error) error {
	var ge *GraphError
	if errors.As(err, &ge) {
		return errors.New(mailenums.ErrAutomationGraphInvalid + ": " + ge.Detail())
	}
	return errors.New(mailenums.ErrAutomationGraphInvalid)
}

// graphInvalidRequestError 请求体里的 definition 不是合法 JSON → 业务错误（带解析器原文）。
//
// reason 是 json 解码器给的位置说明（数据，不是内部细节），所以作为具名参数进词条，
// 让运营知道「哪一段不是 JSON」，而不是只看到一句「流程定义不合法」。
func graphInvalidRequestError(reason string) error {
	return errors.New(mailenums.ErrAutomationGraphInvalid + ": " +
		i18n.ErrorDetail(mailenums.DetailGraphRequestNotJSON, "reason", reason))
}

// listStatusOutlet 联系人状态 → list_status_branch 的出口名。
//
// 三档映射刻意是**封闭**的：`subscribed` 走 active、`pending`（待确认同意）走 non_active、
// 其余（含退订 / 抑制 / 空串）走 not_in_list。多出来的状态归到「不在名单」而不是「未激活」，
// 因为「不在名单」的语义是「这个人不该被营销」，那才是保守的一侧。
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

// abOutletOf 决定这次执行落哪个变体（确定性分桶）。
//
// 找不到可用出口时**报错**而不是静默走第一个：静默的后果是 A/B 统计里所有失败案例都被
// 计进 A 组，而运营看不出分布已经歪了。
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
	// 权重和不足 100 时落桶可能落在所有变体之外（校验会拒，但库里的图可能被改过）。
	return "", errors.New(mailenums.EncodeRunText(mailenums.RunKeyOutletMissing,
		map[string]string{mailenums.RunArgNode: node.Key, mailenums.RunArgOutlet: "weight"}))
}

// bucketOf 确定性分桶：同一个 (runID, nodeKey) **永远**落同一个桶。
//
// 为什么必须确定性：A/B 的统计前提是「同一个人反复经过这个节点时看到的是同一个变体」。
// 用 map 迭代顺序或 rand 的话，队列重试 / 补跑会让同一个人换分支 —— 那不只是统计作废，
// 用户还会先后收到两个版本的邮件。
//
// 用 FNV-1a 而不是 hash/maphash：后者每次进程启动换种子，跨重启就不确定了。
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

// postWebhook 执行一次回调（同步、有超时、不跟随重定向）。
//
// 三条约束：
//  1. **不跟随重定向**：重定向目标没过 SSRF 校验，跟随等于把校验绕过去；
//  2. **执行时再校验一次地址**：库里的 URL 可能被直接改过（保存时的校验只挡住正常路径）；
//  3. 返回状态码是为了让它进节点日志 —— 「第三方到底回了什么」是排障的第一个问题。
func (s *Service) postWebhook(ctx context.Context, node AutomationNode) (int, error) {
	raw := strings.TrimSpace(toString(node.Params["url"]))
	if raw == "" {
		return 0, errors.New(mailenums.EncodeRunText(mailenums.RunKeyWebhookNoURL, nil))
	}
	// SSRF 判定：内网 / 环回 / 链路本地一律拒绝（判定本体在 pkg/ssrf，与 webhook 模块共用）。
	//
	// 这里**不豁免**任何客户端：`webhookClient` 是可注入的（测试与特殊部署替换），
	// 而「因为测试要发请求所以跳过校验」正是把防线开出后门的那种写法。要验回调成功
	// 的路径，用 httptest 起一个监听 127.0.0.1 的服务是行不通的（它仍在内网）——
	// 所以 T4 只判「被拒后流程继续」，成功路径由 pkg/ssrf 与 webhook 侧的用例覆盖。
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
			// 不跟随重定向：目标未过 SSRF 校验。
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	// 只读一小段：回执内容不进日志（可能含第三方敏感信息），状态码足够定位。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("webhook 返回 %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}
