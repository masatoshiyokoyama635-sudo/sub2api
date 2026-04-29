export interface GatewayChatMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
}

export interface GatewayChatRequest {
  model: string
  messages: GatewayChatMessage[]
  stream?: false
}

export interface GatewayChatResponse {
  id?: string
  model?: string
  choices?: Array<{
    message?: {
      role?: string
      content?: string
    }
  }>
  usage?: {
    prompt_tokens?: number
    completion_tokens?: number
    total_tokens?: number
  }
}

export interface GatewayImageRequest {
  model: string
  prompt: string
  size: string
  n: number
}

export interface GatewayImageItem {
  url?: string
  b64_json?: string
  revised_prompt?: string
}

export interface GatewayImageResponse {
  created?: number
  data?: GatewayImageItem[]
  usage?: unknown
}

async function parseGatewayResponse<T>(response: Response): Promise<T> {
  const contentType = response.headers.get('content-type') || ''
  const body = contentType.includes('application/json') ? await response.json() : await response.text()

  if (!response.ok) {
    if (body && typeof body === 'object') {
      const error = (body as { error?: { message?: string }; message?: string }).error
      throw new Error(error?.message || (body as { message?: string }).message || `Gateway request failed with status ${response.status}`)
    }
    throw new Error(String(body || `Gateway request failed with status ${response.status}`))
  }

  return body as T
}

async function postGateway<T>(path: string, apiKey: string, payload: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload),
    signal
  })
  return parseGatewayResponse<T>(response)
}

export function createChatCompletion(apiKey: string, payload: GatewayChatRequest, signal?: AbortSignal): Promise<GatewayChatResponse> {
  return postGateway<GatewayChatResponse>('/v1/chat/completions', apiKey, payload, signal)
}

export function createImageGeneration(apiKey: string, payload: GatewayImageRequest, signal?: AbortSignal): Promise<GatewayImageResponse> {
  return postGateway<GatewayImageResponse>('/v1/images/generations', apiKey, payload, signal)
}

export const gatewayAPI = {
  createChatCompletion,
  createImageGeneration
}
