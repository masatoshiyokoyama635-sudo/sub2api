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

## v0.1.162 自定义候选（2026-07-20，未提交）
- [x] 从已发布自定义 v0.1.161 提交 `8ffbe61a74172efc90754570aa0f7afe4896c013` 创建独立工作区 `E:/claude-cache/sub2api-v162-candidate` 和分支 `merge/v0.1.162-chat-image-tools`；精确核验官方 annotated tag object `34b7a5ad70b4b9b9bb96955562fe632ad625d783`、peeled commit `27f094e0960ebd8e52de7ff7e763c6fec2ff4057`，根目录 v0.1.160 冻结 merge 现场未触碰。
- [x] 完成 `--no-commit` 三方融合并把 `backend/cmd/server/VERSION` 更新为 `0.1.162`；保留 AI Chat、旧 AI Images、Canvas 安全外链、默认 CNY/动态币种、自定义 Docker 与 v0.1.161 安全加固。
- [x] 修复融合后非 Grok 阻塞：Prompt Audit worker 在首次 dispatch/每个 chunk 前验证 expected/active/load error/mode/group，配置提交后激活失败立即推进 expected version 并标记旧 snapshot untrusted；异步图片下载增加 socket/redirect SSRF 防护；自定义 updater/rollback 严格验证 official stable release provenance；Docker 默认 `BUILD_TYPE=source`；旧套餐空/缺失币种在用户端和管理员端统一保持 CNY；更新/回滚浏览器 timeout 为后端 deadline 加一分钟余量；补齐 Image Storage 前端保存/step-up/连接测试。
- [x] 自定义 GHCR workflow 改为仓库级串行 concurrency，并在发布前同时保护完整 SHA 和短 SHA 标签；仍明确生产发布必须记录/固定 manifest digest，短 SHA 不作为唯一 provenance 锚点。
- [x] Windows Go 1.26.5 验证：标准 `go test ./...`、全包编译 `go test -run '^$' ./...`、`go vet -tags unit ./...` 通过；`go test -p 1 -tags unit ./... -skip '^TestGrokOAuthHandlerQueryQuotaProbesUpstream$'` 全包通过。完整 unit 唯一失败为 Grok 专属测试桩未实现候选 `GrokBillingSnapshotCAS`，导致响应缺 `billing`；该问题来自 v0.1.161 自定义父线，官方 v0.1.162 同名测试与官方实现自洽，不得误报为官方/flaky 或 unit 全绿。生产 account repository 已实现 CAS，且用户当前不使用 Grok，因此按既定范围保留为已知 Grok-only CI failure。
- [x] Linux WSL Go 1.26.5 `-race` 定向通过：Prompt Audit 配置提交激活失败/worker trust、图片 direct/redirect SSRF、custom updater 保护。
- [x] 前端最终验证通过：Vitest `483/483` suites、`1282/1282` tests，typecheck、ESLint、production build；目标融合回归 `44/44` 通过。pnpm audit exceptions、workflow YAML、Linux installer GitHub token 测试、`git diff --cached --check`、冲突标记/临时 artifact/密钥模式检查通过。
- [x] 2026-07-20 已创建本地 merge commit `b480d880c252ae76f6610452545eeba6cefff25b`（`chore: merge upstream v0.1.162`），父提交为自定义 v0.1.161 `8ffbe61a74172efc90754570aa0f7afe4896c013` 与官方 v0.1.162 peeled commit `27f094e0960ebd8e52de7ff7e763c6fec2ff4057`；二进制版本为 `0.1.162`。
- [x] 2026-07-20 已将该提交推送到 fork 的 `feature/chat-image-tools`，触发 Custom Docker Image run `29764586330`；test job（后端编译/回归、前端 typecheck/lint/Vitest/build）和 build job 均成功。GHCR 多架构镜像已发布，manifest digest 为 `sha256:6a494d2ad820d713b67b630c03dc97d90997b47829b370fbdc7f917330e4ea86`，标签为 `chat-image-tools`、`chat-image-tools-b480d88` 和完整 SHA 标签。尚未部署生产。
- [x] 2026-07-20 继续终审后确认初版 post-commit fence 仍有两个 HIGH 并发窗口：旧 Reload 可在 sequence 检查后、Save 提交栅栏前安装 N-1，或在成功 Save 后回写过期失败状态。现已把 Reload sequence 分配、成功/失败终态写入，以及 Save 的 commit+fence 发布统一纳入 `installMu` 线性化协议，并区分 locked/unlocked helper 防止自锁；新增旧 Reload 成功/失败两种跨 Save 回归测试。Windows Go 1.26.5 的 Prompt Audit 包测试、全包 compile、标准 `go test ./...`、`go vet -tags unit ./...` 已重新通过。Linux race 首次因全新 module cache 访问 `proxy.golang.org` 超时，改用 E 盘已有 module cache 和 `goproxy.cn` 后通过（`ok`，4.536s）。前端/CI 文件未变，沿用此前 `483/483` suites、`1282/1282` tests、typecheck/lint/build 全通过证据。最终只读 review 未发现新的 CRITICAL/HIGH/发布阻塞 MEDIUM；本轮新增 Go 修复已通过测试并包含在本地 merge commit 中。

