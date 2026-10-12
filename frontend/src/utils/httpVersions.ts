export type HttpVersionIssue =
  | 'tlsRequired'
  | 'outboundTlsRequired'
  | 'http2OnlyRequiresTls'
  | 'clientVersionMismatch'
  | 'pathHostRequiresHttp1'
  | 'pathAndHostTogether'
  | 'invalidVersion'

export interface HttpInboundLike {
  version?: unknown
  tls_id?: unknown
  out_json?: Record<string, unknown> | null
}

const supportedVersions = new Set([1, 2, 3])

export function normalizeHttpVersions(value: unknown, fallback: number[] = [1, 2]): unknown[] {
  const candidates = Array.isArray(value) ? value : value === undefined || value === null ? fallback : [value]
  return Array.from(new Set(candidates.length > 0 ? candidates : fallback))
}

export function effectiveInboundHttpVersions(inbound: Pick<HttpInboundLike, 'version'>): unknown[] {
  return normalizeHttpVersions(inbound.version)
}

function validVersion(value: unknown, allowAuto = false): value is number {
  return typeof value === 'number' && Number.isInteger(value) &&
    (supportedVersions.has(value) || (allowAuto && value === 0))
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
  const versions = effectiveInboundHttpVersions(inbound)
  const invalidVersion = versions.some(version => !validVersion(version))
  if (invalidVersion) return 'invalidVersion'
  const client = inbound.out_json
  if (!client) return undefined
  const clientVersion = client.version === undefined || client.version === null ? 0 : client.version
  if (!validVersion(clientVersion, true)) return 'invalidVersion'
  if (hasHttpPathAndHost(client)) return 'pathAndHostTogether'
  if ((clientVersion === 2 || clientVersion === 3) && hasHttpPathOrHost(client)) return 'pathHostRequiresHttp1'
  if (clientVersion !== 0 && !versions.includes(clientVersion)) return 'clientVersionMismatch'
  return undefined
}

export function httpInboundIssue(
  inbound: HttpInboundLike,
  tlsConfigs: Array<Record<string, any>>,
): HttpVersionIssue | undefined {
  const versions = effectiveInboundHttpVersions(inbound)
  if (versions.some(version => !validVersion(version))) return 'invalidVersion'
  if (versions.includes(3) && !hasEnabledInboundTls(inbound, tlsConfigs)) return 'tlsRequired'
  if (versions.includes(2) && !versions.includes(1) && !hasEnabledInboundTls(inbound, tlsConfigs)) return 'http2OnlyRequiresTls'
  return httpClientIssue(inbound)
}
