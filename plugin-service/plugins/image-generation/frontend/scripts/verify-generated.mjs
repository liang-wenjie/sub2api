import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const files = ['../../web/index.html', '../../web/assets/app.js', '../../web/assets/app.css']

async function hashes() {
  return new Map(await Promise.all(files.map(async relativePath => {
    const path = fileURLToPath(new URL(relativePath, import.meta.url))
    const digest = createHash('sha256').update(await readFile(path)).digest('hex')
    return [relativePath, digest]
  })))
}

const before = await hashes()
const build = spawnSync('npm run build', [], {
  cwd: fileURLToPath(new URL('..', import.meta.url)),
  shell: true,
  stdio: 'inherit',
})
if (build.error) {
  console.error(build.error.message)
  process.exit(1)
}
if (build.status !== 0) process.exit(build.status ?? 1)
const after = await hashes()
const changed = files.filter(file => before.get(file) !== after.get(file))

if (changed.length > 0) {
  console.error(`Generated frontend output was stale: ${changed.join(', ')}`)
  process.exit(1)
}

console.log('Generated frontend output is reproducible.')
