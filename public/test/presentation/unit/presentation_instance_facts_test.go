package unit

// presentation_instance_facts_test.go — 按实体 id 集合下推的实例投影（PRD-011 有状态档）。
//
// 背景：发布记录页在有状态筛选时必须先知道全工程发布状态才能过滤，于是它把**全工程
// 商品 id** 交给发布端口。presentation 侧从前的读法是「读本工程该类型全部实例，再在
// Go 里按 map 过滤」—— 搬到内存的行数与点名的实体无关。现在下推成 entity_id IN (...)
// 并只取七列（身份 / 路径 / stale / active 指针）。
//
// 本用例钉住五件事：命中集合正确（含**投影里的 project_id 真的被读出来**）、投影**不含**
// 大字段（override_document / render_mode 没有被 Select 出来）、空清单不查库（contract 里
// 空 id 意味全工程，由调用方改用 ListInstances，这里不得偷偷退化成全量）、
// 空白与重复 id 不影响结果（含跨批重复不重搬）、跨工程不可见。

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/public/test/support"
)

// seedPublishInstances 给一批实体各建一条实例（role 决定角色），返回实体 → 实例 id。
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
	// 同一实体可以同时有归档角色（175 迁移把唯一键放宽成 实体 + 角色）：本查询按
	// entity_id 过滤、不看角色，所以实体 a 应回两条 —— 与 ListInstances 的全量语义一致。
	seedPublishInstances(t, f, f.projectID, []string{a}, presentationmodel.InstanceRoleArchive)
	// 另一个工程有一条自己的实例（用**另一个实体 id**，唯一索引 entity_type+entity_id+
	// instance_role 不带工程维度，同 id 同角色跨工程插不进去）：工程作用域若没生效，
	// 从本工程点名它就会命中。
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
			// project_id 在投影白名单里：这一列是判隔离与排查分组的依据，
			// 漏选时不会报错，只会恒为空串（看起来像「实例不属于任何工程」）。
			if row.ProjectID != f.projectID {
				t.Fatalf("投影缺 project_id 或工程作用域没生效：拿到工程 %q（期望 %q）", row.ProjectID, f.projectID)
			}
			if row.URLPath == "" {
				t.Fatalf("投影漏了 url_path：%+v", row)
			}
			// template_id / override_document / render_mode 不在投影白名单里：被 Select
			// 出来就说明「只取七列」形同虚设（大字段正是这屏不需要搬的带宽）。
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
		// 反向确认：那条行确实存在（否则上面的空集是「夹具没插进去」的假绿）。
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
			// 判据是「一条实例都没有」：退化成全量时这里会拿回 a 的两条 + b 的一条，
			// 只断言 len 也不够 —— 连类型都不该变，所以直接比对象。
			if len(rows) != 0 {
				t.Fatalf("空清单 %q 应返回空集且不退化成全量，实际 %d 条：%+v", empty, len(rows), rows)
			}
		}
	})

	t.Run("跨批重复 id 只搬一次且不漏批", func(t *testing.T) {
		// 500 是 model 的分批大小：造 501 个（重复 id 混在批界两侧的原位前缀）+ 头尾各补
		// 一次重复，逼出「同一 id 落在两个批次」「批界正好切在重复处」两种形态。
		// 断言两件事：不报错（重复不影响 SQL 形状），且 b 的实例只出现一次。
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
