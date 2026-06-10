# zh-ai 中转站用户使用说明

本文档是根据当前 zh-ai（Sub2API 自定义版）实际项目整理的用户教程，重点覆盖控制台入口、API Key 创建、外部客户端调用、网页内 AI 对话和 AI 生图功能。

> 访问入口：`https://ai.zh-zh.top`
>
> 控制台登录：`https://ai.zh-zh.top/login`

## 1. zh-ai 是什么

zh-ai 是一个 AI API 网关 / 中转站。用户在控制台创建自己的 API Key 后，可以通过统一入口调用后台已接入的 Claude、GPT/OpenAI、Gemini、Antigravity 等模型。

平台主要能力：

- **统一 API Key**：一个 Key 绑定一个分组，按该分组可用的上游模型进行调用。
- **统一调用入口**：兼容 OpenAI、Anthropic/Claude、Gemini 等常见协议。
- **自动路由**：根据 API Key 所属分组和请求路径，把请求转发到对应上游。
- **计费与限额**：请求会按分组定价、订阅额度或账户余额进行扣费/限额校验。
- **网页内体验**：控制台内置「AI 对话」和「AI 生图」，不用额外配置客户端也能测试模型。
- **用量可查**：用户可以在控制台查看 API Key、用量记录、订阅、订单和余额等信息。

## 2. 快速开始

### 2.1 登录控制台

1. 打开 `https://ai.zh-zh.top/login`。
2. 使用你的账号登录。
3. 登录后进入用户控制台。

用户侧常用菜单包括：

- **AI 对话**：网页里直接选择分组和 API Key 进行聊天测试。
- **AI 生图**：网页里直接选择 gpt-image 分组和 API Key 生成图片。
- **API 密钥**：创建、复制、管理自己的 API Key。
- **用量记录**：查看请求消耗、模型、时间等调用记录。
- **可用渠道 / 渠道状态**：查看自己能使用的分组和服务状态。
- **我的订阅 / 充值/订阅 / 我的订单**：查看订阅、购买套餐或充值记录。
- **兑换 / 返利 / 个人资料**：兑换码、邀请返利和账号资料管理。

### 2.2 创建 API Key

1. 进入左侧菜单 **API 密钥**。
2. 点击创建密钥。
3. 选择你要使用的分组。
4. 如页面提供额度、过期时间、IP 限制等选项，可按需求配置。
5. 创建后复制 API Key，并保存到本地安全位置。

建议把 Key 放到环境变量中：

```bash
export ZH_AI_API_KEY="sk-你的密钥"
```

注意事项：

- API Key 不要发到公开聊天、公开仓库或网页前端代码里。
- 如果 Key 泄露，立即在控制台删除或禁用旧 Key，然后重新创建。
- 如果启用了 IP 白名单，请确认调用服务器的出口 IP 已加入白名单。

### 2.3 充值、订阅与价格说明

控制台里的充值比例、订阅套餐、分组倍率和可用模型以后台实时显示为准。不同分组可能有不同倍率、限额和有效期。

常见模式：

- **余额模式**：按实际模型消耗从账户余额扣除。
- **订阅模式**：按日 / 周 / 月额度限制使用，适合长期或高频调用。
- **分组倍率**：某些分组可能配置倍率，例如 0.5x 表示按标价的一半消耗额度。

购买前建议先确认：

1. 你要用的模型属于哪个分组。
2. 该分组是余额模式还是订阅模式。
3. API Key 是否绑定到了正确分组。
4. 模型名是否来自当前 Key 可访问的模型列表。

## 3. 网关地址与接口总览

### 3.1 控制台地址

| 用途 | 地址 |
|---|---|
| 首页 / 控制台入口 | `https://ai.zh-zh.top` |
| 登录页 | `https://ai.zh-zh.top/login` |
| 用户 AI 对话页面 | `https://ai.zh-zh.top/ai/chat` |
| 用户 AI 生图页面 | `https://ai.zh-zh.top/ai/images` |
| API Key 管理页面 | `https://ai.zh-zh.top/keys` |

### 3.2 API Base URL / Endpoint

