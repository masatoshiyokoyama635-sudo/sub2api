# zz AI 中转站专属用户教程

> 本文档面向 `https://ai.zh-zh.top` 用户，只覆盖：客户端接入、AI 对话、AI 生图、充值订阅、邀请返利。  
> 参考 Fern 文档的 OpenAI 兼容接入写法后，已将示例中的站点、路径和功能改为 zz 自己的中转站实现。

## 0. 使用前准备

### 0.1 常用入口

| 用途 | 地址 |
|---|---|
| 控制台首页 | `https://ai.zh-zh.top` |
| 登录页 | `https://ai.zh-zh.top/login` |
| API 密钥 | `https://ai.zh-zh.top/keys` |
| AI 对话 | `https://ai.zh-zh.top/ai/chat` |
| AI 生图 | `https://ai.zh-zh.top/ai/images` |
| 充值 / 订阅 | `https://ai.zh-zh.top/purchase` |
| 我的订阅 | `https://ai.zh-zh.top/subscriptions` |
| 我的订单 | `https://ai.zh-zh.top/orders` |
| 邀请返利 | `https://ai.zh-zh.top/affiliate` |

备用入口：如果主入口临时不可用，可尝试 `https://ai.zh-zh.cloud`，接口路径保持一致，例如 `https://ai.zh-zh.cloud/v1`。

### 0.2 创建 API Key

1. 登录控制台。
2. 打开左侧菜单 **API 密钥**。
3. 点击创建密钥。
4. 选择要使用的分组，例如 GPT / Claude / Gemini / gpt-image 等。
5. 创建后复制 API Key，并妥善保存。

建议把密钥放到环境变量里：

```bash
export ZH_AI_API_KEY="sk-你的密钥"
```

安全提醒：

- 不要把完整 API Key 发到公开群聊、截图、教程或 GitHub 仓库。
- 如果怀疑 Key 泄露，立即在控制台删除或禁用旧 Key，然后重新创建。
- 如果 Key 设置了 IP 白名单，请确认你的设备或服务器出口 IP 已加入白名单。

---

## 1. 客户端接入教程

本章按 Fern 客户端接入文档的结构整理，并替换为 zz AI 中转站的真实地址。第一次接入建议先完成这 5 步：

1. 登录 `https://ai.zh-zh.top`。
2. 如果你有兑换码，先完成兑换，并确认余额或套餐已经到账。
3. 进入 **API 密钥** 页面创建新的 API Key。
4. 打开模型列表或调用 `/v1/models`，复制当前账号实际可用的完整模型名。
5. 再把 Base URL、API Key 和模型名填入客户端或代码中。

接入完成后，建议通过 **仪表盘、API 密钥、用量记录、模型列表** 四个位置检查账号余额、套餐状态、Key 状态、请求是否到达、token 消耗、费用和错误信息。

### 1.1 Base URL 与鉴权速查

| 客户端 / 协议 | Base URL | 常用场景 |
|---|---|---|
| OpenAI Compatible | `https://ai.zh-zh.top/v1` | Cursor、Cline、Codex CLI、OpenCode、Qwen Code、Postman、OpenAI SDK、Chatbox、Cherry Studio 等 |
| Claude / Anthropic | `https://ai.zh-zh.top` | Anthropic SDK、Claude Code、Claude 兼容客户端 |
| Gemini 原生 | `https://ai.zh-zh.top/v1beta` | Gemini SDK / CLI / 原生 Gemini 客户端 |
| Antigravity Claude | `https://ai.zh-zh.top/antigravity` | Antigravity Claude 分组 |
| Antigravity Gemini | `https://ai.zh-zh.top/antigravity/v1beta` | Antigravity Gemini 分组 |

普通网关推荐：

```text
Authorization: Bearer sk-你的密钥
```

Gemini 原生客户端通常使用：

```text
x-goog-api-key: sk-你的密钥
```

不要把 Key 放进 URL 参数，例如 `?key=...` 或 `?api_key=...`，否则容易被浏览器历史、日志或代理记录。

### 1.2 先查询当前 Key 可用模型

OpenAI / Claude 兼容模型列表：

```bash
curl https://ai.zh-zh.top/v1/models \
  -H "Authorization: Bearer $ZH_AI_API_KEY"
```

