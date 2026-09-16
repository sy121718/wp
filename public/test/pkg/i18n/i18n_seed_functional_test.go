package i18n_test

// i18n_seed_functional_test.go — i18n 数据层（P0：表结构 + 词条 seed）验证。
//
// 覆盖：
//  1) 055/056/057 表结构迁移 + 058 enums 词条 seed 在真实 PostgreSQL 上执行且可重复执行（幂等）；
//  2) 干净 schema 上的精确行数（zh-CN 195 / en-US 79）、分类推导与来源备注；
//  3) 接口级：种子词条经 pkg/i18n 缓存命中后，pkg/response 的 JSON 响应返回文案而不是裸 key
//     （纯 key / key|param / key: detail 三种形态 + Accept-Language 语言切换）。
//
// PG 不可用时 t.Skip（与其他功能测试一致）。

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go_wp/pkg/database"
	"go_wp/pkg/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	adminenums "go_wp/internal/module/admin/enums"
	pageenums "go_wp/internal/module/page/enums"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// i18nTestDSN 拼装 libpq 风格 DSN（与 support.pgDSN 一致）。
func i18nTestDSN(dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		support.DefaultPGHost, support.DefaultPGUser, support.DefaultPGPassword, dbname, support.DefaultPGPort)
}

// newI18nTestConfig 构造指向指定库的测试配置。
func newI18nTestConfig(dbname string) *viper.Viper {
	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", dbname)
	cfg.Set("database.host", support.DefaultPGHost)
	cfg.Set("database.port", 5432)
	cfg.Set("database.user", support.DefaultPGUser)
	cfg.Set("database.password", support.DefaultPGPassword)
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 2)
	cfg.Set("i18n.default_lang", "zh-CN")
	cfg.Set("i18n.auto_refresh", false)
	return cfg
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// countRows 统计满足条件的行数。
func countRows(t *testing.T, db *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()
	var n int64
	query := db.Table(table)
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Count(&n).Error; err != nil {
		t.Fatalf("统计 %s 行数失败: %v", table, err)
	}
	return n
}

