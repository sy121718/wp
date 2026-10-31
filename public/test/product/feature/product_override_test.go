package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
)

func addMarkerComponent(doc any, marker string) bool {
	m, ok := doc.(map[string]any)
	if !ok {
		return false
	}
	root, ok := m["root"].([]any)
	if !ok {
		return false
	}
	node := map[string]any{
		"id":    "pd-override-marker",
		"type":  "core.heading",
		"props": map[string]any{"text": marker, "tag": "h2"},
	}
	m["root"] = append(root, node)
	return true
}

func ctxBG() context.Context { return context.Background() }

func documentWithMarker(t *testing.T, f *detailFixture, instanceID, projectID, marker string) json.RawMessage {
	t.Helper()
	inst, err := f.pres.Get(ctxBG(), &presentationdto.GetReq{ID: instanceID, ProjectID: projectID})
	if err != nil || inst == nil || len(inst.Document) == 0 {
		t.Fatalf("读取实例文档失败: %v", err)
	}
	var doc any
	if err := json.Unmarshal(inst.Document, &doc); err != nil {
		t.Fatalf("文档反序列化失败: %v", err)
	}
	if !addMarkerComponent(doc, marker) {
		t.Fatalf("无法在文档根数组追加标记组件")
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("文档序列化失败: %v", err)
	}
	return out
}

func instanceRenderMode(t *testing.T, f *detailFixture, instanceID string) (mode string, hasDoc bool) {
	t.Helper()
	var row struct {
		RenderMode string `gorm:"column:render_mode"`
		HasDoc     bool   `gorm:"column:has_doc"`
	}
	if err := f.db.Raw("SELECT render_mode, (override_document IS NOT NULL) AS has_doc "+
		"FROM presentation_instances WHERE id = ?", instanceID).Scan(&row).Error; err != nil {
		t.Fatalf("读取渲染模式失败: %v", err)
	}
	return row.RenderMode, row.HasDoc
}

func TestProductOverrideDocumentLifecycle(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "双轨测试商品", "/mode-smoke", "双轨测试描述", 10, 20)
	inst := f.publish(t, productID, "/mode-smoke")

	if mode, hasDoc := instanceRenderMode(t, f, inst.ID); mode != "template" || hasDoc {
		t.Fatalf("新建实例应为 template 模式且无独立文档，实际 mode=%s hasDoc=%v", mode, hasDoc)
	}
	doc := documentWithMarker(t, f, inst.ID, f.projectID, "OVERRIDE-MARKER-282")

	if _, err := f.pres.SaveOverrideDocument(ctxBG(), &presentationdto.SaveOverrideReq{
		InstanceID: inst.ID, ProjectID: f.projectID, Document: doc,
	}); err == nil {
		t.Fatalf("未确认的转入独立应被拒绝")
	}
	if mode, hasDoc := instanceRenderMode(t, f, inst.ID); mode != "template" || hasDoc {
		t.Fatalf("被拒绝的保存不应改变模式，实际 mode=%s hasDoc=%v", mode, hasDoc)
	}

	if _, err := f.pres.SaveOverrideDocument(ctxBG(), &presentationdto.SaveOverrideReq{
		InstanceID: inst.ID, ProjectID: f.projectID, Document: doc, ConfirmDetach: true,
	}); err != nil {
		t.Fatalf("确认后保存失败: %v", err)
	}
	if mode, hasDoc := instanceRenderMode(t, f, inst.ID); mode != "document" || !hasDoc {
		t.Fatalf("确认后应为 document 模式且有独立文档，实际 mode=%s hasDoc=%v", mode, hasDoc)
	}
	if html := activeHTML(t, "/mode-smoke"); !strings.Contains(html, "OVERRIDE-MARKER-282") {
		t.Fatalf("保存后产物应包含独立文档的标记")
	}

	newName := "双轨测试商品改名"
	if _, err := f.products.Update(ctxBG(), &productdto.UpdateReq{
		ID: productID, ProjectID: f.projectID, Name: &newName,
	}); err != nil {
		t.Fatalf("更新商品失败: %v", err)
	}
	if _, err := f.pres.Rebuild(ctxBG(), &presentationdto.RebuildReq{
		EntityID: productID, ProjectID: f.projectID,
	}); err != nil {
		t.Fatalf("数据更新重建失败: %v", err)
	}
	html := activeHTML(t, "/mode-smoke")
	if !strings.Contains(html, newName) {
		t.Fatalf("数据更新后产物应包含新名称")
	}
	if !strings.Contains(html, "OVERRIDE-MARKER-282") {
		t.Fatalf("数据更新不应冲掉独立文档")
	}
	if mode, _ := instanceRenderMode(t, f, inst.ID); mode != "document" {
		t.Fatalf("改商品数据不应改变渲染模式，实际 %s", mode)
	}

	if _, err := f.pres.SaveOverrideDocument(ctxBG(), &presentationdto.SaveOverrideReq{
		InstanceID: inst.ID, ProjectID: f.projectID, Document: doc,
	}); err != nil {
		t.Fatalf("结构未变的重复保存应幂等成功: %v", err)
	}

	if _, err := f.pres.ReapplyPreset(ctxBG(), &presentationdto.ReapplyPresetReq{
		InstanceID: inst.ID, ProjectID: f.projectID,
	}); err != nil {
		t.Fatalf("重新套用预设失败: %v", err)
	}
	if mode, hasDoc := instanceRenderMode(t, f, inst.ID); mode != "template" || hasDoc {
		t.Fatalf("重新套用预设后应为 template 模式且无独立文档，实际 mode=%s hasDoc=%v", mode, hasDoc)
	}
	if html := activeHTML(t, "/mode-smoke"); strings.Contains(html, "OVERRIDE-MARKER-282") {
		t.Fatalf("重新套用预设后产物不应再含独立文档的标记")
	}
}

