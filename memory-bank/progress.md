# Sub2API 进度记录

## 已完成

### 部署
- [x] Docker Compose 部署到 VPS（Oracle Cloud ARM，IP: 149.118.145.171）
- [x] PostgreSQL + Redis 容器正常运行
- [x] 域名 `ai.zh-zh.top` 配置完成
- [x] 管理后台可正常访问
- [x] 2026-05-28 新增国内备案入口备用线路 `ai.zh-zh.cloud`，通过国内服务器 `118.25.1.151` 的 Nginx 反代到现有 `ai.zh-zh.top` 新加坡加速链路，并验证 `/v1/models` 可返回模型列表

### Kiro Gateway 接入（2026-05-10）
- [x] 已将 `jwadow/kiro-gateway` 部署到同一 VPS，容器名为 `kiro-gateway`
- [x] 已加入 `sub2api_sub2api-network` Docker 内网，未暴露公网端口
- [x] 已启用多账号模式并成功加载 `/opt/kiro-gateway/accounts/acc1.json`
- [x] 已确认 Sub2API Claude/Anthropic 分组可用 `http://kiro-gateway:8000` + `PROXY_API_KEY` 原始值接入，模型先填 `claude-opus-4.7`
- [x] 已将状态文件改为目录挂载 `/opt/kiro-gateway/state:/app/state`，解决单文件挂载导致状态保存失败的问题
- [x] 启动日志确认 `Loaded 1 account(s)`、`Successfully initialized account`、`Account system initialized successfully`，且不再出现 `Failed to save state`

### 源码
- [x] 2026-04-23 从 GitHub 克隆源码到本地 `E:\vis project\zz sub2api`
- [x] 2026-04-29 本地源码更新到 upstream `v0.1.120`，并创建 `feature/chat-image-tools` 分支继续开发
- [x] 2026-04-29 创建用户 fork：`https://github.com/masatoshiyokoyama635-sudo/sub2api`
- [x] 2026-04-29 推送自定义功能分支：`feature/chat-image-tools`

### AI 工具页面（2026-04-29）
- [x] 新增用户侧 AI 对话页面，可选择分组与该分组下的 active API Key 调用现有 `/v1/chat/completions` 网关
- [x] 新增用户侧 AI 生图页面，用户应选择 gpt-image 分组，可选择分组下 active API Key 调用现有 `/v1/images/generations` 网关
- [x] 左侧侧边栏新增 AI 对话 / AI 生图入口，并补充中英文国际化文案
- [x] 前端单元测试、类型检查、lint 和生产构建通过

### 用户教程文档（2026-05-28）
- [x] 基于原 `zh-ai 中转站说明文档.pdf` 的 12 页文本和当前源码事实，重写 zh-ai 用户使用说明
- [x] 新增 `docs/ZH_AI_USER_GUIDE_CN.md`，覆盖真实控制台 URL、网关接口、鉴权方式、网页 AI 对话和网页 AI 生图功能
- [x] 文档审查确认关键 URL、接口路径、鉴权方式、AI 对话/AI 生图说明与源码一致

### 专属客户端教程文档（2026-06-05）
- [x] 学习 `lzz.docs.buildwithfern.com` 的 API 概览、快速入门和图像生成文档结构，提取 OpenAI 兼容接入、聊天、生图、充值订阅相关写法
- [x] 新增 `docs/ZZ_AI_CLIENT_USER_GUIDE_CN.md`，只覆盖客户端接入、AI 对话、AI 生图、充值订阅、邀请返利教程
- [x] 文档按当前 zz AI 中转站实际实现改写：OpenAI 兼容 Base URL 为 `https://ai.zh-zh.top/v1`，网页 AI 对话为 `/ai/chat`，网页 AI 生图为 `/ai/images` 且使用 `/v1/images/generations`，充值订阅为 `/purchase`，邀请返利为 `/affiliate`
- [x] 2026-06-06 按用户反馈用本机 Chrome/CDP 重新读取 Fern 快速入门与客户端子页面，补全客户端接入章节：Cursor、Cline、Codex CLI、OpenCode、Qwen Code、Claude Code、Postman/cURL、OpenAI SDK

