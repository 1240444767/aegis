package service

import (
	"bytes"
	"html"
	"net/http"
	"regexp"
	"strings"

	appdomain "aegis/internal/domain/app"
	apperrors "aegis/pkg/errors"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldhtml "github.com/yuin/goldmark/renderer/html"
)

// 更新说明的两种撰写格式。
//
// 一份版本只存一种「原文」：html 格式下原文就是富文本编辑器给的 HTML；markdown 格式下原文是
// notes_markdown，release_notes 由服务端从原文渲染。两种格式下 release_notes 都是净化过的 HTML，
// 所以只认 HTML 的消费方（官网、电脑版、iOS、旧版 App、旧检测接口）不需要知道格式这回事。
//
// Markdown 的渲染规则（客户端自己排版时必须照这几条来，否则同一份说明两边长得不一样）：
//   - CommonMark + GFM：表格、删除线、任务列表、裸链接自动识别；
//   - 段落内的单个换行就是换行（hard wrap）。更新说明是一行一条地写的，按 CommonMark 默认把
//     换行折成空格，「修复 A\n修复 B」会挤成一行；
//   - 允许行内 HTML，但只有净化白名单里的标签与属性能留下来，文字颜色写作
//     <span style="color:#D32F2F">…</span>。

const releaseNotesMarkdownMaxRunes = 20000

// richTextColorPattern 白名单里唯一允许的样式取值：#RGB、#RGBA、#RRGGBB、#RRGGBBAA，以及 rgb()/rgba()。
var richTextColorPattern = regexp.MustCompile(`(?i)^\s*(#[0-9a-f]{3,4}|#[0-9a-f]{6}|#[0-9a-f]{8}|rgba?\(\s*\d{1,3}\s*,\s*\d{1,3}\s*,\s*\d{1,3}\s*(,\s*(0|1|0?\.\d+)\s*)?\))\s*$`)

// releaseMarkdown goldmark 实例可以并发使用，构造一次全局复用。
var releaseMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(
		goldhtml.WithHardWraps(),
		// 行内 HTML 原样输出，交给下一步的 bluemonday 决定留什么。
		goldhtml.WithUnsafe(),
	),
)

// renderReleaseMarkdown 把 Markdown 原文渲染成净化后的 HTML。
func renderReleaseMarkdown(source string) string {
	if strings.TrimSpace(source) == "" {
		return ""
	}
	var out bytes.Buffer
	if err := releaseMarkdown.Convert([]byte(source), &out); err != nil {
		// goldmark 只在写出失败时报错，写进 bytes.Buffer 不会失败；真走到这里也不能让说明消失。
		return sanitizeRichText("<p>" + strings.ReplaceAll(html.EscapeString(source), "\n", "<br>") + "</p>")
	}
	return sanitizeRichText(out.String())
}

// normalizeReleaseMarkdown 统一换行并去掉首尾空白。行尾空格不动：两个行尾空格在 Markdown 里也是换行。
func normalizeReleaseMarkdown(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	return strings.Trim(source, " \t\n")
}

// parseReleaseNotesFormat 认不出的格式报错，而不是悄悄按 HTML 存：
// 那样作者写的 Markdown 会被当成一段 HTML 净化，井号与星号原样显示在用户面前。
func parseReleaseNotesFormat(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", appdomain.ReleaseNotesHTML:
		return appdomain.ReleaseNotesHTML, nil
	case appdomain.ReleaseNotesMarkdown, "md":
		return appdomain.ReleaseNotesMarkdown, nil
	default:
		return "", apperrors.New(40045, http.StatusBadRequest, "更新说明格式只能是 html 或 markdown")
	}
}

// releaseNotesFormatOf 存量行与旧缓存里的格式可能是空串，一律视为 html。
func releaseNotesFormatOf(format string) string {
	if format == appdomain.ReleaseNotesMarkdown {
		return appdomain.ReleaseNotesMarkdown
	}
	return appdomain.ReleaseNotesHTML
}

// releaseNotesMarkdownOf 只有 markdown 格式才带原文出去；html 格式下即便库里残留了原文也不发。
func releaseNotesMarkdownOf(rel *appdomain.Release) string {
	if releaseNotesFormatOf(rel.NotesFormat) != appdomain.ReleaseNotesMarkdown {
		return ""
	}
	return rel.NotesMarkdown
}

// applyReleaseNotes 把一次保存里的说明字段合进版本。
//
//   - 指定了 notesFormat 就按它；没指定但只交了 notesMarkdown，按 markdown；都没有则沿用原格式。
//   - markdown 格式每次保存都从原文重新渲染：白名单收紧或放宽之后，下一次保存就按新规则出 HTML。
//   - html 格式清掉 Markdown 原文，免得客户端拿到一份和正文对不上的过期原文。
func applyReleaseNotes(item *appdomain.Release, mutation appdomain.ReleaseMutation) error {
	switch {
	case mutation.NotesFormat != nil:
		format, err := parseReleaseNotesFormat(*mutation.NotesFormat)
		if err != nil {
			return err
		}
		item.NotesFormat = format
	case mutation.NotesMarkdown != nil && mutation.Notes == nil:
		item.NotesFormat = appdomain.ReleaseNotesMarkdown
	default:
		item.NotesFormat = releaseNotesFormatOf(item.NotesFormat)
	}

	if item.NotesFormat == appdomain.ReleaseNotesMarkdown {
		if mutation.NotesMarkdown != nil {
			item.NotesMarkdown = normalizeReleaseMarkdown(*mutation.NotesMarkdown)
		}
		if len([]rune(item.NotesMarkdown)) > releaseNotesMarkdownMaxRunes {
			return apperrors.New(40045, http.StatusBadRequest, "更新说明不超过 20000 个字符")
		}
		item.Notes = renderReleaseMarkdown(item.NotesMarkdown)
	} else {
		if mutation.Notes != nil {
			item.Notes = sanitizeRichText(*mutation.Notes)
		}
		item.NotesMarkdown = ""
	}
	if richTextIsEmpty(item.Notes) {
		item.Notes = ""
		item.NotesMarkdown = ""
	}
	item.Summary = releaseSummary(item.Notes)
	return nil
}

// ReleaseNotesPreview 控制台编辑时的预览：与保存走同一条渲染与净化，所见即所存。
type ReleaseNotesPreview struct {
	Format  string `json:"format"`
	HTML    string `json:"html"`
	Summary string `json:"summary"`
}

// PreviewReleaseNotes 不落库，只返回保存后会得到的 HTML 与摘要。
func (s *ReleaseService) PreviewReleaseNotes(format string, markdown string, notes string) (*ReleaseNotesPreview, error) {
	parsed, err := parseReleaseNotesFormat(format)
	if err != nil {
		return nil, err
	}
	item := appdomain.Release{}
	mutation := appdomain.ReleaseMutation{NotesFormat: &parsed}
	if parsed == appdomain.ReleaseNotesMarkdown {
		mutation.NotesMarkdown = &markdown
	} else {
		mutation.Notes = &notes
	}
	if err := applyReleaseNotes(&item, mutation); err != nil {
		return nil, err
	}
	return &ReleaseNotesPreview{Format: item.NotesFormat, HTML: item.Notes, Summary: item.Summary}, nil
}
