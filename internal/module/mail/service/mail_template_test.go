package mailservice

// mail_template_test.go — 模板正文的两条路径（T5）。
//
// 判据落在 bodyOf 上而不是发一封真邮件：发信要 SMTP / 抑制名单 / 队列，而要验的是
// 「渲染读哪一份正文」。这个函数的判据最容易被写错成「编译产物非空就用它」——
// 那样删掉 body_json 之后旧产物还会继续生效，而用户以为自己已退回代码模式。

import (
	"testing"

	mailmodel "go_wp/internal/module/mail/model"
)

// TestBodyOfPrefersCompiledWhenTreePresent body_json 非空 → 编译产物。
func TestBodyOfPrefersCompiledWhenTreePresent(t *testing.T) {
	tpl := &mailmodel.MailTemplateEntity{
		BodyHTML:         "<p>CODE</p>",
		BodyText:         "CODE",
		BodyJSON:         mailmodel.JSONMap{"schema": float64(1)},
		BodyCompiledHTML: "<table>VISUAL</table>",
		BodyCompiledText: "VISUAL",
	}
	html, text := bodyOf(tpl)
	if html != "<table>VISUAL</table>" || text != "VISUAL" {
		t.Fatalf("body_json 在场时应取编译产物，实际 html=%q text=%q", html, text)
	}
}

// TestBodyOfFallsBackToCodeMode 树不在 → 代码模式（与改造前逐字等价）。
func TestBodyOfFallsBackToCodeMode(t *testing.T) {
	tpl := &mailmodel.MailTemplateEntity{
		BodyHTML: "<p>CODE</p>",
		BodyText: "CODE",
		// 产物还在但树被删了：**必须**回落到 body_html / body_text。
		BodyCompiledHTML: "<table>STALE</table>",
		BodyCompiledText: "STALE",
	}
	html, text := bodyOf(tpl)
	if html != "<p>CODE</p>" || text != "CODE" {
		t.Fatalf("树不在时应回落到代码模式，实际 html=%q text=%q", html, text)
	}
}

// TestBodyOfNilTemplate 空模板不该 panic。
func TestBodyOfNilTemplate(t *testing.T) {
	html, text := bodyOf(nil)
	if html != "" || text != "" {
		t.Fatalf("空模板应返回空串，实际 %q / %q", html, text)
	}
}