| 用途 | 地址 |
|---|---|
| OpenAI 兼容客户端 Base URL | `https://ai.zh-zh.top/v1` |
| OpenAI Chat Completions | `https://ai.zh-zh.top/v1/chat/completions` |
| OpenAI Responses API | `https://ai.zh-zh.top/v1/responses` |
| OpenAI 图片生成 | `https://ai.zh-zh.top/v1/images/generations` |
| OpenAI 图片编辑 | `https://ai.zh-zh.top/v1/images/edits` |
| Claude / Anthropic Messages API | `https://ai.zh-zh.top/v1/messages` |
| Claude / Anthropic Base URL | `https://ai.zh-zh.top` |
| Gemini 原生 API | `https://ai.zh-zh.top/v1beta` |
| Antigravity Claude | `https://ai.zh-zh.top/antigravity/v1/messages` |
| Antigravity Claude Base URL | `https://ai.zh-zh.top/antigravity` |
| Antigravity Gemini | `https://ai.zh-zh.top/antigravity/v1beta` |

说明：

- `/v1/chat/completions` 会根据 Key 所属分组自动路由。OpenAI 分组走 OpenAI 兼容处理；非 OpenAI 分组走兼容转换处理。
- `/v1/images/generations` 和 `/v1/images/edits` 当前只支持后台配置的图片分组；对用户来说请选择 **gpt-image 分组**。该分组底层属于 OpenAI 图片通道，如果 Key 所属分组不是图片通道，会返回不支持图片接口。
- Gemini 原生接口推荐使用 `/v1beta`，更适合 Gemini SDK / CLI 类客户端。

## 4. 鉴权方式

推荐使用 Bearer Token：

```text
Authorization: Bearer sk-你的密钥
```

普通网关也兼容：

```text
x-api-key: sk-你的密钥
x-goog-api-key: sk-你的密钥
```

Gemini 原生接口推荐：

```text
x-goog-api-key: sk-你的密钥
```

不建议把密钥放在 URL 参数里，例如 `?key=...` 或 `?api_key=...`，因为容易被浏览器历史、日志或代理记录泄露。普通网关会拒绝 query 参数形式的 API Key；Gemini 兼容路径仅在 `/v1beta` 和 `/antigravity/v1beta` 为部分客户端保留了 `key` 参数兼容。`api_key` query 参数会被拒绝。

## 5. 查询可用模型

### 5.1 OpenAI / Claude 兼容模型列表

```bash
curl https://ai.zh-zh.top/v1/models \
  -H "Authorization: Bearer $ZH_AI_API_KEY"
```

### 5.2 Gemini 模型列表

```bash
curl https://ai.zh-zh.top/v1beta/models \
  -H "x-goog-api-key: $ZH_AI_API_KEY"
```

模型名必须使用接口返回或后台展示的真实模型名。不同 API Key 因为分组不同，可用模型可能不一样。

## 6. 网页内 AI 对话

当前项目新增了用户侧 **AI 对话** 页面，路径为：

```text
https://ai.zh-zh.top/ai/chat
```

使用步骤：

1. 登录控制台。
2. 点击左侧菜单 **AI 对话**。
3. 在「分组」下拉框中选择要测试的分组。
4. 在「API 密钥」下拉框中选择该分组下的 active API Key。
5. 在「模型」输入框填写模型名，例如 `gpt-5.5`，也可以换成你当前分组支持的模型名。
6. 在输入框输入问题，点击发送。

页面实际调用的是：

```text
POST /v1/chat/completions
Authorization: Bearer <你选择的 API Key>
```

请求体结构：

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

页面会显示：

- 当前分组
- 当前 API Key
- 当前模型
- 最近一次延迟
- prompt tokens / completion tokens / total tokens
- 对话消息

如果页面提示没有分组或没有密钥，请先到 **API 密钥** 页面创建并绑定正确分组。

## 7. 网页内 AI 生图

当前项目新增了用户侧 **AI 生图** 页面，路径为：

```text
https://ai.zh-zh.top/ai/images
```

使用步骤：

1. 登录控制台。
2. 点击左侧菜单 **AI 生图**。
3. 选择 **gpt-image 分组**。
4. 选择该分组下 active 状态的 API Key。
5. 填写图片模型名，例如 `gpt-image-2`，也可以换成后台支持的图片模型。
6. 选择图片尺寸：`1024x1024`、`1024x1536` 或 `1536x1024`。
7. 选择生成数量：1 到 4 张。
8. 输入提示词并点击生成。

页面实际调用的是：

```text
POST /v1/images/generations
Authorization: Bearer <你选择的 API Key>
```

请求体结构：

```json
{
  "model": "gpt-image-2",
  "prompt": "一只穿着宇航服的猫，赛博朋克风格，高清细节",
  "size": "1024x1024",
  "n": 1
}
```

图片接口说明：