Gemini 原生模型列表：

```bash
curl https://ai.zh-zh.top/v1beta/models \
  -H "x-goog-api-key: $ZH_AI_API_KEY"
```

客户端里的 `model` 必须填写接口返回或后台展示的真实模型名，不要只写模糊简称。不同 API Key 绑定的分组不同，可用模型也不同。

### 1.3 Cursor 接入

Cursor 支持 OpenAI Compatible 接口，适合在 IDE 里直接使用 zz AI 中转站模型。

1. 打开 **Cursor Settings**。
2. 进入 **Models**。
3. 开启 **OpenAI API Key**。
4. 开启 **Override OpenAI Base URL**。
5. 填入：

| 配置项 | 值 |
|---|---|
| OpenAI API Key | `sk-你的密钥` |
| Override OpenAI Base URL | `https://ai.zh-zh.top/v1` |
| Model Name | 从 `/v1/models` 或后台模型页复制的模型名，例如 `gpt-5.5` |

如果模型列表里没有你要用的模型，点击 **Add Custom Model**，输入完整模型名后添加。验证时关闭 Auto 模式，在模型下拉栏手动选择刚添加的模型，然后发送测试消息。

常见问题：

- 找不到模型：关闭 Auto 模式，手动选择自定义模型。
- 连接失败或 Unauthorized：检查 API Key、Base URL 是否为 `https://ai.zh-zh.top/v1`、模型名是否可用、账号是否有余额或订阅。

### 1.4 Cline 接入

Cline 是 VS Code 智能编程插件，支持自定义 OpenAI Compatible 服务端点。

1. 在 VS Code 扩展市场安装 **Cline**。
2. 打开 Cline 配置页。
3. 首次使用选择 **Bring my own API key**；已配置过则从右上角设置进入。
4. 填入：

| 配置项 | 值 |
|---|---|
| API Provider | `OpenAI Compatible` |
| API Key | `sk-你的密钥` |
| Base URL | `https://ai.zh-zh.top/v1` |
| Model ID | 从 `/v1/models` 复制的模型名 |

如果使用 Qwen3、QwQ、R1 等需要特殊消息格式或 reasoning 的模型，请在 Cline 的 **MODEL CONFIGURATION** 里开启对应的 reasoning / R1 messages 选项。

验证方式：保存后让 Cline 执行一个简单任务，例如“解释当前文件”。模型能正常返回即配置成功。

### 1.5 Codex CLI 接入

Codex CLI 通过 OpenAI Compatible 接口接入。Windows 用户建议先安装 Node.js LTS，并重新打开终端。

验证环境：

```bash
node --version
npm --version
```

安装最新版 Codex CLI：

```bash
npm install -g @openai/codex
```

如果必须使用旧版 Chat Completions 接入方式，可安装 0.80.0：

```bash
npm install -g @openai/codex@0.80.0
```

配置环境变量：

```bash
export ZH_AI_API_KEY="sk-你的密钥"
```

编辑 `~/.codex/config.toml`：

```toml
model_provider = "zz-ai"
model = "gpt-5.5"
model_reasoning_effort = "high"

[model_providers.zz-ai]
name = "zz AI"
base_url = "https://ai.zh-zh.top/v1"
env_key = "ZH_AI_API_KEY"
wire_api = "responses"
```

说明：

- `base_url` 固定填写 `https://ai.zh-zh.top/v1`。
- `model` 填写当前 Key 可用的真实模型名。
- 新版 Codex 使用 Responses API，因此 `wire_api = "responses"`。
- 如果你使用 `@openai/codex@0.80.0`，才把 `wire_api` 改成 `chat`。

旧版 Chat Completions 示例：

```toml
model_provider = "zz-ai"
model = "gpt-5.5"

[model_providers.zz-ai]
name = "zz AI"
base_url = "https://ai.zh-zh.top/v1"
env_key = "ZH_AI_API_KEY"
wire_api = "chat"
```

启动验证：

```bash
cd /path/to/your/project
codex
```

常见问题：

