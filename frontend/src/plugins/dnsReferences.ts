import { i18n } from '@/locales'
import { push } from 'notivue'

export type DnsResolverField = 'domain_resolver' | 'default_domain_resolver' | 'stun_domain_resolver'
export type DnsExternalStore = 'inbounds' | 'outbounds' | 'endpoints' | 'services'

export type DnsTagReference =
  | { kind: 'default-dns' }
  | { kind: 'dns-rule'; indexPath: number[] }
  | { kind: 'route-resolve-rule'; indexPath: number[] }
  | { kind: 'config-resolver'; field: DnsResolverField; path: string }
  | { kind: 'external-resolver'; store: DnsExternalStore; tag: string; field: DnsResolverField; path: string }

export type DnsReferenceStores = Partial<Record<DnsExternalStore, unknown[]>>

const resolverFields = new Set<DnsResolverField>([
  'domain_resolver',
  'default_domain_resolver',
  'stun_domain_resolver',
])

const optionContainers = new Set([
  'servers',
  'rules',
  'rule_set',
  'tls',
  'reality',
  'handshake',
  'realm',
  'http_client',
  'acme',
  'certificate_provider',
  'control_dialer',
  'tunnel_dialer',
  'mesh_with',
  'verify_client_url',
  'dashboard',
])

const configResolverRoots = ['dns', 'route', 'ntp', 'http_clients', 'certificate_providers', 'inbounds', 'outbounds', 'endpoints', 'services'] as const
const dnsRuleServerActions = new Set(['route', 'evaluate'])
const dnsTagConditionFields = ['dns_server_address', 'dns_search_domain'] as const

export class DnsTagRenameCollisionError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'DnsTagRenameCollisionError'
  }
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const isResolverField = (key: string): key is DnsResolverField =>
  resolverFields.has(key as DnsResolverField)

const childPath = (path: string, key: string | number): string => {
  if (typeof key === 'number') return `${path}[${key}]`
  return path ? `${path}.${key}` : key
}

type ResolverVisitor = (reference: {
  field: DnsResolverField
  path: string
  value: string
  replace: (next: string) => void
}) => void

function visitKnownOptionFields(value: unknown, visit: ResolverVisitor, path = '', ancestors = new WeakSet<object>()): void {
  if (Array.isArray(value)) {
    if (ancestors.has(value)) return
    ancestors.add(value)
    value.forEach((item, index) => visitKnownOptionFields(item, visit, childPath(path, index), ancestors))
    ancestors.delete(value)
    return
  }
  if (!isRecord(value) || ancestors.has(value)) return

  ancestors.add(value)
  for (const [key, child] of Object.entries(value)) {
    const propertyPath = childPath(path, key)
    if (isResolverField(key)) {
      if (typeof child === 'string') {
        visit({ field: key, path: propertyPath, value: child, replace: next => { value[key] = next } })
      } else if (isRecord(child) && typeof child.server === 'string') {
        visit({ field: key, path: childPath(propertyPath, 'server'), value: child.server, replace: next => { child.server = next } })
      }
    }
    if (optionContainers.has(key)) visitKnownOptionFields(child, visit, propertyPath, ancestors)
    if (key === 'handshake_for_server_name' && isRecord(child)) {
      for (const [name, handshake] of Object.entries(child)) {
        visitKnownOptionFields(handshake, visit, `${propertyPath}[${JSON.stringify(name)}]`, ancestors)
      }
    }
  }
  ancestors.delete(value)
}

function visitConfigResolverFields(config: Record<string, unknown>, visit: ResolverVisitor): void {
  for (const root of configResolverRoots) {
    if (Object.hasOwn(config, root)) visitKnownOptionFields(config[root], visit, root)
  }
}

function visitRules(
  rules: unknown,
  visit: (rule: Record<string, unknown>, indexPath: number[]) => void,
  parentPath: number[] = [],
  ancestors = new WeakSet<object>(),
): void {
  if (!Array.isArray(rules) || ancestors.has(rules)) return
  ancestors.add(rules)
  rules.forEach((value, index) => {
    if (!isRecord(value) || ancestors.has(value)) return
    ancestors.add(value)
    const indexPath = [...parentPath, index + 1]
    visit(value, indexPath)
    if (Array.isArray(value.rules)) visitRules(value.rules, visit, indexPath, ancestors)
    ancestors.delete(value)
  })
  ancestors.delete(rules)
}

function hasDnsTagCondition(rule: Record<string, unknown>, tag: string): boolean {
  return dnsTagConditionFields.some(field => {
    const map = rule[field]
    return isRecord(map) && Object.hasOwn(map, tag)
  })
}

