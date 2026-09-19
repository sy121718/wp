package pagemodel_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	pagemodel "go_wp/internal/module/page/model"
	"go_wp/internal/pipeline"
	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestDependencyLookupIncludesEachLocaleLedger(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	project := uuid.NewString()
	support.SeedProjectRow(t, db, project, "多语言反查")
	m := pagemodel.NewPageModel(db)
	for _, ledger := range []string{"published", "staged"} {
		t.Run(ledger, func(t *testing.T) {
			page, a, b, history := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
			insertPage(t, db, page, project, "/"+page, "多语言页", false, false)
			insertArtifact(t, db, a, page, 1)
			insertArtifact(t, db, b, page, 2)
			insertArtifact(t, db, history, page, 3)
			setPointers(t, db, page, b, b)
			dep := pipeline.BlockKey(uuid.NewString())
			oldDep := pipeline.BlockKey(uuid.NewString())
			insertDependency(t, db, a, page, dep)
			insertDependency(t, db, history, page, oldDep)
			if ledger == "published" {
				if err := db.Exec(`INSERT INTO page_publications(page_id,lang,active_path,artifact_id,artifact_hash,published_at,update_time) VALUES (?,'en-US',?,?,?,now(),now())`, page, "/en/"+page, a, "hash").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Exec(`INSERT INTO page_stagings(page_id,lang,artifact_id,artifact_hash,draft_version,update_time) VALUES (?,'en-US',?,'hash',1,now())`, page, a).Error; err != nil {
					t.Fatal(err)
				}
			}
			refs, err := m.ListPagesByDependency(context.Background(), project, dep.Kind, dep.Key)
			if err != nil || len(refs) != 1 || refs[0].ID != page {
				t.Fatalf("语言账本引用被漏掉: %+v %v", refs, err)
			}
			for _, txMode := range []bool{false, true} {
				var ids []string
				if txMode {
					err = m.Transaction(context.Background(), func(tx *gorm.DB) error {
						var e error
						ids, e = m.MarkStaleByDependencyTx(context.Background(), tx, project, dep.Kind, dep.Key, time.Now())
						return e
					})
				} else {
					ids, err = m.MarkStaleByDependency(context.Background(), project, dep.Kind, dep.Key, time.Now())
				}
				if err != nil || len(ids) != 1 || ids[0] != page {
					t.Fatalf("语言账本失效漏标(tx=%v): %v %v", txMode, ids, err)
				}
			}
			oldRefs, err := m.ListPagesByDependency(context.Background(), project, oldDep.Kind, oldDep.Key)
			if err != nil || len(oldRefs) != 0 {
				t.Fatalf("历史产物不应保留有效引用: %+v %v", oldRefs, err)
			}
		})
	}
}
