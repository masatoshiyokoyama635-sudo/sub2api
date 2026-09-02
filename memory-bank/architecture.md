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
- **Docker 镜像**：`ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`（自定义 AI 工具版；当前源码基线为官方 `v0.1.173`，自定义镜像内版本应显示为 `0.1.173-zz`；生产部署固定 GitHub Actions 输出的 manifest digest）
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

## v0.1.168 合并架构边界（2026-07-28）
- 当前候选从已发布自定义 v0.1.166 merge commit `e5e78d42058cc8040b0aa850305e73a9ddc1c380` 创建回滚分支 `backup/pre-v168-e5e78d420` 和候选分支 `merge/v0.1.168-chat-image-tools`，合入官方 annotated tag object `58106606685b1b59c2986e77fb799ba27ea7d75e`（peeled commit `99c8e4bf7564823bafbab369acab6539e734c1bb`）；源码版本与嵌入版本断言统一为 `0.1.168`。
- 官方 v0.1.168 引入 Passkey、Model Plaza、Kimi K3、`SKIP_SETUP` 和用户/API Key 字段级更新，并包含 Prompt Audit 解密死锁、Claude OAuth system cache breakpoint、Codex API Key Web Search、GPT-5.6 max effort、透传模型映射和 OpenAI Live store 韧性等修复。
- 依赖注入与路由融合同时接入 Model Plaza 的 optional JWT，并保留本地 `StrictStepUpAuthMiddleware` 对 Prompt Audit 高敏感接口的保护；HTTP 两阶段 shutdown、普通连接和 hijacked connection 跟踪继续保留。
- Prompt Audit 配置管理保留本地 reload/save 线性化 fence、blocking fail-closed 元数据和 shutdown 生命周期约束，同时接入官方无效密文降级：解密失败的端点仍保留在活动配置中但标记 `TokenInvalid` 且禁用，其他有效端点可继续工作。新写入端点令牌仍要求固定 `TOTP_ENCRYPTION_KEY`。
- AI Chat、AI Images 路由、Infinite Canvas 外链、默认 CNY/动态币种和订阅 USD-to-CNY 显式换算继续保留。Custom Docker 仍由 `feature/chat-image-tools` push 触发 GitHub Actions 构建 Linux amd64/arm64 多架构镜像，生产部署只固定 workflow 输出的 manifest digest。

## v0.1.169 合并架构边界（2026-07-31）
- 当前候选从自定义 v0.1.168 merge commit `50d1c88802c31d427fda528b708f89770534cc97` 创建回滚分支 `backup/pre-v169-50d1c8880` 和候选分支 `merge/v0.1.169-chat-image-tools`，合入官方 annotated tag object `830b5f507396b858874b171feae1cbcfce1caded`（peeled commit `26d894ef4f50645a4bf1030e378ac892f17d0223`）；源码版本与嵌入版本断言统一为 `0.1.169`。
- 官方 v0.1.169 收紧 Responses 子路径与 Gemini 模型名的上游 URL 闭集校验，容器 Compose 默认加入 `no-new-privileges:true`，补齐定价兜底资源复制，并修复代理断流熔断误隔离全部账号、GLM-5.2 子串定价、Anthropic `count_tokens` 参数、Claude Code 分类、订阅到期标签、Token 刷新和 SMTP 邮件格式等问题。
- 网关路由保留本地 composite model routing、OpenAI 图片意图门控和 AI Chat/AI Images/Canvas 定制，同时接入官方 `guardResponsesSubpath` 与 Gemini action/model path guard；代理断流熔断保留本地调度流程，并合入官方 3 秒事件合并、可配置关闭和无账号时的 fail-open 重试。
- 根 Dockerfile 已包含 `/app/backend/resources` 到 `/app/resources` 的定价兜底资源复制；`deploy/docker-compose.yml` 新增 `security_opt: no-new-privileges:true`。GHCR 仍由 `feature/chat-image-tools` push 触发 linux/amd64、linux/arm64 构建，生产部署只固定 Actions 输出的 manifest digest。

