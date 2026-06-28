export function buildMasqueConfig(endpoint: any): string {
  const yamlStr = (value: any) => JSON.stringify(String(value ?? ''))
  const lines = [
    `- name: ${yamlStr(endpoint.tag ?? 'masque')}`,
    `  type: masque`,
    `  server: ${yamlStr(endpoint.server ?? '')}`,
    `  port: ${endpoint.port ?? 443}`,
    `  network: ${yamlStr(endpoint.network ?? 'quic')}`,
    `  private-key: ${yamlStr(endpoint.private_key ?? '')}`,
    `  public-key: ${yamlStr(endpoint.public_key ?? '')}`,
    `  ip: ${yamlStr(endpoint.ip ?? '')}`,
  ]

  if (endpoint.ipv6) {
    lines.push(`  ipv6: ${yamlStr(endpoint.ipv6)}`)
  }
  if (endpoint.mtu) {
    lines.push(`  mtu: ${endpoint.mtu}`)
  }
  if (endpoint.udp !== undefined) {
    lines.push(`  udp: ${endpoint.udp ? 'true' : 'false'}`)
  }
  return lines.join('\n')
}
