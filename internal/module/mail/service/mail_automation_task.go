package mailservice

// mail_automation_task.go — 自动化实例推进的任务接入（issue #38 P3）。
//
// 两个入口都会推进实例：
//   · 事件触发时**立即**投递一次（RunNow）；
//   · 等待节点挂起时按 next_run_at **延时投递**（EnqueueAt）。
//
// 延时用队列而不是自建调度器：队列的延时是持久化的（重启不丢），
// 且失败重试、超时、可视化都在队列侧已解决。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/queue"
)

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
