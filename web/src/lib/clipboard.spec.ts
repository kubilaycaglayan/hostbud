import { Terminal } from '@xterm/xterm'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { copySelection, installOsc52, OSC52_MAX_BYTES, pasteClipboard, WriteOnlyClipboard } from './clipboard'
import { useToastsStore } from '@/stores/toasts'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

function stubClipboard(impl: Partial<Clipboard>) {
  vi.stubGlobal('navigator', { ...navigator, clipboard: impl })
}

const b64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)))

/** A real (unopened) xterm: its parser runs OSC handlers on write. */
function osc(term: Terminal, seq: string): Promise<void> {
  return new Promise((resolve) => term.write(seq, resolve))
}

describe('OSC 52', () => {
  it('writes decoded text to the clipboard (any selection parameter)', async () => {
    const write = vi.fn(async () => {})
    const term = new Terminal({ allowProposedApi: true })
    installOsc52(term, new WriteOnlyClipboard(write))
    await osc(term, `\x1b]52;c;${b64('héllo')}\x07`)
    await osc(term, `\x1b]52;;${b64('from tmux')}\x07`)
    expect(write.mock.calls).toEqual([['héllo'], ['from tmux']])
    term.dispose()
  })

  it('refuses reads: a query gets no reply and never touches the clipboard', async () => {
    const provider = new WriteOnlyClipboard(vi.fn(async () => {}))
    const read = vi.spyOn(provider, 'readText')
    const term = new Terminal({ allowProposedApi: true })
    const replies: string[] = []
    term.onData((d) => replies.push(d))
    installOsc52(term, provider)
    await osc(term, '\x1b]52;c;?\x07')
    await osc(term, '\x1b]52;p;?\x07')
    expect(replies).toEqual([])
    expect(read).not.toHaveBeenCalled()
    term.dispose()
  })

  it('ignores oversized, empty and undecodable payloads', async () => {
    const write = vi.fn(async () => {})
    const p = new WriteOnlyClipboard(write)
    p.writeText('c', 'x'.repeat(OSC52_MAX_BYTES + 1))
    p.writeText('c', 'é'.repeat(OSC52_MAX_BYTES / 2 + 1)) // 2 bytes each
    p.writeText('c', '')
    p.writeText('c', 'x'.repeat(OSC52_MAX_BYTES))
    expect(write).toHaveBeenCalledTimes(1)

    const term = new Terminal({ allowProposedApi: true })
    installOsc52(term, new WriteOnlyClipboard(write))
    await osc(term, '\x1b]52;c;not*base64\x07')
    expect(write).toHaveBeenCalledTimes(1)
    term.dispose()
  })

  it('a refused write is silent', async () => {
    const debug = vi.spyOn(console, 'debug').mockImplementation(() => {})
    const p = new WriteOnlyClipboard(() => Promise.reject(new Error('NotAllowedError')))
    p.writeText('c', 'x')
    await vi.waitFor(() => expect(debug).toHaveBeenCalled())
    debug.mockRestore()
  })
})

describe('copySelection / pasteClipboard', () => {
  const fakeTerm = (selection: string) => ({ getSelection: () => selection, paste: vi.fn() }) as unknown as Terminal & {
    paste: ReturnType<typeof vi.fn>
  }

  it('copies the selection', async () => {
    const writeText = vi.fn(async () => {})
    stubClipboard({ writeText })
    await copySelection(fakeTerm('some text'))
    expect(writeText).toHaveBeenCalledWith('some text')
    await copySelection(fakeTerm(''))
    expect(writeText).toHaveBeenCalledTimes(1)
  })

  it('a refused copy shows a toast', async () => {
    stubClipboard({ writeText: () => Promise.reject(new Error('denied')) })
    await copySelection(fakeTerm('x'))
    expect(useToastsStore().toasts.map((t) => `${t.title}: ${t.message}`)).toEqual([
      "Couldn't copy: The browser blocked clipboard access.",
    ])
  })

  it('no clipboard API at all (not a secure context) also shows the toast', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: undefined })
    await copySelection(fakeTerm('x'))
    await pasteClipboard(fakeTerm(''))
    expect(useToastsStore().toasts.map((t) => t.title)).toEqual(["Couldn't copy", "Couldn't paste"])
  })

  it('pastes the clipboard through term.paste', async () => {
    stubClipboard({ readText: async () => 'echo a\necho b' })
    const t = fakeTerm('')
    await pasteClipboard(t)
    expect(t.paste).toHaveBeenCalledWith('echo a\necho b')
  })
})
