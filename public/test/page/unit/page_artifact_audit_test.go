package unit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
)

// TestAuditPublicationReportsOrphanArtifacts 反向对账：磁盘上有、无人认领的产物目录（IDX-015）。
//
// 正向巡检（链接是否可达）能发现「线上 404」，但反过来「磁盘上有一堆没人引用的产物目录」
// 此前完全看不见 —— 它们只占磁盘，却因为内容寻址（同一 hash 一份）很难靠人工判断能不能删。
func TestAuditPublicationReportsOrphanArtifacts(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	// helper 内部已经用 t.Setenv 指定了产物根；读回来放测试文件，保证两边看的是同一个目录。
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	if root == "" {
		t.Fatal("产物根未设置")
	}
	// 1) 有主目录：page_artifacts 里有行（模拟正常产物）。
	ownedHash := "owned-hash-1"
	writeArtifactDir(t, root, ownedHash)
	// 2) 孤儿目录：磁盘上有、数据库里没有任何行。
	orphanHash := "orphan-hash-9"
	writeArtifactDir(t, root, orphanHash)
	// page_artifacts 行需要 page_id；用真实页面（同时验证「页面存在但产物行缺失」也算孤儿）。
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/audit", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	if err = db.Exec(`INSERT INTO page_artifacts (
		id, page_id, version, source_document, page_document_schema_version, source_hash,
		build_input_manifest, build_input_hash, artifact_provider, artifact_key, artifact_hash,
		compiler_version, registry_version, manifest, payload_state, note, created_by, created_at, lang
	) VALUES (gen_random_uuid(), ?, 1, '{}'::jsonb, 1, 'src', '{}'::jsonb, 'bih', 'local', 'key-1', ?,
		'v1', 'r1', '{}'::jsonb, 'available', '', gen_random_uuid(), now(), 'zh-CN')`, page.ID, ownedHash).Error; err != nil {
		t.Fatalf("插入产物行失败: %v", err)
	}
	res, err := svc.AuditPublication(ctx)
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if res.OrphanChecked != 2 {
		t.Fatalf("两个产物目录都应被计入受检数，实际 %d", res.OrphanChecked)
	}
	if len(res.Orphans) != 1 || res.Orphans[0].Hash != orphanHash {
		t.Fatalf("应只报出无人认领的那个目录，实际 %+v", res.Orphans)
	}
	if res.Orphans[0].Bytes == 0 || res.Orphans[0].Files == 0 {
		t.Fatalf("孤儿目录应带上体积与文件数（运维判断清理收益用），实际 %+v", res.Orphans[0])
	}
}

// TestAuditPublicationTreatsExternalOwnersAsOwned 注入的外部属主清单内的目录不算孤儿。
//
// 自动发布实例与手工页面共用同一个 artifacts 根：漏接外部清单会把实例产物全报成孤儿，
// 而一份「看不出真假」的对账结果比没有对账更糟。
func TestAuditPublicationTreatsExternalOwnersAsOwned(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	sharedHash := "presentation-hash-7"
	writeArtifactDir(t, root, sharedHash)
	// 模拟装配层的注入（真实链路里是 presentation 的 ListArtifactHashes）。
	setter, ok := any(svc).(interface {
		SetExternalArtifactOwners(func(ctx context.Context) ([]string, error))
	})
	if !ok {
		t.Fatal("页面服务未提供产物属主注入点")
	}
	setter.SetExternalArtifactOwners(func(context.Context) ([]string, error) {
		return []string{sharedHash}, nil
	})
	res, err := svc.AuditPublication(context.Background())
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if len(res.Orphans) != 0 {
		t.Fatalf("外部属主认领的目录不该报成孤儿：%+v", res.Orphans)
	}
}

// writeArtifactDir 在产物根下造一个 hash 目录（含一个文件）。
func writeArtifactDir(t *testing.T, root, hash string) {
	t.Helper()
	dir := filepath.Join(root, "artifacts", hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建产物目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("写产物文件失败: %v", err)
	}
}
