package feature

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/pipeline"
	json "go_wp/pkg/jsonx"
	"go_wp/public/migrations"
	"gorm.io/gorm"
)

func TestRetiredSiteSlotsMigrationKeepsPublishedFacts(t *testing.T) {
	db, api, projectID := newPageService(t)
	ctx := context.Background()
	page, err := api.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/retired-slot-preserved", DraftDocument: json.RawMessage(plainActionDocument)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = api.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatal(err)
	}
	before, err := api.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if err != nil || len(before.Publications) != 1 {
		t.Fatalf("需要真实发布记录: %+v %v", before, err)
	}
	pub := before.Publications[0]
	active := filepath.Join(pipeline.ActiveRoot(), strings.TrimPrefix(pub.ActivePath, "/"), "index.html")
	bytesBefore, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	var retired migrations.Migration

	for _, m := range migrations.All() {
		if strings.HasPrefix(m.Version, "138-") || m.Version == "688-published-action-dependencies" {
			if err := db.Exec(m.SQL).Error; err != nil {
				t.Fatal(err)
			}
		}
		if m.Version == "692-retired-site-slots" {
			retired = m
		}
	}
	if retired.SQL == "" {
		t.Fatal("缺少正式692迁移")
	}
	if err := db.Exec(`INSERT INTO page_site_slots (id,project_id,slot,page_id) VALUES (gen_random_uuid(),?,'cart',?)`, projectID, page.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"site_slot", "page_target", "presentation_target"} {
		if err := db.Exec(`INSERT INTO page_dependencies (page_id,artifact_id,dependency_kind,dependency_key) VALUES (?,?,?,?)`, page.ID, *pub.ArtifactID, kind, "retired-migration-probe").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE retired_slot_ready AS " + retired.CheckSQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE retired_slot_ready").Error
	}); err != nil {
		t.Fatal(err)
	}
	for round := range 2 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			for index, statement := range migrations.SplitStatements(retired.SQL) {
				if strings.HasPrefix(strings.TrimSpace(statement), "DELETE") {
					name := fmt.Sprintf("retired_slot_delete_%d", index)
					if err := tx.Exec("PREPARE " + name + " AS " + statement).Error; err != nil {
						return err
					}
					if err := tx.Exec("DEALLOCATE " + name).Error; err != nil {
						return err
					}
				}
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("清退及幂等重放第%d轮: %v", round, err)
		}
		var ready int
		if err := db.Raw(retired.CheckSQL).Scan(&ready).Error; err != nil || ready != 1 {
			t.Fatalf("旧表及两依赖约束必须完整清退: %d %v", ready, err)
		}
		var kinds []string
		if err := db.Table("page_dependencies").Where("artifact_id=? AND dependency_key=?", *pub.ArtifactID, "retired-migration-probe").Order("dependency_kind").Pluck("dependency_kind", &kinds).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(kinds, []string{"page_target", "presentation_target"}) {
			t.Fatalf("仅旧依赖可删，显式目标需保留: %v", kinds)
		}
	}

	if err := migrations.Run(db); err != nil {
		t.Fatal(err)
	}
	var ready int
	if err := db.Raw(retired.CheckSQL).Scan(&ready).Error; err != nil || ready != 1 {
		t.Fatalf("正式迁移重放后仍须退役: %d %v", ready, err)
	}
	after, err := api.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if err != nil || !reflect.DeepEqual(before.Publications, after.Publications) || !reflect.DeepEqual(before.DraftDocument, after.DraftDocument) {
		t.Fatalf("发布事实和源文档不得改变: %v", err)
	}
	bytesAfter, err := os.ReadFile(active)
	if err != nil || !reflect.DeepEqual(bytesBefore, bytesAfter) {
		t.Fatalf("活跃产物字节不得改变: %v", err)
	}
}