// columnCount 统计某表上存在的一批列（用于断言新增列真实落库）。
func columnCount(t *testing.T, db *gorm.DB, table string, columns ...string) int64 {
	t.Helper()
	var n int64
	err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name IN (?)`,
		table, columns).Scan(&n).Error
	if err != nil {
		t.Fatalf("查询 %s 列失败: %v", table, err)
	}
	return n
}

// TestI18nEnumsSeedSchemaAndIdempotency 在干净隔离 schema 上验证注册的迁移 + seed：
// 结构落库、精确行数、分类推导、en-US 不伪造，且重复执行幂等。
func TestI18nEnumsSeedSchemaAndIdempotency(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}

	// 全量迁移 + seed 连跑两轮：第二轮应全部命中幂等守卫，不报错、不重复插入。
	for round := 1; round <= 2; round++ {
		if err := migrations.Run(db); err != nil {
			t.Fatalf("第 %d 轮结构迁移失败: %v", round, err)
		}
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮 seed 失败: %v", round, err)
		}
	}

	// 1) sys_i18n 补 category / remark（055）
	if got := columnCount(t, db, "sys_i18n", "category", "remark"); got != 2 {
		t.Fatalf("sys_i18n 应存在 category/remark 两列，实际 %d", got)
	}
	// 2) sys_i18n_revision 单行表（056）
	if got := countRows(t, db, "sys_i18n_revision", ""); got != 1 {
		t.Fatalf("sys_i18n_revision 应为单行，实际 %d 行", got)
	}
	// 3) sys_menus.title_key 列（057）
	if got := columnCount(t, db, "sys_menus", "title_key"); got != 1 {
		t.Fatalf("sys_menus 应存在 title_key 列，实际 %d", got)
	}

	// 4) 词条行数（干净 schema，精确断言）
	// 起点：195/79 为 058 enums 词条；+25/+25 为 059 后台外壳 shell.* 词条；
	// +13/+13 为 060 访客面组件 site.component.* 词条；+1/+1 为 065 语言切换器
	// 容器无障碍标签；+16/+16 为 176 商品列表组件词条、+26/+26 为 177 其余组件词条（含 userForms 的九个默认标题）（审计 I18N-010）。
	// +157/+157 为 179-182 四个模块的 enums 文案词条（order 62 / user 41 / mail 32 / cart 22，
	// 审计 I18N-002）——常量值改成 i18n key 之后，文案本身挪进了这张表。
	// +259/+259 为前端后台模板的三批抽取词条（187 settings/plugins/media 101 条、
	// 188 article/content 149 条、189 自定义 404 页 9 条，审计 I18N-001 与 SEO-013 的配套）。
	// +7/+7 为 198 masterdata 模块文案 key 化（审计 CQ-010 收尾）：6 个 Err + 1 个 Msg 的值
	// 从中文原文改成 i18n key 之后，文案本身挪进了这张表。
	// 此后片段与站点词条（site.fragment.* 等）按批次继续追加，
	// 数字随之增长 —— 更新这两个数时要顺带确认新批次**中英都已补齐**
	//（下面的分组断言与「en 不许缺行」的检查就是为这件事兜底的）。
	zhCount := countRows(t, db, "sys_i18n", "lang = ?", "zh-CN")
	enCount := countRows(t, db, "sys_i18n", "lang = ?", "en-US")
	if zhCount != 3019 {
		t.Fatalf("sys_i18n zh-CN 行数应为 3019（含 site.fragment.* 片段词条、176 商品列表词条，以及 187/188/189/190/191/192/193/197 八批后台模板抽取：settings/plugins/media 101、article/content 149、自定义 404 页 9、营销订单类 765、商品库存类 582、站点结构类 298、系统管理类 342、HTMX/仪表盘 8 —— 审计 I18N-001 与 SEO-013 的落地；+7 为 198 masterdata 模块文案 key 化；+17 为 216 webhook 模块文案 key 化 —— 8 个 Err*（含 SSRF 五个）与 4 个 Msg*，审计 CQ-010；+38 为 217 SEO 控制台页面文案 key 化 —— 该页 436d20b 随门禁脚本一起提交时漏了 key 化，门禁因此从第一天红着；+60 为 219 重定向管理页文案 key 化，审计 SEO-025；+15 为主题包导入导出的错误与提示文案 key 化，审计 VIS-014），实际 %d", zhCount)
	}
	if enCount != 2903 {
		t.Fatalf("sys_i18n en-US 行数应为 2903（同上；webhook 的 17 个、SEO 控制台的 38 个、重定向管理页的 60 个与主题包的 15 个 key 中英各一行），实际 %d", enCount)
	}

	// 4-B) 访客面组件词条（060）：13 个 key，zh-CN/en-US 各一行；中英必须都有（不许缺翻译）。
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'site.component.%' AND lang = ?", "zh-CN"); got != 56 {
		t.Fatalf("site.component.* zh-CN 应为 56 行（060 的 13 + 065 的 1 + 176 的 16 + 177 的 26），实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'site.component.%' AND lang = ?", "en-US"); got != 56 {
		t.Fatalf("site.component.* en-US 应为 56 行（060 的 13 + 065 的 1 + 176 的 16 + 177 的 26），实际 %d", got)
	}
	// 065 语言切换器容器标签：中英各一行且取值不同。
	if got := countRows(t, db, "sys_i18n", "item_key = 'site.component.languages.label' AND lang = ?", "zh-CN"); got != 1 {
		t.Fatalf("site.component.languages.label zh-CN 应为 1 行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key = 'site.component.languages.label' AND lang = ?", "en-US"); got != 1 {
		t.Fatalf("site.component.languages.label en-US 应为 1 行，实际 %d", got)
	}
	// 逐条断言：中英取值不同（文案确实被翻译，不是复制中文）+ 占位符仅 %s。
	siteKeys := []string{
		"site.component.gallery.prev", "site.component.gallery.next",
		"site.component.slider.prev", "site.component.slider.next", "site.component.slider.slide_label",
		"site.component.nav.label", "site.component.video.title",
		"site.component.countdown.days", "site.component.countdown.hours",
		"site.component.countdown.minutes", "site.component.countdown.seconds",
		"site.component.form.submit", "site.component.rating.label",
	}
	for _, key := range siteKeys {
		var zh, en string
		if err := db.Table("sys_i18n").Select("item_value").
			Where("item_key = ? AND lang = ?", key, "zh-CN").Scan(&zh).Error; err != nil {
			t.Fatalf("查询 %s zh-CN 失败: %v", key, err)
		}
		if err := db.Table("sys_i18n").Select("item_value").
			Where("item_key = ? AND lang = ?", key, "en-US").Scan(&en).Error; err != nil {
			t.Fatalf("查询 %s en-US 失败: %v", key, err)
		}
		if zh == "" || en == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", key, zh, en)
		}
		if zh == en {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", key, zh)
		}
		if !i18n.HasStringPlaceholdersOnly(zh) || !i18n.HasStringPlaceholdersOnly(en) {
			t.Fatalf("%s 只允许 %%s 占位符（zh=%q en=%q）", key, zh, en)
		}
	}
	var ratingZH, ratingEN string
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "site.component.rating.label", "zh-CN").Scan(&ratingZH).Error
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "site.component.rating.label", "en-US").Scan(&ratingEN).Error
	if ratingZH != "评分 %s / %s" || ratingEN != "Rated %s out of %s" {
		t.Fatalf("rating.label 词条不符：zh=%q en=%q", ratingZH, ratingEN)
	}

	// 4-A) 后台外壳词条：059 的 25 个 key + UI-001 的 shell.nav.open/close（窄屏抽屉按钮的
	// 无障碍标签）+ UI-011 的 shell.htmx.error/network/timeout（HTMX 失败的三种兜底文案）。
	// zh-CN/en-US 各一行；分页文案占位符仅 %s。
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'shell.%' AND lang = ?", "zh-CN"); got != 30 {
		t.Fatalf("shell.* zh-CN 应为 30 行（059 的 25 + shell.nav.* 2 + shell.htmx.* 3），实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'shell.%' AND lang = ?", "en-US"); got != 30 {
		t.Fatalf("shell.* en-US 应为 30 行（同 zh-CN），实际 %d", got)
	}
	var shellValue string
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "shell.brand", "en-US").Scan(&shellValue).Error; err != nil {
		t.Fatalf("查询 shell.brand en-US 失败: %v", err)
	}
	if shellValue != "Admin Console" {
		t.Fatalf("shell.brand en-US = %q，want Admin Console", shellValue)
	}
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "shell.pagination.info", "zh-CN").Scan(&shellValue).Error; err != nil {
		t.Fatalf("查询 shell.pagination.info zh-CN 失败: %v", err)
	}
	if !i18n.HasStringPlaceholdersOnly(shellValue) {
		t.Fatalf("shell.pagination.info zh-CN 只允许 %%s 占位符，实际 %q", shellValue)
	}

	// 5) 命中 a2 的 key：zh-CN + en-US 各一行，值取自 a2
	if got := countRows(t, db, "sys_i18n", "item_key = ?", "ErrAdminNotFound"); got != 2 {
		t.Fatalf("ErrAdminNotFound 应有 zh-CN/en-US 两行，实际 %d", got)
	}
	var value string
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "ErrAdminNotFound", "zh-CN").Scan(&value).Error; err != nil {
		t.Fatalf("查询 ErrAdminNotFound zh-CN 失败: %v", err)
	}
	if value != "管理员不存在" {
		t.Fatalf("ErrAdminNotFound zh-CN = %q，want 管理员不存在", value)
	}

	// 6) 新增 key（a2 无）：只有 zh-CN，不得伪造 en-US
	if got := countRows(t, db, "sys_i18n", "item_key = ?", "ErrMenuDepthExceeded"); got != 1 {
		t.Fatalf("ErrMenuDepthExceeded 应只有 zh-CN 一行（en-US 待翻译），实际 %d", got)
	}
	// 7) 新 key 化的原中文直值常量：值 = 常量名（key），文案进 zh-CN
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "MsgPageCreated", "zh-CN").Scan(&value).Error; err != nil {
		t.Fatalf("查询 MsgPageCreated zh-CN 失败: %v", err)
	}
	if value != "页面创建成功" {
		t.Fatalf("MsgPageCreated zh-CN = %q，want 页面创建成功", value)
	}

	// 8) category 推导：Err* → error；msg_* → msg；其余 → ui
	categories := map[string]string{"ErrSystemError": "error", "msg_operation_success": "msg", "MsgPageCreated": "ui"}
	for key, want := range categories {
		var got string
		if err := db.Table("sys_i18n").Select("category").
			Where("item_key = ? AND lang = ?", key, "zh-CN").Scan(&got).Error; err != nil {
			t.Fatalf("查询 %s category 失败: %v", key, err)
		}
		if got != want {
			t.Fatalf("%s category = %q，want %q", key, got, want)
		}
	}

	// 9) remark 记录词条来源文件（便于回溯）
	var remark string
	if err := db.Table("sys_i18n").Select("remark").
		Where("item_key = ? AND lang = ?", "MsgPageCreated", "zh-CN").Scan(&remark).Error; err != nil {
		t.Fatalf("查询 MsgPageCreated remark 失败: %v", err)
	}
	if remark != "internal/module/page/enums/page_enums.go" {
		t.Fatalf("MsgPageCreated remark = %q，want 来源文件路径", remark)
	}
}

// TestI18nEnumsSeedResponseTranslation 接口级验证：在独立临时库上跑全量迁移 + seed，
// 经 pkg/i18n 缓存命中后，pkg/response 的 JSON 响应返回文案而不是裸 key。
//
// 用独立临时库（而非共享 wp_test）是因为 pkg/i18n 的缓存加载走全局 database 组件，
// 而 wp_test 的历史残留表结构不完整；建库/删库逻辑与 pkg/response 的 MySQL 用例同构。
func TestI18nEnumsSeedResponseTranslation(t *testing.T) {
	dbName := "go_test_i18n_" + randomSuffix()
	admin, err := gorm.Open(postgres.Open(i18nTestDSN("postgres")), &gorm.Config{})
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}
	if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Skipf("跳过：创建临时测试库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName)
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := newI18nTestConfig(dbName)
	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = i18n.Close()
		_ = database.Close()
	})

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("结构迁移失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("词条 seed 失败: %v", err)
	}
	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("初始化 i18n 缓存失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/ok", func(c *gin.Context) { response.Success(c, gin.H{"id": 1}) })
	engine.GET("/notfound", func(c *gin.Context) { response.ErrorWithMessage(c, http.StatusNotFound, enums.ErrNotFound) })
	engine.GET("/admin", func(c *gin.Context) { response.ErrorWithMessage(c, http.StatusNotFound, adminenums.ErrAdminNotFound) })
	engine.GET("/page", func(c *gin.Context) { response.SuccessWithMessage(c, pageenums.MsgPageCreated, nil) })
	engine.GET("/locked", func(c *gin.Context) {
		response.ErrorWithMessage(c, http.StatusLocked, adminenums.ErrAccountLocked+"|5m0s")
	})
	engine.GET("/detail", func(c *gin.Context) {
		response.ErrorWithMessage(c, http.StatusBadRequest, adminenums.MsgBadRequest+": boom")
	})

	fetch := func(path, acceptLang string) response.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if acceptLang != "" {
			req.Header.Set("Accept-Language", acceptLang)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		var resp response.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析 %s 响应失败: %v（body=%s）", path, err, w.Body.String())
		}
		return resp
	}

	cases := []struct {
		name string
		path string
		lang string
		key  string
		want string
	}{
		{"Success 通用消息", "/ok", "zh-CN", "msg_operation_success", "操作成功"},
		{"纯 key 形态", "/notfound", "zh-CN", "ErrNotFound", "请求资源不存在"},
		{"模块错误 key", "/admin", "zh-CN", "ErrAdminNotFound", "管理员不存在"},
		{"模块成功 key", "/page", "zh-CN", "MsgPageCreated", "页面创建成功"},
		{"key|param 形态", "/locked", "zh-CN", "ErrAccountLocked", "账号已被锁定，请 5m0s 后重试"},
		{"key: detail 形态", "/detail", "zh-CN", "ErrInvalidParams", "请求参数错误: boom"},
		{"Accept-Language 切英文", "/admin", "en-US", "ErrAdminNotFound", "Admin not found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := fetch(tc.path, tc.lang)
			if resp.Message == tc.key {
				t.Fatalf("message 仍是裸 key %q（未命中 i18n 缓存）", tc.key)
			}
			if resp.Message != tc.want {
				t.Fatalf("message = %q, want %q", resp.Message, tc.want)
			}
		})
	}

	// 不支持的语言回退默认语言（zh-CN），不应返回空串或裸 key。
	resp := fetch("/notfound", "ja-JP")
	if resp.Message != "请求资源不存在" {
		t.Fatalf("不支持语言应回退默认语言，实际 %q", resp.Message)
	}
}