## v0.1.163 自定义候选（2026-07-22，本地构建）
- [x] 将 v0.1.160、v0.1.161、v0.1.162、v0.1.163 候选从多个外部 worktree 整理为 `E:/vis project/zz sub2api` 单工作树内的 Git 基线分支；清理 `E:/claude-cache/sub2api-v161-candidate`、`sub2api-v162-candidate`、`sub2api-v163-candidate`，并用本地提交 `7a9033658` 保存旧 v0.1.160 合并现场。
- [x] 从自定义 v0.1.162 提交 `b480d880c` 合入官方 `v0.1.163` peeled commit `d0bdd7e77`；解决 `backend/cmd/server/main.go`、`SubscriptionPlanCard.vue` 及其测试三处冲突，同时保留本地完整 shutdown/cleanup、默认 CNY 与上游周/月有效期修复；源码版本和版本测试更新为 `0.1.163`。
- [x] 前端使用 E 盘缓存和 Corepack `pnpm 10.33.2` frozen install；typecheck、ESLint、合并定向测试 `15/15`、全量 Vitest `494/494` suites 和 `1309/1309` tests、production build 全部通过。
- [x] Windows Go 1.26.5 使用 E 盘 GOPATH/module/build/tmp cache 完成 `go test -run '^$' ./...` 全包编译、无外部 OpenAI 凭据的 `go test ./...` 全量测试、`go vet ./...` 和 Windows amd64 构建；首次依赖下载因 `proxy.golang.org` IPv6 超时，切换 `goproxy.cn` 后通过，与候选源码无关。
- [x] 最终 merge commit `826ecb06d4c4df47ced0c61e35870c081a64da90` 已推送到 fork `feature/chat-image-tools`；Custom Docker Image run `29979060018` 成功，manifest digest 为 `sha256:888cbb0398ac91e8e0d84a6df7b43c739befefa972a1bb21c4152374ccef3c4b`。
- [x] 用户已按上述 digest 在 `/opt/sub2api/docker-compose.yml` 固定镜像并完成部署，容器 revision 为 `826ecb06d4c4df47ced0c61e35870c081a64da90`。临时 Windows 构建产物随后已清理。

## v0.1.164 自定义候选（2026-07-23）
- [x] 在唯一工作目录 `E:/vis project/zz sub2api` 从自定义 v0.1.163 提交 `826ecb06d` 合入官方 `v0.1.164` peeled commit `cd8bb98c4`；未创建额外 worktree 或候选文件夹。
- [x] 解决 `wire_gen.go`、`setting_handler_update.go`、`http.go`、`router.go` 四处冲突：保留严格 step-up、原子设置写入和完整 shutdown/cleanup，同时接入 composite route resolver、支付宝移动 deep link 与 Ollama Cloud 用量服务。
- [x] 修复合并后的 cleanup 测试夹具参数缺口；`backend/cmd/server/VERSION` 和嵌入版本测试更新为 `0.1.164`。
- [x] Go 1.26.5 使用 E 盘 GOPATH/module/build/tmp cache 完成 `go test -run '^$' ./...`、Custom Docker workflow 同款回归、`go vet ./...` 和无外部 OpenAI 凭据的 `go test ./...`，全部通过。
- [x] 前端 Corepack pnpm 10.33.2 的 typecheck、ESLint、全量 Vitest `192/192` files、`1341/1341` tests 和 production build 全部通过；仅有既有 Vite chunk 与 Browserslist 提示。
- [x] 发布路径保持为推送 `feature/chat-image-tools` 后由 GitHub Actions 构建 GHCR 多架构镜像；本地不构建 Windows 可执行文件，部署以 Action 输出的 manifest digest 为准。