### 自定义 Docker 镜像流程（2026-04-29 / 2026-04-30 / 2026-05-05 / 2026-05-07）
- [x] 新增 `.github/workflows/custom-docker.yml`，用于 `feature/chat-image-tools` 分支自动构建 GHCR 镜像
- [x] 自定义镜像稳定标签规划为 `ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`
- [x] 工作流已推送到 fork，GitHub Actions run `25124118394` 已成功构建并推送 GHCR 镜像；该次镜像内部版本号来自旧 `VERSION` 文件，显示为 `0.1.119-zz`
- [x] 2026-04-29 已将 `backend/cmd/server/VERSION` 同步到 `0.1.120` 并推送 commit `e670122f`，触发重新构建正确版本镜像
- [x] 等待 `0.1.120-zz` GHCR 镜像构建完成并确认可拉取
- [x] VPS `docker-compose.yml` 中 `sub2api` 镜像从官方镜像切换到自定义 GHCR 镜像
- [x] 2026-04-29 用户已在原 VPS 测试自定义 Docker 镜像成功，确认服务可运行且可回滚官方镜像
- [x] 2026-04-30 官方发布 `v0.1.121` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图功能
- [x] 2026-04-30 已将 `backend/cmd/server/VERSION` 同步为 `0.1.121`，用于重新构建 `0.1.121-zz` 自定义 GHCR 镜像
- [x] 2026-04-30 GitHub Actions run `25165651723` 成功构建并推送自定义 GHCR 镜像，用户已在 VPS 更新部署成功
- [x] 2026-05-05 官方发布 `v0.1.123` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-05 已将 `backend/cmd/server/VERSION` 同步为 `0.1.123`，用于重新构建 `0.1.123-zz` 自定义 GHCR 镜像
- [x] 2026-05-05 GitHub Actions run `25377558141` 成功构建并推送自定义 GHCR 镜像
- [x] 2026-05-05 用户已在 VPS 通过 Docker Compose 拉取并更新自定义镜像成功
- [x] 2026-05-07 官方发布 `v0.1.125` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-07 已将 `backend/cmd/server/VERSION` 同步为 `0.1.125`，用于重新构建 `0.1.125-zz` 自定义 GHCR 镜像
- [x] 2026-05-07 GitHub Actions run `25500374871` 成功构建并推送 `0.1.125-zz` 自定义 GHCR 镜像
- [x] 2026-05-13 官方发布 `v0.1.126` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-13 已将 `backend/cmd/server/VERSION` 同步为 `0.1.126`，用于重新构建 `0.1.126-zz` 自定义 GHCR 镜像
- [x] 2026-05-13 GitHub Actions run `25840119082` 成功构建并推送 `0.1.126-zz` 自定义 GHCR 镜像
- [x] 2026-05-20 官方发布 `v0.1.129` 后，已在本地合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-20 已将 `backend/cmd/server/VERSION` 同步为 `0.1.129`，用于重新构建 `0.1.129-zz` 自定义 GHCR 镜像
- [x] 2026-05-20 前端验证通过：`typecheck`、目标单测 13 个、生产构建；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-05-20 GitHub Actions run `26167691555` 成功构建并推送 `0.1.129-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签 `chat-image-tools-33e9d34`）
- [x] 2026-05-24 官方发布 `v0.1.130` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-24 已将 `backend/cmd/server/VERSION` 同步为 `0.1.130`，用于重新构建 `0.1.130-zz` 自定义 GHCR 镜像
- [x] 2026-05-26 官方发布 `v0.1.131` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-26 已将 `backend/cmd/server/VERSION` 同步为 `0.1.131`，用于重新构建 `0.1.131-zz` 自定义 GHCR 镜像
- [x] 2026-05-26 GitHub Actions run `26443782001` 成功构建并推送 `0.1.131-zz` 自定义 GHCR 镜像
- [x] 2026-05-27 官方发布 `v0.1.132` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-27 已将 `backend/cmd/server/VERSION` 同步为 `0.1.132`，用于重新构建 `0.1.132-zz` 自定义 GHCR 镜像
- [x] 2026-05-28 GitHub Actions run `26549978144` 成功构建并推送 `0.1.132-zz` 自定义 GHCR 镜像
- [x] 2026-05-29 官方发布 `v0.1.133` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图与支付人民币符号补丁
- [x] 2026-05-29 已将 `backend/cmd/server/VERSION` 同步为 `0.1.133`，用于重新构建 `0.1.133-zz` 自定义 GHCR 镜像
- [x] 2026-06-06 官方发布 `v0.1.134` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图、支付人民币符号补丁和历史图片计费识别逻辑
- [x] 2026-06-06 已将 `backend/cmd/server/VERSION` 同步为 `0.1.134`，用于重新构建 `0.1.134-zz` 自定义 GHCR 镜像
- [x] 2026-06-06 本地前端验证通过：`npm --prefix frontend run typecheck`、`npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results.json`（结果 `707/707` 通过，JSON 摘要 `success: true`，但该 npm 命令返回码异常为 1）、`npm --prefix frontend run build`；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [ ] 2026-06-06 已准备通过 fork GitHub Actions 构建并推送 `0.1.134-zz` 自定义 GHCR 镜像，等待 Actions 结果确认
- [x] 2026-06-09 官方发布 `v0.1.135` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图、支付人民币符号补丁和历史图片计费识别逻辑
- [x] 2026-06-09 已将 `backend/cmd/server/VERSION` 同步为 `0.1.135`，用于重新构建 `0.1.135-zz` 自定义 GHCR 镜像
- [x] 2026-06-09 本地前端验证通过：`npm --prefix frontend run typecheck`、`npm --prefix frontend run build`；`npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-135.json` 生成 JSON 摘要 `success: true`、`718/718` 通过，但 npm 命令返回码异常为 1；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-06-09 GitHub Actions run `27190759099` 成功构建并推送 `0.1.135-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`）
- [x] 2026-06-10 官方发布 `v0.1.136` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图、支付人民币符号补丁、历史图片计费识别逻辑和专属客户端教程文档
- [x] 2026-06-10 已将 `backend/cmd/server/VERSION` 同步为 `0.1.136`，用于重新构建 `0.1.136-zz` 自定义 GHCR 镜像
- [x] 2026-06-10 本地前端验证通过：`npm --prefix frontend run typecheck`、`npm --prefix frontend run build`；`npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-136.json` 生成 JSON 摘要 `success: true`、`726/726` 通过，但 npm 命令返回码异常为 1；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-06-10 首次 GitHub Actions run `27275018452` 构建 `0.1.136-zz` 失败，原因为 Docker 前端构建阶段没有复制官方 v0.1.136 新增的 `docs/legal/admin-compliance.*.md`，导致 Vite raw import 解析失败
- [x] 2026-06-10 已补充 `.dockerignore` 和根 `Dockerfile`，仅放行并复制 `docs/legal/admin-compliance.zh.md`、`docs/legal/admin-compliance.en.md` 到 Docker 前端构建环境
- [x] 2026-06-10 GitHub Actions run `27275566706` 成功构建并推送 `0.1.136-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签 `chat-image-tools-c5c5c51`）

