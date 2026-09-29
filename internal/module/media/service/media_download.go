package mediaservice

// media_download.go — 媒体资源包（zip）下载计划：
//   单图 GET /api/media/download?id=1        → <原文件名>_package.zip
//   批量 GET /api/media/download/batch?ids=1,2,3 → media_export_<日期>.zip
//   zip 内固定四目录：original/（恒有原图）、webp/、thumb/、medium/；
//   变体未 ready 的目录放 README.txt 说明生成状态。
//   service 只产出打包计划（zip 条目名 + 本地路径），handler 据此流式写响应，不落盘临时文件。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mediato "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"

	"gorm.io/gorm"

	"go_wp/pkg/i18n"
)

// zipDirOriginal 等四个 zip 内固定目录名。
const (
	zipDirOriginal = "original"
	zipDirWebp     = "webp"
	zipDirThumb    = "thumb"
	zipDirMedium   = "medium"
)

// sanitizeZipName 清洗 zip 内条目名：去掉路径分隔符与控制字符，防止 zip-slip / 目录逃逸，
// 清洗后为空时回退 "file"。
func sanitizeZipName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	var b strings.Builder
	for _, r := range replacer.Replace(name) {
		if r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return out
}

// variantTypeToZipDir 变体类型 → zip 目录名。
func variantTypeToZipDir(vt string) string {
	switch vt {
	case mediamodel.VariantTypeThumb:
		return zipDirThumb
	case mediamodel.VariantTypeMedium:
		return zipDirMedium
	case mediamodel.VariantTypeWebp:
		return zipDirWebp
	default:
		return ""
	}
}

// zipDirVariantType zip 目录名 → 变体类型（README 文案用）。
func zipDirVariantType(dir string) string {
	switch dir {
	case zipDirThumb:
		return mediamodel.VariantTypeThumb
	case zipDirMedium:
		return mediamodel.VariantTypeMedium
	case zipDirWebp:
		return mediamodel.VariantTypeWebp
	default:
		return ""
	}
}

// variantStatusText 变体状态文案（README 用，按产物语言取词）。
var variantStatusTexts = []struct{ Status, Key, Fallback string }{
	{mediamodel.VariantStatusPending, "admin.media.variantStatus.pending", "排队中（pending）"},
	{mediamodel.VariantStatusProcessing, "admin.media.variantStatus.processing", "生成中（processing）"},
	{mediamodel.VariantStatusFailed, "admin.media.variantStatus.failed", "生成失败（failed）"},
}

func variantStatusText(status, lang string) string {
	for _, s := range variantStatusTexts {
		if s.Status == status {
			return i18n.Translate(s.Key, s.Fallback, lang)
		}
	}
	return i18n.Translate(mediaenums.VariantStatusMissing, "无变体记录（missing）", lang)
}

// buildVariantReadme 生成变体目录的 README.txt 内容（该变体未 ready 时占位说明）。
//
// 文案按 lang 取词：README 会跟着 zip 落到用户机器上，是**导出产物**的一部分，
// 不该恒定是中文（词条缺失时回落代码里的中文原文）。
func buildVariantReadme(dirName, status, lang string) string {
	tr := func(key, fallback string) string { return i18n.Translate(key, fallback, lang) }
	var b strings.Builder
	b.WriteString(tr(mediaenums.PackageTitle, "go_wp 媒体资源包"))
	b.WriteString("\n====================\n")
	// 占位符是命名形态（{name}）：词条可被运营在后台改，裸 % 会让 Sprintf 输出乱码。
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageDir, "目录：{name}",
		map[string]string{"name": dirName}))
	b.WriteString("\n")
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageVariantType, "内容：{name} 变体",
		map[string]string{"name": zipDirVariantType(dirName)}))
	b.WriteString("\n")
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageStatus, "状态：{name}",
		map[string]string{"name": variantStatusText(status, lang)}))
	b.WriteString("\n")
	b.WriteString(tr(mediaenums.PackageVariantUnavailable, "说明：该变体文件当前不可用，原图见 original/ 目录。"))
	b.WriteString("\n")
	b.WriteString(tr(mediaenums.PackageRegenHint, "可在媒体库详情页点「重新生成变体」，生成完成后重新下载。"))
	b.WriteString("\n")
	return b.String()
}

