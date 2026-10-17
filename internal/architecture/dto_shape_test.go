// dto_shape_test.go — 全仓 dto 目录的**形状门禁**：一个域只能有 req / resp 两份文件。
//
// 口径（2026-10-18 用户拍板，product 模块已按此收口）：控制面 DTO 目录里，一个业务域最多两份
// 文件 —— `<域>_req.go`（接手的入参）/ `<域>_resp.go`（返回的回执）。别的命名（xxx_dto.go、
// xxx_view.go、xxx_req 之外的一切）都要先判断「这份形状是进还是出」才能定位，同一屏的入参
// 与回执也常常散在不同文件里。
//
// **pending 是必须缩小的清单**：登记的是此刻还没收口的文件，判据双向 ——
// 登记了却已不存在同样失败（收口完不删登记 = 变红），所以它不会退化成注释。
// 新写的 dto 文件一律直接按 req / resp 命名，不要往 pending 里加。
package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pendingDTOFiles 还没收口的存量 dto 文件（收到一个删一条）。
var pendingDTOFiles = map[string]bool{
	"internal/module/admin/dto/dept_dto.go": true,
	"internal/module/admin/dto/menu_dto.go": true,
	"internal/module/ai/dto/ai_chat.go": true,
	"internal/module/ai/dto/ai_session_dto.go": true,
	"internal/module/ai/dto/ai_token_dto.go": true,
	"internal/module/analytics/dto/analytics_dto.go": true,
	"internal/module/artifact/dto/artifact_dto.go": true,
	"internal/module/block/dto/block_dto.go": true,
	"internal/module/blueprint/dto/blueprint_dto.go": true,
	"internal/module/build/dto/build_dto.go": true,
	"internal/module/content/dto/content_dto.go": true,
	"internal/module/contenttemplate/dto/contenttemplate_dto.go": true,
	"internal/module/mail/dto/mail_tracking.go": true,
	"internal/module/navigation/dto/navigation_dto.go": true,
	"internal/module/order/dto/order_attr.go": true,
	"internal/module/order/dto/order_coupon_audit.go": true,
	"internal/module/order/dto/order_customer_cohort.go": true,
	"internal/module/order/dto/order_customer_growth.go": true,
	"internal/module/order/dto/order_customer_rfm.go": true,
	"internal/module/order/dto/order_customer_segment.go": true,
	"internal/module/order/dto/order_sales_overview.go": true,
	"internal/module/presentation/dto/presentation_dto.go": true,
	"internal/module/project/dto/locale_dto.go": true,
	"internal/module/project/dto/shipping_policy_dto.go": true,
	"internal/module/project/dto/site_settings_dto.go": true,
	"internal/module/project/dto/theme_dto.go": true,
	"internal/module/publication/dto/publication_dto.go": true,
	"internal/module/publication/dto/publication_receipt.go": true,
	"internal/module/sysconfig/dto/sysconfig_dict.go": true,
	"internal/module/sysconfig/dto/sysconfig_dto.go": true,
	"internal/module/user/dto/user_guest.go": true,
}

var dtoFileName = regexp.MustCompile(`^[a-z0-9_]+_(req|resp)\.go$`)

func TestDTOFileNamesAreReqOrResp(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	dirs, err := filepath.Glob(filepath.Join(root, "internal/module/*/dto"))
	if err != nil {
		t.Fatalf("枚举 dto 目录失败: %v", err)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel, err := filepath.Rel(root, filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("计算相对路径失败: %v", err)
			}
			rel = filepath.ToSlash(rel)
			if dtoFileName.MatchString(name) {
				continue
			}
			seen[rel] = true
			if !pendingDTOFiles[rel] {
				t.Errorf("%s：dto 目录只允许 <域>_req.go / <域>_resp.go —— 把入参并进 _req.go、回执并进 _resp.go", rel)
			}
		}
	}
	for rel := range pendingDTOFiles {
		if !seen[rel] {
			t.Errorf("pendingDTOFiles 登记了 %s，但该文件已不存在（或已合规）—— 请删掉这条登记", rel)
		}
	}
}
