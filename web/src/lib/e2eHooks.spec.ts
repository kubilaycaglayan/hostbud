import { afterEach, describe, expect, it } from 'vitest'
import { installE2EHooks, removeE2EHooks } from './e2eHooks'

afterEach(() => removeE2EHooks())

describe('e2e hooks', () => {
  it('expose termText once installed, and go away', () => {
    installE2EHooks({ termText: () => 'buffer' })
    expect(window.__hostbud?.termText()).toBe('buffer')
    removeE2EHooks()
    expect(window.__hostbud).toBeUndefined()
  })

  it('this build is not an e2e build', () => {
    expect(import.meta.env.VITE_E2E).not.toBe('1')
  })
})
