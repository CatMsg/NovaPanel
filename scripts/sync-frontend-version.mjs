import { readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..')
const versionPath = path.join(repoRoot, 'config', 'version')
const frontendDir = path.join(repoRoot, 'frontend')
const packageJsonPath = path.join(frontendDir, 'package.json')
const packageLockPath = path.join(frontendDir, 'package-lock.json')

const packageJsonRaw = await readFile(packageJsonPath, 'utf8')
const packageLockRaw = await readFile(packageLockPath, 'utf8')

const version = (await readFile(versionPath, 'utf8')).trim()
if (!version) {
  throw new Error(`empty version file: ${versionPath}`)
}

const packageJson = JSON.parse(packageJsonRaw)
packageJson.version = version

const packageLock = JSON.parse(packageLockRaw)
packageLock.version = version
if (!packageLock.packages || !packageLock.packages['']) {
  throw new Error(`package-lock root entry missing: ${packageLockPath}`)
}
packageLock.packages[''].version = version

await writeFile(packageJsonPath, `${JSON.stringify(packageJson, null, 2)}\r\n`)
await writeFile(packageLockPath, `${JSON.stringify(packageLock, null, 2)}\r\n`)

console.log(`synchronized frontend version to ${version}`)
