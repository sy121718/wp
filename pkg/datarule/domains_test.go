package datarule

import (
	"sync"
	"testing"
	"time"
)

// TestDomainRegistryIsRaceFree 域的注册与读取并发时不得发生数据竞争。
//
// 背景（这是本次修复的直接动因）：域表此前是「裸 map + sync.RWMutex」，但
// resolveDomain（plugin.go）直接遍历包级 map 且**没有加锁** —— engine.go 的注释
// 声称「读写一律经 registeredDomainMu」而实现并非如此。这不是「偶尔读到旧值」那种
// 软故障：Go 运行时对并发 map 读写会直接 fatal。
// 现改为「不可变快照 + atomic.Pointer 全量替换」，本用例在 -race 下证明读侧与写侧
// 不再共享可变内存。
//
// 只断言「不 panic / race detector 不报」，不断言可见性时序 —— 注册与读之间没有因果关系。
// 所有写者覆盖**同一个域标识**（不新增条目），避免给其它用例留下一堆注册域。
func TestDomainRegistryIsRaceFree(t *testing.T) {
	const writers, readers, rounds = 4, 8, 100

	var wg sync.WaitGroup
	start := make(chan struct{})

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < rounds; i++ {
				_ = RegisterDomain(DomainConfig{
					Domain:      "RACE_PROBE",
					DomainLabel: "并发探针",
					TableName:   "race_probe_table",
				})
			}
		}()
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < rounds; i++ {
				_ = resolveDomain("race_probe_table")
				_, _ = GetDomain("RACE_PROBE")
				_ = GetRegisteredDomains()
			}
		}()
	}
	close(start)
	wg.Wait()

	if got, ok := GetDomain("RACE_PROBE"); !ok || got.TableName != "race_probe_table" {
		t.Fatalf("并发注册后应能读到该域: %+v ok=%v", got, ok)
	}
	if got := resolveDomain("race_probe_table"); got != "RACE_PROBE" {
		t.Fatalf("resolveDomain 应按表名命中，实际 %q", got)
	}
}

// TestToScalarRejectsTimeFailClosed 时间类型必须 fail-closed，不能退化成字符串字典序。
//
// 字典序与 SQL 时间语义不等价，且错误方向是**放行**（详见 toScalar 注释）：
// 值 2026-09-19T01:00:00+08:00 与条件 GT 2026-09-19 20:00:00 相比，
// 字典序拿 'T'(0x54) 与 ' '(0x20) 比会判「命中」，而真实时间早于条件 ——
// 「只允许创建某时刻之后的数据」会被静默绕过。
func TestToScalarRejectsTimeFailClosed(t *testing.T) {
	now := time.Now()
	if _, err := toScalar(now); err == nil {
		t.Fatal("time.Time 应返回 error：字典序比较与 SQL 时间语义不等价")
	}
	if _, err := toScalar(&now); err == nil {
		t.Fatal("*time.Time 同样应返回 error")
	}
}
