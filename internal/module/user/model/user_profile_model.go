package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserProfileEntity struct {
	ID        uint64  `gorm:"column:id;primaryKey"`
	UserID    uint64  `gorm:"column:user_id;uniqueIndex"`
	FirstName *string `gorm:"column:first_name"`
	LastName  *string `gorm:"column:last_name"`
	Gender    int     `gorm:"column:gender;default:0"`

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

func (UserProfileEntity) TableName() string { return "user_profiles" }

type UserPreferenceEntity struct {
	ID          uint64  `gorm:"column:id;primaryKey"`
	UserID      uint64  `gorm:"column:user_id;uniqueIndex"`
	Theme       *string `gorm:"column:theme"`
	Locale      *string `gorm:"column:locale"`
	Timezone    *string `gorm:"column:timezone"`
	PageSize    int     `gorm:"column:page_size;default:20"`
	EmailNotify bool    `gorm:"column:email_notify;default:true"`
	SmsNotify   bool    `gorm:"column:sms_notify;default:false"`

	ProfileVisibility string     `gorm:"column:profile_visibility;default:public"`
	ShowOnline        bool       `gorm:"column:show_online;default:true"`
	CreateTime        *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime        *time.Time `gorm:"column:update_time"`
}

func (UserPreferenceEntity) TableName() string { return "user_preferences" }

type UserProfileModel struct{ db *gorm.DB }

func NewUserProfileModel(db *gorm.DB) *UserProfileModel { return &UserProfileModel{db: db} }

func (m *UserProfileModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&UserProfileEntity{})
}

func (m *UserProfileModel) GetByUserID(ctx context.Context, userID uint64) (e *UserProfileEntity, err error) {
	e = &UserProfileEntity{}
	err = m.DB(ctx).Where("user_id = ?", userID).First(e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return e, err
}

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

type UserPreferenceModel struct{ db *gorm.DB }

func NewUserPreferenceModel(db *gorm.DB) *UserPreferenceModel { return &UserPreferenceModel{db: db} }

func (m *UserPreferenceModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&UserPreferenceEntity{})
}

func (m *UserPreferenceModel) GetByUserID(ctx context.Context, userID uint64) (e *UserPreferenceEntity, err error) {
	e = &UserPreferenceEntity{}
	err = m.DB(ctx).Where("user_id = ?", userID).First(e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return e, err
}

func (m *UserPreferenceModel) Upsert(ctx context.Context, e *UserPreferenceEntity) (err error) {
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"theme", "locale", "timezone", "page_size", "email_notify",
			"sms_notify", "profile_visibility", "show_online", "update_time",
		}),
	}).Create(e).Error
}
