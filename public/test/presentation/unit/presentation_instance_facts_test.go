package unit

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/public/test/support"
)

func seedPublishInstances(t *testing.T, f *presFixture, projectID string, entityIDs []string, role string) map[string]string {
	t.Helper()
	var templateID string
	if err := f.db.Raw("SELECT id FROM content_templates WHERE project_id = ? LIMIT 1", f.projectID).
		Scan(&templateID).Error; err != nil {
		t.Fatalf("取模板失败：%v", err)
	}
	if templateID == "" {
		t.Fatal("没有可用模板（fixture 没建 content_template）")
	}
	out := make(map[string]string, len(entityIDs))
	for i, entityID := range entityIDs {
		id := uuid.NewString()
		if err := f.db.Exec(`INSERT INTO presentation_instances
			(id, project_id, entity_type, entity_id, instance_role, url_path, template_id, stale, create_time, update_time)
			VALUES (?, ?, 'product', ?, ?, ?, ?, true, now(), now())`,
			id, projectID, entityID, role, fmt.Sprintf("/p/%s/%s/%d", projectID, role, i), templateID).Error; err != nil {
			t.Fatalf("夹具备料失败：%v", err)
		}
		out[entityID] = id
	}
	return out
}

func TestListInstanceFactsByEntitiesPushesEntityFilterDown(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	f.createTemplate(t)
	ctx := context.Background()
	m := presentationmodel.NewModel(f.db)

	a, b := uuid.NewString(), uuid.NewString()
	seedPublishInstances(t, f, f.projectID, []string{a, b}, presentationmodel.InstanceRoleDetail)

	seedPublishInstances(t, f, f.projectID, []string{a}, presentationmodel.InstanceRoleArchive)

	otherProject := uuid.NewString()
	support.SeedProjectRow(t, f.db, otherProject, "另一个工程")
	foreign := uuid.NewString()
	seedPublishInstances(t, f, otherProject, []string{foreign}, presentationmodel.InstanceRoleDetail)

	t.Run("按实体集合命中且投影不含大字段", func(t *testing.T) {
		rows, err := m.ListInstanceFactsByEntities(ctx, f.projectID, "product", []string{a})
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("实体 a 在本工程有详情 + 归档两条实例（另一个工程那条不算），实际 %d 条", len(rows))
		}
		for _, row := range rows {
			if row.EntityID != a {
				t.Fatalf("过滤没下推：拿回了实体 %s", row.EntityID)
			}

			if row.ProjectID != f.projectID {
				t.Fatalf("投影缺 project_id 或工程作用域没生效：拿到工程 %q（期望 %q）", row.ProjectID, f.projectID)
			}
			if row.URLPath == "" {
				t.Fatalf("投影漏了 url_path：%+v", row)
			}

			if row.TemplateID != "" || len(row.OverrideDocument) != 0 || row.RenderMode != "" {
				t.Fatalf("大字段不应被读出：template=%q override=%q render=%q",
					row.TemplateID, row.OverrideDocument, row.RenderMode)
			}
		}
	})

	t.Run("跨工程同一点名不可见", func(t *testing.T) {
		rows, err := m.ListInstanceFactsByEntities(ctx, f.projectID, "product", []string{foreign})
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("本工程不应看到另一个工程的实例，实际 %d 条", len(rows))
		}

		mine := presentationmodel.NewModel(f.db)
		hits, err := mine.ListInstanceFactsByEntities(ctx, otherProject, "product", []string{foreign})
		if err != nil {
			t.Fatalf("反查失败：%v", err)
		}
		if len(hits) != 1 {
			t.Fatalf("另一工程应看到自己的这条实例，实际 %d 条", len(hits))
		}
	})

	t.Run("重复与空白 id 不去重也不错配", func(t *testing.T) {
		rows, err := m.ListInstanceFactsByEntities(ctx, f.projectID, "product",
			[]string{" " + b + " ", b, ""})
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if len(rows) != 1 || rows[0].EntityID != b {
			t.Fatalf("应只命中实体 b 的一条，实际 %+v", rows)
		}
	})

	t.Run("空清单不查库也不退化成全量", func(t *testing.T) {
		for _, empty := range [][]string{nil, {}, {""}, {"  ", ""}} {
			rows, err := m.ListInstanceFactsByEntities(ctx, f.projectID, "product", empty)
			if err != nil {
				t.Fatalf("空清单 %q 不该把空白串送进 uuid 列（那是 22P02）：%v", empty, err)
			}

			if len(rows) != 0 {
				t.Fatalf("空清单 %q 应返回空集且不退化成全量，实际 %d 条：%+v", empty, len(rows), rows)
			}
		}
	})

	t.Run("跨批重复 id 只搬一次且不漏批", func(t *testing.T) {

		ids := make([]string, 0, 512)
		ids = append(ids, " "+b+" ")
		for i := 0; i < 500; i++ {
			ids = append(ids, uuid.NewString())
		}
		ids = append(ids, b, " ", "")
		rows, err := m.ListInstanceFactsByEntities(ctx, f.projectID, "product", ids)
		if err != nil {
			t.Fatalf("跨批重复 id 查询失败：%v", err)
		}
		hits := 0
		for _, row := range rows {
			if row.EntityID == b {
				hits++
			}
			if row.ProjectID != f.projectID {
				t.Fatalf("跨批读到了工程 %s 的实例", row.ProjectID)
			}
		}
		if hits != 1 {
			t.Fatalf("b 的实例应恰好命中一次（重复 id 不能让它跨批被搬两遍），实际 %d 次（共 %d 行）", hits, len(rows))
		}
	})

	t.Run("entity_type 谓词照常生效", func(t *testing.T) {
		rows, err := m.ListInstanceFactsByEntities(ctx, f.projectID, "article", []string{a})
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("entity_type 谓词没生效，实际 %d 条", len(rows))
		}
	})

	t.Run("只点名一个实体时不带回别的实体", func(t *testing.T) {
		rows, err := m.ListInstanceFactsByEntities(ctx, f.projectID, "product", []string{b})
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if len(rows) != 1 || rows[0].EntityID != b {
			t.Fatalf("只点名 b 时不应带回别的实体，实际 %+v", rows)
		}
	})
}
