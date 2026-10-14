-- +migrate Up
-- 更新说明支持 Markdown 撰写。
--
-- notes_format = 'markdown' 时 notes_markdown 是作者写的原文，release_notes 仍是服务端从原文渲染、
-- 再经同一套白名单净化的 HTML —— 官网、电脑版、iOS 与旧版 App 只认 HTML，这一列的含义不能变。
-- 新版 App 拿到 Markdown 原文后自己排版，标题、列表、引用、代码与文字颜色都不会在 HTML 的转换里丢掉。
-- 存量行落在 'html'，行为与升级前一致。本文件可重复执行。

ALTER TABLE app_versions
    ADD COLUMN IF NOT EXISTS notes_format   VARCHAR(16) NOT NULL DEFAULT 'html',
    ADD COLUMN IF NOT EXISTS notes_markdown TEXT        NOT NULL DEFAULT '';
