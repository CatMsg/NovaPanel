export function appendUrlPathSegment(baseUrl: string, segment: string): string {
  const value = baseUrl.trim()
  const normalizedSegment = segment.replace(/^\/+|\/+$/g, '')
  if (!value || !normalizedSegment) return ''

  const suffixIndex = value.search(/[?#]/)
  const base = suffixIndex < 0 ? value : value.slice(0, suffixIndex)
  const suffix = suffixIndex < 0 ? '' : value.slice(suffixIndex)
  const rooted = base.startsWith('/')
  const trimmedBase = base.replace(/\/+$/, '')
  const pathBase = trimmedBase || (rooted ? '' : base)
  return `${pathBase}/${normalizedSegment}${suffix}`
}