## v0.1.170 合并架构边界（2026-08-02）
- 当前候选从已发布自定义 v0.1.169 merge commit `be9ba60b519e01a710173f56282c33f2e61fc0d1` 创建回滚分支 `backup/pre-v170-be9ba60b5` 和候选分支 `merge/v0.1.170-chat-image-tools`，合入官方 annotated tag object `60286d35e4b6dc6851ab69f890c2d1b7b7a3bcb8`（peeled commit `c043c24774228ba891ddf90d783aa6dc7d0855b5`）；源码版本与嵌入版本断言统一为 `0.1.170`。
- 官方 v0.1.170 新增分组级利润控制、全 API Key 平台上游计费倍率探测与可选自动同步、内容审核代理、Prompt Audit 仅审计最新输入、筛选结果全选和批量删除并发限制；利润控制与倍率自动同步默认关闭。migrations `192_group_profit_control.sql` 与 `193_group_profit_control_auth_cache_invalidation.sql` 在启动时自动执行。
- Prompt Audit 冲突采用语义并集：异步审计继续保留完整正文，blocking 可选最新用户输入及前一条 assistant/model 输出；同时保留本地 1 MiB 扫描载荷、2 MiB 原始请求上限和有界类型化错误。图片存储测试中的重复 `roundTripFunc` 已去重，data URL 官方修复与本地 SSRF 回归均保留。
- AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、订阅汇率、Strict Step-up、原子设置更新和自定义 Docker 发布链路继续保留。GHCR 仍由 `feature/chat-image-tools` push 触发 linux/amd64、linux/arm64 构建，生产部署固定 workflow 输出的 manifest digest。
- v0.1.170 merge commit `0d9619ffe79e1415127fbc0f5f28ffc0ca91b499` 已由 Custom Docker Image run `30747270102` 成功发布；稳定标签、短 SHA 标签与完整 SHA 标签均指向 manifest digest `sha256:259a9ab6a4336bca34e4dc2da7cebb90d6b7b31bba3bed9cbba5160162e5d6e7`，manifest 包含 linux/amd64 与 linux/arm64。

## v0.1.171 合并架构边界（2026-08-04）
- 当前候选从 v0.1.170 发布记录提交 `12a2a139327cdccf40b9bfc38b5f3671939eab40` 创建回滚分支 `backup/pre-v171-12a2a1393` 和候选分支 `merge/v0.1.171-chat-image-tools`，合入官方 annotated tag object `afd154b92aac36c6dafb1fa8e181ca827c78c465`（peeled commit `f0e7a9c7a23a7d02fb159b62fa809621eb0475a6`）；三方合并无文本冲突，源码版本与嵌入版本断言统一为 `0.1.171`。
- 官方 v0.1.171 接入腾讯天御与阿里云验证码 2.0、Codex 出站身份与版本同步、组合分组推理强度策略、OpenAI 重置额度缓存、过载错误有界重试和退款 `require_force` 流程；同时保留本地 AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、Strict Step-up、原子设置更新和自定义 Docker 发布链路。
- 合并后的 Custom Docker 测试契约已补齐 `OpenAICodexVersionSyncService` cleanup 依赖，并将嵌入版本回归断言同步为 `0.1.171`；这些修复不改变生产业务路径。
- merge commit `fdc747c1c69132a8742f8a141d944dafb55036d6` 及后续测试修复提交已推送到 fork 的 `feature/chat-image-tools`。Custom Docker Image run `30976386453` 的 test/build job 全部成功，GHCR 稳定标签、短 SHA 标签和完整 SHA 标签均指向 manifest digest `sha256:d6eeec3ef08cf0052dc342854a92dfcbe7db62989ba0b08e8b97efdc2f0b578c`，manifest 包含 linux/amd64 与 linux/arm64。生产部署仍需单独执行。