## v0.1.165 自定义候选（2026-07-25，本地验证）
- [x] 在唯一工作目录 `E:/vis project/zz sub2api` 从自定义 v0.1.164 提交 `ed9b3a84c` 创建回滚分支 `backup/pre-v165-ed9b3a84c` 和候选分支 `merge/v0.1.165-chat-image-tools`，合入官方 annotated tag object `892c8fa3ab80ada8a624668808c3e575da7c04d5`（peeled commit `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29`）。三方合并无文本冲突。
- [x] 保留 AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、Prompt Audit 严格 step-up、自定义 updater/shutdown 和 Custom Docker `BUILD_TYPE=custom`；同时接入官方 ChatGPT Live、Claude Opus 5、Ollama Cloud 请求驱动刷新、usage `session_id`、公告预览和注册邮箱别名去重等更新。
- [x] 官方标签中的 `backend/cmd/server/VERSION` 仍为 `0.1.164`，候选已显式同步为 `0.1.165`，嵌入版本回归断言同步更新。
- [x] 修复合并测试契约：上游 `GroupsView` 新增 `getLiveCapability()` 后，本地列设置与复制分组测试 mock 补齐该方法并默认返回 `{ supported: false }`；同时为 Live 开关加入按表单隔离的异步失效令牌、活动弹窗/平台/编辑分组校验和加载禁用态，避免关闭弹窗或切换平台后的延迟响应回写旧表单。新增 2 条竞态回归测试，前端全量 Vitest `194/194` files、`1356/1356` tests 通过。
- [x] 前端 pnpm 9.15.9 frozen install、typecheck、ESLint、production build 全部通过；仅保留既有 Browserslist、chunk size 和静态/动态 import 提示。
- [x] Windows Go 1.26.5 使用 E 盘 GOPATH/module/build/tmp cache 完成全包编译、无外部凭据的 `go test ./...`、Custom Docker workflow 同款 unit 回归和 `go vet ./...`，全部通过；首次模块下载访问 `proxy.golang.org` IPv6 超时，切换项目 Dockerfile 同款 `goproxy.cn` 后成功。
- [x] 使用 Dockerfile 同款 `CGO_ENABLED=0`、`-tags embed`、`0.1.165-zz`、`BuildType=custom` 参数完成 Linux amd64/arm64 交叉编译并校验 ELF machine。amd64 SHA256 为 `688fdcc0d8a269a2cf1646175d5c584460d37ee4538e92cc8324100827246f7e`，arm64 SHA256 为 `87d7a30e4957e546c905ccaf5546bdc425a3e189fb0cecec371bd816f74d9513`。
- [ ] 本机没有 Docker/Podman/buildah，WSL 当前启动异常，因此尚未组装/运行 OCI 镜像；候选尚未推送 `feature/chat-image-tools`、尚未触发 GHCR workflow、尚未部署生产。后续发布仍以 GitHub Actions 输出的 manifest digest 为准。

## v0.1.166 自定义候选（2026-07-27，本地验证）
- [x] 在唯一工作目录 `E:/vis project/zz sub2api` 从自定义 v0.1.165 提交 `9038f46f7` 创建 `backup/pre-v166-9038f46f7` 与 `merge/v0.1.166-chat-image-tools`，合入官方 `v0.1.166` peeled commit `dc893dd0b`；解决设置更新、Prompt Audit、路由和 OpenAI WebSocket 适配层共 9 个文本冲突。
- [x] 设置融合同时保留本地原子事务/安全基线与官方部分 PUT 语义；panel rate limiter 与 Strict Step-up 同时进入管理路由；WebSocket 逐轮模型映射、计费统计、响应模型恢复与图片生成权限门控均保留。`backend/cmd/server/VERSION` 和嵌入版本测试更新为 `0.1.166`。
- [x] 修复合并后的测试契约：Prompt Audit fake store 使用新的 `Public() (PublicConfig, error)`；路由测试补齐 panel limiter 参数；Grok OAuth handler unit 测试桩实现 identity-bound billing CAS，使既有 `billing` 响应断言恢复，生产 Grok 逻辑未修改。
- [x] Windows Go 1.26.5 使用 E 盘 GOPATH/module/build/tmp cache 完成冲突包定向测试、`go test -run '^$' ./...` 全包编译、Custom Docker workflow 同款 unit 回归、`go test -tags unit ./internal/handler/admin` 和 `go vet ./...`，全部通过。
- [x] 前端使用 E 盘 pnpm store 与 Corepack `pnpm 9.15.9` frozen install；typecheck、ESLint、全量 Vitest `197/197` files、`1373/1373` tests 和 production build 全部通过；仅保留既有 Browserslist、chunk size 和静态/动态 import 提示。
- [x] 使用 Dockerfile 同款 `CGO_ENABLED=0`、`-tags embed`、`0.1.166-zz`、`BuildType=custom` 参数完成 Linux amd64/arm64 交叉编译并校验 ELF machine。amd64 SHA256 为 `89b77f8f2b49ad7d6d1d7b178e904ef2a698757df171e4159754a5ed7a18e0f4`，arm64 SHA256 为 `6633ffaadafe98594e5aad649a51710e5370bf20e80e36199f788502bf06a284`。
- [x] merge commit `e5e78d42058cc8040b0aa850305e73a9ddc1c380` 已推送到 fork `feature/chat-image-tools`；Custom Docker Image run `30259860571` 成功，manifest digest 为 `sha256:a950a5c94ebf7ee2fb84f18984caac4ca2e15b8cb5d5ec2f84428abab3292e8b`。生产部署结果尚未在项目记录中确认。