func TestTemplateUpdateSkipsDocumentModeInstances(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	idA := f.createProduct(t, "跟随模板商品", "/mode-a", "A", 10, 20)
	instA := f.publish(t, idA, "/mode-a")
	idB := f.createProduct(t, "独立文档商品", "/mode-b", "B", 10, 20)
	instB := f.publish(t, idB, "/mode-b")

	docB := documentWithMarker(t, f, instB.ID, f.projectID, "OVERRIDE-MARKER-B")
	if _, err := f.pres.SaveOverrideDocument(ctxBG(), &presentationdto.SaveOverrideReq{
		InstanceID: instB.ID, ProjectID: f.projectID, Document: docB, ConfirmDetach: true,
	}); err != nil {
		t.Fatalf("B 转独立失败: %v", err)
	}
	if err := f.db.Exec("UPDATE presentation_instances SET stale = false WHERE id IN (?, ?)",
		instA.ID, instB.ID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}

	if _, err := f.pres.MarkStaleByDependency(ctxBG(), "content_template",
		"content_template:"+instA.TemplateID); err != nil {
		t.Fatalf("模板依赖扇出失败: %v", err)
	}
	var staleA, staleB bool
	if err := f.db.Raw("SELECT stale FROM presentation_instances WHERE id = ?", instA.ID).Scan(&staleA).Error; err != nil {
		t.Fatalf("读取 A stale 失败: %v", err)
	}
	if err := f.db.Raw("SELECT stale FROM presentation_instances WHERE id = ?", instB.ID).Scan(&staleB).Error; err != nil {
		t.Fatalf("读取 B stale 失败: %v", err)
	}
	if !staleA {
		t.Fatalf("template 模式的实例应被模板换代标记待重建")
	}
	if staleB {
		t.Fatalf("document 模式的实例不应被模板换代标记（它有自己的文档）")
	}
}

