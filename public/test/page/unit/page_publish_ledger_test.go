package unit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
)

// TestRecoverPendingPublicationCompletesWhenSwitchHappened 崩在「已切访问面、未写数据库」之间时补齐状态（TX-009）。
//
// 这是 TX-009 的核心场景：符号链接已指向新产物（线上真的生效了），而数据库活跃指针
// 还停在旧值。没有回执时只能人工比对；有回执 + 链接实际指向就能判定并补齐。
func TestRecoverPendingPublicationCompletesWhenSwitchHappened(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/crash", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-crash-1"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	// 访问面已切换：/crash 指向该产物（与生产一致，符号链接指向 artifacts 目录）。
	linkActivePath(t, root, "/crash", hash)
	insertPendingReceipt(t, db, page.ID, "/crash", "zh-CN", artifactID)

	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}
	// 数据库活跃指针已补齐（「DB 没跟上」的那一半）。
	var activePath, activeHash string
	if err := db.Raw("SELECT active_path, artifact_hash FROM page_publications WHERE page_id = ? AND lang = ?", page.ID, "zh-CN").
		Row().Scan(&activePath, &activeHash); err != nil {
		t.Fatalf("读取激活记录失败: %v", err)
	}
	if activePath != "/crash" || activeHash != hash {
		t.Fatalf("激活记录应补齐为本次产物，实际 %q / %q", activePath, activeHash)
	}
	// 回执已结案，下次启动不会重复判定。
	if got := receiptState(t, db, "/crash"); got != "committed" {
		t.Fatalf("回执应结案为 committed，实际 %q", got)
	}
}

// TestRecoverPendingPublicationAbortsWhenSwitchNeverHappened 切换没发生时只结案、不改状态（TX-009）。
//
// 判定必须保守：证据不足时改数据库（或改文件）比什么都不做更危险 —— 这里断言
// 「不写激活记录、只把回执标成 rolled_back」。
func TestRecoverPendingPublicationAbortsWhenSwitchNeverHappened(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/nocrash", DraftDocument: json.RawMessage(emptyRevDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	hash := "hash-crash-2"
	writeArtifactDir(t, root, hash)
	artifactID := insertArtifactRow(t, db, page.ID, hash)
	// 访问面没有链接（切换没发生），只有回执留在库里。
	insertPendingReceipt(t, db, page.ID, "/nocrash", "zh-CN", artifactID)

	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 0 || rolledBack != 1 {
		t.Fatalf("应回滚 1 条、补完成 0 条，实际 %d / %d", recovered, rolledBack)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM page_publications WHERE page_id = ?", page.ID).Scan(&count).Error; err != nil {
		t.Fatalf("统计激活记录失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("切换未发生时不写激活记录，实际 %d 条", count)
	}
	if got := receiptState(t, db, "/nocrash"); got != "rolled_back" {
		t.Fatalf("回执应标为 rolled_back，实际 %q", got)
	}
}

// recoverPending 调用回执恢复入口（运维能力不进服务契约，走类型断言）。
func recoverPending(t *testing.T, svc pagecontract.PageService, ctx context.Context) (int, int, error) {
	t.Helper()
	recoverer, ok := svc.(interface {
		RecoverPendingPublications(context.Context) (int, int, error)
	})
	if !ok {
		t.Fatal("页面服务未提供发布回执恢复入口")
	}
	return recoverer.RecoverPendingPublications(ctx)
}

// insertArtifactRow 直插一条 page_artifacts 行（返回 id）：回执恢复要按 id 反查 hash。
func insertArtifactRow(t *testing.T, db *gorm.DB, pageID, hash string) string {
	t.Helper()
	var id string
	// 一条完整 SQL：gorm 的 Raw 只把首参当语句，其余参数按占位符取值。
	statement := "INSERT INTO page_artifacts (" +
		"id, page_id, version, source_document, page_document_schema_version, source_hash, " +
		"build_input_manifest, build_input_hash, artifact_provider, artifact_key, artifact_hash, " +
		"compiler_version, registry_version, manifest, payload_state, note, created_by, create_time, lang" +
		") VALUES (gen_random_uuid(), ?, 1, '{}'::jsonb, 1, 'src', '{}'::jsonb, 'bih', 'local', 'key-x', ?, " +
		"'v1', 'r1', '{}'::jsonb, 'available', '', gen_random_uuid(), now(), 'zh-CN') RETURNING id"
	if err := db.Raw(statement, pageID, hash).Scan(&id).Error; err != nil {
		t.Fatalf("插入产物行失败: %v", err)
	}
	return id
}

// linkActivePath 在访问面根目录下建一个指向产物的符号链接（模拟「切换已发生」）。
func linkActivePath(t *testing.T, root, urlPath, hash string) {
	t.Helper()
	link := filepath.Join(root, "public", "active", strings.TrimPrefix(urlPath, "/"))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("创建访问面目录失败: %v", err)
	}
	// 必须与生产同形：相对链接（形如 ../../../artifacts/<hash>）。Inspect 靠剥掉
	// "../" 前缀还原产物 key —— 用绝对路径建的链接在真实系统里根本不会被产生，
	// 用它测出来的结论也不作数。
	target, err := filepath.Rel(filepath.Dir(link), filepath.Join(root, "artifacts", hash))
	if err != nil {
		t.Fatalf("计算相对链接目标失败: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("创建符号链接失败: %v", err)
	}
}

// insertPendingReceipt 直插一条 pending 的访问面切换回执（模拟「崩在切换前后」留下的痕迹）。
func insertPendingReceipt(t *testing.T, db *gorm.DB, pageID, path, lang, artifactID string) {
	t.Helper()
	data := "{\"lang\":\"" + lang + "\"}"
	// DB-019/020 第一批：receipts.id 已是 bigint identity，不再手填 uuid；时间列改名 create_time。
	if err := db.Exec("INSERT INTO publication_receipts (source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time) VALUES ('page', ?, 'switch_active', ?, ?, 'pending', ?::jsonb, now())",
		pageID, path, artifactID, data).Error; err != nil {
		t.Fatalf("插入回执失败: %v", err)
	}
}

// receiptState 读回执状态。
func receiptState(t *testing.T, db *gorm.DB, path string) string {
	t.Helper()
	var state string
	if err := db.Raw("SELECT receipt_state FROM publication_receipts WHERE path = ?", path).Scan(&state).Error; err != nil {
		t.Fatalf("读取回执状态失败: %v", err)
	}
	return state
}