## v0.1.168 自定义候选（2026-07-28，本地验证）
- [x] 在唯一工作目录 `E:/vis project/zz sub2api` 从已发布自定义 v0.1.166 merge commit `e5e78d420` 创建 `backup/pre-v168-e5e78d420` 与 `merge/v0.1.168-chat-image-tools`，合入官方 `v0.1.168` peeled commit `99c8e4bf7`；官方在 v0.1.166 后直接发布 v0.1.168，本仓库不存在需要单独合并的官方 v0.1.167 tag。
- [x] 解决 `wire_gen.go`、Prompt Audit 配置管理/类型/测试、HTTP server、router 和 settings update 测试共 7 个文本冲突；Model Plaza optional JWT 与本地 Strict Step-up、连接跟踪、原子设置更新和 Prompt Audit reload/save fence 均同时保留。
- [x] 按官方 v0.1.168 语义适配 Prompt Audit：存量端点令牌解密失败时配置继续可见，对应端点标记无效并禁用；blocking/async 模式、版本 fence 与 runner 行为均增加或更新回归断言。AI Chat、AI Images、Canvas、默认 CNY/动态币种和订阅汇率定制均已核对保留。
- [x] Windows Go 1.26.5 使用 E 盘 GOPATH/module/build/tmp cache 完成 `go test ./internal/securityaudit`、`go test -run '^$' ./...`、Custom Docker workflow 同款回归、无外部 OpenAI 凭据的 `go test ./...`、`go vet ./...`，以及 handler/securityaudit/routes/service 的 unit 测试，全部通过。
- [x] 前端使用 E 盘 pnpm store 与 Corepack `pnpm 9.15.9` frozen install；typecheck、ESLint、全量 Vitest `200/200` files、`1391/1391` tests 和 production build 全部通过；仅保留既有 Browserslist、chunk size 和静态/动态 import 提示。
- [x] 使用 Dockerfile 同款 `CGO_ENABLED=0`、`-tags embed`、`0.1.168-zz`、`BuildType=custom` 参数完成 Linux amd64/arm64 交叉编译并校验 ELF machine。amd64 SHA256 为 `551466978ad6766e554e7c60cceeb41484ecb7d880e69660437766c20930add8`，arm64 SHA256 为 `55a413a16f8e27264c060b6a088fe32b4e10ae5aae5816b3d481cf7e08393195`。
- [x] 发布链路保持为将 merge commit 推送到 fork `feature/chat-image-tools`，由 `.github/workflows/custom-docker.yml` 完成测试和 GHCR 多架构镜像发布；本节记录本地验收，线上 run、不可变标签和 manifest digest 以 GitHub Actions 发布记录为准。