- `wire_api = chat is no longer supported`：新版 Codex 改用 `wire_api = "responses"`。
- `401 Unauthorized`：确认环境变量名与 `env_key` 一致，当前终端已加载新变量，Key 没有复制错。
- `404 Not Found`：确认 `base_url` 包含 `/v1`，模型名正确，`wire_api` 与 Codex 版本匹配。

### 1.6 OpenCode 接入

OpenCode 是终端 AI 编程工具，可通过自定义 OpenAI Compatible Provider 调用模型。

安装：

```bash
curl -fsSL https://opencode.ai/install | bash
```

进入项目目录运行：

```bash
opencode
```

推荐通过内置命令添加自定义提供商：

```text
/connect
```

在向导里选择 **OpenAI Compatible** 或 **Custom Provider**，填入：

| 配置项 | 值 |
|---|---|
| Provider Name | `zz AI` |
| API Key | `sk-你的密钥` |
| Base URL | `https://ai.zh-zh.top/v1` |
| Model | 从 `/v1/models` 复制的模型名 |

验证：重启 OpenCode 后输入 `/models`，选择刚添加的模型，再发送测试消息。

常见问题：

- `/models` 看不到模型：确认模型名已写入配置，OpenCode 已重启；部分版本需要手动添加模型名。
- `401`：检查 Key、账号状态和 Base URL。

### 1.7 Qwen Code 接入

Qwen Code 支持 OpenAI Compatible Provider。

启动：

```bash
qwen
```

方式一：通过 `/auth` 配置：

```text
/auth
```

依次选择或填写：

```text
Provider: OpenAI Compatible
Base URL: https://ai.zh-zh.top/v1
API Key: sk-你的密钥
Model: 从 /v1/models 复制的模型名
```

方式二：通过 `settings.json` 固定模型列表：

```json
{
  "env": {
    "ZH_AI_API_KEY": "sk-你的密钥"
  },
  "modelProviders": {
    "openai": [
      {
        "id": "gpt-5.5",
        "name": "zz AI - gpt-5.5",
        "baseUrl": "https://ai.zh-zh.top/v1",
        "envKey": "ZH_AI_API_KEY"
      }
    ]
  },
  "security": {
    "auth": {
      "selectedType": "openai"
    }
  },
  "model": {
    "name": "gpt-5.5"
  },
  "$version": 3
}
```

验证：重新运行 `qwen`，发送测试消息；如需切换模型，输入 `/model`。

### 1.8 Claude Code 接入

Claude Code 使用 Anthropic 兼容入口，不使用 OpenAI Compatible 的 `/v1` Base URL。

普通 Claude 分组：

```bash
export ZH_AI_API_KEY="sk-你的密钥"
export ANTHROPIC_BASE_URL="https://ai.zh-zh.top"
export ANTHROPIC_AUTH_TOKEN="$ZH_AI_API_KEY"
export ANTHROPIC_API_KEY=""
```

如果使用 Antigravity Claude 分组：

```bash
export ZH_AI_API_KEY="sk-你的密钥"
export ANTHROPIC_BASE_URL="https://ai.zh-zh.top/antigravity"
export ANTHROPIC_AUTH_TOKEN="$ZH_AI_API_KEY"
export ANTHROPIC_API_KEY=""
```

如果 Claude Code 要求完成官方 onboarding，可编辑或新增 `~/.claude.json`，Windows 通常是 `C:\Users\<用户名>\.claude.json`，合并：

```json
{
  "hasCompletedOnboarding": true
}
```

验证：

```bash
claude /status
```

确认能看到类似：

```text
Auth token: ANTHROPIC_AUTH_TOKEN
Anthropic base URL: https://ai.zh-zh.top
```

再发送测试消息：

```bash
claude "你好"
```

如果仍连接官方 Anthropic 服务，通常是环境变量没有生效：重启终端，确认 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN` 和空的 `ANTHROPIC_API_KEY`。

### 1.9 Postman / cURL 调试

Postman 适合先验证 Key、Base URL 和模型名是否正确。

新建请求：

| 配置项 | 值 |
|---|---|
| Method | `POST` |
| URL | `https://ai.zh-zh.top/v1/chat/completions` |
| Authorization | `Bearer sk-你的密钥` |
| Content-Type | `application/json` |

Body 选择 raw / JSON：