## v0.1.172 合并架构边界（2026-08-07）
- 当前候选从 v0.1.171 发布记录提交 `7dec0c4dba54106a8d0c3a300a5deec1bec1570d` 创建回滚分支 `backup/pre-v172-7dec0c4db` 和候选分支 `merge/v0.1.172-chat-image-tools`，合入官方 annotated tag object `61ba94d2e85a00ba639fc870b91946b1bd2f990d`（peeled commit `155c494964c3ea6ecc31f52679525c1034bf0f16`）；54 个提交、208 个文件自动合并，无文本冲突。
- 官方 v0.1.172 修复 OAuth 登录补全流程的高危账号接管漏洞，并新增上游响应模型审计、Antigravity Gemini 3.6 Flash 映射；同时修复 Codex `codex-tui` 出站身份、故障转移、Responses/Anthropic 内容块、订阅日额度午夜刷新、计费精度、建连/TLS 超时和多项账号/媒体边界问题。
- 新增 migrations `194_add_usage_log_upstream_response_model.sql` 与 `195_add_usage_log_upstream_model_mismatch_index_notx.sql`：用量日志上游响应模型字段与 mismatch 并发索引均为增量变更，migration runner 保留非事务索引失败清理/重试逻辑。官方本轮未修改 Dockerfile、Go/Node 依赖锁定或 Compose 入口。
- 合并保留本地 AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、Strict Step-up、原子设置更新、支付补丁及 Custom Docker workflow；`backend/cmd/server/VERSION` 与嵌入版本断言统一为 `0.1.172`。
- merge commit `584c265b17dbfd7971f049bc0c7e1d392e473090` 已推送到 fork 的 `feature/chat-image-tools`。Custom Docker Image run `31235663120` 的 test/build job 全部成功，GHCR 稳定标签、短 SHA 标签和完整 SHA 标签均指向 manifest digest `sha256:44453b038fe3faf016682f5fccab07c4ee176eae29b7c16a13abd9d769c46eaf`，manifest 包含 linux/amd64 与 linux/arm64。生产部署仍需单独执行。

## v0.1.173 合并架构边界（2026-08-09）
- 当前候选从自定义 v0.1.172 提交 `5cb376e48f8c463c1ea194adc4051400b41bf4a8` 创建回滚分支 `backup/pre-v173-5cb376e48` 与候选分支 `merge/v0.1.173-chat-image-tools`，合入官方 annotated tag object `9e2a27ad39201a14074982bae331c4610161586a`（peeled commit `29009f0b2ea14edf3b11ae2564fb617ff91a03b4`）。
- 官方比较范围为 120 个提交、352 个文件（+33,307/-2,271）。本地融合保留 AI Chat、AI Images、Canvas 外链、Prompt Audit/Strict Step-up、原子设置、支付补丁及 fork 专用 Custom Docker workflow；Dockerfile 继续支持 `BUILD_TYPE=custom`。
- v0.1.173 新增 Grok/xAI SSO/refresh_token、媒体/Voice/Web Search、模型映射与调度门禁，以及被动渠道监控 V2；新增 migrations `194_channel_monitor_v2.sql` 至 `206_channel_monitor_v2_privacy_defaults.sql`、`217_group_video_model_prices.sql` 至 `220_clear_non_grok_video_generation_config.sql`。同号的 `194/195` 文件与 v0.1.172 用量日志迁移按完整文件名共存。
- 迁移 220 会先将非 Grok/非 composite 分组的视频价格保存到 `groups_video_price_backup_220`，再清理历史视频价格；生产升级前导出相关 `groups` 配置并暂停管理端写入，启动后核对快照表。
- 合并后的测试契约修复仅涉及前端重复 `getLiveCapability` mock 与 `provideCleanup` 的 V2 聚合器参数，未改变业务路径；`backend/cmd/server/VERSION` 和嵌入版本断言统一为 `0.1.173`。
- merge commit `b3b28ff2f5f8e865f66f09cfa7609a99df24bfa9` 已推送到 fork `feature/chat-image-tools`。Custom Docker Image run `31310903130`（test `93238472413`、build `93239327957`）全部成功，构建参数为 `VERSION=0.1.173-zz`、`BUILD_TYPE=custom`，平台为 linux/amd64、linux/arm64。
- GHCR 三个标签 `chat-image-tools`、`chat-image-tools-b3b28ff`、`chat-image-tools-b3b28ff2f5f8e865f66f09cfa7609a99df24bfa9` 均指向 OCI manifest digest `sha256:431d89555d653e96eb0cab7c3375ccc79485955ea23ad14bb642225dd3731103`；生产部署继续固定该 digest。

