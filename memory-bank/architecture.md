# Sub2API 架构说明

## 项目概述
Sub2API 是一个 AI 订阅转 API 的网关系统，将各种 AI 订阅账户（OpenAI、Anthropic、Gemini 等）统一转换为 OpenAI 兼容的 API 接口。

## 技术栈
- **后端**：Go 1.25.7 + Gin + Ent ORM
- **前端**：Vue 3.4+ / Vite 5+ / TailwindCSS（嵌入 Go 二进制）
- **数据库**：PostgreSQL 15+
- **缓存/队列**：Redis 7+
- **部署**：Docker Compose（镜像 `weishaw/sub2api:latest`）

## 目录结构
```
sub2api/
├── backend/                    # Go 后端
│   ├── internal/
│   │   ├── handler/           # HTTP 处理器（admin/api/gateway）
│   │   ├── service/           # 业务逻辑层
│   │   ├── repository/        # 数据访问层
│   │   └── ent/               # Ent ORM schema
│   ├── migrations/            # PostgreSQL 迁移文件
│   └── resources/             # 内置资源（模型定价JSON等）
├── frontend/                   # Vue 3 前端
│   ├── src/
│   │   ├── views/admin/       # 管理后台视图
│   │   ├── components/        # UI 组件（含定价卡片等）
│   │   ├── api/               # API 客户端
│   │   ├── i18n/              # 国际化
│   │   └── utils/             # 工具函数
├── deploy/                     # 部署配置（docker-compose、config.yaml）
├── docs/                       # 文档
└── tools/                      # 工具脚本
```

## 核心模块

### 在线更新系统
- 后端 `UpdateService`（`backend/internal/service/update_service.go`）查询 GitHub Release
- 前端 `VersionBadge.vue` 展示版本号和更新提示
- API 端点：`GET /api/v1/admin/system/check-updates`、`POST /api/v1/admin/system/update`
- 流程：查询 GitHub Release → 下载对应平台二进制 → SHA256 校验 → 原子替换 → 重启
- 支持 Rollback（保留 .backup 文件）

### 备份系统
- 后端 `BackupService`（`backend/internal/service/backup_service.go`）
- 使用 `pg_dump` 导出数据库，通过 S3 兼容协议上传到对象存储
- 前端 `BackupView.vue` 管理备份配置和操作
- 支持手动备份、定时备份（cron）、S3 云存储、保留策略
- S3 实现使用 AWS SDK v2（`backup_s3_store.go`），兼容腾讯云 COS

### 定价系统
- **channel_model_pricing** 表：渠道+平台维度的模型定价，`models` 字段为 JSONB 数组
- **channel_pricing_intervals** 表：定价条目的分级区间
- **platform** 字段：区分不同平台（openai/anthropic/gemini 等）
- **billing_mode**：token（按token计费）、per_request（按请求计费）、image（图片计费）
- 支持通配符模型匹配（如 `claude-*`）
- 定价解析链：Channel 自定义 -> LiteLLM 远程数据 -> 内置 Fallback

### 渠道系统
- 渠道绑定平台（OpenAI/Anthropic/Gemini 等）
- 每个平台可配置多个分组（group）
- 每个平台可有多个定价条目，每个条目关联一组模型
- 支持 AI 账户管理（OAuth/API Key）

### 网关系统
- 统一 OpenAI 兼容 API 接口
- HTTP/2 + WebSocket 支持
- TLS 指纹伪装
- 连接池隔离策略（account/proxy/account_proxy）
- Sora 视频生成支持

### 支付界面
- 用户侧充值/订阅页位于 `frontend/src/views/user/PaymentView.vue`
- 充值自定义金额输入组件为 `frontend/src/components/payment/AmountInput.vue`
- 订阅套餐卡组件为 `frontend/src/components/payment/SubscriptionPlanCard.vue`
- 支付金额、订阅售价、原价使用人民币符号 `¥`；账户余额/订阅额度中明确命名为 `*_usd` 的字段仍按 USD 配额展示为 `$`
- 后端创建订单时，`BalanceRechargeMultiplier` 只影响余额充值入账额度；订阅订单提交给支付宝/微信等支付网关的金额始终按套餐售价加手续费计算，不做充值倍率折算

