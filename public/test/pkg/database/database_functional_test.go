package database_test

import (
	"testing"
	"time"

	"go_wp/pkg/database"

	"github.com/spf13/viper"
)

type sampleRecord struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"size:64;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

func TestDatabaseInitAndCRUDWithSQLite(t *testing.T) {
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatalf("关闭数据库失败: %v", err)
		}
	})

	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "sqlite")
	cfg.Set("database.dbname", ":memory:")
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 1)

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}

	if !database.IsInited() {
		t.Fatalf("数据库初始化状态错误: 期望=true 实际=false")
	}

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}
	if err := db.AutoMigrate(&sampleRecord{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}

	row := sampleRecord{Name: "functional-test", CreatedAt: time.Now()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("插入测试数据失败: %v", err)
	}

	var got sampleRecord
	if err := db.First(&got, row.ID).Error; err != nil {
		t.Fatalf("查询测试数据失败: %v", err)
	}
	if got.Name != row.Name {
		t.Fatalf("查询结果不正确: 期望=%s 实际=%s", row.Name, got.Name)
	}
}
