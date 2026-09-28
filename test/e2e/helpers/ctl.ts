// Failure switches on the e2e stack (hostbud-e2e-ctl, see ctl/server.mjs).
async function call(action: string): Promise<void> {
  const res = await fetch(`http://hostbud-e2e-ctl:8090${action}`, { method: 'POST' })
  if (!res.ok) throw new Error(`ctl ${action}: ${res.status} ${await res.text()}`)
}

/** How many app restarts this worker has asked for (see fixtures.ts). */
export const appRestarts = { count: 0 }

export const ctl = {
  /** docker restart hostbud-e2e-app */
  restartApp: () => {
    appRestarts.count++
    return call('/restart-app')
  },
  /** Stops hostbud-e2e-app, leaving Caddy and the offline app shell available. */
  appStop: () => call('/app/stop'),
  /** Starts hostbud-e2e-app and waits for /api/health through Caddy. */
  appStart: () => call('/app/start'),
  /** Restarts hostbud-e2e-app and waits for /api/health through Caddy
   * (V2-M1 restart safety). */
  appRestart: () => {
    appRestarts.count++
    return call('/app/restart')
  },
  /** Restarts hostbud-e2e-app-multi and waits for its /api/health through
   * Caddy (V2-M2 Cap after restart). */
  multiRestart: () => call('/multi/restart'),
  /** Restarts the isolated V2-M5 app and waits for health through Caddy. */
  llmRestart: () => call('/llm/restart'),
  /** Stops sshd on the target, dropping every open connection. */
  stopSshd: () => call('/sshd/stop'),
  startSshd: () => call('/sshd/start'),
  /** Rotates only the disposable target's ed25519 host key. */
  rotateHostKey: () => call('/hostkey/rotate'),
  /** Restores the per-run target host key pinned by hostbud at startup. */
  restoreHostKey: () => call('/hostkey/restore'),
  /** Stalls target tmux commands for at most the switch TTL. */
  stallTmux: () => call('/stall/tmux/on'),
  unstallTmux: () => call('/stall/tmux/off'),
  /** Stalls SFTP startup for a bounded TTL. */
  stallSftp: (ttl = 60) => call(`/stall/sftp/on?ttl=${ttl}`),
  unstallSftp: () => call('/stall/sftp/off'),
  /** Pauses the proxy for a bounded interval (auto-unpauses after ttl). */
  pauseCaddy: (ttl = 30) => call(`/caddy/pause?ttl=${ttl}`),
  unpauseCaddy: () => call('/caddy/unpause'),
  /** Changes the fake tailscaled identity mapping used by the T8 scenarios. */
  tsMap: (mapping: 'allowed' | 'stranger' | 'unknown' | 'error') => call(`/ts/map/${mapping}`),
  restartTSApp: () => call('/ts/restart'),
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
