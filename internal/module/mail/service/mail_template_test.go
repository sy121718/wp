package mailservice

import (
	"testing"

	mailmodel "go_wp/internal/module/mail/model"
)

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

func TestBodyOfFallsBackToCodeMode(t *testing.T) {
	tpl := &mailmodel.MailTemplateEntity{
		BodyHTML: "<p>CODE</p>",
		BodyText: "CODE",

		BodyCompiledHTML: "<table>STALE</table>",
		BodyCompiledText: "STALE",
	}
	html, text := bodyOf(tpl)
	if html != "<p>CODE</p>" || text != "CODE" {
		t.Fatalf("树不在时应回落到代码模式，实际 html=%q text=%q", html, text)
	}
}

func TestBodyOfNilTemplate(t *testing.T) {
	html, text := bodyOf(nil)
	if html != "" || text != "" {
		t.Fatalf("空模板应返回空串，实际 %q / %q", html, text)
	}
}
