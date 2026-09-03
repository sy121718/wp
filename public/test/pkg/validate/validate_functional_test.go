package validate_test

import (
	"testing"

	"go_wp/pkg/validate"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

type validateUser struct {
	ID       uint   `gorm:"primaryKey"`
	Username string `gorm:"size:64;not null"`
	Email    string `gorm:"size:128;not null"`
}

func TestValidateUniqueAndExists(t *testing.T) {
	db := openValidateDB(t)

	seed := validateUser{Username: "alice", Email: "alice@example.com"}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("写入测试数据失败: %v", err)
	}

	fieldMap := map[string]string{
		"id":       "id",
		"username": "username",
		"email":    "email",
	}

	unique, err := validate.IsUnique(db, &validateUser{}, "username", "bob", fieldMap)
	if err != nil {
		t.Fatalf("检查唯一性失败: %v", err)
	}
	if !unique {
		t.Fatalf("username=bob 应当唯一")
	}

	exists, err := validate.IsExists(db, &validateUser{}, "username", "alice", fieldMap)
	if err != nil {
		t.Fatalf("检查存在性失败: %v", err)
	}
	if !exists {
		t.Fatalf("username=alice 应当存在")
	}

	uniqueExclude, err := validate.IsUniqueExclude(db, &validateUser{}, "username", "alice", seed.ID, fieldMap)
	if err != nil {
		t.Fatalf("检查排除主键唯一性失败: %v", err)
	}
	if !uniqueExclude {
		t.Fatalf("排除当前记录后应当唯一")
	}

	uniqueExcludeField, err := validate.IsUniqueExcludeField(db, &validateUser{}, "username", "alice", "id", seed.ID, fieldMap)
	if err != nil {
		t.Fatalf("检查排除字段唯一性失败: %v", err)
	}
	if !uniqueExcludeField {
		t.Fatalf("排除指定字段后应当唯一")
	}
}

func TestValidateReturnsErrorForInvalidFieldMapping(t *testing.T) {
	db := openValidateDB(t)

	_, err := validate.IsUnique(db, &validateUser{}, "username", "alice", map[string]string{
		"username": "username;drop table users",
	})
	if err == nil {
		t.Fatalf("非法字段映射应当返回错误")
	}

	_, err = validate.IsExists(db, &validateUser{}, "unknown", "alice", map[string]string{"username": "username"})
	if err == nil {
		t.Fatalf("白名单外字段应当返回错误")
	}
}

func TestValidateReturnsErrorWhenDatabaseUnavailable(t *testing.T) {
	db := openValidateDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层数据库连接失败: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭数据库连接失败: %v", err)
	}

	_, err = validate.IsUnique(db, &validateUser{}, "username", "alice", map[string]string{"username": "username"})
	if err == nil {
		t.Fatalf("数据库不可用时应当返回错误")
	}
}

func openValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过校验链路测试：%v", err)
	}
	if err := db.AutoMigrate(&validateUser{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	return db
}
