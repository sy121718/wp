package mediamodel

// media_variant_model.go — sys_media_variant 表访问单元（Repository）:
// 仅做单表 CRUD 与单表聚合，业务规则（状态机、生成编排）在 service 层。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameSysMediaVariant = "sys_media_variant"

// 变体类型常量（与迁移 048_media_variant.sql 的取值一致）。
const (
	VariantTypeThumb = "thumb"
	// VariantTypeSmall 中间档：768 Fit Lanczos。
	//
	// 为什么需要它（2026-10-02 补）：srcset 原先只有 320 / 1280 两档，而组件的
	// sizes 是「≤640px 视口 100vw、其余 50vw」—— DPR=2 的 375pt 手机需要约 750
	// 设备像素：320 太小，1280 又远大于所需。浏览器在「满足所需的最小候选」规则下
	// 只能选 1280，移动端因此下载一份桌面对图像。768 正好落在这一档。
	VariantTypeSmall  = "small"
	VariantTypeMedium = "medium"
	// VariantTypeFull 全尺寸槽位：与原图同尺寸的 JPEG 重编码。
	//
	// 本槽位此前叫 webp —— 那个名字是骗人的：产物从来是 JPEG（编码统一走
	// image_processor.go 的 encodeJPEGBytes / variantJPEGQuality），从不输出 WebP。
	// 将来真要接 WebP/AVIF 输出时，这个名字会把改代码的人骗一次（以为换掉编码器
	// 就是 WebP，实际上类型名与产物格式是两回事），所以在造成误解之前改名 full。
	VariantTypeFull = "full"
)

// 变体生成状态常量（varchar，服务端状态机）。
const (
	VariantStatusPending    = "pending"
	VariantStatusProcessing = "processing"
	VariantStatusReady      = "ready"
	VariantStatusFailed     = "failed"
)

// VariantTypes 返回全部受支持的变体类型（生成顺序固定）。
func VariantTypes() []string {
	// 顺序即生成顺序，按边长升序：thumb(320) → small(768) → medium(1280) → full(原尺寸)。
	// 这个切片的**长度就是「该附件应有几条变体记录」的判据**（见 variantBackfillReason），
	// 所以增删档位会顺带让存量附件判为「记录条数不齐」，由调度器幂等补偿重建 —— 这是设计。
	return []string{VariantTypeThumb, VariantTypeSmall, VariantTypeMedium, VariantTypeFull}
}

// MediaVariantEntity 对应 sys_media_variant 表。
type MediaVariantEntity struct {
	ID           uint64     `gorm:"column:id;primaryKey"`
	AttachmentID uint64     `gorm:"column:attachment_id"`
	VariantType  string     `gorm:"column:variant_type"`
	FilePath     string     `gorm:"column:file_path"`
	Width        *int       `gorm:"column:width"`
	Height       *int       `gorm:"column:height"`
	FileSize     int64      `gorm:"column:file_size"`
	MimeType     *string    `gorm:"column:mime_type"`
	Status       string     `gorm:"column:status;default:pending"`
	CreateTime   time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime   *time.Time `gorm:"column:update_time"`
}

func (MediaVariantEntity) TableName() string { return tableNameSysMediaVariant }

// MediaVariantModel 封装 sys_media_variant 表的数据访问。
type MediaVariantModel struct {
	db *gorm.DB
}

// NewMediaVariantModel 创建变体模型。
func NewMediaVariantModel(db *gorm.DB) *MediaVariantModel {
	return &MediaVariantModel{db: db}
}

// varDB 返回绑定 MediaVariantEntity 的 GORM DB 实例。
func (m *MediaVariantModel) varDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&MediaVariantEntity{})
}

