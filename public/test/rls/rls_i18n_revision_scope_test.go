package rlstest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
)

func TestRLS_ContentRevisionScopedByProject(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()
	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")

	unscoped := func() string {
		var latest *time.Time
		err := db.Table("sys_translation").Select("max(update_time) AS latest").Scan(&latest).Error
		if err != nil || latest == nil {
			return ""
		}
		return "trans-max-" + latest.UTC().Format(time.RFC3339Nano)
	}

	scoped := func(projectID string) string {
		var latest *time.Time
		err := rls.InProjectScope(ctx, db, projectID, func(tx *gorm.DB) error {
			return tx.Table("sys_translation").Select("max(update_time) AS latest").Scan(&latest).Error
		})
		if err != nil || latest == nil {
			return ""
		}
		return "trans-max-" + latest.UTC().Format(time.RFC3339Nano)
	}

	before := scoped(pA)
	if before != "" {
		t.Fatalf("本工程尚未写入译文时 revision 应为空，实际 %q", before)
	}

	const src = "夏季衬衫"
	if err := rls.InProjectScope(ctx, db, pA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO sys_translation (project_id, source_hash, context, lang, source_text, target_text, engine)
			VALUES (?, ?, 'product.name', 'en-US', ?, 'Summer Shirt', 'manual')`,
			pA, i18n.ContentHash(src), src).Error
	}); err != nil {
		t.Fatalf("写入本工程译文失败: %v", err)
	}

	after := scoped(pA)
	if after == "" || after == before {
		t.Fatalf("本工程译文写入后 revision 应推进，实际 before=%q after=%q", before, after)
	}

	if got := unscoped(); got != "" {
		t.Fatalf("不带作用域时不应看到本工程的译文行（revision 应为空），实际 %q", got)
	}

	if got := i18n.ContentRevisionForProject(ctx, ""); got != i18n.ContentRevision() {
		t.Fatalf("工程 id 为空应退回 ContentRevision()：%q != %q", got, i18n.ContentRevision())
	}
}
