package presentationservice

// presentation_publish_plan_test.go — 发布会话的两条纯逻辑契约（PERF-01）。
//
// 覆盖面刻意窄：只钉住「锁收窄之后新增/改变的那些判定」——
//   1) 输入解析失败（模板 / 覆盖文档）的对外错误形态：必须原样透出，否则各调用方的
//      白名单映射会退化成一句笼统的「构建失败」，运营据此不知道该去建模板还是换模板；
//   2) 实例状态指纹的判定依据：哪些列进指纹（并发提交必须被检出）、哪些列不进
//      （update_time 不进 —— 依赖扇出重复标 stale 不该把提交判成冲突）。
//
// 端到端的并发语义在 public/test/presentation/unit（真实 PG + 真实装配）。

import (
	"errors"
	"strings"
	"testing"
	"time"

	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
)

// TestPublishFailedErrKeepsInputErrorRaw 输入解析失败原样透出，其余构建失败带 ErrBuildFailed。
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

// TestInstanceStateFingerprintDetectsCommit 指纹只覆盖「提交会覆盖的列」。
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

	// 一次成功提交会换掉快照与指针 —— 必须检出（L4：旧构建不许覆盖新稿）。
	published := row(func(e *presentationmodel.InstanceEntity) {
		e.CurrentSnapshotID, e.StagedSnapshotID = &snapB, &snapB
		e.StagedArtifactID, e.ActiveArtifactID = &artB, &artB
	})
	if base.equal(published) {
		t.Fatal("快照 / 指针推进必须被指纹检出")
	}
	// 清 stale 是提交的收尾动作，反方向（被置 true）同样要检出。
	if base.equal(row(func(e *presentationmodel.InstanceEntity) { e.Stale = true })) {
		t.Fatal("stale 变化必须被指纹检出")
	}
	// 模板绑定 / 路径 / 渲染模式 / 独立文档任一被并发改掉都要检出（本次提交会写这些列）。
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
	// update_time 不进指纹：依赖扇出重复标 stale（本来就 stale）只刷这一列，不该触发重试。
	now := time.Now().UTC()
	touchedAt := row(func(e *presentationmodel.InstanceEntity) { e.Stale, e.UpdatedAt = true, now })
	touchedLater := row(func(e *presentationmodel.InstanceEntity) {
		e.Stale, e.UpdatedAt = true, now.Add(time.Hour)
	})
	if !touchedAt.equal(touchedLater) {
		t.Fatal("update_time 不进指纹：只刷它的 repeated MarkStale 不该把提交判成冲突")
	}
	// 发布时间是提交收尾写入的列，必须进指纹。
	if base.equal(row(func(e *presentationmodel.InstanceEntity) { e.PublishedAt = &now })) {
		t.Fatal("published_at 变化必须被指纹检出")
	}
}
