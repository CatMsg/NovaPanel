import { Dial } from "./dial"

export const EpTypes = {
  Wireguard: 'wireguard',
  Warp: 'warp',
  Tailscale: 'tailscale',
  Masque: 'masque',
  Mieru: 'mieru',
}

type EpType = typeof EpTypes[keyof typeof EpTypes]

interface EndpointBasics {
  id: number
  type: EpType
  tag: string
}

export interface WgPeer {
  address: string
  port: number
  public_key: string
  pre_shared_key?: string
  allowed_ips?: string[]
  persistent_keepalive_interval?: number
  reserved?: number[]
}

export interface WireGuard extends EndpointBasics, Dial {
  system?: boolean
  name?: string
  mtu?: number
  address: string[]
  private_key: string
  listen_port: number
  peers: WgPeer[]
  udp_timeout?: string
  workers?: number
  ext: any
}

export interface Warp extends WireGuard {}

export interface Tailscale extends EndpointBasics, Dial {
  state_directory?: string
  auth_key?: string
  control_url?: string
  ephemeral?: boolean
  hostname?: string
  accept_routes?: boolean
  exit_node?: string
  exit_node_allow_lan_access?: boolean
  advertise_routes?: string[]
  advertise_exit_node?: boolean
  relay_server_port?: number
  relay_server_static_endpoints?: string[]
  system_interface?: boolean
  system_interface_name?: string
  system_interface_mtu?: number
  udp_timeout?: string
}

export interface Masque extends EndpointBasics {
  server: string
  port: number
  network: 'quic'
  private_key: string
  public_key: string
  sni?: string
  keepalive?: number
  remote_dns_resolve?: boolean
  ip: string
  mtu?: number
  udp?: boolean
}

export interface Mieru extends EndpointBasics {
  server: string
  port: number
  port_range?: string
  transport: 'TCP' | 'UDP'
  username: string
  password: string
  multiplexing: 'MULTIPLEXING_OFF' | 'MULTIPLEXING_LOW' | 'MULTIPLEXING_MIDDLE' | 'MULTIPLEXING_HIGH'
  handshake_mode: 'HANDSHAKE_STANDARD' | 'HANDSHAKE_NO_WAIT'
  traffic_pattern: 'DEFAULT' | 'BALANCED' | 'ENHANCED'
  quota_1d_gb: number
  quota_30d_gb: number
  mtu: number
}

// Create interfaces dynamically based on EpTypes keys
type InterfaceMap = {
  [Key in keyof typeof EpTypes]: {
    type: string
    [otherProperties: string]: any // You can add other properties as needed
  }
}

// Create union type from InterfaceMap
export type Endpoint = InterfaceMap[keyof InterfaceMap]

// Create defaultValues object dynamically
const defaultValues: Record<EpType, Endpoint> = {
  wireguard: { type: EpTypes.Wireguard, address: ['10.0.0.2/32','fe80::2/128'], private_key: '', listen_port: 0 },
  warp: { type: EpTypes.Warp, address: [], private_key: '', listen_port: 0, mtu: 1420, peers: [{ address: '', port: 0, public_key: ''}] },
  tailscale: { type: EpTypes.Tailscale, domain_resolver: 'local' },
  masque: { type: EpTypes.Masque, server: '', port: 443, network: 'quic', private_key: '', public_key: '', sni: '', keepalive: 25, remote_dns_resolve: false, ip: '', mtu: 1380, udp: true },
  mieru: { type: EpTypes.Mieru, server: '', port: 0, port_range: '', transport: 'TCP', username: '', password: '', multiplexing: 'MULTIPLEXING_LOW', handshake_mode: 'HANDSHAKE_STANDARD', traffic_pattern: 'DEFAULT', quota_1d_gb: 0, quota_30d_gb: 0, mtu: 1400 },
}

export function createEndpoint<T extends Endpoint>(type: string,json?: Partial<T>): Endpoint {
  const defaultObject: Endpoint = { ...defaultValues[type], ...(json || {}) }
  return defaultObject
}
