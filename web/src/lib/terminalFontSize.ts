import { reactive } from 'vue'

export const DEFAULT_TERMINAL_FONT_SIZE = 14
export const MIN_TERMINAL_FONT_SIZE = 8
export const MAX_TERMINAL_FONT_SIZE = 24

// This module lives for the lifetime of the page. It intentionally does not
// use localStorage or the account UI-state API.
const sizes = reactive(new Map<string, number>())

export function terminalFontSizeKey(machine: string, session: string): string {
  return JSON.stringify([machine, session])
}

export function terminalFontSize(machine: string, session: string): number {
  return sizes.get(terminalFontSizeKey(machine, session)) ?? DEFAULT_TERMINAL_FONT_SIZE
}

export function setTerminalFontSize(machine: string, session: string, size: number): number {
  const bounded = Math.max(MIN_TERMINAL_FONT_SIZE, Math.min(MAX_TERMINAL_FONT_SIZE, size))
  sizes.set(terminalFontSizeKey(machine, session), bounded)
  return bounded
}
