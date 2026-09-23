export type FleetTrafficBudget = {
  enabled: boolean
  supported: boolean
  limitBytes: number
  reserveBytes?: number
  clientPoolBytes?: number
  usedBytes: number
  meteredRxBytes: number
  meteredTxBytes: number
  offsetBytes: number
  accountingMode: string
  level: string
  blocked: boolean
  error?: string
}

export type TrafficBudgetHistorySample = {
  id?: number
  dateTime: number
  periodStart: number
  meteredRxBytes: number
  meteredTxBytes: number
  usedBytes: number
  clientPoolBytes: number
  level: string
}

export type TrafficBudgetHistory = {
  periodStart?: string
  periodEnd?: string
  samples: TrafficBudgetHistorySample[]
}

export const trafficBudgetCap = (budget?: FleetTrafficBudget) => {
  if (!budget) return 0
  const clientPool = Number(budget.clientPoolBytes)
  if (Number.isFinite(clientPool) && clientPool > 0) return clientPool
  const providerLimit = Number(budget.limitBytes)
  return Number.isFinite(providerLimit) && providerLimit > 0 ? providerLimit : 0
}

export const trafficBudgetPercent = (budget?: FleetTrafficBudget) => {
  const cap = trafficBudgetCap(budget)
  if (!budget || cap <= 0) return 0
  return Math.min(100, Math.max(0, Number(budget.usedBytes) / cap * 100))
}

export const trafficBudgetReadable = (budget?: FleetTrafficBudget) => {
  return Boolean(budget?.enabled && budget.supported && budget.level !== 'error' && !budget.error && trafficBudgetCap(budget) > 0)
}

export const trafficHistorySeries = (history?: TrafficBudgetHistory | null) => {
  const samples = [...(history?.samples ?? [])].sort((left, right) => left.dateTime - right.dateTime)
  return {
    labels: samples.map(sample => sample.dateTime * 1000),
    used: samples.map(sample => Number(sample.usedBytes) || 0),
    rx: samples.map(sample => Number(sample.meteredRxBytes) || 0),
    tx: samples.map(sample => Number(sample.meteredTxBytes) || 0),
  }
}
