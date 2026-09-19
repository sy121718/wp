package mailservice

// mail_automation_run.go — 节点执行器（issue #38 P3）。
//
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

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

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
		return s.failRun(ctx, run.ID, "流程已被删除")
	}
	def, derr := ParseDefinition(automation.Definition)
	if derr != nil {
		return s.failRun(ctx, run.ID, "流程定义不合法: "+derr.Error())
	}
	byKey := make(map[string]AutomationNode, len(def.Nodes))
	for _, n := range def.Nodes {
		byKey[n.Key] = n
	}

	contact, err := s.m.GetContact(ctx, run.ContactID)
	if err != nil {
		// 联系人被删了：实例没有意义，标记停止（不是失败 —— 不是流程的问题）。
		return s.stopRun(ctx, run.ID, "联系人不存在")
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
			return s.failRun(ctx, run.ID, fmt.Sprintf("节点不存在: %s", current))
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
				s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, "触发进入流程")
			}
			current = node.Next
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeDelay:
			if done {
				current = node.Next
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
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, fmt.Sprintf("等待 %d 分钟后继续", mins))
			// 先推进游标再挂起：唤醒时直接从下一步开始（见文件头说明）。
			if err = s.m.UpdateRunFields(ctx, run.ID, map[string]any{
				"status":       mailmodel.RunStatusWaiting,
				"current_node": nullable(node.Next),
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
				current = node.Next
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
				s.logNode(ctx, run.ID, node, mailmodel.NodeStatusFailed, "发信失败: "+serr.Error())
				_ = s.m.UpdateRunFields(ctx, run.ID, map[string]any{
					"error_message": serr.Error(), "update_time": time.Now(),
				})
				return serr
			}
			detail := "已发信"
			if res != nil && res.Suppressed {
				detail = "地址在抑制名单中，未发送"
			}
			if subject != "" {
				detail += "（主题覆盖: " + subject + "）"
			}
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, detail)
			contact = refreshContact(ctx, s, contact)
			current = node.Next
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeBranch:
			matched, berr := s.evalConditions(ctx, contact, node.Params)
			if berr != nil {
				// 条件写错不该静默走 no 分支 —— 那会让「配置错误」伪装成「条件不满足」。
				return s.failRun(ctx, run.ID, berr.Error())
			}
			branch := node.No
			tag := "条件不满足，走 no 分支"
			if matched {
				branch = node.Yes
				tag = "条件满足，走 yes 分支"
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
				current = node.Next
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
				fmt.Sprintf("加标签 %v，去标签 %v", added, removed))
			contact = refreshContact(ctx, s, contact)
			current = node.Next
			if err = s.advance(ctx, run.ID, current); err != nil {
				return err
			}
			if current == "" {
				return s.completeRun(ctx, run.ID)
			}

		case NodeTypeEnd:
			s.logNode(ctx, run.ID, node, mailmodel.NodeStatusOK, "流程结束")
			return s.completeRun(ctx, run.ID)

		default:
			return s.failRun(ctx, run.ID, "未知节点类型: "+node.Type)
		}
	}

	// 步数超限：多半是图被改坏了（保存时的环检测本该拦住）。
	return s.failRun(ctx, run.ID, "流程执行步数超过上限，可能存在环")
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

// evalConditions 求值条件分支（**AND 语义**：全部满足才走 yes）。
//
// 支持的条件（前缀式，便于将来扩展且不引入表达式解析器）：
//
//	opened              打开过任意营销邮件
//	clicked             点击过任意营销链接
//	subscribed          当前是已订阅
//	has_tag:<标签>      当前带某个标签
//
// **未知条件直接报错**，不静默当 false —— 否则「配置写错」会伪装成「条件不满足」，
// 表现是「这批人怎么都走了 no 分支」，极难查。
func (s *Service) evalConditions(ctx context.Context, contact *mailmodel.MailContactEntity, params map[string]any) (bool, error) {
	conds := toStringSlice(params["conditions"])
	if len(conds) == 0 {
		return false, errors.New("条件分支没有条件")
	}
	for _, c := range conds {
		cond := strings.TrimSpace(c)
		switch {
		case cond == "":
			continue
		case cond == "opened":
			has, err := s.m.ContactHasEvent(ctx, contact.ID, mailmodel.EventTypeOpen)
			if err != nil {
				return false, err
			}
			if !has {
				return false, nil
			}
		case cond == "clicked":
			has, err := s.m.ContactHasEvent(ctx, contact.ID, mailmodel.EventTypeClick)
			if err != nil {
				return false, err
			}
			if !has {
				return false, nil
			}
		case cond == "subscribed":
			if contact.Status != mailmodel.ContactStatusSubscribed {
				return false, nil
			}
		case strings.HasPrefix(cond, "has_tag:"):
			tag := strings.TrimSpace(strings.TrimPrefix(cond, "has_tag:"))
			if tag == "" {
				return false, errors.New("has_tag 条件缺少标签名")
			}
			tags, err := s.m.GetContactTags(ctx, contact.ID)
			if err != nil {
				return false, err
			}
			found := false
			for _, t := range tags {
				if t == tag {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		default:
			return false, fmt.Errorf("未知条件: %s", cond)
		}
	}
	return true, nil
}

func refreshContact(ctx context.Context, s *Service, c *mailmodel.MailContactEntity) *mailmodel.MailContactEntity {
	row, err := s.m.GetContact(ctx, c.ID)
	if err != nil {
		return c
	}
	return row
}
