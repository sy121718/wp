package model

// user_session_model.go — 登录设备台账的表访问单元（issue #36）。
//
// 职责边界（见迁移 123 的表注释）：**会话状态不在本表**，它在 Redis；
// 本表负责让用户「看见并踢掉自己的其它设备」，以及回答「这个账号最近在哪登录过」。
// 换句话说：Redis 决定「这个请求是不是已登录」，本表决定「用户能看到哪些登录记录、能不能撤销」。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// UserSessionEntity 对应 user_sessions 表。
type UserSessionEntity struct {
	ID     uint64 `gorm:"column:id;primaryKey"`
	UserID uint64 `gorm:"column:user_id;type:bigint"`
	// TokenHash 会话令牌的 SHA-256 —— **只存哈希不存明文**。
	//
	// 与密码同理：这张表可能被读（后台排障、备份泄漏），明文令牌等于可直接冒用会话。
	// 会话令牌本身是高熵随机串，等值查询即可，不需要加盐。
	TokenHash    string     `gorm:"column:token_hash;type:varchar(64);uniqueIndex"`
	UserAgent    *string    `gorm:"column:user_agent;type:varchar(255)"`
	IP           *string    `gorm:"column:ip;type:varchar(50)"`
	Location     *string    `gorm:"column:location;type:varchar(100)"`
	LastActiveAt *time.Time `gorm:"column:last_active_at;type:timestamp(3)"`
	CreatedAt    *time.Time `gorm:"column:created_at;type:timestamp(3);autoCreateTime"`
	// RevokedAt 撤销时间：非空即「已踢掉」。不物理删除 —— 用户要能看到「某设备在某时刻被移除」。
	RevokedAt *time.Time `gorm:"column:revoked_at;type:timestamp(3)"`
}

// TableName 表名（迁移 123）。
func (UserSessionEntity) TableName() string { return "user_sessions" }

// UserSessionModel user_sessions 表访问单元。
type UserSessionModel struct{ db *gorm.DB }

// NewUserSessionModel 构造。
func NewUserSessionModel(db *gorm.DB) *UserSessionModel { return &UserSessionModel{db: db} }

// DB 返回绑定本表的句柄（只允许本 model 的仓储方法消费）。
func (m *UserSessionModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&UserSessionEntity{})
}

// Create 登记一次登录（建一条设备台账）。
//
// token_hash 上有唯一索引：同一个令牌重复登记会被数据库挡下 —— 那属于调用方的逻辑错误
// （令牌是一次性的），让它显式失败比静默产生两行同令牌记录好。
func (m *UserSessionModel) Create(ctx context.Context, e *UserSessionEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// GetByID 按主键取一条（撤销单设备时先取行做归属校验）。
func (m *UserSessionModel) GetByID(ctx context.Context, id uint64) (e *UserSessionEntity, err error) {
	e = &UserSessionEntity{}
	err = m.DB(ctx).Where("id = ?", id).First(e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return e, err
}

// GetByTokenHash 按令牌哈希取一条（撤销 / 校验用）。
func (m *UserSessionModel) GetByTokenHash(ctx context.Context, hash string) (e *UserSessionEntity, err error) {
	e = &UserSessionEntity{}
	err = m.DB(ctx).Where("token_hash = ?", hash).First(e).Error
	return e, err
}

// ListActiveByUser 列某用户**未撤销**的设备，最近活跃在前。
//
// 排序口径：最后活跃时间优先，从未活跃过的（NULL）用创建时间兜底 ——
// 刚登录还没发过请求的设备不该被排到最后。
func (m *UserSessionModel) ListActiveByUser(ctx context.Context, userID uint64) (list []*UserSessionEntity, err error) {
	err = m.DB(ctx).Where("user_id = ? AND revoked_at IS NULL", userID).
		Order("COALESCE(last_active_at, created_at) DESC, id DESC").Find(&list).Error
	return list, err
}

// Revoke 撤销某用户的一个设备（**条件里必须同时带 user_id**）。
//
// 只按 id 撤销是一个越权漏洞：设备 id 是自增整数，任何人把 id 换成别人的
// 就能把别人的设备踢下线。归属校验写在 SQL 条件里，而不是「先查出来再比 user_id」——
// 后者在并发下有两个独立语句之间的窗口。
func (m *UserSessionModel) Revoke(ctx context.Context, userID, id uint64, at time.Time) (affected int64, err error) {
	tx := m.DB(ctx).Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).
		Update("revoked_at", at)
	return tx.RowsAffected, tx.Error
}

// RevokeByTokenHash 按令牌哈希撤销（登出：调用方手里只有令牌，没有行 id）。
func (m *UserSessionModel) RevokeByTokenHash(ctx context.Context, hash string, at time.Time) (affected int64, err error) {
	tx := m.DB(ctx).Where("token_hash = ? AND revoked_at IS NULL", hash).Update("revoked_at", at)
	return tx.RowsAffected, tx.Error
}

// RevokeAll 撤销某用户的全部设备（改密码、管理员封禁、用户主动「退出全部设备」）。
//
// keepTokenHash 非空时保留那一个（「退出其它设备」把当前设备留下来）。
func (m *UserSessionModel) RevokeAll(ctx context.Context, userID uint64, keepTokenHash string, at time.Time) (affected int64, err error) {
	q := m.DB(ctx).Where("user_id = ? AND revoked_at IS NULL", userID)
	if keepTokenHash != "" {
		q = q.Where("token_hash <> ?", keepTokenHash)
	}
	tx := q.Update("revoked_at", at)
	return tx.RowsAffected, tx.Error
}

// TouchActive 更新设备最后活跃时间（由会话刷新路径调用）。
func (m *UserSessionModel) TouchActive(ctx context.Context, id uint64, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("last_active_at", at).Error
}
