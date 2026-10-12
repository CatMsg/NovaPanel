export type HttpVersionIssue =
  | 'tlsRequired'
  | 'outboundTlsRequired'
  | 'http2OnlyRequiresTls'
  | 'singleVersionRequired'
  | 'pathHostRequiresHttp1'
  | 'pathAndHostTogether'
  | 'invalidVersion'

export interface HttpInboundLike {
  version?: unknown
  tls_id?: unknown
  out_json?: Record<string, unknown> | null
}

const supportedVersions = new Set([1, 2, 3])

export function normalizeHttpVersions(value: unknown, fallback: number[] = []): unknown[] {
  const candidates = Array.isArray(value) ? value : value === undefined || value === null ? fallback : [value]
  return [...candidates]
}

export function effectiveInboundHttpVersions(inbound: Pick<HttpInboundLike, 'version'>): unknown[] {
  return normalizeHttpVersions(inbound.version)
}

function validVersion(value: unknown, allowAuto = false): value is number {
  return typeof value === 'number' && Number.isInteger(value) &&
    (supportedVersions.has(value) || (allowAuto && value === 0))
}

export function selectedInboundHttpVersion(value: unknown): number | undefined {
  const candidate = Array.isArray(value) ? value.length === 1 ? value[0] : undefined : value
  return validVersion(candidate) ? candidate : undefined
}

export function requiresHttpVersionSelection(value: unknown): boolean {
  return value === undefined || value === null || (Array.isArray(value) && value.length !== 1)
}

export function hasHttpPathOrHost(options: Record<string, unknown> | null | undefined): boolean {
  if (!options) return false
  if (typeof options.path === 'string' && options.path.length > 0) return true
  const headers = options.headers
  if (typeof headers !== 'object' || headers === null || Array.isArray(headers)) return false
  const hostEntry = Object.entries(headers).find(([key]) => key.trim().toLowerCase() === 'host')
  if (!hostEntry) return false
  const value = hostEntry[1]
  if (typeof value === 'string') return value !== ''
  return Array.isArray(value) && typeof value[0] === 'string' && value[0] !== ''
}

export function hasHttpPathAndHost(options: Record<string, unknown> | null | undefined): boolean {
  if (!options) return false
  const hasPath = typeof options.path === 'string' && options.path.length > 0
  return hasPath && hasHttpPathOrHost({ headers: options.headers })
}

export function httpOutboundIssue(outbound: Record<string, unknown>): HttpVersionIssue | undefined {
  const version = outbound.version === undefined || outbound.version === null ? 0 : outbound.version
  if (!validVersion(version, true)) return 'invalidVersion'
  if (hasHttpPathAndHost(outbound)) return 'pathAndHostTogether'
  if ((version === 2 || version === 3) && hasHttpPathOrHost(outbound)) return 'pathHostRequiresHttp1'
  const tlsEnabled = typeof outbound.tls === 'object' && outbound.tls !== null &&
    (outbound.tls as Record<string, unknown>).enabled === true
  if (!tlsEnabled && version === 3) return 'tlsRequired'
  if (!tlsEnabled && version === 2 && outbound.disable_version_fallback === true) return 'outboundTlsRequired'
  return undefined
}

export function hasEnabledInboundTls(inbound: Pick<HttpInboundLike, 'tls_id'>, tlsConfigs: Array<Record<string, any>>): boolean {
  const tlsId = Number(inbound.tls_id ?? 0)
  const selectedTls = tlsConfigs.find(config => Number(config.id) === tlsId)
  return tlsId > 0 && selectedTls?.server?.enabled === true
}

export function httpClientIssue(
  inbound: HttpInboundLike,
): HttpVersionIssue | undefined {
  const version = selectedInboundHttpVersion(inbound.version)
  if (version === undefined) return undefined
  const client = inbound.out_json
  if (!client) return undefined
  if (hasHttpPathAndHost(client)) return 'pathAndHostTogether'
  if ((version === 2 || version === 3) && hasHttpPathOrHost(client)) return 'pathHostRequiresHttp1'
  return undefined
}

export function synchronizeHttpInboundClientVersion(inbound: HttpInboundLike): boolean {
  const version = selectedInboundHttpVersion(inbound.version)
  if (version === undefined) return false

  const client = inbound.out_json ?? {}
  client.version = version
  if (version === 2) client.disable_version_fallback = true
  else delete client.disable_version_fallback
  inbound.out_json = client
  return true
}

export function httpInboundIssue(
  inbound: HttpInboundLike,
  tlsConfigs: Array<Record<string, any>>,
): HttpVersionIssue | undefined {
  const versions = effectiveInboundHttpVersions(inbound)
  if (versions.some(version => !validVersion(version))) return 'invalidVersion'
  if (versions.length !== 1) return 'singleVersionRequired'

  const version = versions[0]
  if (version === 3 && !hasEnabledInboundTls(inbound, tlsConfigs)) return 'tlsRequired'
  if (version === 2 && !hasEnabledInboundTls(inbound, tlsConfigs)) return 'http2OnlyRequiresTls'
  return httpClientIssue(inbound)
}
