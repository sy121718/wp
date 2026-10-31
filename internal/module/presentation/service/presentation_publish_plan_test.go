package presentationservice

import (
	"errors"
	"strings"
	"testing"
	"time"

	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
)

func TestPublishSessionFieldsCoverAllPhases(t *testing.T) {
	m := publishSessionMetrics{
		startedAt: time.Now(), attempts: 2, langs: 3,
		lockWaitFreeze: 11 * time.Millisecond, freeze: 22 * time.Millisecond,
		compile:        333 * time.Millisecond,
		lockWaitCommit: 44 * time.Millisecond, commit: 55 * time.Millisecond,
	}
	fields := publishSessionFields("inst-1", m, publishOutcomeFailed)
	if len(fields)%2 != 0 {
		t.Fatalf("字段必须成对（key/value）：%v", fields)
	}
	pairs := map[string]any{}
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			t.Fatalf("字段名必须是字符串：%v", fields[i])
		}
		if _, dup := pairs[key]; dup {
			t.Fatalf("字段 %s 被重复写出", key)
		}
		pairs[key] = fields[i+1]
	}
	want := map[string]any{
		"instanceId":       "inst-1",
		"langs":            3,
		"attempts":         2,
		"retried":          true,
		"lockWaitFreezeMs": int64(11),
		"freezeMs":         int64(22),
		"compileMs":        int64(333),
		"lockWaitCommitMs": int64(44),
		"commitMs":         int64(55),
		"outcome":          publishOutcomeFailed,
	}
	for key, wantValue := range want {
		got, ok := pairs[key]
		if !ok {
			t.Fatalf("分阶段耗时日志缺少字段 %s（实际写出：%v）", key, pairs)
		}
		if got != wantValue {
			t.Fatalf("字段 %s = %v，期望 %v", key, got, wantValue)
		}
	}

	if _, ok := pairs["totalMs"]; !ok {
		t.Fatal("缺少 totalMs：无法回答「这次发布总共花了多久」")
	}
	if len(pairs) != len(want)+1 {
		t.Fatalf("字段集合与预期不符（多了未登记字段？）：%v", pairs)
	}

	once := publishSessionFields("inst-1", publishSessionMetrics{startedAt: time.Now(), attempts: 1}, publishOutcomeOK)
	if got := fieldValue(once, "retried"); got != false {
		t.Fatalf("单次尝试的会话 retried 应为 false，实际 %v", got)
	}
	if got := fieldValue(once, "outcome"); got != publishOutcomeOK {
		t.Fatalf("成功会话的 outcome 应为 %s，实际 %v", publishOutcomeOK, got)
	}
}

func TestPublishOutcomeOf(t *testing.T) {
	if got := publishOutcomeOf(nil); got != publishOutcomeOK {
		t.Fatalf("无错误应为 %s，实际 %s", publishOutcomeOK, got)
	}
	if got := publishOutcomeOf(errors.New("构建失败")); got != publishOutcomeFailed {
		t.Fatalf("有错误应为 %s，实际 %s", publishOutcomeFailed, got)
	}
}

func fieldValue(fields []any, key string) any {
	for i := 0; i+1 < len(fields); i += 2 {
		if k, _ := fields[i].(string); k == key {
			return fields[i+1]
		}
	}
	return nil
}

func TestPublishFailedErrKeepsInputErrorRaw(t *testing.T) {
	inner := errors.New(presentationenums.ErrNoTemplate)
	got := publishFailedErr(&publishInputError{err: inner})
	if !errors.Is(got, inner) {
		t.Fatalf("输入解析失败必须原样透出（调用方按白名单映射成可操作的指引），实际 %v", got)
	}
	if strings.Contains(got.Error(), presentationenums.ErrBuildFailed) {
		t.Fatalf("输入解析失败不该被裹成 ErrBuildFailed：%v", got)
	}
	if built := publishFailedErr(errors.New("Jet 编译失败")); !strings.Contains(built.Error(), presentationenums.ErrBuildFailed) {
		t.Fatalf("真正的构建失败必须带 ErrBuildFailed 前缀，实际 %v", built)
	}
}

func TestInstanceStateFingerprintDetectsCommit(t *testing.T) {
	artA, artB := "art-a", "art-b"
	snapA, snapB := "snap-a", "snap-b"
	row := func(mut func(*presentationmodel.InstanceEntity)) instanceStateFingerprint {
		e := &presentationmodel.InstanceEntity{
			TemplateID: "tpl-1", URLPath: "/products/a",
			RenderMode:        presentationmodel.RenderModeTemplate,
			CurrentSnapshotID: &snapA, StagedSnapshotID: &snapA,
			StagedArtifactID: &artA, ActiveArtifactID: &artA,
		}
		if mut != nil {
			mut(e)
		}
		return fingerprintOfInstance(e)
	}

	base := row(nil)
	if !base.equal(row(nil)) {
		t.Fatal("同一个实例状态的指纹必须相等（否则每次提交都会把自己判成冲突）")
	}

	published := row(func(e *presentationmodel.InstanceEntity) {
		e.CurrentSnapshotID, e.StagedSnapshotID = &snapB, &snapB
		e.StagedArtifactID, e.ActiveArtifactID = &artB, &artB
	})
	if base.equal(published) {
		t.Fatal("快照 / 指针推进必须被指纹检出")
	}

	if base.equal(row(func(e *presentationmodel.InstanceEntity) { e.Stale = true })) {
		t.Fatal("stale 变化必须被指纹检出")
	}

	for name, mut := range map[string]func(*presentationmodel.InstanceEntity){
		"模板绑定": func(e *presentationmodel.InstanceEntity) { e.TemplateID = "tpl-2" },
		"线上路径": func(e *presentationmodel.InstanceEntity) { e.URLPath = "/products/b" },
		"渲染模式": func(e *presentationmodel.InstanceEntity) { e.RenderMode = presentationmodel.RenderModeDocument },
		"独立文档": func(e *presentationmodel.InstanceEntity) {
			e.OverrideDocument = []byte("{\"root\":[]}")
		},
	} {
		if base.equal(row(mut)) {
			t.Fatalf("%s 被并发改动必须被指纹检出（提交会覆盖这些列）", name)
		}
	}

	now := time.Now().UTC()
	touchedAt := row(func(e *presentationmodel.InstanceEntity) { e.Stale, e.UpdatedAt = true, now })
	touchedLater := row(func(e *presentationmodel.InstanceEntity) {
		e.Stale, e.UpdatedAt = true, now.Add(time.Hour)
	})
	if !touchedAt.equal(touchedLater) {
		t.Fatal("update_time 不进指纹：只刷它的 repeated MarkStale 不该把提交判成冲突")
	}

	if base.equal(row(func(e *presentationmodel.InstanceEntity) { e.PublishedAt = &now })) {
		t.Fatal("published_at 变化必须被指纹检出")
	}
}
