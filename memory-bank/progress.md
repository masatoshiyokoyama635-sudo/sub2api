# Sub2API 进度记录

## 已完成

### 部署
- [x] Docker Compose 部署到 VPS（Oracle Cloud ARM，IP: 149.118.145.171）
- [x] PostgreSQL + Redis 容器正常运行
- [x] 域名 `ai.zh-zh.top` 配置完成
- [x] 管理后台可正常访问

### 源码
- [x] 2026-04-23 从 GitHub 克隆源码到本地 `E:\vis project\zz sub2api`
- [x] 2026-04-29 本地源码更新到 upstream `v0.1.120`，并创建 `feature/chat-image-tools` 分支继续开发
- [x] 2026-04-29 创建用户 fork：`https://github.com/masatoshiyokoyama635-sudo/sub2api`
- [x] 2026-04-29 推送自定义功能分支：`feature/chat-image-tools`

### AI 工具页面（2026-04-29）
- [x] 新增用户侧 AI 对话页面，可选择分组与该分组下的 active API Key 调用现有 `/v1/chat/completions` 网关
- [x] 新增用户侧 AI 生图页面，仅展示 OpenAI 分组，可选择分组下 active API Key 调用现有 `/v1/images/generations` 网关
- [x] 左侧侧边栏新增 AI 对话 / AI 生图入口，并补充中英文国际化文案
- [x] 前端单元测试、类型检查、lint 和生产构建通过

### 自定义 Docker 镜像流程（2026-04-29 / 2026-04-30）
- [x] 新增 `.github/workflows/custom-docker.yml`，用于 `feature/chat-image-tools` 分支自动构建 GHCR 镜像
- [x] 自定义镜像稳定标签规划为 `ghcr.io/masatoshiyokoyama635-sudo/sub2api:chat-image-tools`
- [x] 工作流已推送到 fork，GitHub Actions run `25124118394` 已成功构建并推送 GHCR 镜像；该次镜像内部版本号来自旧 `VERSION` 文件，显示为 `0.1.119-zz`
- [x] 2026-04-29 已将 `backend/cmd/server/VERSION` 同步到 `0.1.120` 并推送 commit `e670122f`，触发重新构建正确版本镜像
- [x] 等待 `0.1.120-zz` GHCR 镜像构建完成并确认可拉取
- [x] VPS `docker-compose.yml` 中 `sub2api` 镜像从官方镜像切换到自定义 GHCR 镜像
- [x] 2026-04-29 用户已在原 VPS 测试自定义 Docker 镜像成功，确认服务可运行且可回滚官方镜像
- [x] 2026-04-30 官方发布 `v0.1.121` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图功能
- [x] 2026-04-30 已将 `backend/cmd/server/VERSION` 同步为 `0.1.121`，用于重新构建 `0.1.121-zz` 自定义 GHCR 镜像

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
