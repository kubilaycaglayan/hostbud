/* global console */
// hostbud-e2e-ctl: lets the Playwright runner (which has no Docker access)
// trigger a fixed set of failure scenarios on the throwaway e2e stack.
// Reachable only on the hostbud-e2e network. No arguments are accepted:
// each route runs one hard-coded command.
import { execFile } from 'node:child_process'
import { createServer } from 'node:http'

const actions = {
  'POST /restart-app': ['docker', ['restart', '--time', '5', 'hostbud-e2e-app']],
  'POST /sshd/stop': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'stop']],
  'POST /sshd/start': ['docker', ['exec', 'hostbud-e2e-target', '/usr/local/bin/sshd-ctl.sh', 'start']],
  // A network cut: the app's TCP connections hang (no close), as when a
  // phone's Wi-Fi drops. Restore keeps the alias the Caddyfile proxies to.
  'POST /network/cut': ['docker', ['network', 'disconnect', 'hostbud-e2e', 'hostbud-e2e-app']],
  'POST /network/restore': ['docker', ['network', 'connect', '--alias', 'hostbud', 'hostbud-e2e', 'hostbud-e2e-app']],
}

createServer((req, res) => {
  if (req.method === 'GET' && req.url === '/health') {
    res.writeHead(200).end('ok\n')
    return
  }
  const action = actions[`${req.method} ${req.url}`]
  if (!action) {
    res.writeHead(404).end('unknown action\n')
    return
  }
  execFile(action[0], action[1], { timeout: 120_000 }, (err, stdout, stderr) => {
    if (err) {
      res.writeHead(500).end(`${err.message}\n${stderr}`)
      return
    }
    res.writeHead(200).end(stdout)
  })
}).listen(8090, () => console.log('hostbud-e2e-ctl listening on :8090'))
