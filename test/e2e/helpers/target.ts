import { execFile } from 'node:child_process'

// The runner's own ssh client, playing "a real terminal" on the target.
// Keys: the agent socket (SSH_AUTH_SOCK) and /keys/known_hosts, both per run.
const SSH_OPTS = [
  '-o', 'BatchMode=yes',
  '-o', 'StrictHostKeyChecking=yes',
  '-o', 'UserKnownHostsFile=/keys/known_hosts',
  '-o', 'ConnectTimeout=5',
  '-o', 'ControlMaster=auto',
  '-o', 'ControlPath=/tmp/e2e-cm-%C',
  '-o', 'ControlPersist=60',
]

/** Single-quotes s for a POSIX shell. */
export function shq(s: string): string {
  return `'${s.replaceAll("'", `'\\''`)}'`
}

export interface RunResult {
  code: number
  stdout: string
  stderr: string
}

export class Target {
  constructor(readonly host = 'hostbud-e2e-target', readonly user = 'dev') {}

  /** Runs a shell command line on the target; never throws on exit codes. */
  exec(command: string, timeoutMs = 15_000): Promise<RunResult> {
    return new Promise((resolve, reject) => {
      execFile(
        'ssh',
        [...SSH_OPTS, `${this.user}@${this.host}`, '--', command],
        { timeout: timeoutMs },
        (err, stdout, stderr) => {
          if (err && typeof err.code !== 'number') return reject(err)
          resolve({ code: err ? Number(err.code) : 0, stdout, stderr })
        },
      )
    })
  }

  /** Runs a shell command line on the target; throws unless it exits 0. */
  async run(command: string): Promise<string> {
    const r = await this.exec(command)
    if (r.code !== 0) throw new Error(`target: \`${command}\` exited ${r.code}: ${r.stderr.trim()}`)
    return r.stdout
  }

  /** `tmux <args…>` with every argument quoted; throws on failure. */
  tmux(...args: string[]): Promise<string> {
    return this.run(['tmux', ...args].map(shq).join(' '))
  }

  /** Session names from `tmux ls` ([] when no server is running). */
  async sessions(): Promise<string[]> {
    const r = await this.exec("tmux list-sessions -F '#{session_name}'")
    if (r.code !== 0) {
      if (/no server running|error connecting/.test(r.stderr)) return []
      throw new Error(`target: tmux list-sessions exited ${r.code}: ${r.stderr.trim()}`)
    }
    return r.stdout.split('\n').filter(Boolean)
  }

  /** What tmux really shows in the session's active pane (ground truth). */
  capture(session: string): Promise<string> {
    return this.tmux('capture-pane', '-p', '-t', `=${session}:`)
  }

  /** `tmux display -p <format>` for the session. */
  async display(session: string, format: string): Promise<string> {
    return (await this.tmux('display-message', '-p', '-t', `=${session}:`, format)).trim()
  }

  /** Kills every session on the target (throwaway target only). */
  async resetTmux(): Promise<void> {
    await this.exec('tmux kill-server')
  }
}

/** A session name that is unique per test run. */
export function uniqueName(prefix: string): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 8)}`
}
