-- +migrate Down
-- 重构删掉的列与表（报名、种子承诺、旧的价格与限额字段）不恢复：旧代码已不存在，恢复出来也没有读写它们的地方。
DROP INDEX IF EXISTS idx_lottery_activities_app_sort;
DROP INDEX IF EXISTS idx_lottery_draws_pending;
DROP INDEX IF EXISTS idx_lottery_draws_activity_time;
DROP INDEX IF EXISTS idx_lottery_draws_app_time;
DROP INDEX IF EXISTS idx_lottery_draws_user_time;
DROP INDEX IF EXISTS uq_lottery_draws_request;
DROP INDEX IF EXISTS uq_lottery_draws_no;
DROP TABLE IF EXISTS lottery_user_states;
