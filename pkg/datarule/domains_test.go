package datarule

import (
	"sync"
	"testing"
	"time"
)

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

func TestToScalarRejectsTimeFailClosed(t *testing.T) {
	now := time.Now()
	if _, err := toScalar(now); err == nil {
		t.Fatal("time.Time 应返回 error：字典序比较与 SQL 时间语义不等价")
	}
	if _, err := toScalar(&now); err == nil {
		t.Fatal("*time.Time 同样应返回 error")
	}
}
