package unit

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	pubcontract "go_wp/internal/module/publication/contract"
	pubdto "go_wp/internal/module/publication/dto"
)

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

	linkActivePath(t, root, "/crash", hash)
	insertPendingReceipt(t, db, page.ID, "/crash", "zh-CN", artifactID)

	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}

	var activePath, activeHash string
	if err := db.Raw("SELECT active_path, artifact_hash FROM page_publications WHERE page_id = ? AND lang = ?", page.ID, "zh-CN").
		Row().Scan(&activePath, &activeHash); err != nil {
		t.Fatalf("读取激活记录失败: %v", err)
	}
	if activePath != "/crash" || activeHash != hash {
		t.Fatalf("激活记录应补齐为本次产物，实际 %q / %q", activePath, activeHash)
	}

	if got := receiptState(t, db, "/crash"); got != "committed" {
		t.Fatalf("回执应结案为 committed，实际 %q", got)
	}
}

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

func insertArtifactRow(t *testing.T, db *gorm.DB, pageID, hash string) string {
	t.Helper()
	var id string

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

func seedUpdateURLArtifact(t *testing.T, db *gorm.DB, pageID, urlPath string) (hash, artifactID string) {
	t.Helper()
	art, err := publish.NewArtifact([]byte("<html><body>seed</body></html>"), &publish.Manifest{
		ManifestSchemaVersion:     1,
		PageDocumentSchemaVersion: 1,
		CompilerVersion:           "v1",
		SourceID:                  pageID,
		SourceType:                "page",
		CanonicalPath:             urlPath,
		SourceHash:                "src",
		BuildInputHash:            "bih",
		Lang:                      "zh-CN",
	})
	if err != nil {
		t.Fatalf("构造产物失败: %v", err)
	}
	store := &publish.LocalStore{Root: publish.DefaultArtifactRoot()}
	if _, perr := store.PutArtifact(art); perr != nil {
		t.Fatalf("落盘产物失败: %v", perr)
	}
	return art.Hash, insertArtifactRow(t, db, pageID, art.Hash)
}

func linkActivePath(t *testing.T, root, urlPath, hash string) {
	t.Helper()
	link := filepath.Join(root, "public", "active", strings.TrimPrefix(urlPath, "/"))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("创建访问面目录失败: %v", err)
	}

	target, err := filepath.Rel(filepath.Dir(link), filepath.Join(root, "artifacts", hash))
	if err != nil {
		t.Fatalf("计算相对链接目标失败: %v", err)
	}

	if rerr := os.Remove(link); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		t.Fatalf("清理旧访问面链接失败: %v", rerr)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("创建符号链接失败: %v", err)
	}
}

func insertPendingReceipt(t *testing.T, db *gorm.DB, pageID, path, lang, artifactID string) {
	t.Helper()
	data := "{\"lang\":\"" + lang + "\"}"

	if err := db.Exec("INSERT INTO publication_receipts (source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time) VALUES ('page', ?, 'switch_active', ?, ?, 'pending', ?::jsonb, now())",
		pageID, path, artifactID, data).Error; err != nil {
		t.Fatalf("插入回执失败: %v", err)
	}
}

func insertPendingUpdateURLReceipt(t *testing.T, db *gorm.DB, pageID, newPath, oldPath, lang, artifactID string) int64 {
	t.Helper()

	data := fmt.Sprintf(`{"projectId":%q,"lang":%q,"oldPath":%q,"redirect":false}`,
		projectIDOfPage(t, db, pageID), lang, oldPath)
	var id int64
	if err := db.Raw("INSERT INTO publication_receipts (source_type, source_id, action, path, to_artifact_id, receipt_state, receipt_data, create_time) VALUES ('page', ?, 'update_url', ?, ?, 'pending', ?::jsonb, now()) RETURNING id",
		pageID, newPath, artifactID, data).Scan(&id).Error; err != nil {
		t.Fatalf("插入改 URL 回执失败: %v", err)
	}
	return id
}

func setPublishWindowFault(t *testing.T, svc pagecontract.PageService, fn func() error) {
	t.Helper()
	injector, ok := svc.(interface{ SetPublishWindowFault(func() error) })
	if !ok {
		t.Fatal("页面服务未提供发布窗口故障注入口")
	}
	injector.SetPublishWindowFault(fn)
}

