package mailservice

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go_wp/pkg/utils"
)

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

func TestRunMailAutomationTickLoopFirstRunThenInterval(t *testing.T) {

	var first int32
	stopFirst := runMailAutomationTickLoop(context.Background(), func(context.Context) { atomic.AddInt32(&first, 1) }, 2*time.Second)
	defer stopFirst()
	deadline := time.Now().Add(300 * time.Millisecond)
	for atomic.LoadInt32(&first) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&first); got < 1 {
		t.Fatalf("首跑应当先于第一个间隔（2s）发生，实际 %d 轮", got)
	}
	stopFirst()

	var count int32
	stop := runMailAutomationTickLoop(context.Background(), func(context.Context) { atomic.AddInt32(&count, 1) }, 20*time.Millisecond)
	defer stop()
	deadline = time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&count) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&count); got < 3 {
		t.Fatalf("ticker 未驱动后续轮次，实际 %d 轮", got)
	}

	stop()
	time.Sleep(60 * time.Millisecond)
	before := atomic.LoadInt32(&count)
	time.Sleep(80 * time.Millisecond)
	if after := atomic.LoadInt32(&count); after != before {
		t.Fatalf("stop 之后循环仍在跑：%d → %d", before, after)
	}
}

func TestStartMailAutomationSchedulerSkippedInTestProcess(t *testing.T) {
	if !utils.IsTestProcess() {
		t.Fatal("前提不成立：测试进程应当被 utils.IsTestProcess() 识别")
	}

	StartMailAutomationScheduler(context.Background(), nil)
	StartMailAutomationScheduler(context.Background(), &Service{})

	StartMailAutomationSchedulerWithInterval(context.Background(), nil, time.Millisecond)
}

// 关键出口：先 cancel 再 stop 时，stop 必须等在途一轮结束（关闭数据库不能抢跑）。
func TestMailAutomationStopWaitsInFlightRound(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	stop := runMailAutomationTickLoop(ctx, func(context.Context) {
		close(started)
		<-release
	}, time.Hour)
	<-started
	cancel()
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("在途一轮未结束时 stop 就返回了")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("在途一轮结束后 stop 仍未返回")
	}
}

// 邮件保留期清理同样接应用 ctx：nil service 不应启动或 panic。
func TestMailRetentionSchedulerNilService(t *testing.T) {
	StartMailRetentionSchedulerWithInterval(context.Background(), nil, time.Millisecond)()
}
