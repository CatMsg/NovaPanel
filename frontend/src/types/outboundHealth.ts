export type HealthStatus = 'healthy' | 'unhealthy' | 'untested'
export type FailoverRole = 'current' | 'candidate' | 'member'

export interface OutboundHealthSnapshot {
  tag: string
  status: HealthStatus
  latestDelay: number
  averageDelay: number
  observedAvailability: number
  samples: number
  successes: number
  failures: number
  lastChecked?: string
  lastSuccess?: string
  lastError?: string
  publicIp?: string
  countryCode?: string
  colo?: string
  identityUpdatedAt?: string
}

export interface FailoverPolicy {
  tag: string
  members: string[]
  testUrl: string
  intervalSeconds: number
  failureThreshold: number
  recoveryThreshold: number
  enabled: boolean
}

export interface FailoverProbe {
  tag: string
  ok: boolean
  delay: number
  error?: string
}

export interface FailoverStatus {
  policy: FailoverPolicy
  current: string
  candidate?: string
  pending: number
  lastChecked?: string
  lastSwitch?: string
  error?: string
  probes: FailoverProbe[]
}

export interface FailoverRelation {
  key: string
  policy: string
  role: FailoverRole
}

export interface OutboundHealthCard {
  tag: string
  health: OutboundHealthSnapshot
  relations: FailoverRelation[]
}
