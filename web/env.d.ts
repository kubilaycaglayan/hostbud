/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** "1" only in the e2e image: exposes window.__hostbud test hooks. */
  readonly VITE_E2E?: string
}
