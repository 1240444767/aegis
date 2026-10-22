-- +migrate Up
-- 客户端崩溃报告：用户在崩溃屏上点「发送此报告」后，客户端免登录上报的一份纯文本报告。
-- 只在用户逐份同意时上报，规则见 docs/crash-reports.md。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。
CREATE TABLE IF NOT EXISTS app_crash_reports (
    id BIGSERIAL PRIMARY KEY,
    -- 对外编号（CR + 日期 + 随机串），上报接口返回它，用户反馈时报这个号即可查到
    public_id VARCHAR(32) NOT NULL,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 上报时带了令牌才有；账号注销后报告保留、只解除归属
    user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    -- 客户端自己的记录编号：同一应用内唯一，重复上报返回原来那一条
    report_id VARCHAR(64) NOT NULL,
    -- java / native / anr / exit
    kind VARCHAR(16) NOT NULL,
    title VARCHAR(512) NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    -- 完整纯文本报告，上限 256 KB（服务层校验）
    report TEXT NOT NULL,
    report_bytes INTEGER NOT NULL DEFAULT 0,
    app_version VARCHAR(64) NOT NULL DEFAULT '',
    version_code BIGINT NULL,
    os_version VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(128) NOT NULL DEFAULT '',
    abi VARCHAR(64) NOT NULL DEFAULT '',
    process_name VARCHAR(256) NOT NULL DEFAULT '',
    -- 从哪里发出的（如 crash_screen）
    source VARCHAR(32) NOT NULL DEFAULT '',
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    -- 请求头里的设备补充信息（X-Device-*）与按设备字典翻译的展示名
    device_info JSONB NOT NULL DEFAULT '{}'::jsonb,
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    client_ip VARCHAR(64) NOT NULL DEFAULT '',
    country_code VARCHAR(8) NOT NULL DEFAULT '',
    country VARCHAR(64) NOT NULL DEFAULT '',
    region VARCHAR(64) NOT NULL DEFAULT '',
    city VARCHAR(64) NOT NULL DEFAULT '',
    -- 管理员标记「已处理」
    resolved_at TIMESTAMPTZ NULL,
    resolved_by VARCHAR(128) NOT NULL DEFAULT '',
    -- 崩溃发生的时间（客户端时钟，晚于服务器时间太多的按服务器时间记）
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_app_crash_reports_report UNIQUE (appid, report_id),
    CONSTRAINT uq_app_crash_reports_public UNIQUE (public_id),
    CONSTRAINT ck_app_crash_reports_kind CHECK (kind IN ('java', 'native', 'anr', 'exit'))
);

CREATE INDEX IF NOT EXISTS idx_app_crash_reports_app_time
    ON app_crash_reports(appid, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_app_crash_reports_app_version
    ON app_crash_reports(appid, app_version);
CREATE INDEX IF NOT EXISTS idx_app_crash_reports_user
    ON app_crash_reports(appid, user_id) WHERE user_id IS NOT NULL;
-- 限频按「同一设备 / 同一 IP 最近一小时报了几份」直接数库，两条部分索引让它只扫一小段
CREATE INDEX IF NOT EXISTS idx_app_crash_reports_device_recent
    ON app_crash_reports(appid, device_id, created_at DESC) WHERE device_id <> '';
CREATE INDEX IF NOT EXISTS idx_app_crash_reports_ip_recent
    ON app_crash_reports(appid, client_ip, created_at DESC) WHERE client_ip <> '';