```json
{
  "model": "gpt-5.5",
  "messages": [
    {
      "role": "user",
      "content": "你好，简单介绍一下你自己。"
    }
  ]
}
```

同一请求也可以用 cURL：

```bash
curl https://ai.zh-zh.top/v1/chat/completions \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.5",
    "messages": [
      {
        "role": "user",
        "content": "你好，简单介绍一下你自己。"
      }
    ]
  }'
```

返回里有 `choices` 字段，说明接口调用成功。

### 1.10 OpenAI SDK 示例

Python：

```python
from openai import OpenAI
import os

client = OpenAI(
    api_key=os.environ["ZH_AI_API_KEY"],
    base_url="https://ai.zh-zh.top/v1",
)

response = client.chat.completions.create(
    model="gpt-5.5",
    messages=[{"role": "user", "content": "你好，简单介绍一下你自己"}],
)

print(response.choices[0].message.content)
```

TypeScript：

```ts
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: process.env.ZH_AI_API_KEY,
  baseURL: "https://ai.zh-zh.top/v1",
});

const response = await client.chat.completions.create({
  model: "gpt-5.5",
  messages: [{ role: "user", content: "你好，简单介绍一下你自己" }],
});

console.log(response.choices[0]?.message?.content);
```

---

## 2. AI 对话教程

AI 对话可以直接在网页里使用，也可以通过 OpenAI Chat Completions 接口调用。

### 2.1 网页 AI 对话

入口：`https://ai.zh-zh.top/ai/chat`

操作步骤：

1. 登录控制台。
2. 点击左侧菜单 **AI 对话**。
3. 选择要测试的分组。
4. 选择该分组下 active 状态的 API Key。
5. 在模型输入框填写模型名，例如 `gpt-5.5`，或填写 `/v1/models` 返回的其他模型名。
6. 输入问题，点击发送。
7. 查看回复、延迟和 token 用量。

网页背后实际调用：

```text
POST /v1/chat/completions
Authorization: Bearer <你选择的 API Key>
```

请求体示例：

```json
{
  "model": "gpt-5.5",
  "messages": [
    {
      "role": "user",
      "content": "你好，简单介绍一下你自己"
    }
  ],
  "stream": false
}
```

### 2.2 API 调用 AI 对话

cURL：

```bash
curl https://ai.zh-zh.top/v1/chat/completions \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.5",
    "messages": [
      { "role": "user", "content": "你好，简单介绍一下你自己" }
    ],
    "stream": false
  }'
```

响应中通常读取：

```text
choices[0].message.content
```

用量字段通常在：

```text
usage.prompt_tokens
usage.completion_tokens
usage.total_tokens
```

### 2.3 对话排查

| 现象 | 处理方式 |
|---|---|
| 提示 API key is required | 检查是否带了 `Authorization: Bearer sk-...` |
| 提示 Invalid API key | 检查 Key 是否复制完整、是否被禁用、是否属于当前站点 |
| 模型报错或 404 | 先请求 `/v1/models`，确认当前 Key 能访问该模型 |
| 余额不足 / 无订阅 / 额度用完 | 到控制台检查余额、订阅有效期、日/周/月额度和 Key 额度 |
| 网页没有可选 Key | 到 **API 密钥** 页面创建 active 状态的 Key，并绑定正确分组 |

---

## 3. AI 生图教程

AI 生图当前使用 OpenAI 图片接口：

```text
POST https://ai.zh-zh.top/v1/images/generations
```

> 注意：参考 Fern 文档里的图像生成示例使用 `/v1/chat/completions` + `modalities`。zz 中转站当前用户侧生图页面使用的是 OpenAI Images 路径 `/v1/images/generations`，请优先按本文档填写。

### 3.1 网页 AI 生图

入口：`https://ai.zh-zh.top/ai/images`

操作步骤：

1. 登录控制台。
2. 点击左侧菜单 **AI 生图**。
3. 选择 **gpt-image 分组**。
4. 选择该分组下 active 状态的 API Key。
5. 填写图片模型名，例如 `gpt-image-2`。
6. 选择图片尺寸：`1024x1024`、`1024x1536` 或 `1536x1024`。
7. 选择生成数量：1 到 4 张。
8. 输入提示词并点击生成。
9. 生成后可复制图片地址或下载图片。

