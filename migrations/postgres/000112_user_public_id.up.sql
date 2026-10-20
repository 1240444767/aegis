-- +migrate Up
-- 对外用户 ID。
--
-- users.id 是 BIGSERIAL，挂在 App 资料页、排行榜、OAuth2 sub 上等于公开了注册顺序和用户总量，
-- 也让「挨个试 ID」变得很便宜。这一版给每个用户发一个随机的 public_id，凡是用户或第三方
-- 能看到的地方都只给它；users.id 退回成纯内部主键（外键、管理端、日志）。
--
-- 形状：16 位十进制数字，取值 [1000000000000000, 9007199254740991]。
--   * 仍是整数：老客户端把 id 当 Long / number 解析，换值不换类型，已发布的版本不受影响；
--   * 上界是 2^53 - 1：JavaScript 的 Number 能精确表示，前端不会因为精度丢尾数；
--   * 号段约 8×10^15，按 1 亿用户算命中已占用号码的概率约十亿分之一，生成时再查重兜底。
--
-- 长期稳定靠三件事：
--   1. 列默认值就是生成函数，任何建号路径（注册、短信、第三方、导入、迁移、管理员新建）
--      不用改代码也拿得到号；
--   2. 触发器禁止 UPDATE 改号，发出去的号永远指向同一个人；
--   3. 删号（注销、管理员删除）时触发器把号记进 user_public_id_retired，生成函数会跳过它，
--      注销后的号永远不会再发给别人 —— 第三方按 sub 存的数据不会串到新用户身上。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS user_public_id_retired (
    public_id  BIGINT PRIMARY KEY,
    retired_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 随机源用 gen_random_uuid()（PG13 起内置，取自强随机数），不用 random()：
-- 后者是可预测的伪随机，拿到几个号就能推出后面的号。
CREATE OR REPLACE FUNCTION aegis_new_user_public_id() RETURNS BIGINT AS $$
DECLARE
    lo CONSTANT BIGINT := 1000000000000000;
    span CONSTANT BIGINT := 9007199254740991 - 1000000000000000 + 1;
    raw BIGINT;
    candidate BIGINT;
BEGIN
    FOR attempt IN 1..32 LOOP
        -- UUIDv4 后 8 字节只有 2 位是固定的变体位，余下 62 位随机，足够覆盖 53 位的号段
        raw := ('x' || encode(substring(uuid_send(gen_random_uuid()) FROM 9 FOR 8), 'hex'))::bit(64)::bigint;
        candidate := lo + ((raw % span) + span) % span;
        IF NOT EXISTS (SELECT 1 FROM users WHERE public_id = candidate)
           AND NOT EXISTS (SELECT 1 FROM user_public_id_retired WHERE public_id = candidate) THEN
            RETURN candidate;
        END IF;
    END LOOP;
    RAISE EXCEPTION 'aegis_new_user_public_id: no free public id after 32 attempts';
END;
$$ LANGUAGE plpgsql VOLATILE;

ALTER TABLE users ADD COLUMN IF NOT EXISTS public_id BIGINT NULL;

-- 唯一索引先于回填建好：NULL 互不冲突，空列上建索引是瞬间的事；而回填时生成函数的查重
-- 要靠它走索引，否则每发一个号都是一次全表扫描，百万用户就是平方级的耗时。
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_public_id ON users(public_id);

-- 回填存量用户。逐行调用生成函数才能让查重看到前面刚发出的号，所以不用一条 UPDATE ... SET = 函数()
-- 的集合写法（同一语句里后面的行看不到前面行的新值）。
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT id FROM users WHERE public_id IS NULL ORDER BY id LOOP
        UPDATE users SET public_id = aegis_new_user_public_id() WHERE id = r.id;
    END LOOP;
END;
$$;

ALTER TABLE users ALTER COLUMN public_id SET DEFAULT aegis_new_user_public_id();
ALTER TABLE users ALTER COLUMN public_id SET NOT NULL;

-- 只在缺的时候加：DROP 再 ADD 会在每次启动时锁表并全表校验一遍
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ck_users_public_id_range') THEN
        ALTER TABLE users ADD CONSTRAINT ck_users_public_id_range
            CHECK (public_id BETWEEN 1000000000000000 AND 9007199254740991);
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION aegis_users_public_id_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.public_id IS DISTINCT FROM OLD.public_id THEN
            RAISE EXCEPTION 'users.public_id is immutable (user %)', OLD.id;
        END IF;
        RETURN NEW;
    END IF;
    -- DELETE：号退役，永不复用
    INSERT INTO user_public_id_retired (public_id) VALUES (OLD.public_id) ON CONFLICT DO NOTHING;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_users_public_id_guard ON users;
CREATE TRIGGER trg_users_public_id_guard
    BEFORE UPDATE OF public_id OR DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION aegis_users_public_id_guard();
