/* global console, fetch, AbortSignal, setTimeout, clearTimeout */
// hostbud-e2e-ctl: lets the Playwright runner (which has no Docker access)
// trigger a fixed set of failure scenarios on the throwaway e2e stack.
// Reachable only on the hostbud-e2e network. No arguments are accepted:
// each route runs one hard-coded command.
import { execFile } from 'node:child_process'
import { createServer } from 'node:http'
import { URL } from 'node:url'

const actions = {
  'POST /restart-app': ['docker', ['restart', '--time', '5', 'hostbud-e2e-app']],
  'POST /app/stop': ['docker', ['stop', '--time', '5', 'hostbud-e2e-app']],
  'POST /app/start': ['docker', ['start', 'hostbud-e2e-app']],
  'POST /sshd/stop': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'stop']],
  'POST /sshd/start': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'start']],
  'POST /stall/tmux/on': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'stall', 'tmux', '60']],
  'POST /stall/tmux/off': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'stall', 'tmux', 'off']],
  'POST /stall/sftp/on': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'stall', 'sftp', '60']],
  'POST /stall/sftp/off': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'stall', 'sftp', 'off']],
  // A network cut: the app's TCP connections hang (no close), as when a
  // phone's Wi-Fi drops. Restore keeps the alias the Caddyfile proxies to.
  'POST /network/cut': ['docker', ['network', 'disconnect', 'hostbud-e2e', 'hostbud-e2e-app']],
  'POST /network/restore': ['docker', ['network', 'connect', '--alias', 'hostbud', 'hostbud-e2e', 'hostbud-e2e-app']],
}

const caddyPauseTimers = new Map()

createServer((req, res) => {
  if (req.method === 'GET' && req.url === '/health') {
    res.writeHead(200).end('ok\n')
    return
  }
  const url = new URL(req.url ?? '/', 'http://hostbud-e2e-ctl')
  if (req.method === 'POST' && url.pathname === '/caddy/pause') {
    const raw = url.searchParams.get('ttl') ?? '30'
    if (!/^\d+$/.test(raw)) { res.writeHead(400).end('ttl must be seconds from 1 to 60\n'); return }
    const ttl = Math.min(60, Math.max(1, Number(raw)))
    if (caddyPauseTimers.has('timer')) clearTimeout(caddyPauseTimers.get('timer'))
    execFile('docker', ['pause', 'hostbud-e2e-caddy'], { timeout: 10_000 }, (err, stdout, stderr) => {
      if (err) { res.writeHead(500).end(`${err.message}\n${stderr}`); return }
      caddyPauseTimers.set('timer', setTimeout(() => {
        execFile('docker', ['unpause', 'hostbud-e2e-caddy'], { timeout: 10_000 }, () => caddyPauseTimers.delete('timer'))
      }, ttl * 1000))
      res.writeHead(200).end(stdout)
    })
    return
  }
  if (req.method === 'POST' && url.pathname === '/caddy/unpause') {
    if (caddyPauseTimers.has('timer')) clearTimeout(caddyPauseTimers.get('timer'))
    execFile('docker', ['unpause', 'hostbud-e2e-caddy'], { timeout: 10_000 }, (err, stdout, stderr) => {
      caddyPauseTimers.delete('timer')
      if (err) { res.writeHead(500).end(`${err.message}\n${stderr}`); return }
      res.writeHead(200).end(stdout)
    })
    return
  }
  const action = actions[`${req.method} ${url.pathname}`]
  if (!action) {
    res.writeHead(404).end('unknown action\n')
    return
  }
  const args = [...action[1]]
  if (url.pathname.endsWith('/on') && url.pathname.startsWith('/stall/')) {
    const ttl = url.searchParams.get('ttl')
    if (ttl !== null) {
      if (!/^\d+$/.test(ttl)) { res.writeHead(400).end('ttl must be seconds from 1 to 60\n'); return }
      args[args.length - 1] = String(Math.min(60, Math.max(1, Number(ttl))))
    }
  }
  execFile(action[0], args, { timeout: 120_000 }, async (err, stdout, stderr) => {
    if (err) {
      res.writeHead(500).end(`${err.message}\n${stderr}`)
      return
    }
    if (req.url === '/app/start') {
      const deadline = Date.now() + 60_000
      while (Date.now() < deadline) {
        try {
          const health = await fetch('http://hostbud-e2e-caddy:9055/api/health', { signal: AbortSignal.timeout(2_000) })
          if (health.ok) {
            res.writeHead(200).end(stdout)
            return
          }
        } catch { /* retry until the app is healthy through Caddy */ }
        await new Promise((resolve) => setTimeout(resolve, 500))
      }
      res.writeHead(504).end('hostbud-e2e-app did not become healthy through Caddy\n')
      return
    }
    res.writeHead(200).end(stdout)
  })
}).listen(8090, () => console.log('hostbud-e2e-ctl listening on :8090'))