- 当前请选择 **gpt-image 分组**；该分组底层属于 OpenAI 图片通道。
- 当前只使用 active 状态的 API Key。
- 返回结果支持图片 URL 或 base64 图片数据。
- 页面只会展示 base64 图片或 HTTPS 图片 URL；非 HTTPS 图片 URL 会被过滤。
- 页面支持复制图片地址和下载图片。
- 图片消耗按后台配置的图片计费规则、分组倍率或订阅额度计算。

## 8. curl 调用示例

### 8.1 Claude / Anthropic Messages API

适合 Claude SDK、Anthropic SDK、Claude Code 等客户端。

```bash
curl https://ai.zh-zh.top/v1/messages \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<Claude模型名>",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": "你好，简单介绍一下你自己"
      }
    ]
  }'
```

`<Claude模型名>` 请替换成后台或 `/v1/models` 返回的实际模型名。

### 8.2 OpenAI Chat Completions

适合兼容 OpenAI Chat Completions 的客户端。

```bash
curl https://ai.zh-zh.top/v1/chat/completions \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<GPT模型名>",
    "messages": [
      {
        "role": "user",
        "content": "你好，简单介绍一下你自己"
      }
    ]
  }'
```

### 8.3 OpenAI Responses API

适合支持 Responses API 的模型和客户端。

```bash
curl https://ai.zh-zh.top/v1/responses \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<GPT模型名>",
    "input": [
      {
        "role": "user",
        "content": "你好"
      }
    ]
  }'
```

### 8.4 GPT reasoning / 深度思考

如果当前模型支持 Responses API 的 `reasoning` 参数，应使用：

```text
https://ai.zh-zh.top/v1/responses
```

不要用 `/v1/chat/completions` 触发 reasoning。

```bash
curl https://ai.zh-zh.top/v1/responses \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5",
    "reasoning": {
      "effort": "high",
      "summary": "detailed"
    },
    "input": [
      {
        "role": "user",
        "content": "你好"
      }
    ]
  }'
```

响应里通常可能出现：

- `type: "reasoning"`：推理摘要，位于 `summary` 字段。
- `type: "message"`：最终回复，位于 `content` 字段。

### 8.5 OpenAI 图片生成

图片生成请使用 **gpt-image 分组**；该分组底层属于 OpenAI 图片通道。

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

### 8.6 Gemini 原生 API

Gemini 使用 `/v1beta` 路径，推荐 `x-goog-api-key` 鉴权。

```bash
curl "https://ai.zh-zh.top/v1beta/models/<Gemini模型名>:generateContent" \
  -H "x-goog-api-key: $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          {
            "text": "你好，简单介绍一下你自己"
          }
        ]
      }
    ]
  }'
```

### 8.7 Antigravity Claude

```bash
curl https://ai.zh-zh.top/antigravity/v1/messages \
  -H "Authorization: Bearer $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<Antigravity Claude模型名>",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": "你好"
      }
    ]
  }'
```

### 8.8 Antigravity Gemini

```bash
curl "https://ai.zh-zh.top/antigravity/v1beta/models/<Antigravity Gemini模型名>:generateContent" \
  -H "x-goog-api-key: $ZH_AI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          {
            "text": "你好"
          }
        ]
      }
    ]
  }'
```

## 9. 常用客户端配置

### 9.1 OpenAI SDK / OpenAI 兼容客户端

Base URL：

```text
https://ai.zh-zh.top/v1
```

Python 示例：

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-你的密钥",
    base_url="https://ai.zh-zh.top/v1",
)

response = client.chat.completions.create(
    model="<GPT模型名>",
    messages=[{"role": "user", "content": "你好"}],
)

print(response.choices[0].message.content)
```

### 9.2 Anthropic SDK / Claude 兼容客户端

Base URL：

```text
https://ai.zh-zh.top
```

Python 示例：

```python
from anthropic import Anthropic

client = Anthropic(
    api_key="sk-你的密钥",
    base_url="https://ai.zh-zh.top",
)

message = client.messages.create(
    model="<Claude模型名>",
    max_tokens=1024,
    messages=[{"role": "user", "content": "你好"}],
)