### 管理端合规确认
- 官方 v0.1.136 新增管理端合规确认门：`backend/internal/service/admin_compliance.go` 保存每个管理员对当前合规版本的确认记录，`backend/internal/server/middleware/admin_compliance.go` 在管理端接口上强制确认
- 前端 `frontend/src/components/admin/AdminComplianceDialog.vue` 通过 `frontend/src/stores/adminCompliance.ts` 拉取状态，展示中英文合规文档并要求输入固定确认短语；确认后才允许继续使用管理端
- 合规确认版本常量为 `AdminComplianceVersion`，升级该常量会让管理员重新确认；确认内容保存到 settings 表的 `admin_compliance_acknowledgement:<adminUserID>` 键

### 用户侧 AI 工具页
- 前端新增 AI 对话与 AI 生图两个用户页面；当前左侧侧边栏保留 AI 对话入口，原 AI 生图入口已替换为“无限画布”外链入口
- “无限画布”入口位于 `frontend/src/components/layout/AppSidebar.vue`，新标签页打开固定地址 `https://canvas.zh-zh.top`，并使用 `rel="noopener noreferrer"` 与 `referrerpolicy="no-referrer"`
- “无限画布”入口不传 API Key、Base URL、用户 token、user_id 或任何 query/hash 参数；用户仍需在 Canvas 站点内手动填写 API Key
- 页面复用用户可用分组与 API Key 数据，先选分组，再选择该分组下 active API Key
- AI 对话直接使用选中的真实 API Key 调用现有 `POST /v1/chat/completions`
- 旧 `/ai/images` 路由和 `AiImageGenerationView.vue` 暂保留，可直接访问；该页面用户应选择 gpt-image 分组，底层属于 OpenAI 图片通道，直接调用现有 `POST /v1/images/generations`
- 不新增 JWT 后端代理接口，继续复用现有网关的 API Key 鉴权、分组路由、账号调度、计费、限额和用量日志

### 用户文档
- `docs/ZH_AI_USER_GUIDE_CN.md` 是 zh-ai 用户使用说明，面向最终用户说明控制台入口、API Key 创建、网关地址、鉴权、网页 AI 对话、网页 AI 生图和常用客户端配置
- `docs/ZZ_AI_CLIENT_USER_GUIDE_CN.md` 是按 Fern 文档风格整理的 zz AI 中转站专属窄范围教程，只覆盖客户端接入、AI 对话、AI 生图、充值订阅、邀请返利，避免把管理端或泛 API 参考内容混入用户教程
- `docs/legal/admin-compliance.zh.md` 与 `docs/legal/admin-compliance.en.md` 是官方 v0.1.136 新增的管理端部署与运营合规承诺文档，前端合规确认弹窗会以内嵌 Markdown 形式展示
- 文档里的模型、价格、额度、套餐和可用渠道以后台实时显示为准，避免把运营配置写死成长期架构事实

## 部署架构
```
主线路：用户请求 → ai.zh-zh.top → 新加坡加速服务器 → original-ai.zh-zh.top → VPS(Oracle Cloud ARM, 149.118.145.171)
  → Sub2API容器(8080) → PostgreSQL容器(5432, 仅内部网络)
                      ↘ Redis容器(6379, 仅内部网络, 有密码)

备用线路：用户请求 → ai.zh-zh.cloud → 国内备案入口服务器(118.25.1.151) → ai.zh-zh.top → 新加坡加速服务器 → original-ai.zh-zh.top → Sub2API
```

### 国内备案入口备用线路
- **入口域名**：`ai.zh-zh.cloud`
- **国内服务器**：OpenCloudOS 9.4，公网 IP `118.25.1.151`
- **用途**：作为备用 API 入口，用户请求和响应经国内 Nginx 反向代理后再进入现有新加坡加速链路
- **反代上游**：`https://ai.zh-zh.top`
- **Nginx 要点**：启用 HTTPS 强制跳转；反代需保留 `Authorization`，开启 SNI（`proxy_ssl_server_name on`），关闭代理缓存/缓冲以支持 SSE 流式输出
- **验证状态**：`https://ai.zh-zh.cloud/v1/models` 已可通过现有 Sub2API API Key 返回模型列表

### 部署信息
- **服务器**：Oracle Cloud ARM
- **域名**：`ai.zh-zh.top`
- **Docker 镜像**：`ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`（自定义 AI 工具版；当前自定义构建版本基于官方 `v0.1.144`，镜像内版本应显示为 `0.1.144-zz`；可回滚官方 `weishaw/sub2api:0.1.144`）
- **部署路径**：`/opt/sub2api/`
- **配置文件**：`/opt/sub2api/.env`

