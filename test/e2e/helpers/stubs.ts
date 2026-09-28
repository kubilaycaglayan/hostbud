import { createHash } from 'node:crypto'
import { shq, Target } from './target.ts'

// The stub claude and codex clients on the throwaway target (V2-M1 T7,
// test/sshd/stubs/stub.py). They run hostbud's injected hooks for real and
// write goal state in the recorded formats; never real agents.

export type StubBehavior =
  | `achieve:${number}`
  | 'decoy'
  | 'fail'
  | 'exit'
  | 'silent'
  | 'quiet-print'
  | 'quiet-print-large'
  | `silent-then-achieve:${number}`
  | 'clear'
  | 'pending'
  | `slow:${number}`

export interface StubLog {
  client: 'claude' | 'codex'
  argv: string[]
  cwd: string
  pid: number
  sessions: string[]
  env: { HOSTBUD_URL: string | null; HOSTBUD_RUN_ID: string | null; HOSTBUD_RUN_TOKEN_sha256: string | null }
}

const STATE = '/home/dev/.hostbud-stubs'

function sha256(text: string): string {
  return createHash('sha256').update(text).digest('hex')
}

export class Stubs {
  constructor(readonly target = new Target()) {}

  /** Clears behaviors, logs and switches (and the stub Codex goal store). */
  async reset(): Promise<void> {
    await this.target.run(`rm -rf ${STATE} /home/dev/.codex/stub-goals.json && mkdir -p ${STATE}/goal ${STATE}/run ${STATE}/log`)
  }

  /** The behavior for every run of an item with this goal condition (the
   * instruction after "/goal "). delay = seconds per turn; extra = more
   * options, e.g. "stops=2" (V2-M3: the last Stop hook fires twice). */
  async setBehavior(condition: string, behavior: StubBehavior, delay = 0.5, extra = ''): Promise<void> {
    await this.target.run(`mkdir -p ${STATE}/goal && echo ${shq(`${behavior} delay=${delay}${extra ? ` ${extra}` : ''}`)} > ${STATE}/goal/${sha256(condition)}`)
  }

  /** The behavior for one run id (wins over the condition's). */
  async setRunBehavior(runId: string, behavior: StubBehavior, delay = 0.5): Promise<void> {
    await this.target.run(`mkdir -p ${STATE}/run && echo ${shq(`${behavior} delay=${delay}`)} > ${STATE}/run/${shq(runId)}`)
  }

  /** Reports an old client version (the version check refuses it). */
  async oldVersion(on: boolean): Promise<void> {
    await this.target.run(on ? `touch ${STATE}/old-version` : `rm -f ${STATE}/old-version`)
  }

  /** Plays "client not installed" (exit 127). */
  async missing(client: 'claude' | 'codex', on: boolean): Promise<void> {
    await this.target.run(on ? `touch ${STATE}/missing-${client}` : `rm -f ${STATE}/missing-${client}`)
  }

  /** What a run's stub logged at start: argv, cwd, HOSTBUD_* env (the token
   * only as a SHA-256) and its agent session ids; null before it started. */
  async log(runId: string, client: 'claude' | 'codex'): Promise<StubLog | null> {
    const r = await this.target.exec(`cat ${STATE}/log/${shq(runId)}-${client}.json`)
    return r.code === 0 ? JSON.parse(r.stdout) as StubLog : null
  }

  /** Every run the stubs saw, by run id, in start order. */
  async runs(): Promise<string[]> {
    const out = await this.target.run(`ls -tr ${STATE}/log 2>/dev/null | sed -n 's/-\\(claude\\|codex\\)\\.json$//p'`)
    return out.split('\n').filter(Boolean)
  }

  /** A run session's HOSTBUD_* variables as tmux holds them. */
  async sessionEnv(session: string): Promise<Record<string, string>> {
    const out = await this.target.tmux('show-environment', '-t', `=${session}`)
    return Object.fromEntries(out.split('\n').filter((l) => l.startsWith('HOSTBUD_')).map((l) => l.split(/=(.*)/s).slice(0, 2)))
  }
}

export const tokenHash = sha256