function renameDnsTagCondition(rule: Record<string, unknown>, oldTag: string, newTag: string): number {
  let updated = 0
  for (const field of dnsTagConditionFields) {
    const map = rule[field]
    if (!isRecord(map) || !Object.hasOwn(map, oldTag)) continue

    map[newTag] = map[oldTag]
    delete map[oldTag]
    updated++
  }
  return updated
}

type DnsTagConditionCollision = {
  scope: 'DNS' | 'route'
  field: typeof dnsTagConditionFields[number]
  indexPath: number[]
}

function findDnsTagConditionCollision(
  config: Record<string, unknown>,
  oldTag: string,
  newTag: string,
): DnsTagConditionCollision | undefined {
  let collision: DnsTagConditionCollision | undefined
  const inspectRules = (rules: unknown, scope: 'DNS' | 'route') => {
    visitRules(rules, (rule, indexPath) => {
      if (collision) return
      for (const field of dnsTagConditionFields) {
        const map = rule[field]
        if (isRecord(map) && Object.hasOwn(map, oldTag) && Object.hasOwn(map, newTag)) {
          collision = { scope, field, indexPath }
          return
        }
      }
    })
  }

  const dns = isRecord(config.dns) ? config.dns : undefined
  const route = isRecord(config.route) ? config.route : undefined
  inspectRules(dns?.rules, 'DNS')
  inspectRules(route?.rules, 'route')
  return collision
}

const externalStores: DnsExternalStore[] = ['inbounds', 'outbounds', 'endpoints', 'services']

export function findDnsTagReferences(
  config: unknown,
  stores: DnsReferenceStores,
  tag: string,
): DnsTagReference[] {
  if (!tag) return []

  const references: DnsTagReference[] = []
  if (!isRecord(config)) return findExternalReferences(stores, tag)

  const dns = isRecord(config.dns) ? config.dns : undefined
  if (dns?.final === tag) references.push({ kind: 'default-dns' })

  visitRules(dns?.rules, (rule, indexPath) => {
    const action = rule.action || 'route'
    if ((dnsRuleServerActions.has(String(action)) && rule.server === tag) || hasDnsTagCondition(rule, tag)) {
      references.push({ kind: 'dns-rule', indexPath })
    }
  })

  const route = isRecord(config.route) ? config.route : undefined
  visitRules(route?.rules, (rule, indexPath) => {
    if ((rule.action === 'resolve' && rule.server === tag) || hasDnsTagCondition(rule, tag)) {
      references.push({ kind: 'route-resolve-rule', indexPath })
    }
  })

  visitConfigResolverFields(config, reference => {
    if (reference.value === tag) {
      references.push({ kind: 'config-resolver', field: reference.field, path: reference.path })
    }
  })

  references.push(...findExternalReferences(stores, tag))
  return references
}

function findExternalReferences(stores: DnsReferenceStores, tag: string): DnsTagReference[] {
  const references: DnsTagReference[] = []
  for (const store of externalStores) {
    const resources = stores[store]
    if (!Array.isArray(resources)) continue
    resources.forEach((resource, index) => {
      const recordTag = isRecord(resource) && typeof resource.tag === 'string' ? resource.tag : ''
      visitKnownOptionFields(resource, reference => {
        if (reference.value === tag) {
          references.push({
            kind: 'external-resolver',
            store,
            tag: recordTag || `#${index + 1}`,
            field: reference.field,
            path: reference.path,
          })
        }
      })
    })
  }
  return references
}

export function renameDnsTagReferences(config: unknown, oldTag: string, newTag: string): number {
  if (!oldTag || !newTag || oldTag === newTag || !isRecord(config)) return 0

  const collision = findDnsTagConditionCollision(config, oldTag, newTag)
  if (collision) {
    const title = i18n.global.t('dns.references.renameBlockedTitle')
    const message = i18n.global.t('dns.references.renameCollisionText', {
      oldTag,
      newTag,
      scope: collision.scope,
      index: collision.indexPath.join('.'),
      field: collision.field,
    })
    push.error({ title, message })
    throw new DnsTagRenameCollisionError(message)
  }

  let updated = 0
  const dns = isRecord(config.dns) ? config.dns : undefined
  if (dns?.final === oldTag) {
    dns.final = newTag
    updated++
  }

  visitRules(dns?.rules, rule => {
    const action = rule.action || 'route'
    if (dnsRuleServerActions.has(String(action)) && rule.server === oldTag) {
      rule.server = newTag
      updated++
    }
    updated += renameDnsTagCondition(rule, oldTag, newTag)
  })

  const route = isRecord(config.route) ? config.route : undefined
  visitRules(route?.rules, rule => {
    if (rule.action === 'resolve' && rule.server === oldTag) {
      rule.server = newTag
      updated++
    }
    updated += renameDnsTagCondition(rule, oldTag, newTag)
  })

  visitConfigResolverFields(config, reference => {
    if (reference.value === oldTag) {
      reference.replace(newTag)
      updated++
    }
  })

  return updated
}
