-- +migrate Up
-- 账号初始化（docs/account-setup.md）：第三方、短信、卡密这类「快速登录即建号」的账号，
-- 账号名是系统生成的（qq_<openid>、手机号、卡面），也没有密码。登录后引导用户设一次用户名和密码。
--
--   user_account_setup   每个需要初始化的账号一行。没有行 = 不需要初始化（自己注册的账号）
--     account_pending    用户名还是系统生成的。设过一次（用户自己或管理员改名）即为 false
--     prompted_at        首次弹窗已展示的时刻；之后只出横幅
--     snoozed_until      用户关掉横幅后，在此之前不再出横幅
--     completed_at       用户名、密码都已就绪的时刻（按应用策略要求的项算）
--
-- 密码是否已设不落在这里：直接看 users.password_hash，改密、找回密码、管理员重置都不必来同步这张表。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。存量回填只在建表那一次做（见下方 fresh）。

DO $$
DECLARE
    fresh BOOLEAN;
BEGIN
    fresh := NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = current_schema() AND table_name = 'user_account_setup');

    CREATE TABLE IF NOT EXISTS user_account_setup (
        user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
        appid BIGINT NOT NULL,
        -- 建号方式：oauth / sms / cardkey
        source VARCHAR(16) NOT NULL,
        -- 第三方渠道（source = oauth 时），如 qq、wechat
        provider VARCHAR(32) NOT NULL DEFAULT '',
        account_pending BOOLEAN NOT NULL DEFAULT TRUE,
        account_set_at TIMESTAMPTZ NULL,
        prompted_at TIMESTAMPTZ NULL,
        snoozed_until TIMESTAMPTZ NULL,
        completed_at TIMESTAMPTZ NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );
    CREATE INDEX IF NOT EXISTS idx_user_account_setup_app ON user_account_setup(appid, completed_at);

    IF fresh THEN
        -- 存量：按档案里记下的建号方式认出快速登录建的号。账号名仍是系统生成的才算用户名待设
        INSERT INTO user_account_setup (user_id, appid, source, provider, account_pending, created_at, updated_at)
        SELECT u.id, u.appid,
               p.extra->>'register_method',
               CASE WHEN p.extra->>'register_method' = 'oauth' THEN COALESCE(p.extra->>'provider', '') ELSE '' END,
               CASE p.extra->>'register_method'
                   WHEN 'oauth' THEN u.account LIKE (COALESCE(p.extra->>'provider', '') || '\_%')
                   WHEN 'sms' THEN u.account = COALESCE(p.phone, '')
                   ELSE TRUE
               END,
               u.created_at, NOW()
        FROM users u
        JOIN user_profiles p ON p.user_id = u.id
        WHERE p.extra->>'register_method' IN ('oauth', 'sms', 'cardkey')
        ON CONFLICT (user_id) DO NOTHING;

        -- 用户名、密码都已就绪的直接记为完成
        UPDATE user_account_setup s SET completed_at = NOW()
        FROM users u
        WHERE u.id = s.user_id AND s.completed_at IS NULL
          AND NOT s.account_pending AND COALESCE(u.password_hash, '') <> '';

        -- 登录标识新增「用户 ID」（16 位对外 ID）：存量策略默认开启，管理员可在接入策略里关掉
        UPDATE app_auth_protocol_policies
        SET identifiers = array_append(identifiers, 'id')
        WHERE NOT ('id' = ANY(identifiers));
    END IF;
END $$;
