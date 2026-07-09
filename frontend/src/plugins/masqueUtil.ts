export function buildMasqueConfig(endpoint: any): string {
  const yamlStr = (value: any) => JSON.stringify(String(value ?? ''))
  const network = String(endpoint.network ?? 'quic')
  const server = String(endpoint.server ?? '').trim()
  const sni = String(endpoint.sni ?? '').trim() || (isIpLiteral(server) ? '' : server)
  const handshakeTimeout = Number(endpoint.handshake_timeout ?? 30)
  const dnsServers = ['1.1.1.1', '8.8.8.8']
  const fields = [
    `name: ${yamlStr(endpoint.tag ?? 'masque')}`,
    `type: masque`,
    `server: ${yamlStr(server)}`,
    `port: ${endpoint.port ?? 443}`,
    `network: ${yamlStr(network)}`,
    `private-key: ${yamlStr(endpoint.private_key ?? '')}`,
    `public-key: ${yamlStr(endpoint.public_key ?? '')}`,
    `congestion-controller: ${yamlStr(endpoint.congestion_controller ?? 'bbr')}`,
    `cwnd: ${Number(endpoint.cwnd ?? 8)}`,
    `bbr-profile: ${yamlStr(endpoint.bbr_profile ?? 'standard')}`,
  ]
  if (endpoint.ip) {
    fields.push(`ip: ${yamlStr(endpoint.ip)}`)
  }
  if (endpoint.mtu) {
    fields.push(`mtu: ${endpoint.mtu}`)
  }
  if (endpoint.udp !== undefined) {
    fields.push(`udp: ${endpoint.udp ? 'true' : 'false'}`)
  }
  if (sni) {
    fields.push(`sni: ${yamlStr(sni)}`)
  }
  if (Number.isFinite(handshakeTimeout) && handshakeTimeout > 0) {
    fields.push(`handshake-timeout: ${handshakeTimeout}`)
  }
  fields.push(`remote-dns-resolve: true`)
  fields.push(`dns: [${dnsServers.map(yamlStr).join(', ')}]`)
  return `- { ${fields.join(', ')} }`
}

function isIpLiteral(host: string): boolean {
  const normalized = host.replace(/^\[|\]$/g, '')
  return /^(\d{1,3}\.){3}\d{1,3}$/.test(normalized) || /^[0-9a-fA-F:]+$/.test(normalized)
}
