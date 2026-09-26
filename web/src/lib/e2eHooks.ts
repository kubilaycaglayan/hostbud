// Test hooks for the e2e suite (docs/ARCHITECTURE.md §13.1). Callers guard
// every use with `import.meta.env.VITE_E2E === '1'` inline: Vite replaces it
// with a constant, so production builds drop this code entirely
// (scripts/check-dist.mjs fails a production build that still has it).

export interface HostbudHooks {
  /** The visible terminal buffer as text (trailing blanks trimmed). */
  termText: () => string
}

declare global {
  interface Window {
    __hostbud?: HostbudHooks
  }
}

export function installE2EHooks(hooks: HostbudHooks): void {
  window.__hostbud = hooks
}

export function removeE2EHooks(): void {
  delete window.__hostbud
}