## v0.1.178 合并架构边界（2026-08-19）
- 当前发布从自定义 v0.1.177 提交 `218796a7d32023419ced9cfa1c47cb98d3fe4d97` 创建回滚分支 `backup/pre-v178-218796a7d` 和候选分支 `merge/v0.1.178-chat-image-tools`，合入官方 annotated tag object `15290e66c66801a7ce435a6d24b178ee9486f284`（peeled commit `e0c48a19ed794a565e3858662520afe0a1f9f0ba`）；版本与嵌入断言统一为 `0.1.178`。
- 官方 v0.1.178 引入 Kimi/智谱/DeepSeek 平台支持、渠道监控配额模式、渠道模型谷峰定价、Codex 指纹迁移与 OpenAI Team 联动熔断；新增 migrations `224_user_platform_quotas_add_cn_providers.sql`、两个 `225_*.sql` 和 `226_channel_monitor_quota_mode.sql`，生产更新前必须先生成可验证的 PostgreSQL dump。
- 合并继续保留 AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、Strict Step-up、原子设置、Prompt Audit、安全 shutdown、自定义 updater 和 GHCR workflow。认证缓存的嵌套 TimePricing periods 必须深拷贝，避免并发读写共享 slice。
- OpenAI compat 结果裁决同时区分计量、failover、cyber 与流状态：有计量 failover 不返回 result，零计量 cyber 交给 exactly-once fallback；零计量 partial output、client disconnect 和 missing-terminal 状态 result 则保留给 handler，Chat 与 Messages 共用同一 helper。
- 最终代码提交 `65c74ca395e6e337e9baa413f4727e8ed3cb16ed` 的 CI、Security 和 Custom Docker 共 8 个 job 全绿。GHCR 完整 SHA 标签固定 OCI index `sha256:94f8cd3a5f34783d22d66e5aa338cfbd9be0ca00e7c77c642f014e4d55ce1b63`，包含已核验 revision 的 linux/amd64 与 linux/arm64；生产只替换 `services.sub2api.image`，VPS 仍由用户手动 SSH 更新。

## v0.1.179 合并架构边界（2026-08-23）
- 当前候选从 v0.1.178 发布记录提交 `71b9de1605e534cc1375107244b06da6e5507a0c` 创建回滚分支 `backup/pre-v179-71b9de160` 和候选分支 `merge/v0.1.179-chat-image-tools`，合入官方 annotated tag object `3c28fad50472b409e18666df617f4237d8ba7007`（peeled commit `75f88be5f75c27771836b586f7de1503afa0e3bc`）；源码版本与嵌入断言统一为 `0.1.179`。
- handler 冲突采用语义并集：保留 partial output/client disconnect 状态供 handler 做 exactly-once 计费与健康度归因，同时接入 composite 解析到 Grok/Kimi/智谱/DeepSeek 时跳过 OpenAI 专属 Messages 分组映射的官方规则。Responses HTTP/WS 的显式图片意图门控继续在渠道映射后的 model/body 上判定。
- 官方 v0.1.179 新增 CN adaptive Chat/Messages/Responses 路由、Composite Codex/CN、渠道服务层级和上下文区间倍率、Anthropic Fast 计费、Responses input_tokens、本地用量聚合索引与多项 OpenAI/WS/Grok 恢复修复；本地 AI Chat、AI Images、Canvas、动态币种、Prompt Audit、Strict Step-up、原子设置、安全 shutdown、Grok identity-bound billing 和自定义 Docker workflow 保持。
- 新迁移 `226_add_usage_log_effective_model_indexes_notx.sql` 与既有 `226_channel_monitor_quota_mode.sql` 按完整文件名共存；runner 对两条并发表达式索引保留 invalid-index 清理重试。227 放开 Composite CN CHECK，228 增加渠道倍率列。长上下文计费门控从 group AND account 改为 group OR account，生产升级前必须确认超过 272k 的 2x/1.5x 账单口径。
- 本地 Go 1.26.6 默认编译、全量 unit、vet，前端 `242/242` files / `1719/1719` tests 和 production build，以及部署 shell 门禁全部通过；CI run `32629576753`、Security Scan run `32629576760` 和 Custom Docker Image run `32629576757` 共 8 个 job 全绿。
- merge commit `80f7809f2860db8f94ef08ea36268b55815d4eeb` 的稳定、短 SHA、完整 SHA 标签均固定 OCI index `sha256:9037f2297b5e345666e726823096cb5e9fbeb1ed0fdbeecff493ce5c46632794`，包含 revision 已核验为完整代码 SHA 的 linux/amd64 child `sha256:5194785f3d4b631073076993396fe235f283d6af846447471e7dc6f26f9343d1` 与 linux/arm64 child `sha256:1b5b623fbdf7cf31f6444538b7a044798c2128c5ee416cd7eaacffced9725fde`；生产继续只替换 `services.sub2api.image`，由用户手动 SSH 更新。

