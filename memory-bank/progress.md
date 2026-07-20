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

### Canvas 入口适配（2026-07-04）
- [x] 按用户确认，将左侧侧边栏原“AI 生图”入口替换为“无限画布”入口，新标签页打开 `https://canvas.zh-zh.top`
- [x] 第一阶段只做纯外链跳转，不传 API Key、Base URL、用户 token、user_id 或任何 query/hash 参数；旧 `/ai/images` 路由和页面暂保留但不再作为侧边栏入口
- [x] 已补充 `nav.infiniteCanvas` 中英文文案，并新增 AppSidebar 目标测试覆盖固定 Canvas URL、新标签页、安全 rel/referrerpolicy 和不再使用 `nav.aiImages` 作为侧边栏图片入口
- [x] 验证通过：`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run test:run -- src/components/layout/__tests__/AppSidebar.spec.ts`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run typecheck`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run lint:check`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run build`

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
- [x] 2026-06-16 官方发布 `v0.1.137` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图、支付人民币符号补丁、历史图片计费识别逻辑和专属客户端教程文档；合并冲突仅出现在 `.dockerignore`，已保留官方 `docs/legal/*.md` 放行规则
- [x] 2026-06-16 已将 `backend/cmd/server/VERSION` 同步为 `0.1.137`，用于重新构建 `0.1.137-zz` 自定义 GHCR 镜像
- [x] 2026-06-16 本地前端验证通过：`npm --prefix frontend run typecheck`、`npm --prefix frontend run build`；`npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-137.json` 生成 JSON 摘要 `success: true`、`727/727` 通过，但 npm 命令返回码异常为 1；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-06-16 GitHub Actions run `27637619001` 成功构建并推送 `0.1.137-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签 `chat-image-tools-3adefcb`，manifest digest `sha256:963cad57d79c8b712a738bcb82d291d09682e6b4632a3e75d7528c519105b5db`）
- [x] 2026-06-22 官方发布 `v0.1.138` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话/AI 生图、支付人民币符号补丁、历史图片计费识别逻辑和专属客户端教程文档；本次合并无冲突
- [x] 2026-06-22 已将 `backend/cmd/server/VERSION` 同步为 `0.1.138`，用于重新构建 `0.1.138-zz` 自定义 GHCR 镜像
- [x] 2026-06-22 本地前端验证通过：`npm --prefix frontend run typecheck`、`npm --prefix frontend run build`；`npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-138.json` 生成 JSON 摘要 `success: true`、`730/730` 通过，但 npm 命令返回码异常为 1；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-06-22 GitHub Actions run `27957305480` 成功构建并推送 `0.1.138-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签 `chat-image-tools-edd2425`，manifest digest `sha256:4fa76c3010ce58e39a02dd145518156b9e4f65668e8c69b9110a6225c3e2131c`）
- [x] 2026-06-27 官方发布 `v0.1.139` 后，已合并到自定义 `feature/chat-image-tools` 分支；按用户确认，长期二改仅保留 AI 对话、AI 生图和支付人民币符号补丁，图片计费相关逻辑跟随官方；合并冲突出现在 `frontend/src/components/admin/usage/UsageTable.vue`、`frontend/src/utils/billingMode.ts`、`frontend/src/views/user/UsageView.vue`，均采用官方版本
- [x] 2026-06-27 已将 `backend/cmd/server/VERSION` 同步为 `0.1.139`，用于重新构建 `0.1.139-zz` 自定义 GHCR 镜像
- [x] 2026-06-27 本地前端验证通过：`npm --prefix frontend run typecheck`、目标失败测试 `20/20` 通过、完整 `npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-139-fixed.json` 生成 JSON 摘要 `success: true`、`746/746` 通过、`npm --prefix frontend run build` 通过；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-06-27 GitHub Actions run `28276433805` 成功构建并推送 `0.1.139-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签 `chat-image-tools-338f326`，manifest digest `sha256:a83294cf99aae8147745440224125f5866a93f72e956c7dec892120f9e14ffed`）
- [x] 2026-06-30 官方发布 `v0.1.141` 后，已合并到自定义 `feature/chat-image-tools` 分支；本次合并冲突出现在 `backend/cmd/server/VERSION`、`backend/internal/service/payment_order.go`、`backend/internal/service/payment_order_result_test.go`，其中支付订单逻辑和测试采用官方 `v0.1.141` 版本，保留 AI 对话、AI 生图和支付人民币符号补丁
- [x] 2026-06-30 已确认官方 `v0.1.141` 已修复订阅订单支付金额错误套用 `BalanceRechargeMultiplier` 的问题：订阅订单按套餐售价加手续费计算支付金额，充值倍率仅影响余额充值入账额度
- [x] 2026-06-30 已将 `backend/cmd/server/VERSION` 同步为 `0.1.141`，用于重新构建 `0.1.141-zz` 自定义 GHCR 镜像
- [x] 2026-06-30 本地前端验证通过：`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run typecheck`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-141.json`（JSON 摘要 `success: true`、`758/758` 通过）、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run build`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run lint:check`；`git diff --check` 和精确冲突标记检查通过；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-06-30 已提交并推送 merge commit `3097596a`（`chore: merge upstream v0.1.141`）到 fork 的 `feature/chat-image-tools` 分支；GitHub Actions run `28452856490` 成功构建并推送 `0.1.141-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签预计为 `chat-image-tools-3097596`）
- [x] 2026-07-01 官方发布 `v0.1.142` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话、AI 生图和支付人民币符号补丁；本次合并无冲突
- [x] 2026-07-01 已将 `backend/cmd/server/VERSION` 同步为 `0.1.142`，用于重新构建 `0.1.142-zz` 自定义 GHCR 镜像；同步修正 `BulkEditAccountModal` 测试中 Antigravity 图片映射预设的上游文案变更（`passthrough`）
- [x] 2026-07-01 本地前端验证通过：`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run typecheck`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-142-fixed.json`（JSON 摘要 `success: true`、`770/770` 通过）、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run build`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run lint:check`；`git diff --check` 和精确冲突标记检查通过；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`），本地 Docker 构建未执行（本机 PATH 中没有 `docker`）
- [x] 2026-07-01 已提交并推送 commit `4a199c82`（`chore: merge upstream v0.1.142`）到 fork 的 `feature/chat-image-tools` 分支；GitHub Actions run `28523495316` 成功构建并推送 `0.1.142-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签预计为 `chat-image-tools-4a199c8`）
- [x] 2026-07-02 官方发布 `v0.1.143` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话、AI 生图和支付人民币符号补丁；本次合并无冲突
- [x] 2026-07-02 已将 `backend/cmd/server/VERSION` 同步为 `0.1.143`，用于重新构建 `0.1.143-zz` 自定义 GHCR 镜像；上游新增 `SubscriptionPlanCard` 依赖 Pinia 后，同步修复 `currencyDisplay.spec.ts` 的测试挂载环境
- [x] 2026-07-02 本地前端验证通过：`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run typecheck`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run lint:check`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-143-fixed.json`（JSON 摘要 `success: true`、`813/813` 通过）、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run build`；`git diff --check` 通过；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`），本地 Docker 构建未执行（本机 PATH 中没有 `docker`）
- [x] 2026-07-02 已提交并推送 commit `11e0b2a8`（`chore: merge upstream v0.1.143`）到 fork 的 `feature/chat-image-tools` 分支；GitHub Actions run `28599090502` 成功构建并推送 `0.1.143-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签 `chat-image-tools-11e0b2a`，manifest digest `sha256:155fdb66a8d5cfe5517d50f6694e8ee330e7fc0565d57048ce418f8166433d22`）
- [x] 2026-07-04 官方发布 `v0.1.144` 后，已合并到自定义 `feature/chat-image-tools` 分支并保留 AI 对话、AI 生图和支付人民币符号补丁；本次合并无冲突
- [x] 2026-07-04 已将 `backend/cmd/server/VERSION` 同步为 `0.1.144`，用于重新构建 `0.1.144-zz` 自定义 GHCR 镜像
- [x] 2026-07-04 本地前端验证通过：`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run typecheck`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run lint:check`、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run test:run -- --reporter=json --outputFile=vitest-results-144.json`（JSON 摘要 `success: true`、`820/820` 通过）、`npm_config_cache=E:/claude-cache/npm npm --prefix frontend run build`；`git diff --check` 和精确冲突标记检查通过；后端 `go test ./...` 未执行（本机 PATH 中没有 `go`）
- [x] 2026-07-04 已提交并推送 commit `0d5821be`（`chore: merge upstream v0.1.144`）到 fork 的 `feature/chat-image-tools` 分支；GitHub Actions run `28699907706` 成功构建并推送 `0.1.144-zz` 自定义 GHCR 镜像（稳定标签 `chat-image-tools`，短 SHA 标签 `chat-image-tools-0d5821b`，manifest digest `sha256:3473d4360628d050cbd5afc4224d3ca87530bb5e1bbc8762c0e0cf521005a581`）；同提交的 CI 与 Security Scan 也成功

### 支付界面货币显示修复（2026-05-01）
- [x] 确认用户侧充值/订阅页面存在支付金额符号显示问题：自定义充值金额输入框和订阅套餐卡价格使用了 `$`
- [x] 已将充值自定义金额输入框与订阅套餐卡售价/原价改为人民币符号 `¥`
- [x] 保留 `daily_limit_usd` / `weekly_limit_usd` / `monthly_limit_usd` 等账户额度字段为 `$`，因为这些字段语义仍是 USD 配额
- [x] 新增前端单元测试覆盖支付金额人民币符号与 USD 配额符号边界
- [x] 相关单测和前端类型检查通过
- [x] 作为 fork 上的长期补丁保留，后续官方更新时直接合并 upstream 到 `feature/chat-image-tools`，仅在同块 UI 冲突时处理

### 订阅支付金额修复（2026-06-28）
- [x] 确认后端订阅订单支付金额错误套用了 `BalanceRechargeMultiplier`，导致 200 元订阅在 1:3 充值倍率下被折算成约 66.67 元
- [x] 已修复创建支付订单金额计算：充值倍率只影响余额充值入账额度，不再影响订阅订单提交给支付宝/微信等支付网关的金额
- [x] 新增后端回归测试覆盖：200 元订阅在 1:3 充值倍率下仍支付 200 元，订阅手续费按 200 元基础计算，余额充值仍按现金金额支付并按倍率入账
- [x] 已处理 code-reviewer 发现的未使用 `calculateGatewayPaymentAmount` 私有函数残留风险
- [x] 本机验证 `git diff --check` 通过；Go 测试未能在本机执行，因为当前环境 PATH 中没有 `go`/`gofmt`

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

## v0.1.161 自定义候选（2026-07-18，未发布）
- [x] 在独立工作区 `E:/claude-cache/sub2api-v161-candidate` 基于自定义 `0c77db2b4` 创建候选分支 `merge/v0.1.161-chat-image-tools`，核验官方 annotated tag `v0.1.161`（peeled commit `19149ca196ee`），未触碰原 v0.1.160 冻结 merge 工作树。
- [x] 合入官方 v0.1.161 并解决 VERSION 冲突为 `0.1.161`；保留 AI Chat、AI Images、Canvas 外链、RMB/动态多币种支付补丁和 custom Docker `BUILD_TYPE`。
- [x] 前端 frozen install、全量 Vitest `1241/1241`、typecheck、lint、production build 通过；Go 1.26.5 Windows 工具链可用，候选全包编译、go vet、定向测试和全包测试已运行，当前未做 Linux race/CI。
- [x] 复审发现并修复 Prompt Audit 敏感路由未挂 step-up 的高风险缺陷：新增不受 `step_up_enabled` 开关影响的 `StrictStepUpAuthMiddleware`，配置、节点探测、完整事件详情和删除接口均强制 JWT+TOTP step-up；同步补充路由回归测试。
- [x] 进一步修复 step-up grant 的旧 JWT 边界：grant 创建和消费均要求 session ID，无 `sid` 的 JWT 返回 `STEP_UP_SESSION_REQUIRED`；相关 middleware、TOTP handler 定向测试通过。
- [ ] 当前仍未提交 merge、未 push、未构建 GHCR、未部署生产；生产继续运行 `0.1.159-zz`。候选仍需安全复审，特别是 Prompt Audit full_prompt/Redis 明文保留、出站节点 SSRF、step-up 设置读取错误 fail-open、Gemini query key 兼容，以及 Linux Go 1.26.5 race/CI。
- [x] 2026-07-18 后续已移植 Prompt Audit 配置单调安装、payload/Redis 上限、worker 配置漂移停止、terminal job 清理、严格 step-up 前端 UX、S3 step-up 取消回归，以及 OpenAI 被动 `image_gen` HTTP/WS 显式意图修复；对应定向 Go/前端测试、Go 全包编译、核心 vet、前端 typecheck/lint/build 已通过。
- [ ] 用户明确本轮不使用 Grok，Grok probe/CAS/媒体资格相关审查问题暂不处理，作为已知风险记录；不得将 Grok 相关失败误报为已修复。
- [ ] 新窗口从 `E:/claude-cache/sub2api-v161-candidate` 继续，只修非 Grok 阻塞：Prompt Audit Start/Shutdown 生命周期竞态、Shutdown 超时后的依赖关闭错误传播、worker lease heartbeat、enqueue bounded cleanup，以及普通 settings 对 `risk_control_enabled` 的严格 step-up 绕过。完成后再执行最终验证；继续禁止 commit、push、镜像与部署。
- [x] 2026-07-19 完成上述非 Grok 阻塞的 TDD RED 阶段：仅扩展 Go 测试/fake，覆盖 PromptService/ConfigManager/Runner Start-vs-Shutdown 串行与 terminal shutdown、长 scanner 独立 lease heartbeat、caller context 取消后的有界 enqueue 双补偿与错误可观察性、`risk_control_enabled` true→false 严格 session-bound step-up、Prompt Audit cleanup 错误/timeout 阻止 Redis/Ent close 并向调用方传播。Go 1.26.5 定向测试均真实运行并按预期失败；未改生产代码、未改 Grok 文件、未 commit/push/构建镜像/部署。
- [x] 2026-07-19 已实现普通 settings 的 `risk_control_enabled` true→false 严格 session-bound step-up：在 handler 内与 `step_up_enabled` 降级合并为一次 `EnforceStepUpAlways`，不把整个路由挂 strict；Admin API Key、无 `sid`、无 grant 均在 settings 写入前拒绝，字段省略/开启/同值语义不变。E 盘 Go 1.26.5 定向 handler 测试通过；未改 Prompt Audit/Grok 文件，未 commit/push/构建/部署。
- [x] 2026-07-19 完成 PromptService、ConfigManager、Runner 生命周期 GREEN：Start/Shutdown 使用单次运行状态机串行，Shutdown timeout 后后台继续 drain、禁止新 Start，后续 Shutdown 等待同一 completion；Starting 阶段 Shutdown 会先 cancel 启动 context，再等待 startup 完成后安全 drain，避免依赖 Start/Shutdown 并发和死锁；ConfigManager 初始 load error 与 PromptService config error 保持可恢复 degraded-running。E 盘 Go 1.26.5 生命周期定向测试通过（`ok`，0.466s）；未改 heartbeat/enqueue/wire cleanup/Grok 文件，未 commit/push/构建/部署。
- [x] 2026-07-19 完成非 Grok 阻塞修复：processing job 增加可注入周期的 lease heartbeat，失败取消 scanner 且不写终态；enqueue Set/Publish 失败使用 `WithoutCancel` + bounded cleanup，Delete/Mark 错误可聚合观测；普通 settings 风险控制关闭执行 Strict JWT+TOTP；应用 cleanup 传播错误并在 Prompt Audit 未安全停止时阻止 Redis/Ent 关闭。Go 1.26.5 全包编译、全包 `go test`（未设置 `OPENAI_API_KEY` 以跳过外部 API 对比测试）、`go vet`、定向安全测试通过；全包测试仅在显式设置外部 OpenAI API key 时失败，原因为外部 API 不可达/超时。前端 pnpm 9.15.9 全量 Vitest `1243/1243`、typecheck、lint、production build 通过；pnpm audit exceptions 检查通过。Windows 无 C 编译器，`go test -race` 未执行；govulncheck 不可用；未 commit/push/构建镜像/部署。
- [x] 2026-07-19 用户确认候选部署于不公开的私人服务器，并接受 Prompt Audit Redis 扫描正文与 `full_prompt` 持久化边界；该项不再作为候选发布阻塞。下一窗口仅继续 Linux Go 1.26.5 race/CI、剩余并发/安全复核、dirty 工作树核对，以及用户后续明确授权后的 commit/push/镜像/部署。

## v0.1.161 非 Grok 阻塞修复（2026-07-19，未发布）
- [x] HTTP shutdown 现在跟踪 serve loop、普通 handler 和 hijacked/WebSocket 连接；Shutdown 超时后先 Close 并等待，无法确认安全停止时跳过应用/Redis/Ent cleanup；partial-init cleanup 不再忽略 Prompt Audit Start 错误。
- [x] Settings 管理更新改为主 settings、auth-source defaults、OpenAI fast policy、payment 的单事务原子写入；PostgreSQL 使用 advisory transaction lock，事务内重读安全开关并检测 baseline conflict；安全降级必须有 strict authorization，无授权服务层调用也 fail-closed。
- [x] Prompt Audit heartbeat 正常 quiesce 与 failure cancel 分离，终态写入前 join heartbeat；heartbeat 丢失或 scanner panic 组合不再由外层 recover 写旧 owner 终态；PublishQueued ambiguous outcome 先查 job 状态，未确认 failed 不删 payload。
- [x] Prompt Audit storage config 拒绝未知字段、未知 scanner、trailing JSON 和非法 risk-control 值；风险控制状态未知/配置不可信时 fail-closed；PromptService/Runner/ConfigManager stopped 状态保留 shutdown error。
- [x] 验证：Windows Go 1.26.5 全包 `go test ./...`、全包 `go vet ./...`、目标 Go 包测试通过；前端 Corepack pnpm 10.33.2 typecheck、lint、全量 Vitest `180/180` 文件 `1243/1243` 测试、production build、pnpm audit exceptions 通过；Linux WSL Go 1.26.5 Prompt Audit race 通过，HTTP/Settings 目标包 race 受 E 盘空间耗尽影响未完整完成。
- [x] 最终只读安全复审确认并修复两项 ConfigManager HIGH：`Save` 对 risk-control 使用严格解析且读取失败保持 unknown/untrusted，过期成功 Reload 不再覆盖最新失败 Reload 的 fail-closed 元数据；新增并发回归测试。修复后 Windows Go 1.26.5 全包 `go test ./...`、全包 `go vet ./...` 和 Linux Prompt Audit race 重新通过，复审确认没有新的 CRITICAL/HIGH/发布阻塞 MEDIUM。Settings fallback 仍是条件性 TOCTOU 风险，标准 PostgreSQL wire 路径使用原子实现。
- [x] 2026-07-20 已创建并推送 merge commit `3540f755aa7cbf43c13cf0889e684445e32f62ab` 到 fork `feature/chat-image-tools`；Security Scan run `29711926123` 成功，Custom Docker Image run `29711926162` 成功，镜像 digest 为 `sha256:027692a1322c80b83a34f27748af199c041504cbb07f9fc97dd3bff81d55668a`，标签为 `chat-image-tools`、`chat-image-tools-3540f75` 和完整 SHA 标签。
- [ ] CI run `29711926110` 的 frontend、shell 通过，但 unit tests 因用户明确排除的既有 Grok 测试 `TestGrokOAuthHandlerQueryQuotaProbesUpstream` 失败；golangci-lint 另报 7 个可修复 lint 问题，已在本地修复但尚未形成第二个 commit/push。当前不应把 CI 说成全绿。

## 当前问题

- lint 修复尚未 commit/push；下一窗口应先检查并暂存这些本地修复，再本地运行 golangci-lint（若可用）或按 CI 报告逐项验证，然后决定是否创建第二个修复 commit。
- CI 的 Grok unit failure 按用户要求保留为已知风险，不修改 Grok 逻辑；如需要 CI 全绿，必须另行取得用户授权处理 Grok。
- Linux 目标包 race 已在有足够空间后通过主要目标包；全包 race 的既有 `internal/service` Gin 全局 `SetMode`/测试桩竞争仍未归因于本轮改动。
- `govulncheck` 本地不可用，但 GitHub Security Scan 已通过。
- Grok probe/CAS/媒体资格风险按用户要求保留，不在本轮处理。


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
