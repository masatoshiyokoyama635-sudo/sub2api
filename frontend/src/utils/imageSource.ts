export interface GatewayImageSource {
  url?: string
  b64_json?: string
}

export function createSafeImageSource(item: GatewayImageSource, currentOrigin: string): string {
  if (item.b64_json) {
    return `data:image/png;base64,${item.b64_json}`
  }
  if (!item.url) return ''

  if (!item.url.startsWith('/')) return ''

  try {
    const url = new URL(item.url, currentOrigin)
    return url.protocol === 'https:' && url.origin === currentOrigin ? url.toString() : ''
  } catch {
    return ''
  }
}
