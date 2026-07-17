import { buildGatewayUrl } from '@/api/url'

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
  response_format: 'b64_json'
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

  if (body && typeof body === 'object') {
    const errorBody = body as { error?: { message?: string }; message?: string }
    if (!response.ok || errorBody.error) {
      throw new Error(errorBody.error?.message || errorBody.message || `Gateway request failed with status ${response.status}`)
    }
  } else if (!response.ok) {
    throw new Error(String(body || `Gateway request failed with status ${response.status}`))
  }

  return body as T
}

function trustedGatewayUrl(path: string): string {
  const resolved = buildGatewayUrl(path)
  if (typeof window === 'undefined') return resolved

  const target = new URL(resolved, window.location.origin)
  if (target.origin !== window.location.origin) {
    throw new Error('Gateway requests carrying API keys must use the current site origin')
  }
  return target.toString()
}

async function postGateway<T>(path: string, apiKey: string, payload: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(trustedGatewayUrl(path), {
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