func setPostSwitchFault(t *testing.T, svc pagecontract.PageService, fn func() error) {
	t.Helper()
	injector, ok := svc.(interface{ SetPostSwitchFault(func() error) })
	if !ok {
		t.Fatal("页面服务未提供提交前窗口故障注入口")
	}
	injector.SetPostSwitchFault(fn)
}

type receiptRouterFailure struct {
	pubcontract.PublicationService
	id  string
	err error
}

func (f receiptRouterFailure) BeginPublishReceipt(context.Context, *pubdto.BeginPublishReceiptReq) (string, error) {
	return f.id, f.err
}

func TestPublishMainlineRecordsCommittedReceipt(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/receipt-main", headingDocument)
	buildAndPublish(t, svc, page.ID)

	state, fromID, toID := receiptOf(t, db, "/receipt-main")
	if state != "committed" {
		t.Fatalf("正常发布后回执应结案为 committed，实际 %q", state)
	}
	if fromID != "" {
		t.Fatalf("首次发布的 from 应为空（本语言从未发布），实际 %q", fromID)
	}
	active := activeArtifactID(t, db, page.ID)
	if toID == "" || toID != active {
		t.Fatalf("回执记录的产物应与激活记录一致，回执 %q / 激活 %q", toID, active)
	}

	detail, derr := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if derr != nil {
		t.Fatalf("查询页面失败: %v", derr)
	}
	if _, serr := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: detail.DraftVersion, DraftPath: "/receipt-main",
		DraftDocument: json.RawMessage(strings.Replace(headingDocument, "你好", "第二版", 1)),
	}); serr != nil {
		t.Fatalf("保存草稿失败: %v", serr)
	}
	buildAndPublish(t, svc, page.ID)

	state2, fromID2, toID2 := receiptOf(t, db, "/receipt-main")
	if state2 != "committed" {
		t.Fatalf("再次发布后回执应结案为 committed，实际 %q", state2)
	}
	if fromID2 != toID {
		t.Fatalf("第二次发布的 from 应为上一次的 to（%q），实际 %q", toID, fromID2)
	}
	if toID2 == "" || toID2 == fromID2 {
		t.Fatalf("第二次发布应切到新产物，from/to 实际 %q / %q", fromID2, toID2)
	}
	if got := receiptCount(t, db, "/receipt-main"); got != 2 {
		t.Fatalf("每次发布登记一条回执，两次发布应为 2 条，实际 %d", got)
	}
}

func TestPublishCrashBetweenActivateAndDBConvergesOnRecovery(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	other := createPage(t, svc, projectID, "/crash-other", headingDocument)
	otherHash := buildAndPublish(t, svc, other.ID)

	page := createPage(t, svc, projectID, "/crash-window", headingDocument)
	built, berr := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if berr != nil {
		t.Fatalf("构建失败: %v", berr)
	}

	injected := errors.New("故障注入：进程终止于切换访问面之后、写数据库之前")
	setPostSwitchFault(t, svc, func() error { return injected })

	if _, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); perr == nil {
		t.Fatal("窗口故障注入后 Publish 应报错，实际返回成功")
	}

	target, lerr := os.Readlink(filepath.Join(publish.ActiveRoot(), "crash-window"))
	if lerr != nil {
		t.Fatalf("访问面符号链接应已建立（切换已发生）: %v", lerr)
	}
	if !strings.Contains(target, built.StagedHash) {
		t.Fatalf("符号链接应指向本次产物 %s，实际 %s", built.StagedHash, target)
	}
	if n := publicationCount(t, db, page.ID); n != 0 {
		t.Fatalf("崩溃窗口里数据库活跃指针不应写入，实际 %d 条", n)
	}
	if got := receiptState(t, db, "/crash-window"); got != "pending" {
		t.Fatalf("窗口回执应保持 pending（交启动恢复判定），实际 %q", got)
	}

	setPostSwitchFault(t, svc, nil)
	recovered, rolledBack, rerr := recoverPending(t, svc, ctx)
	if rerr != nil {
		t.Fatalf("恢复失败: %v", rerr)
	}
	if recovered != 1 || rolledBack != 0 {
		t.Fatalf("应补完成 1 条、回滚 0 条，实际 %d / %d", recovered, rolledBack)
	}
	if got := receiptState(t, db, "/crash-window"); got != "committed" {
		t.Fatalf("恢复后回执应收敛为 committed（不许停在 pending），实际 %q", got)
	}
	if got := activeHashOf(t, db, page.ID); got != built.StagedHash {
		t.Fatalf("恢复应补齐激活记录到本次产物 %s，实际 %q", built.StagedHash, got)
	}

	againRecovered, againRolledBack, aerr := recoverPending(t, svc, ctx)
	if aerr != nil {
		t.Fatalf("重复恢复失败: %v", aerr)
	}
	if againRecovered != 0 || againRolledBack != 0 {
		t.Fatalf("重复恢复不应再判定任何回执，实际 %d / %d", againRecovered, againRolledBack)
	}
	if got := receiptState(t, db, "/crash-window"); got != "committed" {
		t.Fatalf("重复恢复不得改变已结案回执，实际 %q", got)
	}
	if got := receiptState(t, db, "/crash-other"); got != "committed" {
		t.Fatalf("陪跑页面回执不得被改动，实际 %q", got)
	}
	if got := activeHashOf(t, db, other.ID); got != otherHash {
		t.Fatalf("陪跑页面激活记录不得被改动，实际 %q（期望 %s）", got, otherHash)
	}
}

