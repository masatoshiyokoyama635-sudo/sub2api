import { describe, expect, it } from 'vitest'
import { createSafeImageSource } from './imageSource'

describe('createSafeImageSource', () => {
  const origin = 'https://ai.example.com'

  it('allows base64 and same-origin HTTPS image results', () => {
    expect(createSafeImageSource({ b64_json: 'aW1hZ2U=' }, origin)).toBe('data:image/png;base64,aW1hZ2U=')
    expect(createSafeImageSource({ url: '/images/result.png' }, origin)).toBe('https://ai.example.com/images/result.png')
  })

  it.each([
    'https://images.example.net/result.png',
    'https://127.0.0.1/result.png',
    'http://ai.example.com/result.png',
    'not a URL',
  ])('rejects untrusted image URL %s', (url) => {
    expect(createSafeImageSource({ url }, origin)).toBe('')
  })
})
