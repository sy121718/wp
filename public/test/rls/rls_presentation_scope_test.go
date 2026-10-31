package rlstest

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

func seedPresentationInstance(t *testing.T, db *gorm.DB, projectID, templateID string) (string, string) {
	t.Helper()
	id, entityID := uuid.NewString(), uuid.NewString()
	now := time.Now()
	err := presentationmodel.NewModel(db).CreateInstance(context.Background(), &presentationmodel.InstanceEntity{
		ID:         id,
		ProjectID:  projectID,
		EntityType: "article",
		EntityID:   entityID,

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

	if _, err := m.GetInstance(ctx, pA, instA); err != nil {
		t.Fatalf("按 id 读本工程实例应成功，实际: %v", err)
	}

	if _, err := m.GetInstance(ctx, pA, instB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨工程按 id 读实例应 ErrRecordNotFound，实际: %v", err)
	}

	if _, err := m.GetInstanceByEntity(ctx, pA, "article", entityB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨工程按实体读实例应 ErrRecordNotFound，实际: %v", err)
	}

	listA, err := m.ListInstances(ctx, pA, "")
	if err != nil {
		t.Fatalf("列工程 A 实例失败: %v", err)
	}
	if len(listA) != 1 || listA[0].ProjectID != pA || listA[0].ID != instA {
		t.Fatalf("工程 A 应只见自己的 1 行，实际 %+v", listA)
	}
}

func TestRLS_PresentationScope_FailClosedWithoutScope(t *testing.T) {
	db, _ := rlsFixture(t)

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	instA, _ := seedPresentationInstance(t, db, pA, seedPresentationTemplate(t, db, pA))

	var row presentationmodel.InstanceEntity
	err := db.Model(&presentationmodel.InstanceEntity{}).Where("id = ?", instA).First(&row).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("未设 app.project_id 时按 id 读应 ErrRecordNotFound（fail closed），实际: %v", err)
	}

	var raw int64
	if err := db.Table("presentation_instances").Where("project_id = ?", pA).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设作用域时应 0 行可见，实际 %d 行", raw)
	}
}

func TestRLS_PresentationScope_WriteCheckRejectsForeignRow(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	tplB := seedPresentationTemplate(t, db, pB)

	now := time.Now()
	err := rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {

		return tx.Model(&presentationmodel.InstanceEntity{}).Create(&presentationmodel.InstanceEntity{
			ID:           uuid.NewString(),
			ProjectID:    pB,
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

func TestRLS_PresentationScope_TxVariantNeedsScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	m := presentationmodel.NewModel(db)

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	instID, _ := seedPresentationInstance(t, db, pA, seedPresentationTemplate(t, db, pA))

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
