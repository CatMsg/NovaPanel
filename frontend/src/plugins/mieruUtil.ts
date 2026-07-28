export function buildMieruConfig(endpoint: any): string {
  const yamlStr = (value: any) => JSON.stringify(String(value ?? ''))
  const transport = String(endpoint.transport ?? 'TCP').toUpperCase()
  const fields = [
    `name: ${yamlStr(endpoint.tag ?? 'mieru')}`,
    'type: mieru',
    `server: ${yamlStr(String(endpoint.server ?? '').trim())}`,
  ]
  const portRange = String(endpoint.port_range ?? '').trim()
  if (portRange) {
    fields.push(`port-range: ${yamlStr(portRange)}`)
  } else {
    fields.push(`port: ${Number(endpoint.port ?? 0)}`)
  }
  fields.push(
    `transport: ${yamlStr(transport)}`,
    `username: ${yamlStr(endpoint.username ?? '')}`,
    `password: ${yamlStr(endpoint.password ?? '')}`,
    `multiplexing: ${yamlStr(endpoint.multiplexing ?? 'MULTIPLEXING_LOW')}`,
    `handshake-mode: ${yamlStr(endpoint.handshake_mode ?? 'HANDSHAKE_STANDARD')}`,
  )
  return `- { ${fields.join(', ')} }`
}
