-- +migrate Up
-- AI 中转的对话模型能力（docs/ai-gateway.md「模型能力」）：客户端据此决定发不发图片、带不带工具、
-- 给不给思考强度的选项。只是声明，网关不据此拒绝请求 —— 上游不支持的参数照常由上游报错。
-- 键：vision / tools / reasoning（布尔）、reasoningEfforts（可选强度，空表示 low/medium/high）、
-- contextLength（上下文长度，0 表示未知）。存量模型默认只开工具，与此前客户端的推断一致。
ALTER TABLE ai_gateway_models
    ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '{"tools": true}'::jsonb;