print(message.content[0].text)
```

### 9.3 Claude Code

普通 Claude 分组：

```bash
export ANTHROPIC_AUTH_TOKEN="sk-你的密钥"
export ANTHROPIC_BASE_URL="https://ai.zh-zh.top"
```

Antigravity Claude 分组：

```bash
export ANTHROPIC_AUTH_TOKEN="sk-你的密钥"
export ANTHROPIC_BASE_URL="https://ai.zh-zh.top/antigravity"
```

配置后在 Claude Code 里选择当前分组支持的模型名。

### 9.4 Cherry Studio / Chatbox / 其他图形客户端

如果客户端选择 OpenAI 兼容模式：

```text
API Key: sk-你的密钥
Base URL: https://ai.zh-zh.top/v1
Model: 使用后台或 /v1/models 返回的模型名
```

如果客户端选择 Anthropic / Claude 模式：

```text
API Key: sk-你的密钥
Base URL: https://ai.zh-zh.top
Model: 使用后台或 /v1/models 返回的 Claude 模型名
```

如果客户端选择 Gemini 模式：

```text
API Key: sk-你的密钥
Base URL: https://ai.zh-zh.top/v1beta
Model: 使用 /v1beta/models 返回的 Gemini 模型名
```

## 10. 常见问题

### Q1：提示 `API key is required` 怎么办？

检查请求是否带了 API Key。推荐格式：

```text
Authorization: Bearer sk-你的密钥
```

Gemini 原生接口推荐：

```text
x-goog-api-key: sk-你的密钥
```

### Q2：提示 `Invalid API key` 怎么办？

可能原因：

- API Key 复制不完整。
- API Key 已被删除或禁用。
- 使用了其他站点的 Key。
- 客户端把 Key 填到了错误字段里。

### Q3：提示余额不足、无订阅或额度用完怎么办？

请登录控制台检查：

- 账户余额是否充足。
- 当前分组是否需要订阅。
- 订阅是否过期。
- 日 / 周 / 月额度是否达到上限。
- API Key 自身额度或过期时间是否已触发限制。

### Q4：为什么模型名报错？

模型名必须是后台已接入、且当前 Key 所属分组可访问的模型名。建议先查询：

```bash
curl https://ai.zh-zh.top/v1/models \
  -H "Authorization: Bearer $ZH_AI_API_KEY"
```

Gemini 查询：

```bash
curl https://ai.zh-zh.top/v1beta/models \
  -H "x-goog-api-key: $ZH_AI_API_KEY"
```

### Q5：AI 生图页面没有分组怎么办？

AI 生图页面需要使用 gpt-image 分组。请确认：

1. 你的账号有可用的 gpt-image 分组。
2. 该 gpt-image 分组下有 active 状态的 API Key。
3. API Key 没有过期、禁用或额度用完。

### Q6：图片接口返回不支持怎么办？

`/v1/images/generations` 请使用 gpt-image 分组。如果你用的是 Claude、Gemini、Antigravity 等非图片分组，会返回图片接口不支持。

### Q7：GPT reasoning 参数没有效果怎么办？

确认三点：

1. 请求地址是 `/v1/responses`，不是 `/v1/chat/completions`。
2. 模型本身支持 reasoning。
3. 请求体包含：

```json
{
  "reasoning": {
    "effort": "high",
    "summary": "detailed"
  }
}
```

### Q8：Gemini 客户端无法鉴权怎么办？

Gemini 原生接口优先使用：

```text
x-goog-api-key: sk-你的密钥
```

如果客户端只支持 Bearer，也可以尝试：

```text
Authorization: Bearer sk-你的密钥
```

## 11. 安全建议

- 不要把 API Key 写进公开仓库。
- 不要把 API Key 放在前端代码里。
- 不要把 API Key 直接发给不可信的人或机器人。
- 生产环境建议使用环境变量或密钥管理服务保存 Key。
- 如发现 Key 泄露，应立即删除或禁用旧 Key，并重新创建。
- 如支持 IP 白名单，生产环境建议开启。
- 调用日志、截图和教程文档里不要出现真实 Key。

## 12. 备注

本文档根据当前 zh-ai 项目实际实现整理，关键事实包括：

- 用户侧 AI 对话页面实际调用 `POST /v1/chat/completions`。
- 用户侧 AI 生图页面实际调用 `POST /v1/images/generations`。
- AI 生图页面应选择 gpt-image 分组；该分组底层属于 OpenAI 图片通道。
- 用户页面只会列出当前分组下 active 状态的 API Key。
- 普通网关推荐 `Authorization: Bearer` 鉴权，并兼容 `x-api-key`、`x-goog-api-key`。
- Gemini 原生接口推荐 `x-goog-api-key` 鉴权。

具体模型、价格、额度、套餐、充值方式和可用渠道，以 zh-ai 后台实时显示为准。