// appendAttachmentEntries 把单个附件的四目录结构追加进 plan：
// prefix 为空表示单图包根目录，批量模式为 <stem>_<id>/ 子目录。
func (s *Service) appendAttachmentEntries(ctx context.Context, plan *mediato.DownloadPlan, att *mediamodel.AttachmentEntity, prefix, lang string) error {
	if att.StorageType != "local" {
		return errors.New(mediaenums.ErrDownloadStorageNotLocal)
	}

	join := func(parts ...string) string {
		// 过滤空段：单图包 prefix 为空，避免拼出前导斜杠 "/original/..."。
		nonEmpty := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				nonEmpty = append(nonEmpty, part)
			}
		}
		return strings.Join(nonEmpty, "/")
	}

	// 1) original/：原图（恒有）。原文件缺失时降级为 README 说明，不阻断整包。
	origName := sanitizeZipName(att.FileName)
	if srcPath, err := localObjectPath(attachmentStorageKey(att)); err != nil {
		plan.Entries = append(plan.Entries, mediato.DownloadEntry{
			Name:    join(prefix, zipDirOriginal, "README.txt"),
			Content: i18n.Translate(mediaenums.PackageOriginalPathInvalid, "原图存储路径非法，无法打包。\n", lang),
		})
	} else if _, statErr := os.Stat(srcPath); statErr != nil {
		plan.Entries = append(plan.Entries, mediato.DownloadEntry{
			Name:    join(prefix, zipDirOriginal, "README.txt"),
			Content: i18n.Translate(mediaenums.PackageOriginalMissing, "原图物理文件已缺失，仅剩元数据。\n", lang),
		})
	} else {
		plan.Entries = append(plan.Entries, mediato.DownloadEntry{
			Name: join(prefix, zipDirOriginal, origName),
			Path: srcPath,
		})
	}

	// 2) webp/ thumb/ medium/：ready 的变体直接打文件；未 ready 打 README 说明。
	variants, err := s.vm.ListByAttachment(ctx, att.ID)
	if err != nil {
		return err
	}
	byType := make(map[string]mediamodel.MediaVariantEntity, len(variants))
	for _, v := range variants {
		byType[v.VariantType] = v
	}
	for _, vt := range mediamodel.VariantTypes() {
		dir := variantTypeToZipDir(vt)
		v, ok := byType[vt]
		if !ok || v.Status != mediamodel.VariantStatusReady {
			status := ""
			if ok {
				status = v.Status
			}
			plan.Entries = append(plan.Entries, mediato.DownloadEntry{
				Name:    join(prefix, dir, "README.txt"),
				Content: buildVariantReadme(dir, status, lang),
			})
			continue
		}
		absPath, perr := localObjectPath(v.FilePath)
		if perr != nil {
			plan.Entries = append(plan.Entries, mediato.DownloadEntry{
				Name:    join(prefix, dir, "README.txt"),
				Content: buildVariantReadme(dir, mediamodel.VariantStatusFailed, lang),
			})
			continue
		}
		plan.Entries = append(plan.Entries, mediato.DownloadEntry{
			Name: join(prefix, dir, sanitizeZipName(filepath.Base(filepath.ToSlash(v.FilePath)))),
			Path: absPath,
		})
	}
	return nil
}

// mediaDownloadLang 取产物语言（可选变参）：空串 = 默认语言（i18n 内部按默认语言解析）。
func mediaDownloadLang(langs []string) string {
	if len(langs) > 0 {
		return strings.TrimSpace(langs[0])
	}
	return ""
}

// BuildDownloadPlan 构建单个附件的资源包打包计划（契约方法）。
func (s *Service) BuildDownloadPlan(ctx context.Context, attachmentID uint64, langs ...string) (plan *mediato.DownloadPlan, err error) {
	lang := mediaDownloadLang(langs)
	att, err := s.am.GetByID(ctx, attachmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(mediaenums.ErrAttachmentNotFound)
		}
		return nil, err
	}
	plan = &mediato.DownloadPlan{
		FileName: sanitizeZipName(strings.TrimSuffix(att.FileName, filepath.Ext(att.FileName))) + "_package.zip",
		Entries:  []mediato.DownloadEntry{},
	}
	if err := s.appendAttachmentEntries(ctx, plan, att, "", lang); err != nil {
		return nil, err
	}
	return plan, nil
}

// BuildBatchDownloadPlan 构建多个附件的资源包批量打包计划（契约方法）。
// 每图一个 <stem>_<id>/ 子文件夹（id 后缀防重名），子文件夹内同四目录。
func (s *Service) BuildBatchDownloadPlan(ctx context.Context, ids []uint64, langs ...string) (plan *mediato.DownloadPlan, err error) {
	lang := mediaDownloadLang(langs)
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return nil, errors.New(mediaenums.ErrDownloadEmpty)
	}
	plan = &mediato.DownloadPlan{
		FileName: "media_export_" + time.Now().Format("20060102") + ".zip",
		Entries:  []mediato.DownloadEntry{},
	}
	for _, id := range ids {
		att, ferr := s.am.GetByID(ctx, id)
		if ferr != nil {
			if errors.Is(ferr, gorm.ErrRecordNotFound) {
				// 单个附件不存在跳过（软删除/已删除），不阻断整批。
				continue
			}
			return nil, ferr
		}
		stem := strings.TrimSuffix(filepath.Base(filepath.ToSlash(att.FileName)), filepath.Ext(att.FileName))
		prefix := sanitizeZipName(fmt.Sprintf("%s_%d", stem, att.ID))
		if aerr := s.appendAttachmentEntries(ctx, plan, att, prefix, lang); aerr != nil {
			return nil, aerr
		}
	}
	if len(plan.Entries) == 0 {
		return nil, errors.New(mediaenums.ErrDownloadEmpty)
	}
	return plan, nil
}

// normalizeIDs 去重并保序清洗 id 集合（Query ids=1,2,3 解析后的规整）。
func normalizeIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
