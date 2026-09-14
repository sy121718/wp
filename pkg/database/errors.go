package database

import "strings"

// IsUniqueViolation 判断是否为唯一约束冲突（PostgreSQL SQLSTATE 23505）。
//
// 不依赖驱动特有错误类型：用错误文本匹配，order / inventory / coupon 等幂等重放共用。
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlstate 23505") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint")
}