// Transaction 起事务并把句柄交给调用方编排（「清旧记录 + 登记新记录」必须同事务）。
func (m *MediaVariantModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// Create 新增一条变体记录。
func (m *MediaVariantModel) Create(ctx context.Context, e *MediaVariantEntity) error {
	return m.varDB(ctx).Create(e).Error
}

// CreateBatch 批量新增变体记录（同一事务内插入，上传登记使用）。
func (m *MediaVariantModel) CreateBatch(ctx context.Context, list []*MediaVariantEntity) error {
	if len(list) == 0 {
		return nil
	}
	return m.varDB(ctx).Create(&list).Error
}

// DeleteByAttachment 物理删除指定附件的全部变体记录。
// 生成入口「重跑先清旧记录」的幂等动作；附件硬删除时由 FK ON DELETE CASCADE 兜底。
func (m *MediaVariantModel) DeleteByAttachment(ctx context.Context, attachmentID uint64) error {
	return m.varDB(ctx).Where("attachment_id = ?", attachmentID).Delete(&MediaVariantEntity{}).Error
}

// DeleteByAttachmentTx 在调用方给的事务句柄上物理删除指定附件的全部变体记录。
//
// 与下面的 CreateBatchTx 成对使用：「清旧 + 登记新」必须落在同一个事务里。
// 分成两步各自提交时，第二步失败就留下「旧变体没了、新变体也没有」的附件 ——
// 界面上三个变体槽全空，而磁盘上旧变体文件还在（没有记录能指向它们）。
func (m *MediaVariantModel) DeleteByAttachmentTx(ctx context.Context, tx *gorm.DB, attachmentID uint64) error {
	return tx.WithContext(ctx).Model(&MediaVariantEntity{}).Where("attachment_id = ?", attachmentID).Delete(&MediaVariantEntity{}).Error
}

// CreateBatchTx 在调用方给的事务句柄上批量登记变体记录。
func (m *MediaVariantModel) CreateBatchTx(ctx context.Context, tx *gorm.DB, list []*MediaVariantEntity) error {
	if len(list) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&MediaVariantEntity{}).Create(&list).Error
}

// ListForAudit 只读列出变体行，按 id 升序，供存储对账扫描（判断某个变体文件是否有记录）。
func (m *MediaVariantModel) ListForAudit(ctx context.Context, limit int) ([]MediaVariantEntity, error) {
	var list []MediaVariantEntity
	err := m.varDB(ctx).Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

// Update 按 ID 更新变体字段（状态流转 / 生成结果回填）。
func (m *MediaVariantModel) Update(ctx context.Context, id uint64, updates map[string]any) error {
	return m.varDB(ctx).Where("id = ?", id).Updates(updates).Error
}

// UpdateTx 在调用方给的事务句柄上更新变体字段。
//
// 「一批变体一起改状态」（如整体跳过时三条一起标 failed）必须同事务：
// 逐条各自提交时中途失败会留下「一半 failed、一半 processing」的记录集，
// 而 processing 是个不会自愈的中间态（没有任何东西会再来推进它）。
func (m *MediaVariantModel) UpdateTx(ctx context.Context, tx *gorm.DB, id uint64, updates map[string]any) error {
	return tx.WithContext(ctx).Model(&MediaVariantEntity{}).Where("id = ?", id).Updates(updates).Error
}

// GetByFilePath 按变体存储相对路径查询变体记录（构建期从产物 URL 反查附件用：
// 页面里可能直接引用 <stem>_thumb.jpg 这类变体地址）。
func (m *MediaVariantModel) GetByFilePath(ctx context.Context, filePath string) (*MediaVariantEntity, error) {
	var e MediaVariantEntity
	err := m.varDB(ctx).
		Where("file_path = ? OR file_path = ?", filePath, "/"+filePath).
		Order("id ASC").
		First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListByAttachment 查询指定附件的全部变体记录，按 id 升序（即 VariantTypes 的生成顺序）。
func (m *MediaVariantModel) ListByAttachment(ctx context.Context, attachmentID uint64) ([]MediaVariantEntity, error) {
	var list []MediaVariantEntity
	err := m.varDB(ctx).Where("attachment_id = ?", attachmentID).Order("id ASC").Find(&list).Error
	return list, err
}

// ListByAttachmentIDs 批量查询多个附件的变体记录（列表页一次 IN 查询，避免 N+1），
// 返回 attachment_id → 变体列表 的映射，缺失的附件不出现在映射中。
func (m *MediaVariantModel) ListByAttachmentIDs(ctx context.Context, ids []uint64) (map[uint64][]MediaVariantEntity, error) {
	out := make(map[uint64][]MediaVariantEntity)
	if len(ids) == 0 {
		return out, nil
	}
	var list []MediaVariantEntity
	if err := m.varDB(ctx).Where("attachment_id IN ?", ids).Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	for _, e := range list {
		out[e.AttachmentID] = append(out[e.AttachmentID], e)
	}
	return out, nil
}
