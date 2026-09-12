package mailservice

// mail_automation_report.go — 运行实例的排障视图（issue #38 P3，目标 ⑥）。
//
// 排障要回答的问题只有一个：**这个人卡在哪一步、为什么**。
// 光给 status 字段（running / waiting / failed）没有用 —— 运营看不懂，
// 也无法判断「该不该管」。所以每个实例都带一句 Explain。

import (
	"context"
	"errors"
	"fmt"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

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
	at := "流程入口"
	if row.CurrentNode != nil && *row.CurrentNode != "" {
		at = *row.CurrentNode
	}
	switch row.Status {
	case mailmodel.RunStatusWaiting:
		when := "（未设定时间）"
		if row.NextRunAt != nil {
			when = row.NextRunAt.Format("2006-01-02 15:04")
		}
		return fmt.Sprintf("等待中，将在 %s 继续（下一个节点 %s）", when, at)
	case mailmodel.RunStatusRunning:
		return fmt.Sprintf("进行中，正在处理节点 %s", at)
	case mailmodel.RunStatusCompleted:
		return fmt.Sprintf("已完成（共执行 %d 个节点）", res.DoneNodes)
	case mailmodel.RunStatusStopped:
		reason := ""
		if row.ErrorMessage != nil {
			reason = "：" + *row.ErrorMessage
		}
		return "已停止" + reason
	case mailmodel.RunStatusFailed:
		reason := "原因未知"
		if row.ErrorMessage != nil && *row.ErrorMessage != "" {
			reason = *row.ErrorMessage
		}
		return fmt.Sprintf("失败于节点 %s：%s（修正后可重新触发）", at, reason)
	}
	return "状态未知：" + row.Status
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
