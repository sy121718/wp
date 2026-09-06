// Package scaffold 插件脚手架：`plugin init <name>` 生成标准插件模板工程
// （docs/06-plugin-system.md §5，对标 strapi generate / GrapesJS 插件模板）。
//
// 生成即合法：manifest 可过 plugincomp.ParseManifest，模板文件命名符合
// components/{template}.jet 约定，migrations 示例含 CREATE SCHEMA + 建表。
// 开发者拿到模板改内容 → 打包 zip → 上传安装 → 组件进工作台。
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// idRe 插件 ID 白名单（与 plugincomp.idRe 同规则：小写字母开头）。
var idRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,60}$`)

// Files 生成模板文件集合（相对插件根目录的路径 → 内容，不含 name 前缀；
// Write 负责拼 name/ 落盘）。
// name 为插件 ID（小写字母开头，如 "marketing"）。
func Files(name string) (map[string]string, error) {
	if !idRe.MatchString(name) {
		return nil, fmt.Errorf("插件名 %q 非法（小写字母开头，2~61 位，仅字母数字下划线连字符）", name)
	}
	label := title(name)
	comp := name + "_card"
	return map[string]string{
		"manifest.json":               manifestJSON(name, label, comp),
		"components/" + comp + ".jet": componentTemplate(label),
		"migrations/001_init.sql":     migrationSQL(name),
		"assets/README.md":            "在此放置静态资产（css / js / 图片）。\n",
		"README.md":                   readme(name),
	}, nil
}

// Write 落盘到当前目录（生成 name/ 目录；已存在则报错不覆盖）。
func Write(name string) error {
	files, err := Files(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("目录 %s 已存在，拒绝覆盖", name)
	}
	for rel, content := range files {
		dst := filepath.Join(name, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// manifestJSON 生成 manifest（1 个示例组件 + 1 个示例预设 + migrations 骨架）。
func manifestJSON(name, label, comp string) string {
	return fmt.Sprintf(`{
  "id": %q,
  "name": %q,
  "version": "0.1.0",
  "migrations": "migrations",
  "schemaVersion": 1,
  "components": [
    {
      "name": %q,
      "label": %q,
      "hint": "示例卡片组件",
      "template": %q,
      "props": {
        "title": {"kind": "text", "label": "标题", "default": "示例标题"},
        "desc": {"kind": "textarea", "label": "描述", "default": "在这里填写描述。"},
        "bgColor": {"kind": "color", "label": "背景色", "default": "#f5f5f5"}
      },
      "styles": {
        "rules": [
          {"decls": [["display", "block"], ["border-radius", "12px"], ["padding", "24px"]], "bindings": [{"prop": "background", "from": "bgColor"}]},
          {"pseudo": "hover", "decls": [["box-shadow", "0 8px 24px rgba(0,0,0,.12)"]]}
        ]
      }
    }
  ],
  "presets": [
    {
      "id": %q,
      "label": %q,
      "category": "营销区块",
      "document": [
        {"id": "hero", "type": "core.container", "props": {"tag": "section", "layout": {"engine": "flex", "flex": {"direction": "column", "gap": "16px"}}}, "children": [
          {"id": "title", "type": "core.heading", "props": {"text": "预设标题", "tag": "h2"}}
        ]}
      ]
    }
  ]
}
`, name, label, comp, label, comp+".jet", name+"-hero", label+" Hero")
}

// componentTemplate 示例组件 Jet 模板（{{.V.title}} 走 nodeView.V，默认转义）。
func componentTemplate(label string) string {
	return fmt.Sprintf(`<article class="{{ .Classes }}">
  <h3>{{ .V.title }}</h3>
  <p>{{ .V.desc }}</p>
</article>
`)
}

// migrationSQL 示例迁移（CREATE SCHEMA + 建表，docs/06 §8 约定）。
func migrationSQL(name string) string {
	return fmt.Sprintf(`-- 001_init.sql — %s 插件 L1 数据层初始化
CREATE SCHEMA IF NOT EXISTS plugin_%s;

CREATE TABLE IF NOT EXISTS plugin_%s.demo_records (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
`, name, name, name)
}

// readme 打包上传说明。
func readme(name string) string {
	return fmt.Sprintf(`# %s 插件

标准插件模板（docs/06-plugin-system.md）。目录说明：

- manifest.json         插件清单（组件 + 预设 + 迁移声明）
- components/           组件 Jet 模板（{{.V.字段}} 走检查器 props）
- migrations/           版本化 SQL（含 CREATE SCHEMA，安装时按文件名序执行）
- assets/               静态资产（css/js/图片）

## 打包上传

    zip -r %s.zip %s/

到后台「插件管理」上传 %s.zip，启用后组件出现在工作台组件库。
`, title(name), name, name, name)
}

// title 插件显示名：连字符/下划线分词首字母大写（缺省回退原名）。
func title(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
