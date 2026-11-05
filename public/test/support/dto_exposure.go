package support

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

var defaultCredentialPatterns = []string{
	"password", "passwd", "pwd",
	"secret", "credential", "salt",
	"token", "activationkey", "activation_key",
	"api_key", "apikey", "access_key", "private_key", "secret_key",
}

var defaultDTOExposureAllow = map[string]string{

	"AccountItem.HasPassword":         "布尔开关：只回答「有没有设过密码」，不回显任何凭据值",
	"GuestAccountResp.PasswordMailed": "布尔开关：只回答「初始密码有没有寄出去」，字段里没有密码本身",
	"EndpointItem.HasSecret":          "布尔开关：只回答「这个外部集成端点配没配签名密钥」。webhook 的 HMAC 密钥以密文落库、明文只在保存那一刻出现，**没有任何读回接口**（连密文都不出），响应用 HasSecret 而不是回显密钥物 —— 这正是 SEC-009 想要的形状，不是漏网",

	"WarehouseThirdPartyResp.HasCredential":    "布尔开关：只回答「这个仓配没配过对接凭据」（存了密文或引用名），字段里没有凭据本体",
	"WarehouseThirdPartyResp.CredentialMasked": "恒为常量掩码占位（inventoryenums.CredentialMask，****），供表单回显「已配置」；服务端不按用户输入回显任何内容",
	"WarehouseThirdPartyResp.SecretRef":        "凭据的**引用名**（如部署侧的环境变量名）：系统只知道值在别处叫什么、不知道值是什么；名字本身不是秘密，秘密在部署侧",

	"ModelEntry.MaxOutputTokens":   "计量字段：模型声明的最大输出 token 数（整型），不是凭据",
	"ModelRowResp.MaxOutputTokens": "计量字段：模型声明的最大输出 token 数（整型），不是凭据（同 ModelEntry 同名字段）",

	"ProviderCardResp.CSRFToken":        "CSRF 防护令牌：由服务端渲染时注入、随表单回传校验，本来就该出现在页面里；不是 API 凭据",
	"ProviderCardBase.CSRFToken":        "同上：卡片视图的壳键（csrf_token / t / PermSet / Buttons），从页面基座借来给 include 进来的卡片用，不是 API 凭据",
	"SessionListPageResp.CSRFToken":     "同上：会话列表页基座的壳键，供应商卡片从这里借 csrf_token；不是 API 凭据",
	"TranslationMissPageResp.CSRFToken": "同上：未翻译清单报告片段的壳键，随表单回传校验；不是 API 凭据",
	"Provider.HasAPIKey":                "布尔开关：只回答「这个供应商配没配过密钥」。密钥以 AES-GCM 密文落库、无任何读回明文的接口（连密文都不出），响应用 HasAPIKey 而不是回显密钥物",
	"Session.ContextTokens":             "计量字段：上下文窗口已占用的 token 数（整型），不是凭据",
	"SessionItem.Tokens":                "计量字段：会话消息累计 token 数（整型），不是凭据",
	"SessionDetail.VisibleTokens":       "计量字段：当前窗口内可见内容的 token 数（整型），不是凭据",
	"SessionEventItem.ContentTokens":    "计量字段：单条事件内容长度的 token 计数（整型），不是凭据",
	"FoldPlanResult.SegmentTokens":      "计量字段：折叠分段各自的 token 数（整型），不是凭据",

	"TokenItem.TokenPrefix": "展示用前缀（令牌前 12 字符），仅用于辨认是哪一把；与落库哈希无关，不能据此反推明文",
	"TokenCreateResp.Token": "刚生成的一次性明文令牌，只在创建应答出现一次（此后无任何读回明文的接口）；不交给创建者就无法使用",

	"McpPageResp.Tokens": "令牌列表（每项只有前缀、名称、范围、时间与状态，没有任何明文或哈希）；字段名命中词根 \"token\"，值是行列表不是凭据",

	"ChatResult.InputTokens":  "计量字段：上游上报的输入 token 数（整型），不是凭据",
	"ChatResult.OutputTokens": "计量字段：上游上报的输出 token 数（整型），不是凭据",
	"ChatResult.TotalTokens":  "计量字段：上游上报的总 token 数（整型），不是凭据",

	"ChatResult.CachedTokens": "计量字段：上游上报的缓存命中 token 数（整型），不是凭据",

	"SessionUsage.Tokens":             "计量字段：这批会话的事件正文 token 累计（整型），不是凭据",
	"SessionUsage.AvgTokens":          "计量字段：单会话平均 token（Tokens/Sessions 的整除结果，整型），不是凭据",
	"SessionUsage.TokensText":         "展示文本：把 Tokens 缩成「12.3M」这类短串，与整数同源、不含凭据",
	"SessionUsage.AvgTokensText":      "展示文本：把 AvgTokens 缩成短串，与整数同源、不含凭据",
	"SessionTokenUsage.ContextTokens": "计量字段：这条会话此刻仍留在上下文里的 token 数（整型），不是凭据",
	"SessionTokenRow.Tokens":          "计量字段：某一类事件贡献的 token 数（整型），不是凭据",
	"SessionModelUsage.Tokens":        "计量字段：某个 (供应商, 模型) 组合消耗的 token 数（整型），不是凭据",
	"SessionModelUsage.TokensText":    "展示文本：把上面的 Tokens 缩成短串（悬浮卡用），与整数同源、不含凭据",

	"SessionCallRow.Tokens":     "计量字段：这一次调用上游上报的总 token 数（整型），不是凭据",
	"SessionCallRow.TokensText": "展示文本：把上面的 Tokens 缩成短串（悬浮卡用），与整数同源、不含凭据",
}

