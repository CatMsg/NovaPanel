export function buildMasqueConfig(endpoint: any): string {
  const yamlStr = (value: any) => JSON.stringify(String(value ?? ''))
  const network = String(endpoint.network ?? 'quic')
  const dnsServers = ['1.1.1.1', '8.8.8.8']
  const fields = [
    `name: ${yamlStr(endpoint.tag ?? 'masque')}`,
    `type: masque`,
    `server: ${yamlStr(endpoint.server ?? '')}`,
    `port: ${endpoint.port ?? 443}`,
    `network: ${yamlStr(network)}`,
    `private-key: ${yamlStr(endpoint.private_key ?? '')}`,
    `public-key: ${yamlStr(endpoint.public_key ?? '')}`,
    `congestion-controller: ${yamlStr(endpoint.congestion_controller ?? 'bbr')}`,
    `cwnd: ${Number(endpoint.cwnd ?? 32)}`,
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
  fields.push(`remote-dns-resolve: true`)
  fields.push(`dns: [${dnsServers.map(yamlStr).join(', ')}]`)
  return `- { ${fields.join(', ')} }`
}
