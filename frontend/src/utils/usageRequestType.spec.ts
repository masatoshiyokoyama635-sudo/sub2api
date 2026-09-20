import { describe, expect, it } from 'vitest'
import { numericRequestTypeKind, requestTypeLabelKey } from './errorBadges'
import { isUsageRequestType, requestTypeToLegacyStream, resolveUsageRequestType } from './usageRequestType'

describe('background probe usage type', () => {
  it('preserves probe attribution independently of the streaming transport', () => {
    expect(isUsageRequestType('probe')).toBe(true)
    expect(resolveUsageRequestType({ request_type: 'probe', stream: true })).toBe('probe')
    expect(requestTypeToLegacyStream('probe')).toBeNull()
    expect(numericRequestTypeKind(6, true)).toBe('probe')
    expect(requestTypeLabelKey('probe')).toBe('usage.probe')
  })

  it('keeps legacy user traffic classification', () => {
    expect(resolveUsageRequestType({ stream: true })).toBe('stream')
    expect(resolveUsageRequestType({ stream: false })).toBe('sync')
    expect(requestTypeToLegacyStream('stream')).toBe(true)
  })
})
