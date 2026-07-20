import { describe, expect, it } from 'vitest'
import { createSafeImageSource, type GatewayImageSource } from './imageSource'

describe('createSafeImageSource', () => {
  const origin = 'https://ai.example.com'

  it('prefers base64 data over an untrusted URL', () => {
    expect(createSafeImageSource({
      b64_json: 'aW1hZ2U=',
      url: 'https://images.example.net/result.png'
    }, origin)).toBe('data:image/png;base64,aW1hZ2U=')
  })

  it.each([
    {
      name: 'root-relative path',
      url: '/images/result.png',
      currentOrigin: origin,
      expected: 'https://ai.example.com/images/result.png'
    },
    {
      name: 'absolute same-origin HTTPS URL',
      url: 'https://ai.example.com/images/result.png',
      currentOrigin: origin,
      expected: 'https://ai.example.com/images/result.png'
    },
    {
      name: 'current origin with an application path',
      url: 'https://ai.example.com/images/result.png',
      currentOrigin: 'https://ai.example.com/app/index.html',
      expected: 'https://ai.example.com/images/result.png'
    },
    {
      name: 'current origin with a trailing slash',
      url: '/images/result.png',
      currentOrigin: 'https://ai.example.com/',
      expected: 'https://ai.example.com/images/result.png'
    },
    {
      name: 'current origin with a path, query and fragment',
      url: '/images/result.png',
      currentOrigin: 'https://ai.example.com/app/?api_key=sk-do-not-copy#section',
      expected: 'https://ai.example.com/images/result.png'
    }
  ])('allows $name', ({ url, currentOrigin, expected }) => {
    expect(createSafeImageSource({ url }, currentOrigin)).toBe(expected)
  })

  it.each([
    { name: 'cross-origin host', url: 'https://images.example.net/result.png' },
    { name: 'HTTP on the same host', url: 'http://ai.example.com/result.png' },
    { name: 'cross-origin protocol-relative URL', url: '//images.example.net/result.png' },
    { name: 'same-origin protocol-relative URL', url: '//ai.example.com/result.png' },
    { name: 'cross-origin userinfo confusion', url: 'https://ai.example.com@images.example.net/result.png' },
    { name: 'userinfo credentials on the same host', url: 'https://user:password@ai.example.com/result.png' },
    { name: 'changed port', url: 'https://ai.example.com:444/result.png' },
    { name: 'JavaScript scheme', url: 'javascript:alert(1)' },
    { name: 'data scheme', url: 'data:image/png;base64,aW1hZ2U=' },
    { name: 'plain relative path', url: 'images/result.png' },
    { name: 'invalid URL', url: 'https://%' },
    { name: 'IPv4 loopback cross-origin URL', url: 'https://127.0.0.1/result.png' },
    { name: 'IPv6 loopback cross-origin URL', url: 'https://[::1]/result.png' },
    { name: 'localhost cross-origin URL', url: 'https://localhost/result.png' },
    { name: 'invalid URL type', url: 42 }
  ])('rejects $name', ({ url }) => {
    expect(createSafeImageSource({ url } as unknown as GatewayImageSource, origin)).toBe('')
  })

  it.each([
    { name: 'HTTP current origin', currentOrigin: 'http://ai.example.com' },
    { name: 'invalid current origin', currentOrigin: 'not a URL' },
    { name: 'current origin containing userinfo', currentOrigin: 'https://user:password@ai.example.com/app/' }
  ])('rejects a root-relative result with $name', ({ currentOrigin }) => {
    expect(createSafeImageSource({ url: '/images/result.png' }, currentOrigin)).toBe('')
  })

  it('does not append API keys or credentials to an accepted URL', () => {
    const item = {
      url: '/images/result.png',
      apiKey: 'sk-should-not-leak',
      credentials: 'Bearer should-not-leak'
    } as GatewayImageSource & { apiKey: string; credentials: string }

    const source = createSafeImageSource(item, origin)

    expect(source).toBe('https://ai.example.com/images/result.png')
    expect(source).not.toContain('sk-should-not-leak')
    expect(source).not.toContain('Bearer')
  })

  it.each([
    null,
    undefined
  ])('rejects an invalid image result object %s', (item) => {
    expect(createSafeImageSource(item as unknown as GatewayImageSource, origin)).toBe('')
  })
})