func TestPublishAbortsWhenReceiptRegistrationFails(t *testing.T) {
	cases := []struct {
		name string
		id   string
		err  error
	}{
		{"登记报错", "", errors.New("回执登记失败（故障注入）")},
		{"登记返回空 id", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, svc, _, projectID := newPageServiceWithRouter(t, func(real pubcontract.PublicationService) pubcontract.PublicationService {
				return receiptRouterFailure{PublicationService: real, id: tc.id, err: tc.err}
			})
			ctx := context.Background()
			page := createPage(t, svc, projectID, "/receipt-fail", headingDocument)
			if _, berr := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); berr != nil {
				t.Fatalf("构建失败: %v", berr)
			}
			if _, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); !errors.Is(perr, pageservice.ErrPublishLedgerUnavailable) {
				t.Fatalf("登记失败应中止发布并返回 ErrPublishLedgerUnavailable，实际 %v", perr)
			}

			if _, lerr := os.Lstat(filepath.Join(publish.ActiveRoot(), "receipt-fail")); !os.IsNotExist(lerr) {
				t.Fatalf("登记失败时不得切换访问面，链接状态: %v", lerr)
			}
		})
	}
}

func receiptOf(t *testing.T, db *gorm.DB, path string) (state, fromID, toID string) {
	t.Helper()
	var row struct {
		State  string  `gorm:"column:state"`
		FromID *string `gorm:"column:from_id"`
		ToID   *string `gorm:"column:to_id"`
	}

	statement := "SELECT receipt_state AS state, from_artifact_id::text AS from_id, to_artifact_id::text AS to_id " +
		"FROM publication_receipts WHERE path = ? AND action = 'switch_active' ORDER BY id DESC LIMIT 1"
	if err := db.Raw(statement, path).Scan(&row).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	if row.FromID != nil {
		fromID = *row.FromID
	}
	if row.ToID != nil {
		toID = *row.ToID
	}
	return row.State, fromID, toID
}

func receiptCount(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM publication_receipts WHERE path = ? AND action = 'switch_active'", path).Scan(&n).Error; err != nil {
		t.Fatalf("统计回执失败: %v", err)
	}
	return n
}

func activeArtifactID(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	return activeColumnOf(t, db, pageID, "artifact_id")
}

func activeHashOf(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	return activeColumnOf(t, db, pageID, "artifact_hash")
}

func activeColumnOf(t *testing.T, db *gorm.DB, pageID, column string) string {
	t.Helper()
	var row struct {
		Value *string `gorm:"column:value"`
	}
	statement := "SELECT " + column + "::text AS value FROM page_publications WHERE page_id = ?"
	if err := db.Raw(statement, pageID).Scan(&row).Error; err != nil {
		t.Fatalf("读取激活记录失败: %v", err)
	}
	if row.Value == nil {
		return ""
	}
	return *row.Value
}

func publicationCount(t *testing.T, db *gorm.DB, pageID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_publications WHERE page_id = ?", pageID).Scan(&n).Error; err != nil {
		t.Fatalf("统计激活记录失败: %v", err)
	}
	return n
}

func receiptState(t *testing.T, db *gorm.DB, path string) string {
	t.Helper()
	var state string
	if err := db.Raw("SELECT receipt_state FROM publication_receipts WHERE path = ? AND action = 'switch_active'", path).Scan(&state).Error; err != nil {
		t.Fatalf("读取回执状态失败: %v", err)
	}
	return state
}
