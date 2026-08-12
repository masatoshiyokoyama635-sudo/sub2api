import { beforeEach, describe, expect, it, vi } from 'vitest'

const buildGatewayUrlMock = vi.fn((path: string) => `${window.location.origin}${path}`)

vi.mock('@/api/url', () => ({
  buildGatewayUrl: (path: string) => buildGatewayUrlMock(path),
}))

import { gatewayAPI } from '@/api/gateway'

const fetchMock = vi.fn()

describe('gatewayAPI', () => {
  beforeEach(() => {
    fetchMock.mockReset()
    buildGatewayUrlMock.mockImplementation((path: string) => `${window.location.origin}${path}`)
    vi.stubGlobal('fetch', fetchMock)
    localStorage.clear()
  })

  it('sends chat completion requests to the gateway with the selected API key', async () => {
    localStorage.setItem('auth_token', 'login-jwt')
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ choices: [{ message: { content: 'ok' } }] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }))

    await gatewayAPI.createChatCompletion('sk-selected', {
      model: 'gpt-5.5',
      messages: [{ role: 'user', content: 'hello' }],
      stream: false
    })

    expect(fetchMock).toHaveBeenCalledWith('http://localhost:3000/v1/chat/completions', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({
        Authorization: 'Bearer sk-selected',
        'Content-Type': 'application/json'
      }),
      body: JSON.stringify({
        model: 'gpt-5.5',
        messages: [{ role: 'user', content: 'hello' }],
        stream: false
      })
    }))
  })

  it('sends image generation requests to the gateway with the selected API key', async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ data: [{ url: 'https://example.test/image.png' }] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }))

    await gatewayAPI.createImageGeneration('sk-image', {
      model: 'gpt-image-2',
      prompt: 'toy poster',
      size: '1024x1024',
      n: 2,
      response_format: 'b64_json'
    })

    expect(fetchMock).toHaveBeenCalledWith('http://localhost:3000/v1/images/generations', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ Authorization: 'Bearer sk-image' }),
      body: JSON.stringify({
        model: 'gpt-image-2',
        prompt: 'toy poster',
        size: '1024x1024',
        n: 2,
        response_format: 'b64_json'
      })
    }))
  })

  it('fails closed before sending an API key to a cross-origin gateway', async () => {
    buildGatewayUrlMock.mockReturnValue('https://api.example.com/v1/chat/completions')

    await expect(gatewayAPI.createChatCompletion('sk-selected', {
      model: 'gpt-5.5',
      messages: [{ role: 'user', content: 'hello' }],
      stream: false
    })).rejects.toThrow('must use the current site origin')

    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('preserves OpenAI-compatible error messages', async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ error: { message: 'quota exceeded' } }), {
      status: 429,
      headers: { 'Content-Type': 'application/json' }
    }))

    await expect(gatewayAPI.createChatCompletion('sk-selected', {
      model: 'gpt-5.5',
      messages: [{ role: 'user', content: 'hello' }],
      stream: false
    })).rejects.toThrow('quota exceeded')
  })

  it('preserves image errors after JSON keepalive commits HTTP 200', async () => {
    fetchMock.mockResolvedValue(new Response(' \n{"error":{"message":"image quota exceeded"}}', {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }))

    await expect(gatewayAPI.createImageGeneration('sk-image', {
      model: 'gpt-image-2',
      prompt: 'toy poster',
      size: '1024x1024',
      n: 1,
      response_format: 'b64_json'
    })).rejects.toThrow('image quota exceeded')
  })
})