## v0.1.181 合并架构边界（2026-08-25）
- 当前发布从 v0.1.179 发布记录提交 `f665825d45ac78d3f55f9f2d251e98aa5e82d3b6` 创建回滚分支 `backup/pre-v181-f665825d4` 和候选分支 `merge/v0.1.181-chat-image-tools`，合入官方 annotated tag object `2b5768269e08f2b52e00fb7a7f97788145a05ce2`（peeled commit `3af5443b224823ae507a50c7b113aa50604409c8`）；源码版本与嵌入断言统一为 `0.1.181`，Go/Docker builder 统一为 1.27.0。
- 本轮引入实验性 OAuth 出站插件系统及 migrations `229_plugins.sql`、`230_plugin_artifacts.sql`，并接入 OpenAI 重置卡按用量阈值自动使用、Fast `service_tier` 全链路实际档位计费、模型列表读取上限、compact fallback、Codex OAuth 身份与保留工具别名。v0.1.181 进一步修复 Gemini tool schema、Grok CLI User-Agent、Responses Lite `parallel_tool_calls` 和同类型 input status 批量清理。
- OpenAI HTTP/WS 冲突采用语义并集：partial usage、client disconnect、cyber partial result 继续 exactly-once 入账并保持健康度归因；同时保存实际 upstream `service_tier`、自动重置状态、compact 重试和 account identity。WS 入站固定按图片权限门、compatibility normalization、reserved-tool alias、account identity 的顺序处理。
- Prompt Audit 继续使用线性化 Reload/Save 安装栅栏和 unknown/untrusted fail-closed 状态；普通设置保留 session-bound Strict Step-up 与 PostgreSQL 原子事务。shutdown 仍先两阶段停止 HTTP/WS，再依次清理 OpenAI auto-reset、Prompt Audit 和 PluginManager，并传播 cleanup 错误，避免依赖提前关闭。
- merge commit `7791c2a3259520b2267653ecd9dec34b7409d356` 的 CI、Security Scan 和 Custom Docker 全绿；稳定、短 SHA、完整 SHA 标签均固定 OCI index `sha256:3619090a9f087ea96e5875951dfff76fc072d3f3cd75be44659a2a2a41fda79e`，包含 revision 已核验为完整代码 SHA 的 linux/amd64 child `sha256:cb53df8cba8f2a9c7dd38a9fe2f9bfb9ab068d7e2c80e12a72f2aa329a8e591d` 与 linux/arm64 child `sha256:f6976194ff42f101bceef8c0a57c6bc778b61f198e0fcdd0ed87c7d08158c01f`。生产只替换 `services.sub2api.image`，由用户手动 SSH 更新。