type DTOExposureOptions struct {
	Roots []string

	Allow map[string]string

	Patterns []string
}

type DTOExposureFinding struct {
	Key   string
	File  string
	Line  int
	Field string
	Match string
}

func (f DTOExposureFinding) String() string {
	return fmt.Sprintf("%s:%d %s 字段 %q 命中敏感词 %q", f.File, f.Line, f.Key, f.Field, f.Match)
}

func RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("从 %s 向上未找到 go.mod，无法确定仓库根", dir)
		}
		dir = parent
	}
}

func AssertNoCredentialFieldsInDTOs(t *testing.T, opts DTOExposureOptions) {
	t.Helper()
	findings := ScanDTOExposure(t, opts)
	for _, f := range findings {
		t.Errorf("疑似凭据字段暴露：%s（若确认安全，请在允许清单里写明理由）", f.String())
	}
}

func ScanDTOExposure(t *testing.T, opts DTOExposureOptions) []DTOExposureFinding {
	t.Helper()
	root := RepoRoot(t)
	patterns := opts.Patterns
	if len(patterns) == 0 {
		patterns = defaultCredentialPatterns
	}
	allow := make(map[string]string, len(defaultDTOExposureAllow)+len(opts.Allow))
	for k, v := range defaultDTOExposureAllow {
		allow[k] = v
	}
	for k, v := range opts.Allow {
		allow[k] = v
	}
	roots := opts.Roots
	if len(roots) == 0 {
		roots = []string{"internal"}
	}

	var findings []DTOExposureFinding
	fset := token.NewFileSet()
	for _, rel := range roots {
		dirs, err := findDTODirs(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", rel, err)
		}
		for _, dir := range dirs {
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatalf("读取目录 %s 失败: %v", dir, readErr)
			}
			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
					continue
				}
				path := filepath.Join(dir, name)
				file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
				if parseErr != nil {
					t.Fatalf("解析 %s 失败: %v", path, parseErr)
				}
				relPath, _ := filepath.Rel(root, path)
				findings = append(findings, scanDTOFile(fset, file, relPath, name, patterns, allow)...)
			}
		}
	}

	slices.SortFunc(findings, func(a, b DTOExposureFinding) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
	return findings
}

func findDTODirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == "dto" {
			dirs = append(dirs, path)
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	return dirs, nil
}

func scanDTOFile(fset *token.FileSet, file *ast.File, relPath, fileName string, patterns []string, allow map[string]string) []DTOExposureFinding {

	if strings.HasSuffix(fileName, "_req.go") {
		return nil
	}
	var findings []DTOExposureFinding
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok || structType.Fields == nil {
				continue
			}
			if isRequestShape(typeSpec.Name.Name, structType) {
				continue
			}
			for _, field := range structType.Fields.List {
				jsonTag := jsonTagName(field)
				for _, ident := range field.Names {
					fieldName := ident.Name
					if fieldName == "_" {
						continue
					}
					key := typeSpec.Name.Name + "." + fieldName
					if _, ok := allow[key]; ok {
						continue
					}
					for _, candidate := range []string{fieldName, jsonTag} {
						if candidate == "" {
							continue
						}
						lower := strings.ToLower(candidate)
						for _, bad := range patterns {
							if strings.Contains(lower, strings.ToLower(bad)) {
								findings = append(findings, DTOExposureFinding{
									Key:   key,
									File:  relPath,
									Line:  fset.Position(ident.Pos()).Line,
									Field: candidate,
									Match: bad,
								})
								break
							}
						}
					}
				}
			}
		}
	}
	return findings
}

func isRequestShape(typeName string, structType *ast.StructType) bool {
	if strings.HasSuffix(typeName, "Req") || strings.HasSuffix(typeName, "Request") {
		return true
	}
	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag := field.Tag.Value
		if strings.Contains(tag, "binding:") || strings.Contains(tag, "validate:") {
			return true
		}
	}
	return false
}

func jsonTagName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag := strings.Trim(field.Tag.Value, "`")
	const prefix = `json:"`
	idx := strings.Index(tag, prefix)
	if idx < 0 {
		return ""
	}
	rest := tag[idx+len(prefix):]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return ""
	}
	return strings.Split(rest[:end], ",")[0]
}

func AssertNoCredentialFields(t *testing.T, types ...reflect.Type) {
	t.Helper()
	for _, typ := range types {
		for _, field := range dtoFields(typ) {
			if _, ok := defaultDTOExposureAllow[typ.Name()+"."+field.Name]; ok {
				continue
			}
			for _, candidate := range []string{field.Name, field.JSON} {
				if candidate == "" {
					continue
				}
				lower := strings.ToLower(candidate)
				for _, bad := range defaultCredentialPatterns {
					if strings.Contains(lower, bad) {
						t.Errorf("%s 上出现了疑似凭据字段 %q（命中 %q）", typ.Name(), candidate, bad)
					}
				}
			}
		}
	}
}

type dtoField struct {
	Name string
	JSON string
}

func dtoFields(t reflect.Type) []dtoField {
	return collectFields(t, nil, map[reflect.Type]bool{})
}

func collectFields(t reflect.Type, acc []dtoField, seen map[reflect.Type]bool) []dtoField {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return acc
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		acc = append(acc, dtoField{Name: f.Name, JSON: jsonName})
		switch f.Type.Kind() {
		case reflect.Struct, reflect.Ptr, reflect.Slice, reflect.Array:
			acc = collectFields(f.Type, acc, seen)
		}
	}
	return acc
}