### Kiro Gateway 接入
- **容器名**：`kiro-gateway`
- **镜像**：`ghcr.io/jwadow/kiro-gateway:latest`
- **部署路径**：`/opt/kiro-gateway/`
- **网络**：加入 `sub2api_sub2api-network`，不暴露公网端口，仅供 Sub2API 内网访问
- **Sub2API OpenAI Compatible 渠道配置**：Base URL 使用 `http://kiro-gateway:8000/v1`，API Key 使用 `/opt/kiro-gateway/.env` 中的 `PROXY_API_KEY`
- **Sub2API Claude/Anthropic 分组配置**：账号类型选 API Key，URL/Base URL 使用 `http://kiro-gateway:8000`，API Key 使用 `PROXY_API_KEY` 原始值，不加 `Bearer `；Sub2API 会自动拼接 `/v1/messages?beta=true`
- **模型配置**：Kiro Opus 模型先使用 `claude-opus-4.7`
- **多账号配置**：已启用 `ACCOUNT_SYSTEM=true`；账号 JSON 放在 `/opt/kiro-gateway/accounts/`，挂载到 `/app/accounts`
- **状态文件配置**：状态目录 `/opt/kiro-gateway/state/` 挂载到 `/app/state`，`ACCOUNTS_STATE_FILE=/app/state/state.json`；必须挂载目录而不是单个 state 文件，否则保存状态时 rename 会报 `Device or resource busy`