## v0.1.169 自定义候选（2026-07-31，本地验证）
- [x] 从自定义 v0.1.168 merge commit `50d1c8880` 创建 `backup/pre-v169-50d1c8880` 与 `merge/v0.1.169-chat-image-tools`，合入官方 `v0.1.169` peeled commit `26d894ef4`；合并无文本冲突，保留 AI Chat、AI Images、Canvas、Prompt Audit、Model Plaza、默认 CNY/动态币种和订阅汇率定制。
- [x] 将官方源码版本文件和嵌入版本测试从 `0.1.168` 同步为 `0.1.169`；同时接入 Responses/Gemini 上游路径闭集校验、`no-new-privileges` Compose 安全选项、定价资源复制、代理断流熔断改进、Qwen3Guard 辅助字段和官方费率更新。
- [x] Windows Go 1.26.5 使用 E 盘 GOPATH/module/build/tmp cache 完成 `go test -run '^$' ./...` 全包编译、Custom Docker workflow 后端回归、完整 `go test ./...` 和 `go vet ./...`，全部通过。
- [x] 前端使用 E 盘 pnpm store 与 Corepack `pnpm 9.15.9` frozen install；typecheck、ESLint、全量 Vitest `201/201` files、`1404/1404` tests 和 production build 全部通过；仅保留已有 Browserslist、动态 import 和 chunk size 提示。
- [x] 使用 Dockerfile 同款 `CGO_ENABLED=0`、`-tags embed`、`0.1.169-zz`、`BuildType=custom` 参数完成 Linux amd64/arm64 交叉编译并校验 ELF machine。amd64 SHA256 为 `7a3d9b1868d7cb826a9d65d89adab01e33d31c2ca14070e92e71e722a71f801f`，arm64 SHA256 为 `75d7967f81d783e2c2507650e698615204ca1b430022984945d916bb66e1a791`。
- [ ] 候选尚未提交、推送或触发本轮 GHCR workflow；发布完成后以 GitHub Actions 的 test/build 成功结论和 manifest digest 为准，生产部署另行执行。

## v0.1.170 自定义候选（2026-08-02，本地验证）
- [x] 从已发布自定义 v0.1.169 merge commit `be9ba60b5` 创建 `backup/pre-v170-be9ba60b5` 与 `merge/v0.1.170-chat-image-tools`，合入官方 `v0.1.170` peeled commit `c043c2477`；解决 Prompt Audit 两处文本冲突并保留 AI Chat、AI Images、Canvas、默认 CNY/动态币种、订阅汇率和既有安全加固。
- [x] 接入官方分组级利润控制、上游计费倍率探测/自动同步、内容审核代理、最新输入审计、账号批处理和 migrations `192/193`；新开关默认关闭。修复自动融合造成的 `roundTripFunc` 重复定义，并将官方标签中仍为 `0.1.169` 的版本文件和测试同步为 `0.1.170`。
- [x] Windows Go 1.26.5 使用 E 盘 GOPATH/module/build/tmp cache 完成 Prompt Audit 定向测试、`go test -run '^$' ./...` 全包编译、Custom Docker workflow 同款回归、完整 `go test ./...` 和 `go vet ./...`，全部通过。
- [x] 前端使用 E 盘 pnpm store 与 Corepack `pnpm 9.15.9`；typecheck、ESLint、全量 Vitest `206/206` files、`1455/1455` tests 和 production build 全部通过；仅保留已有 Browserslist、动态/静态 import 和 chunk size 提示。
- [x] 使用 Dockerfile 同款 `CGO_ENABLED=0`、`-tags embed`、`0.1.170-zz`、`BuildType=custom` 参数完成 Linux amd64/arm64 交叉编译并校验目标平台。amd64 SHA256 为 `511977393899dd3a62727b6018a9d44265d27f3f55c986c28d06eb064b7c16a1`，arm64 SHA256 为 `9a3429f8624373bde3b20cd08d2d9f6618435ad8fc5f95ba4f7d3d8dcbb1d04f`。
- [x] merge commit `0d9619ffe79e1415127fbc0f5f28ffc0ca91b499` 已推送到 `feature/chat-image-tools`；Custom Docker Image run `30747270102` 的 test/build job 全部成功。GHCR 已发布 `chat-image-tools`、`chat-image-tools-0d9619f` 和完整 SHA 标签，三者均指向 manifest digest `sha256:259a9ab6a4336bca34e4dc2da7cebb90d6b7b31bba3bed9cbba5160162e5d6e7`；manifest 已核验包含 linux/amd64 与 linux/arm64。生产部署另行执行。

