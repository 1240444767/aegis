-- +migrate Up
-- 会员档位（docs/vip-tiers.md）：权益只属于档位，套餐只是档位的售卖规格。
--
--   vip_tiers                       档位：名称与功能。权益的唯一来源
--   vip_plans.tier_id               套餐卖的是哪个档位；vip_plans.features 改为档位功能的镜像（只由仓储同步写）
--   vip_transactions.tier_id        这段会员期属于哪个档位：判定按档位**当前**的功能算
--   vip_transactions.extra_features 这一笔在档位之外另给的功能（自定义发放）
--
-- 之前权益按 plan_id 跟随套餐、套餐删掉就定格成快照：删掉套餐再上架一个同样的，
-- 新套餐是另一行，老会员与它毫无关系；不挂套餐的自定义永久会员永远停在发放那一刻的功能。
-- 档位不随套餐删除而消失，删档位必须指定会员迁往哪个档位，权益不会再悬空。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
-- 存量里「按名字认回已删套餐」「不挂套餐的永久会员归入永久档位」只在加列的那一次做（见下方 fresh）。

CREATE TABLE IF NOT EXISTS vip_tiers (
    id BIGSERIAL PRIMARY KEY,
    -- 与 vip_plans 一致不挂 apps 外键
    appid BIGINT NOT NULL,
    name VARCHAR(64) NOT NULL,
    description TEXT NULL,
    features TEXT[] NOT NULL DEFAULT '{}',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vip_tiers_app ON vip_tiers(appid, sort_order, id);

ALTER TABLE vip_plans ADD COLUMN IF NOT EXISTS tier_id BIGINT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_vip_plans_tier') THEN
        ALTER TABLE vip_plans ADD CONSTRAINT fk_vip_plans_tier
            FOREIGN KEY (tier_id) REFERENCES vip_tiers(id) ON DELETE RESTRICT;
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_vip_plans_tier ON vip_plans(tier_id);

DO $$
DECLARE
    fresh BOOLEAN;
    g RECORD;
    tid BIGINT;
BEGIN
    fresh := NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'vip_transactions' AND column_name = 'tier_id');

    ALTER TABLE vip_transactions ADD COLUMN IF NOT EXISTS tier_id BIGINT NULL;
    ALTER TABLE vip_transactions ADD COLUMN IF NOT EXISTS extra_features TEXT[] NOT NULL DEFAULT '{}';
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_vip_transactions_tier') THEN
        ALTER TABLE vip_transactions ADD CONSTRAINT fk_vip_transactions_tier
            FOREIGN KEY (tier_id) REFERENCES vip_tiers(id) ON DELETE SET NULL;
    END IF;
    CREATE INDEX IF NOT EXISTS idx_vip_transactions_tier ON vip_transactions(tier_id) WHERE tier_id IS NOT NULL;

    -- 1. 每个套餐都要有档位（每次都跑：滚动发布期间旧实例新建的套餐没有档位）。
    --    同一应用里功能相同的套餐归同一个档位；已有功能相同的档位就并进去。
    FOR g IN
        SELECT p.appid, p.fk,
               array_agg(p.id ORDER BY p.sort_order, p.id) AS ids,
               bool_and(p.lifetime) AS all_lifetime,
               (array_agg(p.name ORDER BY p.sort_order, p.id))[1] AS first_name,
               MIN(p.sort_order) AS sort_order
        FROM (SELECT vp.*, ARRAY(SELECT DISTINCT f FROM unnest(vp.features) f ORDER BY f) AS fk
              FROM vip_plans vp WHERE vp.tier_id IS NULL) p
        GROUP BY p.appid, p.fk
    LOOP
        SELECT t.id INTO tid FROM vip_tiers t
        WHERE t.appid = g.appid AND ARRAY(SELECT DISTINCT f FROM unnest(t.features) f ORDER BY f) = g.fk
        ORDER BY t.id LIMIT 1;
        IF tid IS NULL THEN
            INSERT INTO vip_tiers (appid, name, features, sort_order)
            VALUES (g.appid,
                    CASE WHEN g.all_lifetime THEN '永久会员'
                         WHEN cardinality(g.fk) = 0 THEN '基础会员'
                         ELSE g.first_name END,
                    g.fk, g.sort_order)
            RETURNING id INTO tid;
        END IF;
        UPDATE vip_plans SET tier_id = tid WHERE id = ANY(g.ids);
        tid := NULL;
    END LOOP;

    -- 2. 套餐还在的开通记录跟随套餐的档位（每次都跑，幂等）
    UPDATE vip_transactions vt SET tier_id = p.tier_id
    FROM vip_plans p
    WHERE vt.tier_id IS NULL AND vt.plan_id = p.id AND vt.appid = p.appid AND p.tier_id IS NOT NULL;

    -- 3. 老系统迁入的永久会员（000091 / ConvertLegacyLifetimeVip 补记）归入应用的永久档位（每次都跑：导入会再补记）
    UPDATE vip_transactions vt SET tier_id = d.tier_id
    FROM (
        SELECT DISTINCT ON (p.appid) p.appid, p.tier_id
        FROM vip_plans p
        WHERE p.lifetime AND p.tier_id IS NOT NULL
        ORDER BY p.appid, p.is_active DESC, p.sort_order ASC, p.id DESC
    ) d
    WHERE vt.tier_id IS NULL AND vt.appid = d.appid AND vt.lifetime AND vt.pay_channel = 'legacy_import';

    IF fresh THEN
        -- 4. 套餐已删的开通记录：同一应用里有同名、同形态（永久 / 限时）的套餐，就是「删掉再上架」的那个
        UPDATE vip_transactions vt SET tier_id = m.tier_id
        FROM (
            SELECT DISTINCT ON (p.appid, p.name, p.lifetime) p.appid, p.name, p.lifetime, p.tier_id
            FROM vip_plans p
            WHERE p.tier_id IS NOT NULL
            ORDER BY p.appid, p.name, p.lifetime, p.is_active DESC, p.id DESC
        ) m
        WHERE vt.tier_id IS NULL AND vt.plan_id IS NOT NULL AND vt.appid = m.appid
          AND vt.plan_name = m.name AND vt.lifetime = m.lifetime
          AND NOT EXISTS (SELECT 1 FROM vip_plans p WHERE p.id = vt.plan_id AND p.appid = vt.appid);

        -- 5. 仍生效、还没有档位的永久开通（自定义发放、套餐已删）归入应用的永久档位。
        --    看广告领的永久会员按场景算，不动。自定义发放当时选的功能保留为档位之外另给的功能。
        UPDATE vip_transactions vt
        SET tier_id = d.tier_id,
            extra_features = CASE WHEN vt.plan_id IS NULL THEN vt.features ELSE '{}' END
        FROM (
            SELECT DISTINCT ON (p.appid) p.appid, p.tier_id
            FROM vip_plans p
            WHERE p.lifetime AND p.tier_id IS NOT NULL
            ORDER BY p.appid, p.is_active DESC, p.sort_order ASC, p.id DESC
        ) d
        WHERE vt.tier_id IS NULL AND vt.appid = d.appid AND vt.lifetime
          AND vt.revoked_at IS NULL AND vt.pay_channel NOT IN ('admin_revoke', 'ad_reward');
    END IF;
END;
$$;