### 自定义 Docker 部署与回滚
- 用户 fork：`https://github.com/masatoshiyokoyama635-sudo/sub2api`
- 长期自定义功能分支：`feature/chat-image-tools`
- GitHub Actions workflow：`.github/workflows/custom-docker.yml`
- 自定义 GHCR 镜像稳定标签：`ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`
- 官方 v0.1.136 起，前端会 raw import `docs/legal/admin-compliance.zh.md` 与 `docs/legal/admin-compliance.en.md`；根 `Dockerfile` 的 frontend builder 阶段必须在 `pnpm run build` 前复制 `docs/legal/` 到 `/app/docs/legal/`，且 `.dockerignore` 不能排除这两个文件
- VPS 部署方式仍使用 Docker Compose，只替换 `sub2api` 服务的 `image:`，PostgreSQL、Redis、卷和 `.env` 不需要因自定义镜像而大改
- 2026-04-29 用户已在原 VPS 测试自定义 GHCR 镜像成功，当前自定义镜像可作为正式部署镜像使用
- 回滚官方镜像时只需把 `image:` 改回 `weishaw/sub2api:0.1.126` 或官方最新稳定标签，然后执行 `docker compose pull sub2api && docker compose up -d sub2api`
- 官方 0.1.121 固定命令：`cd /opt/sub2api && sed -i 's#weishaw/sub2api:0\.1\.120#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.119#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:latest#weishaw/sub2api:0.1.121#g' docker-compose.yml && docker compose pull sub2api && docker compose up -d sub2api && docker compose ps`
- 自定义镜像回滚官方 0.1.121 命令：`cd /opt/sub2api && sed -i 's#ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.120#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.119#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:latest#weishaw/sub2api:0.1.121#g' docker-compose.yml && docker compose pull sub2api && docker compose up -d sub2api && docker compose ps`
- 2026-04-30 已将官方 `v0.1.121` 合并到自定义分支，保留 AI 对话/AI 生图功能，并把 `backend/cmd/server/VERSION` 同步为 `0.1.121` 以构建 `0.1.121-zz` 自定义镜像
- 2026-04-30 GitHub Actions run `25165651723` 成功构建并推送自定义镜像，用户已在 VPS 通过 Docker Compose 更新部署成功
- 2026-05-05 已将官方 `v0.1.123` 合并到自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.123` 以构建 `0.1.123-zz` 自定义镜像
- 2026-05-05 GitHub Actions run `25377558141` 成功构建并推送自定义镜像，用户已在 VPS 通过 Docker Compose 更新部署成功
- 2026-05-07 已将官方 `v0.1.125` 合并到自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.125` 以构建 `0.1.125-zz` 自定义镜像
- 2026-05-13 已将官方 `v0.1.126` 合并到自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.126` 以构建 `0.1.126-zz` 自定义镜像
- 2026-05-20 已将官方 `v0.1.129` 合并到本地自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.129` 以构建 `0.1.129-zz` 自定义镜像
- 2026-05-24 已将官方 `v0.1.130` 合并到自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.130` 以构建 `0.1.130-zz` 自定义镜像
- 2026-05-26 已将官方 `v0.1.131` 合并到自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.131` 以构建 `0.1.131-zz` 自定义镜像
- 2026-05-27 已将官方 `v0.1.132` 合并到自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.132` 以构建 `0.1.132-zz` 自定义镜像
- 2026-05-29 已将官方 `v0.1.133` 合并到自定义分支，保留 AI 对话/AI 生图功能和支付人民币符号补丁，并把 `backend/cmd/server/VERSION` 同步为 `0.1.133` 以构建 `0.1.133-zz` 自定义镜像
- 2026-06-06 已将官方 `v0.1.134` 合并到自定义分支，保留 AI 对话/AI 生图功能、支付人民币符号补丁和历史图片计费识别逻辑，并把 `backend/cmd/server/VERSION` 同步为 `0.1.134` 以构建 `0.1.134-zz` 自定义镜像
- 2026-06-09 已将官方 `v0.1.135` 合并到自定义分支，保留 AI 对话/AI 生图功能、支付人民币符号补丁和历史图片计费识别逻辑，并把 `backend/cmd/server/VERSION` 同步为 `0.1.135` 以构建 `0.1.135-zz` 自定义镜像
- 2026-06-10 已将官方 `v0.1.136` 合并到自定义分支，保留 AI 对话/AI 生图功能、支付人民币符号补丁、历史图片计费识别逻辑和专属客户端教程文档，并把 `backend/cmd/server/VERSION` 同步为 `0.1.136` 以构建 `0.1.136-zz` 自定义镜像
- 2026-06-16 已将官方 `v0.1.137` 合并到自定义分支，保留 AI 对话/AI 生图功能、支付人民币符号补丁、历史图片计费识别逻辑和专属客户端教程文档，并把 `backend/cmd/server/VERSION` 同步为 `0.1.137` 以构建 `0.1.137-zz` 自定义镜像；GitHub Actions run `27637619001` 成功推送稳定标签 `chat-image-tools` 和短 SHA 标签 `chat-image-tools-3adefcb`
- 2026-06-22 已将官方 `v0.1.138` 合并到自定义分支，保留 AI 对话/AI 生图功能、支付人民币符号补丁、历史图片计费识别逻辑和专属客户端教程文档，并把 `backend/cmd/server/VERSION` 同步为 `0.1.138` 以构建 `0.1.138-zz` 自定义镜像；GitHub Actions run `27957305480` 成功推送稳定标签 `chat-image-tools` 和短 SHA 标签 `chat-image-tools-edd2425`，manifest digest `sha256:4fa76c3010ce58e39a02dd145518156b9e4f65668e8c69b9110a6225c3e2131c`
- 2026-06-27 已将官方 `v0.1.139` 合并到自定义分支；按用户确认，长期二改仅保留 AI 对话、AI 生图和支付人民币符号补丁，图片计费等其他逻辑跟随官方；已把 `backend/cmd/server/VERSION` 同步为 `0.1.139` 以构建 `0.1.139-zz` 自定义镜像；GitHub Actions run `28276433805` 成功推送稳定标签 `chat-image-tools` 和短 SHA 标签 `chat-image-tools-338f326`，manifest digest `sha256:a83294cf99aae8147745440224125f5866a93f72e956c7dec892120f9e14ffed`
- 2026-06-30 已将官方 `v0.1.141` 合并到自定义分支，保留 AI 对话、AI 生图和支付人民币符号补丁；官方 `v0.1.141` 已修复订阅订单不应套用余额充值倍率的问题，本次支付订单逻辑和测试采用官方版本，并把 `backend/cmd/server/VERSION` 同步为 `0.1.141` 以构建 `0.1.141-zz` 自定义镜像；GitHub Actions run `28452856490` 成功推送稳定标签 `chat-image-tools` 和短 SHA 标签预计为 `chat-image-tools-3097596`
- 2026-07-01 已将官方 `v0.1.142` 合并到自定义分支，保留 AI 对话、AI 生图和支付人民币符号补丁；本次合并无冲突，并把 `backend/cmd/server/VERSION` 同步为 `0.1.142` 以构建 `0.1.142-zz` 自定义镜像；GitHub Actions run `28523495316` 成功推送稳定标签 `chat-image-tools` 和短 SHA 标签预计为 `chat-image-tools-4a199c8`，本地 Docker 构建未执行（当前 Windows/Git Bash 环境没有 `docker` CLI）
- 2026-07-02 已将官方 `v0.1.143` 合并到自定义分支，保留 AI 对话、AI 生图和支付人民币符号补丁；本次合并无冲突，并把 `backend/cmd/server/VERSION` 同步为 `0.1.143` 以构建 `0.1.143-zz` 自定义镜像；GitHub Actions run `28599090502` 成功推送稳定标签 `chat-image-tools` 和短 SHA 标签 `chat-image-tools-11e0b2a`，manifest digest `sha256:155fdb66a8d5cfe5517d50f6694e8ee330e7fc0565d57048ce418f8166433d22`
- 2026-07-04 已将官方 `v0.1.144` 合并到自定义分支，保留 AI 对话、AI 生图和支付人民币符号补丁；本次合并无冲突，并把 `backend/cmd/server/VERSION` 同步为 `0.1.144` 以构建 `0.1.144-zz` 自定义镜像；本地前端验证通过，后端 `go test ./...` 未执行（当前 Windows/Git Bash 环境没有 `go` CLI）；GitHub Actions run `28699907706` 成功推送稳定标签 `chat-image-tools` 和短 SHA 标签 `chat-image-tools-0d5821b`，manifest digest `sha256:3473d4360628d050cbd5afc4224d3ca87530bb5e1bbc8762c0e0cf521005a581`
- 支付金额人民币符号修复作为 fork 上的长期补丁保留，后续官方更新时直接把 upstream 合并到 `feature/chat-image-tools`，只在同一块 UI 有冲突时再处理

### 备份存储
- **对象存储**：腾讯云 COS（ap-shanghai）
- **存储桶**：`sub2api-1400654985`（私有读写）
- **访问方式**：子用户 + 最小权限策略

## v0.1.161 候选架构与安全边界（2026-07-18，未发布）
- 候选工作区位于 `E:/claude-cache/sub2api-v161-candidate`，基于自定义 `0c77db2b4` 合入官方 `v0.1.161`；当前仓库根目录仍是未提交的 v0.1.160 冻结现场，生产仍为 `0.1.159-zz`。
- 官方 v0.1.161 的普通敏感操作使用可配置 `StepUpAuthMiddleware`，`step_up_enabled` 缺失/错误默认关闭；Prompt Audit 的配置写入、节点探测、完整事件详情和删除操作属于更高敏感度控制面，必须使用不受全局开关影响且拒绝 Admin API Key 的 `StrictStepUpAuthMiddleware`。
- `StrictStepUpAuthMiddleware` 由 `backend/internal/server/middleware/step_up.go` 定义并由 `backend/internal/server/http.go` / `router.go` / `cmd/server/wire_gen.go` 注入；Prompt Audit 路由在 `backend/internal/server/routes/admin.go` 使用该严格门控，普通账号/备份/导出等路由继续使用开关感知门控。
- Step-up grant 现在必须绑定 JWT 的 session ID；没有 `sid` 的旧 JWT 不能创建或消费 grant，`POST /user/totp/step-up` 对无 session-bound JWT fail-closed。
- Prompt Audit 当前会把异步扫描正文写入 Redis、并按设计持久化 `full_prompt`；用户已确认候选部署在不公开的私人服务器上并接受该边界，因此它不再阻塞本轮候选发布。仍不得在日志、错误或公开回复中泄露实际 prompt/token 内容。
- 2026-07-18 用户明确本轮不会使用 Grok，因此 Grok probe/CAS/媒体资格问题不作为本轮修复范围，但必须保留为已知风险，不能宣称已修复；候选发布前仅继续处理非 Grok 阻塞。
- 非 Grok 待修边界：`PromptService`/`ConfigManager`/`Runner` 的 Start/Shutdown 需完整串行；Shutdown 超时必须向上层传播并阻止 Redis/PostgreSQL 提前关闭；processing job 需要独立 lease heartbeat 并在 heartbeat 失败时取消 scanner；enqueue 失败清理需要独立短时 context 和可观测错误；通用 settings 显式关闭 `risk_control_enabled` 等会削弱 Prompt Audit 的字段必须在任何写入前执行 Strict JWT+TOTP step-up，并拒绝 Admin API Key。
- 2026-07-19 上述非 Grok 边界已完成实现：PromptService、ConfigManager、Runner 使用单次运行状态机和共享 shutdown completion，支持 Start/Shutdown 串行、timeout 后继续后台 drain、禁止 restart，且初始配置 load error 保持可恢复 degraded-running；Runner 为 processing job 启动独立 lease heartbeat，refresh 失败取消 scanner 并禁止旧 owner Complete/Retry/Fail，heartbeat stop 与 scanner 返回边界使用独立停止信号收敛；enqueue 的 Set/Publish 失败均使用 `context.WithoutCancel` + 独立 timeout 执行 payload Delete 和 staging failure 标记，并用稳定错误码聚合可观测错误；应用 cleanup 返回 error，仅在全部应用层（含 Prompt Audit）成功停止后按 Redis→Ent 关闭依赖，主服务退出不再用 `log.Fatalf` 跳过 cleanup。
- 普通 `PUT /api/v1/admin/settings` 不整体挂 strict middleware；handler 仅在 `risk_control_enabled` 显式 true→false 或 `step_up_enabled` true→false 时复用 `middleware.EnforceStepUpAlways`。两者同请求降级合并为一次严格 JWT+TOTP 校验，Admin API Key、无 `sid`、无 grant 均在任何 settings 写入前拒绝；risk-control 字段省略、false→true 和同值保存维持原语义。
- 2026-07-19 Settings 更新进一步收敛为 `SettingService.UpdateSettingsAtomically` + repository transaction：主 settings、auth-source defaults、OpenAI fast policy、payment 在一条事务内构造/校验/批量 upsert；PostgreSQL 通过固定 `pg_advisory_xact_lock` 串行化，锁内重读两个安全开关并检测 baseline conflict；无授权的服务层安全降级返回 `SETTINGS_STRICT_AUTHORIZATION_REQUIRED`。HTTP server 新增 serve/handler/hijacked connection tracker，无法确认请求安全结束时不关闭 Redis/Ent。Prompt Audit heartbeat+scanner panic 不再允许外层 recover 写旧 owner 终态，PublishQueued 状态不确定时保留 payload。
- Prompt Audit 的准确故障策略是“仅已知 blocking intent 时 fail-closed”：当已观察到 `risk_control_enabled=true`、审计启用且 `blocking_enabled=true` 后，配置激活/重载失败会进入 blocking degradation；冷启动完全无法读取配置且没有任何历史 blocking intent 时保持默认关闭的 degraded-running，避免默认关闭功能因 settings 故障拖垮全网关。ConfigManager 使用 `installMu` 作为运行态线性化锁：Reload sequence 分配、成功/失败终态写入，以及 Save 的数据库 commit 与 post-commit fence 发布必须在同一协议下串行；风险开关读取/解析失败或新版本激活失败要记录稳定 load error、保留已知 blocking intent 并把旧 snapshot 标记为 untrusted，任何已读取旧版本的并发 Reload 均不得在新 Save 后安装快照或回写错误状态。

## v0.1.162 候选架构边界（2026-07-20，未提交）
- 独立候选工作区为 `E:/claude-cache/sub2api-v162-candidate`，从已发布自定义 v0.1.161 提交 `8ffbe61a74172efc90754570aa0f7afe4896c013` 创建；合并来源锁定 annotated tag object `34b7a5ad70b4b9b9bb96955562fe632ad625d783`，peeled commit `27f094e0960ebd8e52de7ff7e763c6fec2ff4057`。根目录 v0.1.160 冻结 merge 现场保持未触碰。
- 自定义二进制更新/回滚只有 `BuildType=release` 且运行版本为无 prerelease/build metadata 的合法稳定 SemVer 时才允许；通用 Dockerfile 默认 `BUILD_TYPE=source`，自定义工作流显式注入 `BUILD_TYPE=custom` 和 `0.1.162-zz`，防止错误元数据让官方二进制覆盖 fork。
- 自定义 GHCR 发布使用仓库级串行 concurrency，完整 SHA 与短 SHA 标签在 push 前都做 fail-closed availability 检查；注册表标签检查不是原子不可变存储，生产仍必须以构建输出的 manifest digest 为权威，短 SHA 仅作可读别名。
- 异步图片上游 URL 下载默认使用直连 SSRF-safe transport，拒绝 loopback、私网、link-local、metadata 与 DNS rebinding，并在 redirect 前再次校验目标；为避免代理端解析绕过本地 IP 策略，该下载器有意不继承环境代理。
- 旧 AI Images 页面允许 `b64_json` 或同源绝对/根相对 HTTPS 图片 URL；`currentOrigin` 的 path/query/hash 不会带入结果 URL，跨域、HTTP、userinfo、protocol-relative、data/javascript URL 均拒绝。
- 支付展示继续遵守 fork 长期边界：历史空/缺失套餐币种默认 CNY，显式币种动态显示，`daily_limit_usd`/`weekly_limit_usd`/`monthly_limit_usd` 等额度始终使用美元语义。

## v0.1.163 合并架构边界（2026-07-22，本地候选）
- 项目已整理为单工作树：唯一工作目录为 `E:/vis project/zz sub2api`；`v0.1.160`、`v0.1.161`、`v0.1.162`、`v0.1.163` 分别由 `merge/v0.1.160-chat-image-tools` 至 `merge/v0.1.163-chat-image-tools` 分支保留，原 `E:/claude-cache/sub2api-v16x-candidate` worktree 已注销并清理。旧 v0.1.160 合并现场保存在本地检查点提交 `7a9033658`，不会混入当前候选。
- 当前候选从已发布自定义 v0.1.162 提交 `b480d880c252ae76f6610452545eeba6cefff25b` 合入官方 annotated tag `v0.1.163`（peeled commit `d0bdd7e771636a8d315f542cafd39484f39bd60c`），源码版本和嵌入版本断言统一为 `0.1.163`。
- 官方 v0.1.163 的分组级 OpenAI reasoning effort 上限/映射通过 migration `185_group_reasoning_effort_policy.sql` 持久化，并在 HTTP、WebSocket v1/v2 请求入口统一应用；同时保留 Grok `/responses/compact`、Responses 客户端工具往返、Redis ACL username、调度器元数据与移动端修复。
- 合并继续保留 fork 的 AI Chat、AI Images、同源图片 URL 防护、历史套餐默认 CNY、Prompt Audit 安全加固、自定义更新来源校验以及两阶段 HTTP shutdown/cleanup。后端 `main.go` 冲突采用本地可观测错误返回与完整 cleanup 路径，它覆盖上游避免 shutdown `log.Fatalf` 跳过清理的修复目标。
- v0.1.163 最终 merge commit 为 `826ecb06d4c4df47ced0c61e35870c081a64da90`；Custom Docker Image run `29979060018` 发布的 manifest digest 为 `sha256:888cbb0398ac91e8e0d84a6df7b43c739befefa972a1bb21c4152374ccef3c4b`，用户已按 digest 在 VPS 更新成功。临时 Windows 构建产物已删除，不属于后续发布流程。

## v0.1.164 合并架构边界（2026-07-23）
- 当前候选从已发布自定义 v0.1.163 提交 `826ecb06d4c4df47ced0c61e35870c081a64da90` 合入官方 annotated tag `v0.1.164`（peeled commit `cd8bb98c44303b2c8f04c0da340447c992f0cb7d`），源码版本与嵌入版本断言统一为 `0.1.164`。
- 官方 v0.1.164 新增 composite groups/model routing 与 Ollama Cloud 用量同步，并包含支付宝移动端 deep link、OpenAI OAuth 输入标准化、流中断代理隔离、Grok 402 冷却和简单模式图片等修复。
- 依赖注入与路由冲突采用组合结果：`StrictStepUpAuthMiddleware` 继续保护本地高敏感管理路由，`CompositeRouteResolver` 同时进入 gateway 路由；两阶段 HTTP shutdown、hijacked connection 跟踪和 cleanup 错误传播保持不变。
- 设置更新仍通过 `UpdateSettingsAtomically` 一次提交主设置、认证默认值、OpenAI fast policy 与支付配置；v0.1.164 新增的 `AlipayMobilePrecreateDeepLink` 被纳入同一事务。`OllamaCloudUsageService` 已加入并行 cleanup 步骤，关闭依赖前会先停止后台同步。
- 发布仍只通过推送 `feature/chat-image-tools` 触发 `.github/workflows/custom-docker.yml` 构建 Linux amd64/arm64 GHCR 镜像；本地不生成 Windows 可执行文件。生产部署继续固定 GitHub Actions 输出的 manifest digest。

## v0.1.165 合并架构边界（2026-07-25，本地候选）
- 当前候选从自定义 v0.1.164 提交 `ed9b3a84c7be2a93f4962459bc6e79f67255d9ed` 合入官方 annotated tag `v0.1.165`（tag object `892c8fa3ab80ada8a624668808c3e575da7c04d5`，peeled commit `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29`），源码版本与嵌入版本断言统一为 `0.1.165`。
- 官方 ChatGPT Live 通过 `/v1/live` 与 `/backend-api/codex/realtime/calls` 暴露实时会话网关；分组实体新增 `allow_live`，并由 concurrency/gateway cache 管理 Live 租约和续租。migrations `187` 至 `190` 依次加入 usage `session_id`、Live request type、分组 Live 开关和邮箱别名去重索引。
- Claude Opus 5 的模型常量、Bedrock 映射、内置定价、前端模型映射和限流 scope 均采用官方实现；Ollama Cloud 用量刷新改为请求驱动 debounce，并保留 PostgreSQL 14/15/16 到期查询兼容修复。
- OpenAI Responses HTTP/WS 入口继续在渠道模型映射后使用本地 `IsExplicitImageGenerationIntent`，避免被动 `image_gen` namespace 误判，同时保留官方 item ID/namespace 净化、同账号重试与 Live 会话逻辑。Prompt Audit 的配置写入、节点探测、完整事件读取和删除仍由 `StrictStepUpAuthMiddleware` 保护，官方新增管理路由不削弱该边界。
- 前端 `GroupsView` 在加载时调用官方 `getLiveCapability()` 决定 Live 开关的启用流程；本地列设置和复制分组测试必须提供同名 API mock，避免挂载后的未处理 Promise rejection。Live 探测结果只允许回写仍处于活动状态且平台/编辑分组未变化的表单，关闭弹窗或切换平台会使对应异步令牌失效。AI Chat、AI Images、Canvas 外链及相关路由/语言包继续保留。
- Custom Docker 仍由根 `Dockerfile` 构建前端、嵌入 Go 后端并组装 PostgreSQL 客户端运行层；通用源码构建默认 `BUILD_TYPE=source`，`.github/workflows/custom-docker.yml` 显式注入 `0.1.165-zz` 与 `BUILD_TYPE=custom`。生产发布只认 GHCR workflow 输出的不可变完整 SHA 标签和 manifest digest。

## v0.1.166 合并架构边界（2026-07-27，本地候选）
- 当前候选从自定义 v0.1.165 提交 `9038f46f7a63c298b086f75f45db45c7ca38e2d5` 创建回滚分支 `backup/pre-v166-9038f46f7` 和候选分支 `merge/v0.1.166-chat-image-tools`，合入官方 annotated tag object `80255971d4a6aaa098ba2c5e74e5c7deee22e6e7`（peeled commit `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8`）；源码版本与嵌入版本断言统一为 `0.1.166`。
- 官方面板 API 限流器按认证用户或安全客户端 IP 作用于 auth/user/admin/payment 路由；路由融合同时保留本地 `StrictStepUpAuthMiddleware`，Prompt Audit 高敏感接口不会因新增 limiter 参数而退回普通 step-up。
- 官方“部分 settings PUT 不覆盖未提交字段”已并入本地原子设置事务：handler 计算 `OmittedSettingKeys`，`UpdateSettingsAtomically` 在单次 repository transaction 前删除未提交键，并在部分更新提交成功后从存储重建运行时缓存；省略 `step_up_enabled` 不触发错误的降级授权判断。
- OpenAI Responses WebSocket v2 同时保留官方逐轮模型映射、上游模型计费统计和响应模型恢复，以及本地图片生成权限门控。首帧与后续 `response.create` 都在模型映射后按实际上游模型识别显式图片意图，未授权分组仍以 policy violation 关闭连接。
- Prompt Audit 配置接口采用官方 `Public() (PublicConfig, error)` 契约和 `prompt_audit_config_unavailable` 错误，同时保留本地 blocking degradation、生命周期串行与 shutdown drain 测试。Grok handler unit 测试桩补齐 identity-bound billing CAS，生产 CAS 逻辑未改。
- Custom Docker 继续由 `.github/workflows/custom-docker.yml` 在 `feature/chat-image-tools` push 后构建并发布 GHCR amd64/arm64 镜像。本机 Docker Desktop 二进制缺失，WSL 也没有 Docker/Podman/buildah，因此本地只完成 Dockerfile 同参数的嵌入式 Linux 双架构构建；OCI 镜像仍以 GitHub Actions 输出的不可变标签和 manifest digest 为准。

## 安全状态（2026-04-24 审查）

### 已加固
- PostgreSQL/Redis 端口已通过 iptables 封锁外部访问
- Redis 已设置密码
- UFW 防火墙已启用（allow 22/80/443, deny 5432/6379）
- 管理员密码已修改
- CORS 默认禁止跨域
- 安全头已配置（X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP）
- 认证入口全部有速率限制（Redis + Lua，fail-close 策略）
- Admin API Key 使用 ConstantTimeCompare 防时序攻击
- Ent ORM 参数化查询，无 SQL 注入风险

### 待加固
- trusted_proxies 未配置（Cloudflare 场景下限速可能不准）
- CORS allowed_origins 未配置（跨域请求被拒绝）
- JWT_SECRET 未固定（容器重启后 session 失效）
- TOTP_ENCRYPTION_KEY 未固定（容器重启后 2FA 配置失效）
- URL 白名单默认禁用（SSRF 风险较低，因为端口未暴露）