## v0.1.171 自定义候选（2026-08-04，Actions 已发布）
- [x] 从 v0.1.170 发布记录提交 `12a2a1393` 创建回滚分支 `backup/pre-v171-12a2a1393` 与候选分支 `merge/v0.1.171-chat-image-tools`，抓取官方 annotated tag `v0.1.171`（tag object `afd154b`，peeled commit `f0e7a9c7a`）并执行 `--no-commit --no-ff` 合并；无文本冲突。
- [x] 将 `backend/cmd/server/VERSION` 和嵌入版本回归断言同步为 `0.1.171`，保留 AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、Strict Step-up、原子设置更新、支付补丁和 Custom Docker workflow。
- [x] 修复合并后的测试契约：`wire_gen_test.go` 补入 `OpenAICodexVersionSyncService` cleanup 依赖；版本断言从 `0.1.170` 更新为 `0.1.171`。最终提交序列为 merge `fdc747c1c`、测试夹具修复 `e0137e4c5`、版本断言修复 `e36bb52ac`。
- [x] 本地前端门禁通过：`vue-tsc --noEmit`、ESLint、Vue/TypeScript build、Vitest `212/212` files、`1512/1512` tests、Vite production build；构建仅保留既有 Browserslist、动态/静态 import 和 chunk size 提示。
- [x] 推送 fork `feature/chat-image-tools` 并触发 Custom Docker Image run `30976386453`；后端编译/回归、前端 frozen install/验证和 Buildx 发布全部成功。远程构建参数为 `VERSION=0.1.171-zz`、`BUILD_TYPE=custom`、`COMMIT=e36bb52ac8efed6641a206b4332cdac858a48249`，平台为 linux/amd64、linux/arm64。
- [x] GHCR 已发布以下不可变标签：`chat-image-tools`、`chat-image-tools-e36bb52`、`chat-image-tools-e36bb52ac8efed6641a206b4332cdac858a48249`；三者均指向 OCI manifest digest `sha256:d6eeec3ef08cf0052dc342854a92dfcbe7db62989ba0b08e8b97efdc2f0b578c`。生产部署另行执行，回滚仍以备份分支和固定 digest 为准。

## v0.1.172 自定义候选（2026-08-07，Actions 已发布）
- [x] 从 v0.1.171 发布记录提交 `7dec0c4db` 创建回滚分支 `backup/pre-v172-7dec0c4db` 与候选分支 `merge/v0.1.172-chat-image-tools`，抓取官方 annotated tag `v0.1.172`（tag object `61ba94d`，peeled commit `155c49496`）并执行 `--no-commit --no-ff` 合并；54 个提交、208 个文件无文本冲突。
- [x] 将 `backend/cmd/server/VERSION` 和 `wire_gen_test.go` 嵌入版本断言同步为 `0.1.172`；保留 AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、Strict Step-up、原子设置更新、支付补丁和 Custom Docker workflow。
- [x] 接入官方 OAuth 账号接管修复、上游响应模型审计、Antigravity Gemini 3.6 Flash、Codex `codex-tui` 身份、订阅日额度午夜刷新、计费精度和建连/TLS 超时修复；migrations 194/195 与非事务索引 runner 逻辑已核对。
- [x] 后端使用 E 盘 Go 1.26.5 工具链/模块缓存通过 `go test ./cmd/server -run 'TestEmbeddedVersionMatchesRelease|TestProvideCleanup|TestShutdownHTTPThenCleanup' -count=1`，并通过 `go test ./... -run '^$' -count=1` 全包编译；GitHub Actions 进一步完成 workflow 同款后端回归。
- [x] 本地前端门禁通过：typecheck、ESLint、Vitest `214/214` files、`1530/1530` tests、Vue/TypeScript build 和 Vite production build；仅保留既有 Browserslist、动态/静态 import、chunk size 警告。
- [x] 推送 fork `feature/chat-image-tools` 并触发 Custom Docker Image run `31235663120`；test job（后端编译/回归、前端 frozen install/验证）和 build job 全部成功。远程构建参数为 `VERSION=0.1.172-zz`、`BUILD_TYPE=custom`、`COMMIT=584c265b17dbfd7971f049bc0c7e1d392e473090`，平台为 linux/amd64、linux/arm64。
- [x] GHCR 已发布以下不可变标签：`chat-image-tools`、`chat-image-tools-584c265`、`chat-image-tools-584c265b17dbfd7971f049bc0c7e1d392e473090`；三者均指向 OCI manifest digest `sha256:44453b038fe3faf016682f5fccab07c4ee176eae29b7c16a13abd9d769c46eaf`。生产部署另行执行，回滚仍以备份分支和固定 digest 为准。

