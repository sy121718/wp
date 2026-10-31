package database

import (
	"gorm.io/gorm"
)

func IsFieldExists(db *gorm.DB, model interface{}, field string, value interface{}, excludeID ...uint64) (bool, error) {
	var count int64
	q := db.Model(model).Where(field+" = ?", value)
	if len(excludeID) > 0 && excludeID[0] > 0 {
		q = q.Where("id != ?", excludeID[0])
	}
	err := q.Count(&count).Error
	return count > 0, err
}
