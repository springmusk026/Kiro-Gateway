import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { createWriteStream, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { request, type FullConfig } from '@playwright/test'
import { ADMIN_PASSWORD, DEAD_PROXY, envKey } from './env'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')

function freePort(): Promise<number> {
  return new Promise((ok, fail) => {
    const srv = createServer()
    srv.once('error', fail)
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address()
      srv.close(() => (addr && typeof addr === 'object' ? ok(addr.port) : fail(new Error('no port'))))
    })
  })
}

/** Environment for the gateway: inherited, minus anything that could send traffic elsewhere. */
function gatewayEnv(extra: Record<string, string>): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...process.env }
  for (const k of Object.keys(env)) {
    if (/^(https?|all|no)_proxy$/i.test(k) || k.startsWith('KIRO_') || k === 'DATABASE_URL' || k === 'ADMIN_PASSWORD' || k === 'CONFIG_PATH') {
      delete env[k]
    }
  }
  return { ...env, ADMIN_PASSWORD, LOG_LEVEL: 'warn', ...extra }
}

async function waitHealthy(url: string, proc: ChildProcess, logPath: string) {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (proc.exitCode !== null) break
    try {
      const res = await fetch(url + '/health')
      if (res.ok) return
    } catch {
      /* not listening yet */
    }
    await new Promise((r) => setTimeout(r, 100))
  }
  const log = existsSync(logPath) ? readFileSync(logPath, 'utf8').slice(-4000) : ''
  throw new Error(`gateway at ${url} did not become healthy (exit code ${proc.exitCode})\n${log}`)
}

/** Signs in through the admin API and saves the session cookie for the tests. */
async function saveSession(baseURL: string, path: string) {
  const ctx = await request.newContext({ baseURL })
  const res = await ctx.post('/admin/api/session', { data: { password: ADMIN_PASSWORD, remember: true } })
  if (!res.ok()) throw new Error(`sign-in failed: ${res.status()} ${await res.text()}`)
  await ctx.storageState({ path })
  await ctx.dispose()
}

export default async function globalSetup(config: FullConfig) {
  if (!existsSync(join(repoRoot, 'web', '_shell.html'))) {
    throw new Error('web/_shell.html is missing: run `npm run build` in frontend/ first (it publishes the console into web/)')
  }
  const tmp = mkdtempSync(join(tmpdir(), 'kiro-e2e-'))
  let bin = process.env.E2E_GATEWAY_BIN
  if (!bin) {
    bin = join(tmp, 'kiro-gateway')
    execFileSync('go', ['build', '-o', bin, '.'], { cwd: repoRoot, stdio: 'inherit' })
  }

  const procs: ChildProcess[] = []
  const projects = config.projects.map((p) => p.name)

  for (const name of projects) {
    const dir = join(tmp, name)
    const port = await freePort()
    const settings = { port, host: '127.0.0.1', proxyURL: DEAD_PROXY }
    const extra: Record<string, string> = {}

    if (name === 'postgres') {
      const dbURL = process.env.E2E_DATABASE_URL!
      // Replace whatever a previous run left (settings, accounts, API keys)
      // with a known configuration, then run without a config.json next to
      // the database so the console's migration prompt stays away.
      const seed = join(tmp, 'postgres-seed.json')
      writeFileSync(seed, JSON.stringify(settings))
      execFileSync(bin, ['migrate', '--from', seed, '--force'], { cwd: repoRoot, env: gatewayEnv({ DATABASE_URL: dbURL }), stdio: 'inherit' })
      extra.DATABASE_URL = dbURL
      extra.KIRO_AUTO_IMPORT = '0'
    } else {
      mkdirSync(dir, { recursive: true })
      writeFileSync(join(dir, 'config.json'), JSON.stringify(settings))
    }

    const logPath = join(tmp, `${name}.log`)
    const log = createWriteStream(logPath)
    const proc = spawn(bin, [], {
      cwd: repoRoot, // the console is served from ./web
      env: gatewayEnv({ CONFIG_PATH: join(dir, 'config.json'), ...extra }),
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    proc.stdout!.pipe(log)
    proc.stderr!.pipe(log)
    procs.push(proc)

    const url = `http://127.0.0.1:${port}`
    await waitHealthy(url, proc, logPath)
    const state = join(tmp, `${name}.state.json`)
    await saveSession(url, state)
    process.env[envKey('URL', name)] = url
    process.env[envKey('STATE', name)] = state
    process.env[envKey('LOG', name)] = logPath
  }

  return async () => {
    await Promise.all(
      procs.map(
        (p) =>
          new Promise<void>((done) => {
            if (p.exitCode !== null) return done()
            const kill = setTimeout(() => p.kill('SIGKILL'), 5_000)
            p.once('exit', () => {
              clearTimeout(kill)
              done()
            })
            p.kill('SIGTERM')
          }),
      ),
    )
    if (!process.env.E2E_KEEP_TMP) rmSync(tmp, { recursive: true, force: true })
  }
}