## v0.1.173 自定义候选（2026-08-09，Actions 已发布）
- [x] 从 v0.1.172 发布记录提交 `5cb376e48` 创建回滚分支 `backup/pre-v173-5cb376e48` 与候选分支 `merge/v0.1.173-chat-image-tools`，抓取官方 annotated tag `v0.1.173`（tag object `9e2a27ad`，peeled commit `29009f0b2`）并完成三方合并；官方比较范围为 120 个提交、352 个文件。
- [x] 保留 fork 的 AI Chat、AI Images、Canvas 外链、Prompt Audit/Strict Step-up、原子设置和 Custom Docker workflow；版本文件与嵌入版本断言同步为 `0.1.173`。修复合并后的两个前端重复 mock 声明及 `provideCleanup` 测试夹具漏传 `channelMonitorV2Aggregator` 参数。
- [x] 本地验证通过：后端 `go test -mod=readonly ./cmd/server -run 'TestEmbeddedVersionMatchesRelease|TestProvideCleanup|TestShutdownHTTPThenCleanup' -count=1`、`go test -mod=readonly ./cmd/server -count=1`、`go test -mod=readonly ./... -run '^$' -count=1`；前端 typecheck、ESLint、Vitest `225/225` files（`1580/1580` tests）和 production build 全部通过。
- [x] merge commit `b3b28ff2f5f8e865f66f09cfa7609a99df24bfa9` 已推送到 fork `feature/chat-image-tools`；Custom Docker Image run `31310903130` 的 test/build job 全部成功，构建参数为 `VERSION=0.1.173-zz`、`BUILD_TYPE=custom`、`COMMIT=b3b28ff2f5f8e865f66f09cfa7609a99df24bfa9`，平台为 linux/amd64、linux/arm64。
- [x] GHCR 已发布不可变标签 `chat-image-tools`、`chat-image-tools-b3b28ff` 和 `chat-image-tools-b3b28ff2f5f8e865f66f09cfa7609a99df24bfa9`；三者均指向 manifest digest `sha256:431d89555d653e96eb0cab7c3375ccc79485955ea23ad14bb642225dd3731103`。生产部署仍需单独执行。
- [ ] 部署前按迁移 220 说明导出相关 `groups` 视频定价并暂停管理端写入；启动后核对 `groups_video_price_backup_220`，并确认 Redis 健康后再观察 Grok 异步视频计费日志。

## v0.1.178 自定义发布（2026-08-19，Actions 已发布，OVH 已手动部署）
- [x] 在唯一工作目录 `/Users/hhzz/Developer/sub2api` 从自定义 v0.1.177 提交 `218796a7d32023419ced9cfa1c47cb98d3fe4d97` 创建候选分支 `merge/v0.1.178-chat-image-tools`，合入官方 annotated tag `v0.1.178`（tag object `15290e66c66801a7ce435a6d24b178ee9486f284`，peeled commit `e0c48a19ed794a565e3858662520afe0a1f9f0ba`）；只合正式 tag，未带入上游 `main` 的后续未发布提交。
- [x] 解决 `admin_group.go`、`api_key_auth_cache_impl.go`、`api_key_auth_cache_pricing_test.go` 三处冲突：平台闭集校验与 BillingMode 采用上游 v0.1.178 规则，认证快照继续保留自定义 Long Context/模型定价字段并对含 TimePricing periods 的定价做深拷贝。版本文件与 Wire 嵌入断言同步为 `0.1.178`。
- [x] 保留 AI Chat、AI Images、Canvas 外链、默认 CNY/动态币种、Strict Step-up、原子设置、Prompt Audit、安全 shutdown、自定义 Docker/GHCR workflow；同时接入 Kimi/智谱/DeepSeek、渠道监控配额模式、渠道模型谷峰定价、Codex 指纹迁移、OpenAI Team 联动熔断和上游 v0.1.178 修复。
- [x] 修复合并后 OpenAI partial usage 链路：Responses native/passthrough/Grok 及 Chat/Messages buffered/streaming 保留已观测计量的非 failover `result+err`；有计量的 `UpstreamFailoverError` 抑制 result，避免换号成功后双扣；零计量但携带 partial output、client disconnect 或 missing-terminal 状态的 compat result 保留给 handler 判定。cyber 有计量时统一走 `RecordUsage(CyberBlocked=true)`，无计量时保持 `result=nil` 并只写一条 fallback 用量；Chat 补齐 request payload hash，Responses/Chat/Messages 及 raw-Chat fallback 补齐客户端断开归因，避免污染账号调度健康度。
- [x] 最终后端门禁：Go 1.26.6 `go test -tags unit ./... -run '^$' -count=1` 全包编译、`go vet -tags unit ./...` 及 compat focused regression 通过；父提交上的两个 Chat streaming result 失败已由相同测试命令复现，并在最终代码提交上通过。GitHub CI run `32218065769` 的 unit、integration、frontend、shell、golangci-lint 全绿，Security Scan run `32218065741` 的 backend/frontend security 全绿。
- [x] 前端最终验证通过：typecheck、ESLint、Vitest `233/233` files、`1673/1673` tests、production build；最终边界补丁只改 Go。Integration 除外部 `tls.peet.ws` 本机证书链不受信任导致 `TestJA3Fingerprint` 与 `TestAllProfiles/linux_x64_node_v22171` 失败外，其余包通过。
- [x] 回滚制品位于 `.cache/update-v0.1.178/`：共享 result 判定原始文件 SHA-256 为 `0d12a3bfe837efe38bc96743080cc4dd4f1df0eb942f21b23a3c558b447b20b7`，修改版为 `01d0fb49f3deeab603ba795beb0241176ccfcb364816d89d2adb069d27cb7ba2`；`ROLLBACK.sh` 已在独立副本上执行并恢复到原始哈希，当前源码与 `MODIFIED_FILE` 保持修改状态。
- [x] 最终代码提交 `65c74ca395e6e337e9baa413f4727e8ed3cb16ed` 已推送到 fork `feature/chat-image-tools`。Custom Docker Image run `32218065758` 的 test/build 全绿；稳定、短 SHA、完整 SHA 三个标签均指向 OCI index digest `sha256:94f8cd3a5f34783d22d66e5aa338cfbd9be0ca00e7c77c642f014e4d55ce1b63`，包含 linux/amd64 child `sha256:d30d786445c9b30a4ee034398962a21800b7534db7cc5a70e414f2b0ae3a1e5c` 与 linux/arm64 child `sha256:5aacae1f31995e5e9e53b9f78727b204138992f70cef09fb95fe27272564ad8b`，两边 revision label 均为完整代码 SHA。生产 VPS 未访问，等待用户手动 SSH 固定完整 SHA 标签和 digest。
- [x] 2026-08-20 用户在 OVH `/data/sub2api` 手动固定上述完整 SHA 标签与 digest；`sub2api`、PostgreSQL、Redis 均为 healthy，应用显示 `0.1.178-zz` / revision `65c74ca395e6e337e9baa413f4727e8ed3cb16ed`，健康接口返回 `{"status":"ok"}`，数据库升级前 dump 为 423 MB。

