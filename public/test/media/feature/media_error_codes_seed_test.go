package feature

import (
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 本文件锁 715 这条 seed：media 错误词条的状态码对齐 + ErrReplaceStaleNotifyFailed 补词条。
//
// 放在 feature 包（与 712 / 713 同理）：按 media 精确包可运行，不被 public/migrations
// 里其他域的 register 测试编译问题拖住。
const mediaErrorCodesVersion = "715-media-error-codes-i18n"

func findMediaErrorCodesSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == mediaErrorCodesVersion {
			return seed
		}
	}
	t.Fatalf("种子 %s 未注册（register.go 里漏了 registerMediaErrorCodesI18n？）", mediaErrorCodesVersion)
	return migrations.Seed{}
}

// TestMediaErrorCodesSeedPrepares 按 public 规则做持久化 PREPARE 校验：
// UPDATE + INSERT 混合语句上线前必须能准备成功。
func TestMediaErrorCodesSeedPrepares(t *testing.T) {
	seed := findMediaErrorCodesSeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE media_error_codes_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE media_error_codes_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
}

// TestMediaErrorCodesSeedAlignsStatusAndAddsEntry 钉住三件事：
//  1. ErrUploadFailed / ErrDownloadFailed 的 http_code 在校正后是 500（中英四条）；
//  2. ErrReplaceStaleNotifyFailed 补上了中英两条词条，文案与 500 状态一致；
//  3. 门槛在缺任意一半时变 0 —— 否则这条迁移在存量库上会永远「已完成」而不再执行。
func TestMediaErrorCodesSeedAlignsStatusAndAddsEntry(t *testing.T) {
	seed := findMediaErrorCodesSeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("跑 seed 失败：%v", err)
	}

	// ① 两个 key 的中英行都必须是 500，且文案原样保留（UPDATE 只动 http_code）。
	type codeRow struct {
		ItemKey  string
		Lang     string
		Value    string
		HttpCode int
	}
	var codes []codeRow
	if err := db.Table("sys_i18n").
		Select("item_key, lang, item_value AS value, http_code").
		Where("item_key IN ?", []string{"ErrUploadFailed", "ErrDownloadFailed"}).
		Order("item_key, lang").
		Scan(&codes).Error; err != nil {
		t.Fatalf("读词条失败：%v", err)
	}
	if len(codes) != 4 {
		t.Fatalf("两个 key 应各有中英两条，实际 %d 条：%+v", len(codes), codes)
	}
	wantText := map[string]map[string]string{
		"ErrUploadFailed":   {"zh-CN": "文件上传失败", "en-US": "File upload failed."},
		"ErrDownloadFailed": {"zh-CN": "打包下载失败", "en-US": "Packaged download failed."},
	}
	for _, r := range codes {
		if r.HttpCode != 500 {
			t.Errorf("%s/%s http_code = %d，want 500", r.ItemKey, r.Lang, r.HttpCode)
		}
		if want := wantText[r.ItemKey][r.Lang]; r.Value != want {
			t.Errorf("%s/%s 文案被改动了：%q，want %q（本迁移只校正 http_code）", r.ItemKey, r.Lang, r.Value, want)
		}
	}

	// ② 新补的 key：中英各一条、状态 500、文案非空且不是裸 key。
	var added []codeRow
	if err := db.Table("sys_i18n").
		Select("item_key, lang, item_value AS value, http_code").
		Where("item_key = ?", "ErrReplaceStaleNotifyFailed").
		Order("lang").
		Scan(&added).Error; err != nil {
		t.Fatalf("读词条失败：%v", err)
	}
	if len(added) != 2 {
		t.Fatalf("ErrReplaceStaleNotifyFailed 应有中英两条，实际 %d 条：%+v", len(added), added)
	}
	for _, r := range added {
		if r.HttpCode != 500 {
			t.Errorf("%s http_code = %d，want 500", r.Lang, r.HttpCode)
		}
		if r.Value == "" || r.Value == "ErrReplaceStaleNotifyFailed" {
			t.Errorf("%s 文案不能为空、也不能是裸 key：%q", r.Lang, r.Value)
		}
	}

	// ③ 门槛在完整数据上为 1。
	valueOfGate := func() string {
		var native string
		if err := db.Raw(seed.ConditionSQL).Scan(&native).Error; err != nil {
			t.Fatalf("求值门槛失败：%v", err)
		}
		return native
	}
	if got := valueOfGate(); got != "1" {
		t.Fatalf("数据齐备时门槛应为 1，实际 %q", got)
	}

	// ④ 回退一条 http_code，门槛必须变 0（证明它校验的是「对齐后」的状态，不只是行数）。
	if err := db.Exec("UPDATE sys_i18n SET http_code = 400 WHERE item_key = ? AND lang = ?",
		"ErrUploadFailed", "en-US").Error; err != nil {
		t.Fatalf("回退 http_code 失败：%v", err)
	}
	if got := valueOfGate(); got != "0" {
		t.Fatalf("http_code 未对齐时门槛必须为 0（只数行数会假成立），实际 %q", got)
	}

	// ⑤ 再把新补的 en-US 删掉，同样必须为 0。
	if err := db.Exec("UPDATE sys_i18n SET http_code = 500 WHERE item_key = ? AND lang = ?",
		"ErrUploadFailed", "en-US").Error; err != nil {
		t.Fatalf("恢复 http_code 失败：%v", err)
	}
	if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?",
		"ErrReplaceStaleNotifyFailed", "en-US").Error; err != nil {
		t.Fatalf("删 en-US 失败：%v", err)
	}
	if got := valueOfGate(); got != "0" {
		t.Fatalf("缺 ErrReplaceStaleNotifyFailed 的 en-US 时门槛必须为 0，实际 %q", got)
	}
}
