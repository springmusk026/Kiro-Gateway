// Copies the static console build (dist/client) into ../web, which the Go
// binary serves under /admin. The previous contents of ../web are replaced.
import { cpSync, existsSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const src = join(root, 'dist', 'client')
const dest = join(root, '..', 'web')
if (!existsSync(join(src, '_shell.html'))) {
  console.error('build output missing: run `vite build` first')
  process.exit(1)
}
rmSync(dest, { recursive: true, force: true })
mkdirSync(dest, { recursive: true })
cpSync(src, dest, { recursive: true })
console.log(`published console to ${dest}`)
