import { HumanReadable } from '@/plugins/utils'

export const formatSystemValue = (value: unknown, fallback = '-'): string => {
  if (typeof value !== 'string' && typeof value !== 'number') return fallback
  const normalized = String(value).trim()
  return !normalized || normalized.toLowerCase() === 'unknown' ? fallback : normalized
}

const validBootTime = (bootTime: unknown, nowSeconds = Date.now() / 1000): number | undefined => {
  const boot = typeof bootTime === 'number' ? bootTime : Number(bootTime)
  if (!Number.isFinite(boot) || !Number.isFinite(nowSeconds) || boot <= 0 || nowSeconds < boot) return undefined
  return boot
}

export const formatSystemUptime = (
  bootTime: unknown,
  nowSeconds = Date.now() / 1000,
): string => {
  const boot = validBootTime(bootTime, nowSeconds)
  if (boot === undefined || !Number.isFinite(nowSeconds)) return '-'
  return HumanReadable.formatSecond(nowSeconds - boot)
}

export const formatSystemStartupTime = (bootTime: unknown, locale?: string): string => {
  const boot = validBootTime(bootTime)
  return boot === undefined ? '-' : new Date(boot * 1000).toLocaleString(locale)
}