网页背后实际调用：

```text
POST /v1/images/generations
Authorization: Bearer <你选择的 API Key>
```

请求体示例：

```json
{
  "model": "gpt-image-2",
  "prompt": "一只穿着宇航服的猫，赛博朋克风格，高清细节",
  "size": "1024x1024",
  "n": 1
}
```

### 3.2 API 调用 AI 生图

cURL：

```bash
curl https://ai.zh-zh.top/v1/images/generations \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "一只穿着宇航服的猫，赛博朋克风格，高清细节",
    "size": "1024x1024",
    "n": 1
  }'
```

常见返回字段：

```text
data[0].url
data[0].b64_json
data[0].revised_prompt
```

页面会展示：

- base64 图片数据；
- HTTPS 图片 URL；
- 修订后的提示词（如果上游返回）。

非 HTTPS 图片 URL 可能不会在页面中展示。

### 3.3 生图排查

| 现象 | 处理方式 |
|---|---|
| AI 生图页面没有分组 | 确认账号是否有 gpt-image 分组，以及该分组下是否有 active Key |
| 图片接口提示不支持 | `/v1/images/generations` 需要使用图片通道分组，不要用 Claude / Gemini 等非图片分组 Key |
| 没有图片返回 | 检查模型是否为图片模型、提示词是否明确要求生成图片、余额或订阅额度是否充足 |
| 返回 URL 但页面不展示 | 检查是否为 HTTPS URL；页面会过滤非 HTTPS 图片地址 |
| 生成很慢 | 生图耗时通常比文本对话更长，请等待订单式响应完成或查看用量记录 |

---

## 4. 充值订阅教程

充值和订阅都在同一个页面完成：

```text
https://ai.zh-zh.top/purchase
```

页面会根据后台配置显示可用功能：如果余额充值被关闭，可能只显示订阅；如果支付系统未开启，会提示暂不可用。

### 4.1 余额充值

操作步骤：

1. 登录控制台。
2. 打开 **充值/订阅** 页面。
3. 切换到 **充值** 标签。
4. 查看当前余额。
5. 选择快捷金额，或输入自定义金额。
6. 选择支付方式：支付宝、微信支付、Stripe、Airwallex 等，具体以页面显示为准。
7. 点击 **确认支付**。
8. 按页面提示扫码、跳转或在新窗口完成支付。
9. 支付完成后返回页面，等待订单状态变为完成。
10. 到 **我的订单** 或个人余额处确认到账。

充值页面可能显示：

- 支付金额；
- 手续费；
- 实付金额；
- 到账余额；
- 充值倍率，例如 `1 CNY = x USD`。

### 4.2 购买订阅

操作步骤：

1. 登录控制台。
2. 打开 **充值/订阅** 页面。
3. 切换到 **订阅** 标签。
4. 查看可购买套餐。
5. 选择适合的套餐。
6. 确认套餐信息：分组、价格、有效期、倍率、日/周/月额度、支持模型范围。
7. 选择支付方式。
8. 点击 **确认支付**。
9. 完成支付后，到 **我的订阅** 查看是否生效。

订阅卡片常见字段：

| 字段 | 含义 |
|---|---|
| 分组 | 订阅开通后可使用的模型分组 |
| 价格 | 购买套餐需要支付的金额 |
| 有效期 | 套餐持续时间，例如天、月、年 |
| 倍率 | 当前分组的计费倍率 |
| 日 / 周 / 月限额 | 对应周期内可使用的额度 |
| 支持模型 | 当前套餐覆盖的模型范围，以页面显示为准 |

### 4.3 续费订阅

1. 打开 **我的订阅**：`https://ai.zh-zh.top/subscriptions`。
2. 找到 active 状态的订阅。
3. 点击 **续费**。
4. 系统会跳转到购买页并筛选该分组套餐。
5. 选择套餐并完成支付。

### 4.4 查看订单和支付状态

订单页：`https://ai.zh-zh.top/orders`

常见状态：

