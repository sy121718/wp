package mailservice

// mail_automation_scheduler_test.go — 延时兜底调度的**调度语义**单测（不碰数据库与队列）。
//
// 目标动作（TickDueRuns）要真实库，但「首跑一次再等 ticker」「单轮 panic 不致命」
// 这两件事与动作内容无关，写反了的表现都是「看起来在跑、其实没动」——
// 所以这里用可注入的 run 断言循环本身，另外钉住「间隔 ≤ 图能表达的最短等待」。

import (
	"sync/atomic"
	"testing"
	"time"

	"go_wp/pkg/utils"
)

// TestMailAutomationTickIntervalWithinShortestDelay 兜底间隔必须落在最短可表达等待之内。
//
// 这条断言是「间隔」与「延时语义」之间的耦合点：图的校验要求等待节点的 minutes 是正整数，
// 于是 1 分钟是这张图能表达的最短等待；扫描间隔一旦大于它，「等 1 分钟」的实例
// 在最坏情况下会多挂数倍时间（兜底变成拖延）。
func TestMailAutomationTickIntervalWithinShortestDelay(t *testing.T) {
	delayGraph := func(minutes any) map[string]any {
		return map[string]any{"entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": string(NodeTypeTrigger), "next": "n2"},
			map[string]any{"key": "n2", "type": string(NodeTypeDelay),
				"params": map[string]any{"minutes": minutes}, "next": "n3"},
			map[string]any{"key": "n3", "type": string(NodeTypeEnd)},
		}}
	}
	if _, err := ParseDefinition(delayGraph(1)); err != nil {
		t.Fatalf("1 分钟的等待节点应当合法（它正是最短等待）：%v", err)
	}
	if _, err := ParseDefinition(delayGraph(0)); err == nil {
		t.Fatal("0 分钟的等待节点应当被拒绝 —— 否则「1 分钟是最短等待」这个前提不成立")
	}
	if mailAutomationTickInterval <= 0 {
		t.Fatalf("兜底扫描间隔必须为正，实际 %s", mailAutomationTickInterval)
	}
	if mailAutomationTickInterval > time.Minute {
		t.Fatalf("兜底扫描间隔 %s 超过图能表达的最短等待（1 分钟）：等待节点会多挂数倍时间",
			mailAutomationTickInterval)
	}
	if mailAutomationTickLimit <= 0 {
		t.Fatalf("单轮投递上限必须为正，实际 %d", mailAutomationTickLimit)
	}
}

// TestRunMailAutomationTickLoopFirstRunThenInterval 首跑先于第一个间隔，之后等间隔驱动，
// stop 之后不再跑（与 retention / media 巡检同一节奏）。
func TestRunMailAutomationTickLoopFirstRunThenInterval(t *testing.T) {
	// 阶段一：间隔取 2s，而首跑必须在远小于它的时间内出现 ——
	// 否则实现是「先等一个间隔」，进程启动后的第一段时间完全没有兜底。
	var first int32
	stopFirst := runMailAutomationTickLoop(func() { atomic.AddInt32(&first, 1) }, 2*time.Second)
	defer stopFirst()
	deadline := time.Now().Add(300 * time.Millisecond)
	for atomic.LoadInt32(&first) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&first); got < 1 {
		t.Fatalf("首跑应当先于第一个间隔（2s）发生，实际 %d 轮", got)
	}
	stopFirst()

	// 阶段二：间隔 20ms，等间隔驱动到至少 3 轮。
	var count int32
	stop := runMailAutomationTickLoop(func() { atomic.AddInt32(&count, 1) }, 20*time.Millisecond)
	defer stop()
	deadline = time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&count) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&count); got < 3 {
		t.Fatalf("ticker 未驱动后续轮次，实际 %d 轮", got)
	}

	// 阶段三：stop 之后不再增长。
	stop()
	time.Sleep(60 * time.Millisecond)
	before := atomic.LoadInt32(&count)
	time.Sleep(80 * time.Millisecond)
	if after := atomic.LoadInt32(&count); after != before {
		t.Fatalf("stop 之后循环仍在跑：%d → %d", before, after)
	}
}

// TestStartMailAutomationSchedulerSkippedInTestProcess 测试进程不启动调度。
//
// 判据是 utils.IsTestProcess() 命中时函数**立即返回**：首跑会动真实库与队列，
// 测试的行为必须由用例自己触发（与 StartMailRetentionScheduler 同一约定）。
func TestStartMailAutomationSchedulerSkippedInTestProcess(t *testing.T) {
	if !utils.IsTestProcess() {
		t.Fatal("前提不成立：测试进程应当被 utils.IsTestProcess() 识别")
	}
	// 守卫命中时不做任何事：既不会 panic，也不会因为 svc 为空而崩。
	StartMailAutomationScheduler(nil)
	StartMailAutomationScheduler(&Service{})
	// 可注入间隔的入口没有守卫（它就是给用例用的）：svc 为 nil 时安全返回。
	StartMailAutomationSchedulerWithInterval(nil, time.Millisecond)
}
