// dto_exposure.go — 测试基建：对外响应 DTO 的凭据字段守卫（SEC-009）。
//
// 由来：客户管理的响应 DTO 早有一条反射断言，确保不搬 password / activation_key，
// 但那是针对**一个能力**写的。其余模块（mail 的发信账号含 SMTP 密码、admin 含密码
// 哈希、plugin / order / cart 各有自己的响应结构）没有同等保障。凭据泄露是**加法**
// 造成的 —— 某次顺手把实体整个塞进响应体，人工 review 只能发现当时看到的那一次。
//
// 判据：dto 包里的结构体字段，只要字段名或 json tag 命中敏感词清单，就视为疑似泄露，
// 除非在允许清单里逐条写明理由。
//
// 为什么走源码 AST 而不是反射：反射要求把每个类型逐个登记进测试（登记表会漂移，
// 「新加一个 DTO 忘了登记」恰恰是要防的情形）。AST 按目录扫描，新增文件自动纳入，
// 零维护。反射版断言（AssertNoCredentialFields）仍然保留，用于对关键类型做编译期
// 级别存在的显式钉死 —— 两条一起用：AST 管覆盖，反射管重点。
package support

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// defaultCredentialPatterns 敏感词清单（小写子串匹配）。
//
// 宁可比实际需要更宽：命中之后由人决定进允许清单还是改代码，
// 而漏掉一个词的代价是凭据随响应出去且没有任何提示。
var defaultCredentialPatterns = []string{
	"password", "passwd", "pwd",
	"secret", "credential", "salt",
	"token", "activationkey", "activation_key",
	"api_key", "apikey", "access_key", "private_key", "secret_key",
}

// defaultDTOExposureAllow 允许清单：`包名.类型名.字段名` → 放行理由。
//
// 每条都必须写清楚为什么它不是泄露 —— 这张表是给下一个读代码的人看的，
// 不是给人图省事的开关。
var defaultDTOExposureAllow = map[string]string{
	// 键是「类型名.字段名」而不是「包名.类型名.字段名」：AST 扫描与反射断言两条
	// 路径都要用同一张表，而反射拿不到 AST 里的包别名。表很短，重名歧义在实践中不存在。
	"AccountItem.HasPassword":        "布尔开关：只回答「有没有设过密码」，不回显任何凭据值",
	"GuestAccountResp.PasswordMailed": "布尔开关：只回答「初始密码有没有寄出去」，字段里没有密码本身",
}

// DTOExposureOptions 扫描参数（零值即默认口径：扫 internal 下所有 dto 目录）。
type DTOExposureOptions struct {
	// Roots 扫描根（仓库相对路径），默认 ["internal"]。
	Roots []string
	// Allow 额外的允许清单，与默认清单合并（键 `包名.类型名.字段名`）。
	Allow map[string]string
	// Patterns 替换默认敏感词清单（一般不需要改）。
	Patterns []string
}

// DTOExposureFinding 一条命中。
type DTOExposureFinding struct {
	Key   string // 包名.类型名.字段名
	File  string // 仓库相对路径
	Line  int
	Field string // 命中的字段名或 json tag
	Match string // 命中的敏感词
}

func (f DTOExposureFinding) String() string {
	return fmt.Sprintf("%s:%d %s 字段 %q 命中敏感词 %q", f.File, f.Line, f.Key, f.Field, f.Match)
}

// RepoRoot 从当前工作目录向上找到含 go.mod 的目录。
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

// AssertNoCredentialFieldsInDTOs 扫描 dto 包并断言零命中。
func AssertNoCredentialFieldsInDTOs(t *testing.T, opts DTOExposureOptions) {
	t.Helper()
	findings := ScanDTOExposure(t, opts)
	for _, f := range findings {
		t.Errorf("疑似凭据字段暴露：%s（若确认安全，请在允许清单里写明理由）", f.String())
	}
}

// ScanDTOExposure 扫描 dto 目录，返回未被允许清单放行的命中清单。
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

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings
}

// findDTODirs 找出 root 下所有名为 dto 的目录（含嵌套，如 product/inventory/dto）。
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

// scanDTOFile 扫描单个文件的响应结构体。
func scanDTOFile(fset *token.FileSet, file *ast.File, relPath, fileName string, patterns []string, allow map[string]string) []DTOExposureFinding {
	// 请求体文件整份跳过：改密码的请求**必须**能带 password 字段。
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

// isRequestShape 判断结构体是不是请求体，是则跳过。
//
// 三条判据任一成立即认定请求：名字以 Req / Request 结尾；任一字段带
// binding / validate tag（gin 绑定请求的写法）。请求体带 password 是正常的
// （改密码、登录），把它算成泄露只会让守卫被关掉。
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

// jsonTagName 取 json tag 的字段名（逗号前的部分），没有则返回空串。
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

// AssertNoCredentialFields 反射版断言：给定类型的字段名与 json tag（递归含嵌套）
// 都不含敏感词。用于对关键响应类型做显式钉死（编译期保证类型存在）。
//
// 与 AST 版共用同一张允许清单（键「类型名.字段名」）。
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

// dtoField 一个字段的两种名字：Go 字段名与 json tag 名。
type dtoField struct {
	Name string // Go 字段名
	JSON string // json tag 名（逗号前的部分），无 tag 时为空
}

// dtoFields 递归收集结构体（含嵌套、指针、切片元素）的字段。
func dtoFields(t reflect.Type) []dtoField {
	return collectFields(t, nil, map[reflect.Type]bool{})
}

// collectFields 递归主体。seen 防自引用类型导致的无限递归。
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
