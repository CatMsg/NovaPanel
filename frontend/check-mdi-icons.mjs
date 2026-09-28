import { readFileSync, readdirSync } from 'node:fs'
import { extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('.', import.meta.url))
const errors = []

function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return sourceFiles(path)
    return ['.vue', '.ts', '.tsx', '.js'].includes(extname(path)) ? [path] : []
  })
}

function iconMap(css, label) {
  const icons = new Map()
  const selectors = /\.(mdi-[a-z0-9-]+)::before\s*\{\s*content:\s*"\\(F[0-9A-F]+)";\s*\}/g
  for (const [, name, codepoint] of css.matchAll(selectors)) {
    if (icons.has(name)) errors.push(`${label}: duplicate ${name}`)
    icons.set(name, codepoint)
  }
  return icons
}

const sourceIcons = new Set(sourceFiles(join(root, 'src')).flatMap(file =>
  readFileSync(file, 'utf8').match(/\bmdi-[a-z0-9-]+\b/g) ?? [],
))
if (sourceIcons.size === 0) errors.push('No application icons found')
const mappedIcons = iconMap(readFileSync(join(root, 'src/styles/materialdesignicons.css'), 'utf8'), 'curated CSS')
const fontIcons = iconMap(readFileSync(join(root, 'node_modules/@mdi/font/css/materialdesignicons.css'), 'utf8'), '@mdi/font')

for (const name of [...sourceIcons].sort()) {
  if (!fontIcons.has(name)) errors.push(`${name}: absent from @mdi/font`)
  if (!mappedIcons.has(name)) errors.push(`${name}: missing from curated CSS`)
}
for (const [name, codepoint] of mappedIcons) {
  if (fontIcons.get(name) !== codepoint) errors.push(`${name}: codepoint differs from @mdi/font`)
}

if (errors.length) {
  console.error(errors.join('\n'))
  process.exitCode = 1
} else {
  console.log(`Verified ${sourceIcons.size} application icons against @mdi/font and curated CSS`)
}
