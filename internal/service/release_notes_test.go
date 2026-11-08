package service

import (
	"strings"
	"testing"

	appdomain "aegis/internal/domain/app"
)

func strptr(s string) *string { return &s }

func TestRenderReleaseMarkdownStructure(t *testing.T) {
	html := renderReleaseMarkdown(strings.Join([]string{
		"## 新增",
		"- **钢琴**：88 键",
		"- 节拍器",
		"",
		"1. 第一",
		"2. 第二",
		"",
		"> 引用",
		"",
		"`code` 与 ~~删除~~",
		"",
		"第一行",
		"第二行",
		"",
		"| 列 | 值 |",
		"| --- | --- |",
		"| a | b |",
	}, "\n"))
	for _, want := range []string{
		"<h2>新增</h2>",
		"<li><strong>钢琴</strong>：88 键</li>",
		"<ol>",
		"<blockquote>",
		"<code>code</code>",
		"<del>删除</del>",
		"第一行<br>",
		"<table>",
		"<td>a</td>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered html misses %q:\n%s", want, html)
		}
	}
}

func TestRenderReleaseMarkdownKeepsTextColor(t *testing.T) {
	html := renderReleaseMarkdown(`<span style="color:#D32F2F">卸载原版</span> 与 <font color="#2E7D32">绿色</font>`)
	if !strings.Contains(html, `<span style="color: #D32F2F">卸载原版</span>`) && !strings.Contains(html, `<span style="color:#D32F2F">卸载原版</span>`) {
		t.Fatalf("span color dropped: %s", html)
	}
	if !strings.Contains(html, `<font color="#2E7D32">绿色</font>`) {
		t.Fatalf("font color dropped: %s", html)
	}
}

func TestRenderReleaseMarkdownStripsUnsafeMarkup(t *testing.T) {
	html := renderReleaseMarkdown(strings.Join([]string{
		`<script>alert(1)</script>`,
		`<span style="color:#fff;position:fixed;background:url(javascript:alert(1))">x</span>`,
		`<span style="color:expression(alert(1))">y</span>`,
		`<span onclick="alert(1)">z</span>`,
		`[link](javascript:alert(1))`,
		`<img src="x" onerror="alert(1)">`,
	}, "\n\n"))
	for _, banned := range []string{"<script", "position", "url(", "expression", "onclick", "onerror", "javascript:"} {
		if strings.Contains(strings.ToLower(html), banned) {
			t.Errorf("unsafe %q survived: %s", banned, html)
		}
	}
	if !strings.Contains(html, "#fff") {
		t.Errorf("allowed color dropped together with the unsafe properties: %s", html)
	}
}

func TestApplyReleaseNotesMarkdown(t *testing.T) {
	item := appdomain.Release{}
	err := applyReleaseNotes(&item, appdomain.ReleaseMutation{
		NotesFormat:   strptr("markdown"),
		NotesMarkdown: strptr("\r\n## 修复\r\n- 修复闪退\r\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.NotesFormat != appdomain.ReleaseNotesMarkdown {
		t.Fatalf("format = %q", item.NotesFormat)
	}
	if item.NotesMarkdown != "## 修复\n- 修复闪退" {
		t.Fatalf("markdown not normalized: %q", item.NotesMarkdown)
	}
	if !strings.Contains(item.Notes, "<h2>修复</h2>") || !strings.Contains(item.Notes, "<li>修复闪退</li>") {
		t.Fatalf("notes html = %s", item.Notes)
	}
	if strings.ContainsAny(item.Summary, "#*-<") || !strings.Contains(item.Summary, "修复闪退") {
		t.Fatalf("summary = %q", item.Summary)
	}

	// 沿用格式：只改原文也会重新渲染
	if err := applyReleaseNotes(&item, appdomain.ReleaseMutation{NotesMarkdown: strptr("**加粗**")}); err != nil {
		t.Fatal(err)
	}
	if item.NotesFormat != appdomain.ReleaseNotesMarkdown || !strings.Contains(item.Notes, "<strong>加粗</strong>") {
		t.Fatalf("re-render failed: %q %s", item.NotesFormat, item.Notes)
	}
}

func TestApplyReleaseNotesInfersMarkdownAndSwitchesBack(t *testing.T) {
	item := appdomain.Release{}
	if err := applyReleaseNotes(&item, appdomain.ReleaseMutation{NotesMarkdown: strptr("- a")}); err != nil {
		t.Fatal(err)
	}
	if item.NotesFormat != appdomain.ReleaseNotesMarkdown {
		t.Fatalf("markdown-only mutation should infer markdown, got %q", item.NotesFormat)
	}
	if err := applyReleaseNotes(&item, appdomain.ReleaseMutation{NotesFormat: strptr("html"), Notes: strptr("<p>hi</p><script>x</script>")}); err != nil {
		t.Fatal(err)
	}
	if item.NotesFormat != appdomain.ReleaseNotesHTML || item.NotesMarkdown != "" || item.Notes != "<p>hi</p>" {
		t.Fatalf("switch to html: format=%q markdown=%q notes=%q", item.NotesFormat, item.NotesMarkdown, item.Notes)
	}
}

func TestApplyReleaseNotesLegacyHTMLKeepsFormat(t *testing.T) {
	item := appdomain.Release{Notes: "<p>old</p>"}
	if err := applyReleaseNotes(&item, appdomain.ReleaseMutation{Title: strptr("t")}); err != nil {
		t.Fatal(err)
	}
	if item.NotesFormat != appdomain.ReleaseNotesHTML || item.Notes != "<p>old</p>" {
		t.Fatalf("untouched html notes changed: %q %q", item.NotesFormat, item.Notes)
	}
}

func TestApplyReleaseNotesRejectsBadInput(t *testing.T) {
	item := appdomain.Release{}
	if err := applyReleaseNotes(&item, appdomain.ReleaseMutation{NotesFormat: strptr("rtf")}); err == nil {
		t.Fatal("unknown format accepted")
	}
	long := strings.Repeat("字", releaseNotesMarkdownMaxRunes+1)
	if err := applyReleaseNotes(&item, appdomain.ReleaseMutation{NotesFormat: strptr("markdown"), NotesMarkdown: &long}); err == nil {
		t.Fatal("oversized markdown accepted")
	}
}

func TestApplyReleaseNotesEmptyMarkdown(t *testing.T) {
	item := appdomain.Release{}
	if err := applyReleaseNotes(&item, appdomain.ReleaseMutation{NotesFormat: strptr("markdown"), NotesMarkdown: strptr("  <!-- 草稿 -->  ")}); err != nil {
		t.Fatal(err)
	}
	if item.Notes != "" || item.NotesMarkdown != "" || item.Summary != "" {
		t.Fatalf("empty markdown should clear notes: %+v", item)
	}
}

func TestPublicReleaseCarriesMarkdownOnlyForMarkdownFormat(t *testing.T) {
	md := appdomain.Release{Notes: "<p>x</p>", NotesFormat: "markdown", NotesMarkdown: "x"}
	if got := publicRelease(&md); got.NotesFormat != "markdown" || got.NotesMarkdown != "x" {
		t.Fatalf("markdown release: %+v", got)
	}
	stale := appdomain.Release{Notes: "<p>x</p>", NotesFormat: "", NotesMarkdown: "stale"}
	if got := publicRelease(&stale); got.NotesFormat != "html" || got.NotesMarkdown != "" {
		t.Fatalf("html release leaked markdown: %+v", got)
	}
}
