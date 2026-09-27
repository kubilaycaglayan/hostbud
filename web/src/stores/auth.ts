import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError, authApi, runtimeApi } from '@/api/client'

export type AuthStatus = 'loading' | 'anonymous' | 'authenticated' | 'unreachable' | 'server-error'

export const useAuthStore = defineStore('auth', () => {
  const status = ref<AuthStatus>('loading')
  const email = ref('')
  const serverError = ref('')

  async function check() {
    status.value = 'loading'
    try {
      email.value = (await authApi.me()).email
      await runtimeApi.configureExecTimeout()
      status.value = 'authenticated'
      serverError.value = ''
    } catch (e) {
      email.value = ''
      if (e instanceof ApiError && e.status === 401) {
        status.value = 'anonymous'
        return
      }
      if (!(e instanceof ApiError) || [408, 502, 503, 504].includes(e.status)) {
        status.value = 'unreachable'
        return
      }
      status.value = 'server-error'
      serverError.value = 'hostbud returned an error. Try again later.'
    }
  }

  async function login(address: string, password: string) {
    await authApi.login(address, password)
    await check()
  }

  // Registration doesn't sign in by itself; sign in right after.
  async function register(address: string, password: string) {
    await authApi.register(address, password)
    await login(address, password)
  }

  async function logout() {
    try {
      await authApi.logout()
    } finally {
      status.value = 'anonymous'
      email.value = ''
    }
  }

  /** A protected request answered 401: the session is gone. */
  function sessionEnded() {
    status.value = 'anonymous'
    email.value = ''
  }

  /** After a connection failed: is the sign-in session still valid? True
   * when hostbud itself can't be reached (keep retrying until it's back). */
  async function stillAuthorized(): Promise<boolean> {
    try {
      await authApi.me()
      return true
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        sessionEnded()
        return false
      }
      return true
    }
  }

  return { status, email, serverError, check, login, register, logout, sessionEnded, stillAuthorized }
})
