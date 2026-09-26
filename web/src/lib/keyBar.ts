export type KeyBarKey = 'Escape' | 'Tab' | 'ArrowLeft' | 'ArrowUp' | 'ArrowDown' | 'ArrowRight' | '|' | '~' | '/' | '-'
export type ModifierName = 'ctrl' | 'alt'
export interface ModifierState { armed: boolean; locked: boolean; lastTap: number }
export interface KeyModifiers { ctrl: ModifierState; alt: ModifierState }

const CONTROL_CODES: Record<string, string> = {
  '@': '\x00', ' ': '\x00', a: '\x01', b: '\x02', c: '\x03', d: '\x04', e: '\x05', f: '\x06', g: '\x07', h: '\x08', i: '\x09', j: '\x0a', k: '\x0b', l: '\x0c', m: '\x0d', n: '\x0e', o: '\x0f',
  p: '\x10', q: '\x11', r: '\x12', s: '\x13', t: '\x14', u: '\x15', v: '\x16', w: '\x17', x: '\x18', y: '\x19', z: '\x1a', '[': '\x1b', '\\': '\x1c', ']': '\x1d', '^': '\x1e', _: '\x1f', '?': '\x7f',
}

const ARROWS: Record<string, string> = { ArrowLeft: 'D', ArrowUp: 'A', ArrowDown: 'B', ArrowRight: 'C' }

export function createModifiers(): KeyModifiers {
  return { ctrl: { armed: false, locked: false, lastTap: 0 }, alt: { armed: false, locked: false, lastTap: 0 } }
}

export function toggleModifier(modifiers: KeyModifiers, name: ModifierName, now = Date.now()): void {
  const state = modifiers[name]
  if (state.locked) {
    state.armed = false
    state.locked = false
  } else if (state.armed) {
    state.locked = now - state.lastTap <= 300
    if (!state.locked) state.armed = false
  } else {
    state.armed = true
  }
  state.lastTap = now
}

export function keyBytes(key: KeyBarKey | string, applicationCursorKeys = false, ctrl = false, alt = false): string {
  const arrow = ARROWS[key]
  if (arrow) {
    if (ctrl && alt) return `\x1b\x1b[1;5${arrow}`
    if (ctrl || alt) return `\x1b[1;${ctrl ? '5' : '3'}${arrow}`
    return applicationCursorKeys ? `\x1bO${arrow}` : `\x1b[${arrow}`
  }
  let bytes = key === 'Escape' ? '\x1b' : key === 'Tab' ? '\t' : key
  if (ctrl && key.length === 1) bytes = CONTROL_CODES[key.toLowerCase()] ?? bytes
  if (alt) bytes = `\x1b${bytes}`
  return bytes
}

function consume(modifiers: KeyModifiers): { ctrl: boolean; alt: boolean } {
  const result = { ctrl: modifiers.ctrl.armed, alt: modifiers.alt.armed }
  for (const state of [modifiers.ctrl, modifiers.alt]) {
    if (!state.locked) state.armed = false
  }
  return result
}

export function applyModifiers(input: string, modifiers: KeyModifiers): string {
  if (!input || (!modifiers.ctrl.armed && !modifiers.alt.armed)) return input
  const { ctrl, alt } = consume(modifiers)
  const first = input[0]
  let transformed = ctrl ? CONTROL_CODES[first.toLowerCase()] ?? first : first
  if (alt) transformed = `\x1b${transformed}`
  return transformed + input.slice(1)
}

export function sendKey(key: KeyBarKey, applicationCursorKeys: boolean, modifiers: KeyModifiers): string {
  const bytes = keyBytes(key, applicationCursorKeys, modifiers.ctrl.armed, modifiers.alt.armed)
  consume(modifiers)
  return bytes
}
