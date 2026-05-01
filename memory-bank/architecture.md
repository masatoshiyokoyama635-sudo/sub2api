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

### 用户侧 AI 工具页
- 前端新增 AI 对话与 AI 生图两个用户页面，入口位于左侧侧边栏
- 页面复用用户可用分组与 API Key 数据，先选分组，再选择该分组下 active API Key
- AI 对话直接使用选中的真实 API Key 调用现有 `POST /v1/chat/completions`
- AI 生图仅展示 OpenAI 平台分组，直接调用现有 `POST /v1/images/generations`
- 不新增 JWT 后端代理接口，继续复用现有网关的 API Key 鉴权、分组路由、账号调度、计费、限额和用量日志

## 部署架构
```
用户请求 → ai.zh-zh.top (Cloudflare) → VPS(Oracle Cloud ARM, 149.118.145.171)
  → Sub2API容器(8080) → PostgreSQL容器(5432, 仅内部网络)
                      ↘ Redis容器(6379, 仅内部网络, 有密码)
```

### 部署信息
- **服务器**：Oracle Cloud ARM
- **域名**：`ai.zh-zh.top`
- **Docker 镜像**：`ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`（自定义 AI 工具版；当前自定义构建版本基于官方 `v0.1.121`，镜像内版本显示为 `0.1.121-zz`；可回滚官方 `weishaw/sub2api:0.1.121`）
- **部署路径**：`/opt/sub2api/`
- **配置文件**：`/opt/sub2api/.env`

### 自定义 Docker 部署与回滚
- 用户 fork：`https://github.com/masatoshiyokoyama635-sudo/sub2api`
- 长期自定义功能分支：`feature/chat-image-tools`
- GitHub Actions workflow：`.github/workflows/custom-docker.yml`
- 自定义 GHCR 镜像稳定标签：`ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`
- VPS 部署方式仍使用 Docker Compose，只替换 `sub2api` 服务的 `image:`，PostgreSQL、Redis、卷和 `.env` 不需要因自定义镜像而大改
- 2026-04-29 用户已在原 VPS 测试自定义 GHCR 镜像成功，当前自定义镜像可作为正式部署镜像使用
- 回滚官方镜像时只需把 `image:` 改回 `weishaw/sub2api:0.1.121` 或官方最新稳定标签，然后执行 `docker compose pull sub2api && docker compose up -d sub2api`
- 官方 0.1.121 固定命令：`cd /opt/sub2api && sed -i 's#weishaw/sub2api:0\.1\.120#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.119#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:latest#weishaw/sub2api:0.1.121#g' docker-compose.yml && docker compose pull sub2api && docker compose up -d sub2api && docker compose ps`
- 自定义镜像回滚官方 0.1.121 命令：`cd /opt/sub2api && sed -i 's#ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.120#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.119#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:latest#weishaw/sub2api:0.1.121#g' docker-compose.yml && docker compose pull sub2api && docker compose up -d sub2api && docker compose ps`
- 2026-04-30 已将官方 `v0.1.121` 合并到自定义分支，保留 AI 对话/AI 生图功能，并把 `backend/cmd/server/VERSION` 同步为 `0.1.121` 以构建 `0.1.121-zz` 自定义镜像
- 2026-04-30 GitHub Actions run `25165651723` 成功构建并推送自定义镜像，用户已在 VPS 通过 Docker Compose 更新部署成功

### 备份存储
- **对象存储**：腾讯云 COS（ap-shanghai）
- **存储桶**：`sub2api-1400654985`（私有读写）
- **访问方式**：子用户 + 最小权限策略

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