## v0.1.183 合并架构边界（2026-08-26）
- 当前发布从 v0.1.181 发布记录提交 `2943e164bceef98f1b1c6d70ae7ced1c8cc8d302` 创建回滚分支 `backup/pre-v183-2943e164b` 和候选分支 `merge/v0.1.183-chat-image-tools`，合入官方 annotated tag object `c21fd3382a1c39fe491a96ac6780bac927327ae4`（peeled commit `e8cb019fabf8b55199436229044cbf9aa7a82564`）；`v0.1.183` 完整包含 `v0.1.182`，源码版本与嵌入断言统一为 `0.1.183`。
- 上游新增 Responses 自定义工具 item ID 恢复、邮箱 alias 并发绑定保护、Composite Kimi K3 路由、Antigravity 64000 token 上限与模型映射、Channel Monitor composite 聚合、Anthropic cache TTL 计费、OpenAI OAuth 429 配额快停和 Responses Lite 兼容修复；本轮没有依赖锁、数据库迁移、Docker、deploy 或 workflow 变更。
- WS v2 冲突采用语义并集：首帧与后续帧都保留 Responses Lite account-aware normalization、compatibility、reserved-tool alias、account identity 和 fast policy，同时在转发前执行显式图片意图/分组权限门控；第二轮图片意图仍不能绕过权限。AI Chat、AI Images、Canvas、默认 CNY/动态币种、Prompt Audit、Strict Step-up、原子设置、安全 shutdown、partial/disconnect exactly-once 计费和自定义 GHCR 工作流均保留。
- merge commit `dc2c7faf32e58944d73f8cd60d412f0e5647e019` 的 CI run `32925340186`、Security Scan run `32925340185` 和 Custom Docker Image run `32925340195` 全绿。稳定、短 SHA、完整 SHA 标签均固定 OCI index `sha256:aec0f3df60b301ffb412863684f0e5a3536aeba919d6721606d65f776441e77a`，包含 revision 已核验为完整 merge SHA 的 linux/amd64 child `sha256:42a0f8af33c25732e51738730ffd21fb97388659a9e96a2b8e0b2aa0107a6a44` 与 linux/arm64 child `sha256:ec5d82bee7490ad2f74550951c882f120d4cc06d18ff3f34258e55f7b6830eef`。
- OVH `/data/sub2api/docker-compose.target.json` 仍固定 v0.1.179 完整 SHA 标签与 digest，`pull_policy: never`；应用、PostgreSQL、Redis 均 healthy。生产升级由用户手动执行：先生成 PostgreSQL dump，再显式 pull v0.1.183 完整 SHA+digest，并只重建 `sub2api` 服务。

