# Cyber 会话屏蔽的身份边界

`cyber_session_block_enabled` 控制上游实际返回 `cyber_policy` 后的本地会话屏蔽。仅凭请求历史长度或共享来源信息，不判定会话已被封锁。

## 支持的身份

本地封锁按认证后的 API Key ID、身份类型和明确会话 ID 派生独立哈希，按以下优先级读取：

1. 对话/线程请求头：`conversation_id`、`thread_id`、`thread-id`、`X-Conversation-ID`
2. 请求体：`client_metadata.thread_id`
3. 会话请求头：`session_id`、`session-id`、`X-Session-Id`、`X-OpenCode-Session`
4. 请求体：`client_metadata.session_id`

普通 HTTP、流式 HTTP 和 WebSocket `response.create.response` 包装均使用同一套解析规则。若同一类型的请求头和请求体同时存在但值不一致，或者同类型的多个请求头互相冲突，本次请求视为没有可信身份：不写入跨请求封锁键，`usage_logs.session_id` 也留空，避免错误封锁另一个会话。

同一会话在 HTTP / WebSocket 重连时应保持 ID 稳定。共用一个 API Key 的转发网关必须为不同下游用户的不同会话提供互不复用的 ID，例如由可信网关按用户与会话生成不透明标识。客户端声明的会话 ID 不是用户认证凭据；本机制用于抑制重复请求，不替代上游内容审核。

`prompt_cache_key`、`X-Session-Affinity`、IP、User-Agent、历史相似度和条目数均不构成封锁身份。没有明确会话 ID 时，不写入或查询跨请求的会话封锁；上游真实策略拒绝仍正常透传并记录。同一 WebSocket 连接已有的命中后阻断语义保留。

## 升级影响

新的会话封锁使用带 `thread` / `session` 类型的 v3 哈希命名空间。读取时临时兼容显式身份旧 v2 键，直到这些键按既有 TTL 自然过期；新命中只写 v3。更早版本中无法区分显式会话 ID 和缓存 key 的旧键不能安全继承，仍留在 Redis 中按原 TTL 自行过期，不迁移或删除。

滚动升级期间，旧实例仍执行旧逻辑；需要所有实例运行修复版本才能消除该误拦。本变更不自动解除下游网关已经记录的用户或会话限制。

问题记录：[上游 issue #6831](https://github.com/Wei-Shaw/sub2api/issues/6831)。
