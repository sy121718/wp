package model

// user_profile_model.go — 资料与偏好的表访问单元（issue #36）。
//
// 两张表都是**一对一**（迁移 123 上 user_id 唯一索引）：一行一个用户，不再有第二行。
// 因此「取不到」只有一种原因 —— 还没建过。账号中心的读取路径必须能接受零值行，
// 而不是要求先有行才能进页面（否则新用户第一次打开资料页就是 500）。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserProfileEntity 对应 user_profiles 表（扩展资料）。
type UserProfileEntity struct {
	ID        uint64  `gorm:"column:id;primaryKey"`
	UserID    uint64  `gorm:"column:user_id;uniqueIndex"`
	FirstName *string `gorm:"column:first_name"`
	LastName  *string `gorm:"column:last_name"`
	Gender    int     `gorm:"column:gender;default:0"`
	// Birthday 只用到日期部分（列类型是 DATE）：时分秒在写库时被数据库丢弃。
	Birthday   *time.Time `gorm:"column:birthday"`
	Bio        *string    `gorm:"column:bio"`
	Website    *string    `gorm:"column:website"`
	Locale     *string    `gorm:"column:locale"`
	Timezone   *string    `gorm:"column:timezone"`
	Country    *string    `gorm:"column:country"`
	Province   *string    `gorm:"column:province"`
	City       *string    `gorm:"column:city"`
	Address    *string    `gorm:"column:address"`
	Postcode   *string    `gorm:"column:postcode"`
	Phone      *string    `gorm:"column:phone"`
	Company    *string    `gorm:"column:company"`
	CreateTime *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime *time.Time `gorm:"column:update_time"`
}

// TableName 表名。
func (UserProfileEntity) TableName() string { return "user_profiles" }

// UserPreferenceEntity 对应 user_preferences 表（前台偏好）。
type UserPreferenceEntity struct {
	ID          uint64  `gorm:"column:id;primaryKey"`
	UserID      uint64  `gorm:"column:user_id;uniqueIndex"`
	Theme       *string `gorm:"column:theme"`
	Locale      *string `gorm:"column:locale"`
	Timezone    *string `gorm:"column:timezone"`
	PageSize    int     `gorm:"column:page_size;default:20"`
	EmailNotify bool    `gorm:"column:email_notify;default:true"`
	SmsNotify   bool    `gorm:"column:sms_notify;default:false"`
	// ProfileVisibility public / members / private（取值校验在 service）。
	ProfileVisibility string     `gorm:"column:profile_visibility;default:public"`
	ShowOnline        bool       `gorm:"column:show_online;default:true"`
	CreateTime        *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime        *time.Time `gorm:"column:update_time"`
}

// TableName 表名。
func (UserPreferenceEntity) TableName() string { return "user_preferences" }

// UserProfileModel user_profiles 表访问单元。
type UserProfileModel struct{ db *gorm.DB }

// NewUserProfileModel 构造。
func NewUserProfileModel(db *gorm.DB) *UserProfileModel { return &UserProfileModel{db: db} }

// DB 返回绑定本表的句柄。
func (m *UserProfileModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&UserProfileEntity{})
}

// GetByUserID 取资料；**没有行时返回 (nil, nil)**。
//
// 刻意不返回 gorm.ErrRecordNotFound：一对一表的「还没建」是正常状态而非错误，
// 强迫每个调用方写一次 errors.Is 只会让其中一处漏掉，然后在页面渲染时空指针。
func (m *UserProfileModel) GetByUserID(ctx context.Context, userID uint64) (e *UserProfileEntity, err error) {
	e = &UserProfileEntity{}
	err = m.DB(ctx).Where("user_id = ?", userID).First(e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return e, err
}

// Upsert 按 user_id 插入或更新（并发安全的「保存资料」入口）。
//
// 用 ON CONFLICT DO UPDATE 而不是「先 Select 再 Create/Update」：后者在第一次保存
// 并发提交时会撞唯一索引（两个请求同时发现「没有行」）。
func (m *UserProfileModel) Upsert(ctx context.Context, e *UserProfileEntity) (err error) {
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"first_name", "last_name", "gender", "birthday", "bio", "website", "locale",
			"timezone", "country", "province", "city", "address", "postcode", "phone",
			"company", "update_time",
		}),
	}).Create(e).Error
}

// UpsertTx 事务内 upsert（账号中心「昵称 + 资料」是跨两张表的一次提交，
// 事务边界由 service 决定，model 只接受外部句柄）。
func (m *UserProfileModel) UpsertTx(ctx context.Context, tx *gorm.DB, e *UserProfileEntity) (err error) {
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"first_name", "last_name", "gender", "birthday", "bio", "website", "locale",
			"timezone", "country", "province", "city", "address", "postcode", "phone",
			"company", "update_time",
		}),
	}).Create(e).Error
}

// UserPreferenceModel user_preferences 表访问单元。
type UserPreferenceModel struct{ db *gorm.DB }

// NewUserPreferenceModel 构造。
func NewUserPreferenceModel(db *gorm.DB) *UserPreferenceModel { return &UserPreferenceModel{db: db} }

// DB 返回绑定本表的句柄。
func (m *UserPreferenceModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&UserPreferenceEntity{})
}

// GetByUserID 取偏好；没有行时返回 (nil, nil)（口径同 user_profiles）。
func (m *UserPreferenceModel) GetByUserID(ctx context.Context, userID uint64) (e *UserPreferenceEntity, err error) {
	e = &UserPreferenceEntity{}
	err = m.DB(ctx).Where("user_id = ?", userID).First(e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return e, err
}

// Upsert 按 user_id 插入或更新。
func (m *UserPreferenceModel) Upsert(ctx context.Context, e *UserPreferenceEntity) (err error) {
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"theme", "locale", "timezone", "page_size", "email_notify",
			"sms_notify", "profile_visibility", "show_online", "update_time",
		}),
	}).Create(e).Error
}
