// Failure switches on the e2e stack (hostbud-e2e-ctl, see ctl/server.mjs).
async function call(action: string): Promise<void> {
  const res = await fetch(`http://hostbud-e2e-ctl:8090${action}`, { method: 'POST' })
  if (!res.ok) throw new Error(`ctl ${action}: ${res.status} ${await res.text()}`)
}

export const ctl = {
  /** docker restart hostbud-e2e-app */
  restartApp: () => call('/restart-app'),
  /** Stops hostbud-e2e-app, leaving Caddy and the offline app shell available. */
  appStop: () => call('/app/stop'),
  /** Starts hostbud-e2e-app and waits for /api/health through Caddy. */
  appStart: () => call('/app/start'),
  /** Stops sshd on the target, dropping every open connection. */
  stopSshd: () => call('/sshd/stop'),
  startSshd: () => call('/sshd/start'),
  /** Disconnects hostbud-e2e-app from the e2e network (connections hang). */
  cutNetwork: () => call('/network/cut'),
  /** Reconnects it; a no-op when it is connected. */
  restoreNetwork: async () => {
    try {
      await call('/network/restore')
    } catch (e) {
      if (!/already exists/.test(String(e))) throw e
    }
  },
}