## v0.1.184 合并架构边界（2026-08-31）
- 当前发布从 v0.1.183 发布记录提交 `c1a3a123380762f34d8e5e198d3bf6b4a882a5ca` 创建回滚分支 `backup/pre-v184-c1a3a1233` 和候选分支 `merge/v0.1.184-chat-image-tools`，合入官方 annotated tag `v0.1.184`（peeled commit `e98ef32eb29aecd30d1def615912ec4dc93173f3`）；最终 merge commit 为 `83b345b9c8e68dedda7eceac9811c1dbc7d358ec`，源码版本与嵌入断言统一为 `0.1.184`。
- 三处冲突均采用语义并集：Responses-to-Chat fallback 的三个结果同时保留本地 `ClientDisconnect` 和官方 `UpstreamResponseServiceTier`；WS v2 lifecycle 测试夹具同时保留禁用图片生成的 API key/group 与官方 hooks，并覆盖第二轮图片意图拒绝、cyber side-effect 和 UTF-8 close reason；在线更新服务继续保留 custom build lifecycle guard、标准 semver 比较与 `-zz` normalization，官方旧 `parseVersion` 不再恢复。
- 合并继续保留 AI Chat、AI Images、Canvas、默认 CNY/动态币种、Prompt Audit reload/save 线性化与 fail-closed 元数据、Strict Step-up、原子设置、安全 shutdown、partial/disconnect exactly-once 计费、图片权限门和 Grok identity-bound billing CAS。新增迁移 `231_add_usage_log_native_compaction_v2.sql`、`231_add_usage_log_requested_reasoning_effort.sql`、`231_user_restrict_public_groups.sql`；同为 `231` 前缀但迁移器按完整文件名与 checksum 跟踪，生产数据库必须具备 `ALTER` 权限，`usage_logs` 大表变更仍可能等待 `AccessExclusive` 锁并触及十分钟启动迁移超时。
- 生产观察重点包括 DeepSeek 新峰谷计费与未知模型 fallback、OpenAI/Anthropic/Bedrock terminal/transport failover 可能产生的额外重试账单、WebSocket 大首包桥接和 Codex catalog 重写、图片账号默认 30 分钟冷却，以及 OpenAI TTFT 默认从 visible 改为 semantic 后的面板与调度指标跳变。升级前必须生成可验证 PostgreSQL dump，并核对数据库锁等待、计费抽样和 failover 用量是否只记录一次。
- merge commit 的 CI run `33393872939`、Security Scan run `33393872961`、Custom Docker Image run `33393872877` 均成功。稳定、短 SHA、完整 SHA 标签均固定 OCI index `sha256:84d3ae8247ca0746db4fd82bfce385335105524a14e52a4ff6b531dccf7b5e99`，包含 linux/amd64 child `sha256:570164e5a0cee310c140e44c3ab1ca6f699d9e8cec443a859ed7a9e3ba932e0f` 与 linux/arm64 child `sha256:4944c5f39333efafdacb42b9da2b6f3c730a9222b6c43ddfe01256e0ce1c8a7d`；两边 `org.opencontainers.image.revision` 均核验为 `83b345b9c8e68dedda7eceac9811c1dbc7d358ec`。

## v0.1.185 合并架构边界（2026-09-01）
- 当前发布从 v0.1.184 发布记录提交 `87adb8d18afa1a72c2bd08abd6ee35a80b6658f9` 创建回滚分支 `backup/pre-v185-87adb8d18` 和候选分支 `merge/v0.1.185-chat-image-tools`，合入官方 annotated tag object `c8134f0f55b75719ac228b75a0861f2050b4e164`（peeled commit `2ac784c51a5d0925b324efef2ba6b3446c364781`）；最终 merge commit 为 `dd3e7845a444b2aec11b894634522b16f8b5f068`，版本与嵌入断言统一为 `0.1.185`。
- 三个 changed-in-both Go 文件自动合并为正确语义并集：handler 在 reasoning policy 后规范化严格限定的 Codex delegation bootstrap，本地映射后图片意图门仍先于转发；forward 只为 `UsesOpenAICodexProtocol()` 合成 instructions，本地两阶段图片门和 partial result 保持；WS 只改发给客户端的 capacity-shed 副本，replay、健康与 terminal 判定继续消费原始 payload，每 turn 图片权限门保持。
- 计费架构从 Gemini 平台特例改为目录驱动整单阶梯：22 个 Claude/Gemini/GPT 模型从 `*_above_*` 绝对价导出 200K/272K 阶梯，input/output/cache read/write 各自应用目录倍率；`pricing.override_file` 可按字段浅合并官方目录但默认空。生产需要关注远程 main 价格源漂移、200000/200001 与 272000/272001 边界、orphan/lopsided/dropped ladder 和 override warning。
- DeepSeek 账号成本统计通过 `PricingAt` 与 `NewModelPricingResolver(nil, billingService)` 进入唯一的 `CalculateCostUnified` 峰谷政策，不复制倍率逻辑；自定义账号成本规则和 `ApplyPricingToAccountStats` 仍提前返回。数据库启动重试严格限定在 `PingContext` 就绪探测，迁移只执行一次，避免连接中断后重放 `*_notx.sql` 并发索引；永久配置/认证/迁移错误继续 fail fast。
- 合并继续保留 AI Chat、AI Images、Canvas、默认 CNY/动态币种、Prompt Audit、Strict Step-up、原子设置、安全 shutdown、partial/disconnect exactly-once 计费、图片权限门、Grok identity-bound billing CAS 和 Custom Docker workflow。本轮无数据库 migration/schema 或依赖锁变更；官方 compose 的 PostgreSQL SQL healthcheck 与延长宽限不会自动进入 OVH 的固定 target JSON。
- CI run `33472496074`、Security Scan run `33472496081`、Custom Docker Image run `33472496070` 均成功。稳定、短 SHA、完整 SHA 标签固定 OCI index `sha256:24b1916d42a6f0e06ddbc01b0f895d0e537f4553f5ffe1e6ad907456115a0892`，包含 linux/amd64 child `sha256:afc91bca38b62ab6e33de8f8fdf8134c19a7e5013151ec8aeababf9f8129e304` 与 linux/arm64 child `sha256:8e04b0d3377f655a4c09927fcc24b57df21fc6445550545f563d198d348249f0`；两边 revision 均为完整 merge SHA。生产继续由用户手动 SSH 固定完整 SHA 标签和 OCI digest，仅重建 `sub2api` 服务。