| 状态 | 说明 |
|---|---|
| `PENDING` | 待支付 |
| `PAID` | 已支付，等待充值或订阅开通 |
| `RECHARGING` | 充值处理中 |
| `COMPLETED` | 已完成，余额或订阅已到账 |
| `EXPIRED` | 订单已过期 |
| `CANCELLED` | 订单已取消 |
| `FAILED` | 处理失败，可联系管理员排查 |
| `REFUND_REQUESTED` | 已申请退款 |
| `REFUNDING` | 退款处理中 |
| `REFUNDED` | 已退款 |

### 4.5 充值订阅排查

| 现象 | 处理方式 |
|---|---|
| 没有支付方式 | 后台可能未启用支付，或当前金额不满足支付通道限额 |
| 提示待支付订单过多 | 先到 **我的订单** 完成或取消旧的待支付订单 |
| 支付成功但余额/订阅未到账 | 等待系统补单；仍异常时提供订单号和支付时间给客服 |
| 金额无法提交 | 检查是否低于最低金额、高于最高金额或超过每日限额 |
| 微信/支付宝无法跳转 | 尝试换浏览器、使用扫码支付，或在微信内打开微信支付页面 |

---

## 5. 邀请返利教程

邀请返利入口：

```text
https://ai.zh-zh.top/affiliate
```

如果左侧菜单没有 **邀请返利**，说明该功能可能被后台关闭，或当前账号暂无权限显示。

### 5.1 获取自己的邀请码和邀请链接

1. 登录控制台。
2. 打开 **邀请返利** 页面。
3. 查看 **我的邀请码**。
4. 点击 **复制邀请码**，或复制完整 **邀请链接**。
5. 将邀请链接发给新用户。

邀请链接格式通常是：

```text
https://ai.zh-zh.top/register?aff=你的邀请码
```

系统也兼容：

```text
https://ai.zh-zh.top/register?aff_code=你的邀请码
```

### 5.2 被邀请用户如何注册

新用户通过你的邀请链接打开注册页后：

1. 注册页会自动读取链接中的 `aff` 或 `aff_code`。
2. 用户完成邮箱、密码等注册信息。
3. 提交注册后，系统把该用户绑定为你的邀请用户。
4. 如果用户使用 OAuth 登录/注册，系统也会尽量保留邀请参数。

邀请参数会在浏览器本地短期保存，避免用户跳转登录方式后丢失。

### 5.3 返利如何产生

当被邀请用户完成充值后，你会按生效返利比例获得返利额度：

```text
返利额度 = 被邀请用户充值基础金额 × 你的生效返利比例 / 100
```

页面会显示：

- **我的返利比例**：当前生效比例，可能是全局比例，也可能是管理员给你的专属比例。
- **邀请人数**：已绑定到你名下的新用户数量。
- **可转返利额度**：当前可转入余额的返利额度。
- **冻结中**：新产生但还在冻结期内的返利额度。
- **历史返利额度**：累计产生过的返利额度。
- **已邀请用户**：被邀请用户列表和对应返利明细。

管理员可能配置：

- 返利冻结期；
- 单个被邀请用户返利上限；
- 返利有效期；
- 专属用户返利比例。

因此实际到账规则以后台配置和页面显示为准。

### 5.4 将返利转入余额

1. 打开 **邀请返利** 页面。
2. 确认 **可转返利额度** 大于 0。
3. 点击 **转入余额**。
4. 成功后，可用返利额度会减少，账户余额会增加。
5. 如果有冻结额度，需要等冻结期结束后才能转入。

页面背后使用的用户接口：

```text
GET  /api/v1/user/aff
POST /api/v1/user/aff/transfer
```

普通用户不需要手动调用这些接口，直接在页面操作即可。

### 5.5 邀请返利排查

| 现象 | 处理方式 |
|---|---|
| 邀请返利菜单不显示 | 功能可能被后台关闭，联系管理员确认 |
| 邀请链接注册后没有记录 | 确认新用户是否从你的 `?aff=` 或 `?aff_code=` 链接进入，并完成注册 |
| 有返利但不能提现/转余额 | 检查是否还在冻结期，或可转额度是否为 0 |
| 被邀请用户充值后没有返利 | 可能未绑定邀请关系、超过返利有效期、达到单人上限，或后台关闭了返利 |
| 返利比例不符合预期 | 以页面显示的“我的返利比例”为准；管理员可能配置了专属比例 |