## v0.1.179 自定义候选（2026-08-23，本地验证完成）
- [x] 从 v0.1.178 发布记录提交 `71b9de1605e534cc1375107244b06da6e5507a0c` 创建 `backup/pre-v179-71b9de160` 与 `merge/v0.1.179-chat-image-tools`，合入官方 annotated tag object `3c28fad50472b409e18666df617f4237d8ba7007`（peeled commit `75f88be5f75c27771836b586f7de1503afa0e3bc`）；官方相对 v0.1.178 为 78 个提交、212 个文件，16 项 checks 全绿。
- [x] 解决两处文本冲突：handler 同时保留定制 `isOpenAIPartialClientDisconnect` 与官方带 `*gin.Context` 的 composite Grok/CN Messages 映射豁免；Grok 创建账号测试同时保留 computed 默认值正则和实际 placeholder 绑定断言。官方标签遗漏版本升级，已把 `backend/cmd/server/VERSION` 与嵌入版本断言统一为 `0.1.179`。
- [x] 接入 CN adaptive 三协议、Composite Codex/CN、渠道 fast/flex/上下文区间倍率、Anthropic Fast 计费、`/v1/responses/input_tokens`、可配置代理探测、用量聚合/索引及 OpenAI/WS/Grok 修复；保留 AI Chat、AI Images、Canvas、默认 CNY/动态币种、Prompt Audit、Strict Step-up、原子设置、安全 shutdown、partial-result 计费和自定义 GHCR 发布链路。
- [x] 本地验证通过：Go 1.26.6 默认全包编译、`go test -tags unit ./... -count=1`、`go vet -tags unit ./...`；前端 typecheck、ESLint、Vitest `242/242` files / `1719/1719` tests、production build；Apple Container/Compose/runtime/Caddy 五项 shell 门禁。官方 Go 代理 IPv6 超时后改用已验证的 `goproxy.cn` 补齐本地缓存，未降低测试范围。
- [x] 回滚制品位于 `.cache/update-v0.1.179/`：handler 原始 SHA-256 为 `ac192e5bce74a727b0c9593e50bce4d29a2e121e5943ab6cc8913fb95e5ff3a8`，修改版为 `521ce4b8c4fbac4ccfe2d75efc66c0b05b4fde44141ba4122d8791c2c293a59d`；`ROLLBACK.sh` 已在独立副本上执行并恢复原始哈希，源码与 `MODIFIED_FILE` 保持修改状态。
- [ ] 候选尚未提交、推送或触发 CI/GHCR。v0.1.179 新增同号但不冲突的 `226_add_usage_log_effective_model_indexes_notx.sql` 以及 227/228；生产部署前必须生成 PostgreSQL dump，并确认长上下文计费从 group AND account 改为 OR 后的账单口径。

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
