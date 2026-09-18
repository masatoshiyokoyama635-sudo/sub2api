export type CodexTurnStateMode = 'off' | 'observe' | 'reuse'

export const DEFAULT_CODEX_TURN_STATE_LENGTHS = '292, 332'

export function parseCodexTurnStateLengths(value: string): number[] | null {
  if (!value.trim()) return []
  const items = value.trim().split(/[,，\s]+/)
  if (items.some((item) => !/^\d+$/.test(item))) return null
  const lengths = [...new Set(items.map(Number))]
  if (lengths.length > 8 || lengths.some((length) => !Number.isInteger(length) || length < 100 || length > 2048)) {
    return null
  }
  return lengths
}

export function readCodexTurnStateMode(value: unknown): CodexTurnStateMode {
  return value === 'observe' || value === 'reuse' ? value : 'off'
}

export function formatCodexTurnStateLengths(value: unknown): string {
  if (!Array.isArray(value) || value.some((length) => typeof length !== 'number' || !Number.isInteger(length))) {
    return DEFAULT_CODEX_TURN_STATE_LENGTHS
  }
  const text = value.join(', ')
  return parseCodexTurnStateLengths(text)?.join(', ') ?? DEFAULT_CODEX_TURN_STATE_LENGTHS
}
