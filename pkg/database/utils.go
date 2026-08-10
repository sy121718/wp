package database

import (
	"gorm.io/gorm"
)

// IsFieldExists 检查数据库表中是否存在指定字段值的记录，支持排除指定 ID。
//
// 参数:
//   - db: GORM 数据库连接实例
//   - model: 模型对象，用于指定要查询的表（例如 &User{} 或 User{})
//   - field: 要检查的字段名（数据库列名）
//   - value: 要检查的字段值
//   - excludeID: 可选，排除的记录 ID。不传则不排除；传入则追加 AND id != excludeID
//
// 返回值:
//   - bool: true 表示存在匹配的记录，false 表示不存在
//   - error: 数据库查询错误，查询成功时返回 nil

// exists, err := IsFieldExists(db, &User{}, "email", "user@example.com", userID)
func IsFieldExists(db *gorm.DB, model interface{}, field string, value interface{}, excludeID ...uint64) (bool, error) {
	var count int64
	q := db.Model(model).Where(field+" = ?", value)
	if len(excludeID) > 0 && excludeID[0] > 0 {
		q = q.Where("id != ?", excludeID[0])
	}
	err := q.Count(&count).Error
	return count > 0, err
}