## v0.2.0 合并架构边界（2026-09-02）
- 当前发布从 v0.1.185 发布记录提交 `d5011109c9d2d3e8ebb4f0c7dc973bba66f37339` 创建回滚分支 `backup/pre-v020-d5011109c` 和候选分支 `merge/v0.2.0-chat-image-tools`，合入官方 annotated tag object `dd07c4d8d484878e617c945cc8bacc304a5a6560`（peeled commit `aa236488351eb71e120fc2b6fb32e36b0374c918`）；最终 merge commit 为 `bed05d0635823e32d38ff23e7c753bd74964be38`，源码版本与嵌入断言统一为 `0.2.0`。
- 上游引入 OpenAI Fast 分组强制/免费策略、按模型 reasoning effort 映射与超限动作、Kimi 原生 Responses、Fable 5.1、无 call ID 自动化启动以及四个 schema 迁移。迁移仍按完整文件名/checksum 串行记录；四张 pricing 表增加 nullable `NUMERIC(20,12)` 1h cache-write 列，groups 增加 force/free boolean 与默认 `downgrade` 的 over-limit 字段。
- cache-write 分档保持三条计费不变量：缺省 5m 价继承原统一 cache-write 价、显式 5m 零价不被回退覆盖、priority TTL 档按最终 priority/standard cache-write 比例缩放；自定义 Fast/Flex multiplier 与长上下文倍率仍各自只乘一次。所有新增 pricing 指针在认证/调度快照 Clone 中隔离。
- WebSocket 的进行中 turn 同时由 pending start 与 active response ID 表示，任一存在时 clean EOF/1000 都不能伪装为 graceful terminal；reasoning policy 在模型映射前使用客户端 session model，后续无 model 帧和 `session.update` 模型切换不能绕过 exact/prefix/suffix 规则。Messages raw Chat fallback 与 Responses 路径共享 Fast policy，并以最终出站 tier 驱动 Free Fast 计费。
- 合并继续保留 AI Chat、AI Images、Canvas、默认 CNY/动态币种、Prompt Audit、Strict Step-up、原子设置、安全 shutdown、partial/disconnect exactly-once 计费、图片权限门、Grok identity-bound billing CAS、DeepSeek account-stats `PricingAt`、仅重试 `PingContext` 的数据库就绪流程和 Custom Docker workflow。
- CI run `33608930254`、Security Scan run `33608930210`、Custom Docker Image run `33608930292` 均成功。稳定、短 SHA、完整 SHA 标签固定 OCI index `sha256:19e58de459e5b4c1b8b9907cd2445d80907d0e98057075bc05e5bdfe45000844`，包含 linux/amd64 child `sha256:1d577a2f50fad34d6b5cba132e78b4c46e2325f2c842b98e0bf04a8d60c2f76a` 与 linux/arm64 child `sha256:8b2bf7e2b841eb54b245438241718585ba2b01bb2c2b608a32c8e292117c5a13`；两边 revision 均为完整 merge SHA。OVH 继续由用户手动固定完整 SHA+digest，升级前 dump PostgreSQL，只重建 `sub2api` 服务。

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
