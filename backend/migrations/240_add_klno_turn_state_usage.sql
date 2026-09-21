-- 使用记录保存 Codex 回合状态：
--   1. turn_state：上游本次响应头里新铸的 x-codex-turn-state（不透明 Fernet 密文）
--   2. turn_state_overridden：该账号当时是否配置了 turn-state 覆写
--      （extra.openai_turn_state_override 非空且该账号类型落到 ChatGPT Codex 后端）
--      注意是「配置了」而非「本次出站确实生效」：判定只看账号类型，不看本次打的是
--      哪个端点，所以 OAuth/CPR 账号走 /embeddings、/chat/completions 等端点时同样
--      记 true（头确实被强塞了，只是在那些端点没有意义）。
--
-- 两列都可空：非 Codex 上游、WS 模式拿不到上游响应头时保持 NULL。
-- 不建索引：只做逐条排查用，不参与筛选或聚合。

-- ADD COLUMN 取 ACCESS EXCLUSIVE：热表上可能排在长 SELECT 后面并阻塞后续全部访问。
-- 与 033/038/079/080/081 同型，宁可迁移失败重试也不要把网关卡死。
SET LOCAL lock_timeout = '5s';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS turn_state TEXT;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS turn_state_overridden BOOLEAN;

COMMENT ON COLUMN usage_logs.turn_state IS
    'Codex x-codex-turn-state minted by upstream for this request (opaque Fernet blob)';

COMMENT ON COLUMN usage_logs.turn_state_overridden IS
    'Whether the account had a turn-state override configured at request time (by account type, not by endpoint)';

-- 区分本次 turn-state 覆写的来源：手填 / 自动接管。
--
-- 取值：
--   NULL        没带覆写
--   manual      账号 extra.openai_turn_state_override（手填）
--   auto        自动接管注入的候选
--   auto_stale  自动接管注入，且该候选铸造已超过保鲜期（默认 60 分钟）
--
-- auto 与 auto_stale 刻意分开：候选过了保鲜期仍然照用不删（292 太稀缺），
-- 这两个值的后续铸造结果分布，就是「1 小时到底会不会过期」的直接答案。
-- 确认会过期之后，再让保鲜期真正淘汰候选。

SET LOCAL lock_timeout = '5s';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS turn_state_source TEXT;

COMMENT ON COLUMN usage_logs.turn_state_source IS
    'Source of the turn-state override on this request: manual | auto | auto_stale (NULL = none)';

-- turn_state_overridden 的语义在本次改动里收紧了：239 上线时是「该账号当时配了手填
-- 覆写」（按账号类型推导，不看本次请求），现在是「本次请求真的注入了覆写值」。
-- 239 之后、240 之前写入的行按旧语义理解，之后的按新语义；两段无法 backfill 区分，
-- 做统计时用 turn_state_source IS NOT NULL 更可靠。
COMMENT ON COLUMN usage_logs.turn_state_overridden IS
    'Whether this request actually injected a turn-state override (rows written before migration 240 mean "the account had one configured")';

-- 本次出站实际带的 x-codex-turn-state。
--
-- 与 turn_state 不是一回事：
--   turn_state       上游本次响应头里**新铸**的 blob
--   turn_state_sent  我们**发出去**的 blob（客户端回带的，或覆写/自动接管注入的）
--
-- 为什么必须分开记：实测 4262 条 /responses，请求带了 turn-state 时只有 8.0% 会拿到
-- 新铸 blob。自动接管一开，注入请求里九成以上 turn_state 是 NULL——正好在最想看
-- 「到底发了什么」的时候什么都看不到。

SET LOCAL lock_timeout = '5s';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS turn_state_sent TEXT;

COMMENT ON COLUMN usage_logs.turn_state_sent IS
    'The x-codex-turn-state actually sent upstream on this request (echoed by the client or injected); turn_state is the one the upstream newly minted';