func TestRollbackArtifactAndDocument(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "回滚测试商品", "/rb-smoke", "回滚描述", 10, 20)
	inst := f.publish(t, productID, "/rb-smoke")

	var v1 struct {
		ID   string `gorm:"column:id"`
		Hash string `gorm:"column:artifact_hash"`
	}
	if err := f.db.Raw("SELECT id, artifact_hash FROM presentation_artifacts "+
		"WHERE presentation_instance_id = ? ORDER BY version ASC, lang ASC LIMIT 1", inst.ID).
		Scan(&v1).Error; err != nil || v1.Hash == "" {
		t.Fatalf("读取 v1 产物失败: %v", err)
	}
	var s1 string
	if err := f.db.Raw("SELECT id FROM document_snapshots WHERE presentation_instance_id = ? "+
		"ORDER BY create_time ASC, id ASC LIMIT 1", inst.ID).Scan(&s1).Error; err != nil || s1 == "" {
		t.Fatalf("读取 s1 快照失败: %v", err)
	}

	doc := documentWithMarker(t, f, inst.ID, f.projectID, "OVERRIDE-MARKER-RB")
	if _, err := f.pres.SaveOverrideDocument(ctxBG(), &presentationdto.SaveOverrideReq{
		InstanceID: inst.ID, ProjectID: f.projectID, Document: doc, ConfirmDetach: true,
	}); err != nil {
		t.Fatalf("转独立保存失败: %v", err)
	}
	if html := activeHTML(t, "/rb-smoke"); !strings.Contains(html, "OVERRIDE-MARKER-RB") {
		t.Fatalf("保存后线上应包含新标记")
	}

	if _, err := f.pres.RollbackArtifact(ctxBG(), &presentationdto.RollbackArtifactReq{
		InstanceID: inst.ID, ProjectID: f.projectID, TargetHash: v1.Hash,
	}); err != nil {
		t.Fatalf("产物回滚失败: %v", err)
	}
	var activeID string
	if err := f.db.Raw("SELECT COALESCE(active_artifact_id::text, '') FROM presentation_instances WHERE id = ?",
		inst.ID).Scan(&activeID).Error; err != nil {
		t.Fatalf("读取 active 指针失败: %v", err)
	}
	if activeID != v1.ID {
		t.Fatalf("产物回滚后 active 指针应指向 v1（期望 %s 实际 %s）", v1.ID, activeID)
	}
	if html := activeHTML(t, "/rb-smoke"); strings.Contains(html, "OVERRIDE-MARKER-RB") {
		t.Fatalf("产物回滚后线上应回到 v1 字节")
	}

	var s1Doc string
	if err := f.db.Raw("SELECT document::text FROM document_snapshots WHERE id = ?", s1).Scan(&s1Doc).Error; err != nil {
		t.Fatalf("读取 s1 文档失败: %v", err)
	}
	if _, err := f.pres.RollbackDocument(ctxBG(), &presentationdto.RollbackDocumentReq{
		InstanceID: inst.ID, ProjectID: f.projectID, SnapshotID: s1,
	}); err != nil {
		t.Fatalf("文档回滚失败: %v", err)
	}
	var curDoc string
	if err := f.db.Raw("SELECT COALESCE(override_document::text, '') FROM presentation_instances WHERE id = ?",
		inst.ID).Scan(&curDoc).Error; err != nil {
		t.Fatalf("读取覆盖文档失败: %v", err)
	}
	if !documentsJSONEqual(curDoc, s1Doc) {
		t.Fatalf("文档回滚后覆盖文档应等于 s1 的文档")
	}
	if html := activeHTML(t, "/rb-smoke"); strings.Contains(html, "OVERRIDE-MARKER-RB") {
		t.Fatalf("文档回滚后线上不应再含被回滚掉的标记")
	}

	other := f.publish(t, f.createProduct(t, "另一个商品", "/rb-other", "other", 1, 2), "/rb-other")
	if _, err := f.pres.RollbackDocument(ctxBG(), &presentationdto.RollbackDocumentReq{
		InstanceID: other.ID, ProjectID: f.projectID, SnapshotID: s1,
	}); err == nil {
		t.Fatalf("拿别的实例的快照回滚应被拒绝")
	}
}

func documentsJSONEqual(a, b string) bool {
	var av, bv any
	if json.Unmarshal([]byte(a), &av) != nil || json.Unmarshal([]byte(b), &bv) != nil {
		return false
	}
	an, _ := json.Marshal(av)
	bn, _ := json.Marshal(bv)
	return string(an) == string(bn)
}