### 支付界面货币显示修复（2026-05-01）
- [x] 确认用户侧充值/订阅页面存在支付金额符号显示问题：自定义充值金额输入框和订阅套餐卡价格使用了 `$`
- [x] 已将充值自定义金额输入框与订阅套餐卡售价/原价改为人民币符号 `¥`
- [x] 保留 `daily_limit_usd` / `weekly_limit_usd` / `monthly_limit_usd` 等账户额度字段为 `$`，因为这些字段语义仍是 USD 配额
- [x] 新增前端单元测试覆盖支付金额人民币符号与 USD 配额符号边界
- [x] 相关单测和前端类型检查通过
- [x] 作为 fork 上的长期补丁保留，后续官方更新时直接合并 upstream 到 `feature/chat-image-tools`，仅在同块 UI 冲突时处理

### 安全加固（2026-04-24）
- [x] Redis 密码设置完成（密码已写入 /opt/sub2api/.env）
- [x] PostgreSQL 5432 端口通过 iptables 封锁（iptables-persistent 持久化）
- [x] Redis 6379 端口通过 iptables 封锁（iptables-persistent 持久化）
- [x] UFW 防火墙启用（allow 22/80/443, deny 5432/6379）
- [x] 管理员默认密码已修改
- [x] 安全审查完成，项目整体安全状况良好

### 备份配置（2026-04-24）
- [x] 腾讯云 COS 存储桶创建完成（sub2api-1400654985，ap-shanghai，私有读写）
- [x] 子用户创建 + 最小权限策略配置（cos:PutObject/GetObject/DeleteObject/HeadObject/HeadBucket/GetBucket/ListMultipartUploads）
- [x] S3 备份连接测试通过
- [x] 手动备份测试成功（下载 .sql.gz 文件可正常解压）

## 当前问题

（暂无）

## 部署/回滚记录

### 使用自定义镜像部署
VPS 上把 `/opt/sub2api/docker-compose.yml` 的 `sub2api` 镜像改为：

`ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`

然后执行：

`cd /opt/sub2api && docker compose pull sub2api && docker compose up -d sub2api && docker compose logs -f --tail=100 sub2api`

### 更新到官方 0.1.121 镜像
如果 VPS 仍是官方 `0.1.119`、`0.1.120` 或 `latest`，用下面单行命令固定到官方 `0.1.121`：

`cd /opt/sub2api && sed -i 's#weishaw/sub2api:0\.1\.120#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.119#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:latest#weishaw/sub2api:0.1.121#g' docker-compose.yml && docker compose pull sub2api && docker compose up -d sub2api && docker compose ps`

### 一键恢复官方 Docker 镜像
如果自定义镜像部署后出问题，可把镜像改回官方版本并重启容器。当前推荐先回滚到官方 `0.1.121`：

`cd /opt/sub2api && sed -i 's#ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.120#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:0\.1\.119#weishaw/sub2api:0.1.121#g; s#weishaw/sub2api:latest#weishaw/sub2api:0.1.121#g' docker-compose.yml && docker compose pull sub2api && docker compose up -d sub2api && docker compose ps`

如果 compose 里不是这个自定义镜像字符串，可手动确认 `image:` 行后再替换。回滚只换应用镜像，不改 PostgreSQL、Redis、卷和 `.env`，所以数据应继续保留。

## 待办
- [ ] 配置定时备份（建议 cron: `0 3 * * *`，每天凌晨 3 点）
- [ ] 配置 trusted_proxies（Cloudflare 代理 IP 范围），使限速功能准确
- [ ] 配置 CORS allowed_origins（当前跨域请求被拒绝，可能有警告但不影响使用）
- [ ] 配置 JWT_SECRET 为固定值（避免容器重启后 session 失效）
- [ ] 配置 TOTP_ENCRYPTION_KEY 为固定值（避免容器重启后 2FA 配置失效）
- [ ] 根据需要配置自定义定价策略
