package rlstest

// rls_presentation_scope_test.go — presentation 模块的工程作用域护栏（DB-009 第二批）。
//
// 为什么单独给这个模块写一组：presentation 是第二批里**整模块 0 覆盖**的那一个 ——
// 该 model 的所有方法签名原本都不带工程 id，也因此不属于第一批「签名已带 projectID
// 或实体自带 ProjectID」的可就地覆盖范围。presentation_instances 又在迁移 215 的清单里，
// 所以换非超级角色后，这些路径会**静默返回 0 行**（读）或被 WITH CHECK 拒绝（写）。
//
// 每条断言都必须具备失败能力（不是「调用没报错」）：
//   - 本工程读得到、他工程读不到：把 model 里的 InProjectScope 拿掉会立刻红；
//   - 不设作用域的裸句柄读 0 行：这就是「漏包 scope」在换角色后的真实表现（fail closed 不报错）；
//   - 跨工程写入被拒：策略的 WITH CHECK 缺位时红；
//   - 事务变体不带作用域时更新 0 行、带上后能改到：钉住 rls.ScopeTx 的必要性。
//
// 全程跑在非超级角色下（由 rlsFixture 保证，并用 rls.BypassedRole 自检）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/pkg/rls"
)

// seedPresentationTemplate 经 model 写入路径落一行内容模板（presentation_instances.template_id 是 NOT NULL 外键）。
func seedPresentationTemplate(t *testing.T, db *gorm.DB, projectID string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now()
	err := contenttemplatemodel.NewModel(db).Create(context.Background(), &contenttemplatemodel.TemplateEntity{
		ID:            id,
		ProjectID:     projectID,
		Name:          "护栏模板",
		EntityType:    "article",
		TemplateRole:  contenttemplatemodel.TemplateRoleDetail,
		DraftDocument: []byte(`{"root":[]}`),
		DraftVersion:  1,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("写入内容模板失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return id
}

// seedPresentationInstance 经 model 的写入路径落一行发布实例。
func seedPresentationInstance(t *testing.T, db *gorm.DB, projectID, templateID string) (string, string) {
	t.Helper()
	id, entityID := uuid.NewString(), uuid.NewString()
	now := time.Now()
	err := presentationmodel.NewModel(db).CreateInstance(context.Background(), &presentationmodel.InstanceEntity{
		ID:         id,
		ProjectID:  projectID,
		EntityType: "article",
		EntityID:   entityID,
		// 每行不同路径：presentation_instances 有 UNIQUE (project_id, url_path)。
		URLPath:      "/guard/" + id,
		InstanceRole: presentationmodel.InstanceRoleDetail,
		TemplateID:   templateID,
		Stale:        true,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("写入发布实例失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return id, entityID
}

// TestRLS_PresentationScope_ReadsOwnProjectOnly 实例的读路径只在工程作用域内可见。
func TestRLS_PresentationScope_ReadsOwnProjectOnly(t *testing.T) {
	db, _ := rlsFixture(t)
	m := presentationmodel.NewModel(db)

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	ctx := context.Background()
	tplA := seedPresentationTemplate(t, db, pA)
	tplB := seedPresentationTemplate(t, db, pB)
	instA, _ := seedPresentationInstance(t, db, pA, tplA)
	instB, entityB := seedPresentationInstance(t, db, pB, tplB)

	// 1) 本工程按 id 读得到。
	if _, err := m.GetInstance(ctx, pA, instA); err != nil {
		t.Fatalf("按 id 读本工程实例应成功，实际: %v", err)
	}
	// 2) 拿 A 的作用域读 B 的行必须读不到（RLS 的 USING 过滤）。
	if _, err := m.GetInstance(ctx, pA, instB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨工程按 id 读实例应 ErrRecordNotFound，实际: %v", err)
	}
	// 3) 按实体反查同理。
	if _, err := m.GetInstanceByEntity(ctx, pA, "article", entityB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨工程按实体读实例应 ErrRecordNotFound，实际: %v", err)
	}
	// 4) 列表只出本工程的行。
	listA, err := m.ListInstances(ctx, pA, "")
	if err != nil {
		t.Fatalf("列工程 A 实例失败: %v", err)
	}
	if len(listA) != 1 || listA[0].ProjectID != pA || listA[0].ID != instA {
		t.Fatalf("工程 A 应只见自己的 1 行，实际 %+v", listA)
	}
}

// TestRLS_PresentationScope_FailClosedWithoutScope 不带作用域的读取是静默 0 行 ——
// 这正是「漏包 scope」在换非超级角色后的表现，也是本批要消除的东西。
func TestRLS_PresentationScope_FailClosedWithoutScope(t *testing.T) {
	db, _ := rlsFixture(t)

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	instA, _ := seedPresentationInstance(t, db, pA, seedPresentationTemplate(t, db, pA))

	// 1) 裸句柄（等价于「没接 scope 的路径」）：同一个 id、同一条 SQL，读不到。
	var row presentationmodel.InstanceEntity
	err := db.Model(&presentationmodel.InstanceEntity{}).Where("id = ?", instA).First(&row).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("未设 app.project_id 时按 id 读应 ErrRecordNotFound（fail closed），实际: %v", err)
	}
	// 2) 全表计数同为 0：可见性由会话变量决定，不由 WHERE 决定。
	var raw int64
	if err := db.Table("presentation_instances").Where("project_id = ?", pA).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设作用域时应 0 行可见，实际 %d 行", raw)
	}
}

// TestRLS_PresentationScope_WriteCheckRejectsForeignRow 写入他工程的行被 WITH CHECK 拒绝。
func TestRLS_PresentationScope_WriteCheckRejectsForeignRow(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	tplB := seedPresentationTemplate(t, db, pB)

	now := time.Now()
	err := rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {
		// 刻意不经 model 的包装：这里验证的是数据库层的 WITH CHECK 本身 ——
		// 经包装时行声明的工程就是作用域，测到的是「一致」而不是「拒绝」。
		return tx.Model(&presentationmodel.InstanceEntity{}).Create(&presentationmodel.InstanceEntity{
			ID:           uuid.NewString(),
			ProjectID:    pB, // 作用域是 A，行却声明 B
			EntityType:   "article",
			EntityID:     uuid.NewString(),
			URLPath:      "/guard/cross-" + uuid.NewString(),
			InstanceRole: presentationmodel.InstanceRoleDetail,
			TemplateID:   tplB,
			Stale:        true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}).Error
	})
	if err == nil {
		t.Fatal("跨工程写入必须被策略的 WITH CHECK 拒绝，实际写入成功")
	}
}

// TestRLS_PresentationScope_TxVariantNeedsScope 事务变体必须自己带上作用域。
//
// 这两个断言是一对：同一事务、同一条 UPDATE，不带作用域时影响 0 行（静默，
// 调用方以为改成功了），带上 rls.ScopeTx 之后真正改到 —— 事务变体少一个
// ScopeTx 就是这个后果。
func TestRLS_PresentationScope_TxVariantNeedsScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	m := presentationmodel.NewModel(db)

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	instID, _ := seedPresentationInstance(t, db, pA, seedPresentationTemplate(t, db, pA))

	// 1) 不带作用域的裸更新：0 行受影响且**不报错**。
	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		res := tx.Model(&presentationmodel.InstanceEntity{}).Where("id = ?", instID).
			Update("stale", false)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 0 {
			t.Fatalf("无作用域的事务更新不应命中任何行，实际命中 %d 行", res.RowsAffected)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("无作用域的更新本身不应报错（fail closed 的静默性正是要害）: %v", err)
	}

	// 2) 经 model 的事务变体（内部 rls.ScopeTx）能真正改到本工程的行。
	err = m.Transaction(ctx, func(tx *gorm.DB) error {
		e := &presentationmodel.InstanceEntity{ID: instID, ProjectID: pA, Stale: false, UpdatedAt: time.Now()}
		return m.UpdateInstancePointersTx(tx, pA, e)
	})
	if err != nil {
		t.Fatalf("带作用域的事务更新应成功: %v", err)
	}
	got, err := m.GetInstance(ctx, pA, instID)
	if err != nil {
		t.Fatalf("更新后回读失败: %v", err)
	}
	if got.Stale {
		t.Fatal("事务变体未真正改到本工程的行（stale 仍为 true）")
	}
}
