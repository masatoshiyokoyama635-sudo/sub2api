export interface GatewayImageSource {
  url?: string
  b64_json?: string
}

const HTTPS_PROTOCOL = 'https:'

function parseTrustedOrigin(currentOrigin: string): URL | null {
  if (typeof currentOrigin !== 'string') return null

  try {
    const url = new URL(currentOrigin)
    if (url.protocol !== HTTPS_PROTOCOL || url.username || url.password) return null
    return url
  } catch {
    return null
  }
}

function isSupportedImageUrl(url: string): boolean {
  const isRootRelative = url.startsWith('/') && !url.startsWith('//')
  const isAbsoluteHttps = /^https:\/\//i.test(url)
  return isRootRelative || isAbsoluteHttps
}

export function createSafeImageSource(item: GatewayImageSource, currentOrigin: string): string {
  if (!item || typeof item !== 'object') return ''
  if (typeof item.b64_json === 'string' && item.b64_json) {
    return `data:image/png;base64,${item.b64_json}`
  }
  if (typeof item.url !== 'string' || !isSupportedImageUrl(item.url)) return ''

  const trustedOrigin = parseTrustedOrigin(currentOrigin)
  if (!trustedOrigin) return ''

  try {
    const imageUrl = new URL(item.url, trustedOrigin.origin)
    if (
      imageUrl.protocol !== HTTPS_PROTOCOL
      || imageUrl.origin !== trustedOrigin.origin
      || imageUrl.username
      || imageUrl.password
    ) {
      return ''
    }
    return imageUrl.toString()
  } catch {
    return ''
  }
}
