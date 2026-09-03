package i18n_test

import (
	"testing"
	"time"

	"go_wp/pkg/database"
	"go_wp/pkg/i18n"

	"github.com/spf13/viper"
)

type testI18nRecord struct {
	ID        uint   `gorm:"primaryKey"`
	ItemKey   string `gorm:"column:item_key"`
	Lang      string `gorm:"column:lang"`
	ItemValue string `gorm:"column:item_value"`
	HttpCode  int    `gorm:"column:http_code"`
	Status    int    `gorm:"column:status"`
}

func (testI18nRecord) TableName() string {
	return "sys_i18n"
}

func TestI18nInitUsesConfigAndAutoRefresh(t *testing.T) {
	t.Cleanup(func() {
		if err := i18n.Close(); err != nil {
			t.Fatalf("关闭 i18n 失败: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("关闭数据库失败: %v", err)
		}
	})

	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", "wp_test")
		cfg.Set("database.host", "127.0.0.1")
		cfg.Set("database.port", 5432)
		cfg.Set("database.user", "root")
		cfg.Set("database.password", "root")
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 1)
	cfg.Set("i18n.default_lang", "en-US")
	cfg.Set("i18n.auto_refresh", true)
	cfg.Set("i18n.refresh_interval", "20ms")

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}

	if err := db.AutoMigrate(&testI18nRecord{}); err != nil {
		t.Fatalf("迁移 sys_i18n 失败: %v", err)
	}

	seed := []testI18nRecord{
		{ItemKey: "msg_operation_success", Lang: "zh-CN", ItemValue: "操作成功", HttpCode: 200, Status: 1},
		{ItemKey: "msg_operation_success", Lang: "en-US", ItemValue: "Operation successful", HttpCode: 200, Status: 1},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("写入 i18n 测试数据失败: %v", err)
	}

	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("初始化 i18n 失败: %v", err)
	}

	if got := i18n.GetDefaultLang(); got != "en-US" {
		t.Fatalf("默认语言不正确: got=%s want=%s", got, "en-US")
	}

	if got := i18n.GetText("msg_operation_success", ""); got != "Operation successful" {
		t.Fatalf("默认语言文案不正确: got=%s want=%s", got, "Operation successful")
	}

	if err := db.Model(&testI18nRecord{}).
		Where("item_key = ? AND lang = ?", "msg_operation_success", "en-US").
		Update("item_value", "Operation refreshed").Error; err != nil {
		t.Fatalf("更新 i18n 数据失败: %v", err)
	}

	waitForText(t, func() string {
		return i18n.GetText("msg_operation_success", "")
	}, "Operation refreshed")
}

func TestI18nReinitAppliesLatestRuntimeConfig(t *testing.T) {
	t.Cleanup(func() {
		if err := i18n.Close(); err != nil {
			t.Fatalf("关闭 i18n 失败: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("关闭数据库失败: %v", err)
		}
	})

	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", "wp_test")
		cfg.Set("database.host", "127.0.0.1")
		cfg.Set("database.port", 5432)
		cfg.Set("database.user", "root")
		cfg.Set("database.password", "root")
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 1)
	cfg.Set("i18n.default_lang", "zh-CN")
	cfg.Set("i18n.auto_refresh", true)
	cfg.Set("i18n.refresh_interval", "20ms")

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}

	if err := db.AutoMigrate(&testI18nRecord{}); err != nil {
		t.Fatalf("迁移 sys_i18n 失败: %v", err)
	}

	seed := []testI18nRecord{
		{ItemKey: "msg_reinit", Lang: "zh-CN", ItemValue: "初始中文", HttpCode: 200, Status: 1},
		{ItemKey: "msg_reinit", Lang: "en-US", ItemValue: "Initial english", HttpCode: 200, Status: 1},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("写入 i18n 测试数据失败: %v", err)
	}

	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("首次初始化 i18n 失败: %v", err)
	}

	reinitCfg := viper.New()
	reinitCfg.Set("i18n.default_lang", "en-US")
	reinitCfg.Set("i18n.auto_refresh", false)

	if err := i18n.Init(reinitCfg); err != nil {
		t.Fatalf("重初始化 i18n 失败: %v", err)
	}

	if got := i18n.GetDefaultLang(); got != "en-US" {
		t.Fatalf("重初始化后默认语言不正确: got=%s want=%s", got, "en-US")
	}

	if got := i18n.GetText("msg_reinit", ""); got != "Initial english" {
		t.Fatalf("重初始化后默认语言文案不正确: got=%s want=%s", got, "Initial english")
	}

	if err := db.Model(&testI18nRecord{}).
		Where("item_key = ? AND lang = ?", "msg_reinit", "en-US").
		Update("item_value", "Updated by auto refresh").Error; err != nil {
		t.Fatalf("更新 i18n 数据失败: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if got := i18n.GetText("msg_reinit", ""); got != "Initial english" {
		t.Fatalf("关闭自动刷新后不应更新缓存: got=%s want=%s", got, "Initial english")
	}
}

func TestI18nGetDoesNotExposeMutableAllLangsMap(t *testing.T) {
	t.Cleanup(func() {
		if err := i18n.Close(); err != nil {
			t.Fatalf("关闭 i18n 失败: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("关闭数据库失败: %v", err)
		}
	})

	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", "wp_test")
		cfg.Set("database.host", "127.0.0.1")
		cfg.Set("database.port", 5432)
		cfg.Set("database.user", "root")
		cfg.Set("database.password", "root")
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 1)
	cfg.Set("i18n.default_lang", "zh-CN")

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}
	if err := db.AutoMigrate(&testI18nRecord{}); err != nil {
		t.Fatalf("迁移 sys_i18n 失败: %v", err)
	}

	seed := []testI18nRecord{
		{ItemKey: "msg_copy", Lang: "zh-CN", ItemValue: "中文", HttpCode: 200, Status: 1},
		{ItemKey: "msg_copy", Lang: "en-US", ItemValue: "english", HttpCode: 200, Status: 1},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("写入 i18n 测试数据失败: %v", err)
	}

	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("初始化 i18n 失败: %v", err)
	}

	result := i18n.Get("msg_copy", "zh-CN")
	result.AllLangs["zh-CN"] = "被污染"

	refetched := i18n.Get("msg_copy", "zh-CN")
	if refetched.AllLangs["zh-CN"] != "中文" {
		t.Fatalf("内部缓存不应被外部修改污染: got=%s want=%s", refetched.AllLangs["zh-CN"], "中文")
	}
}

func waitForText(t *testing.T, getter func() string, expected string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if getter() == expected {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("等待 i18n 自动刷新超时: want=%s got=%s", expected, getter())
}
