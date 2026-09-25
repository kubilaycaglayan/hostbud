import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError, authApi } from '@/api/client'

export type AuthStatus = 'loading' | 'anonymous' | 'authenticated'

export const useAuthStore = defineStore('auth', () => {
  const status = ref<AuthStatus>('loading')
  const email = ref('')

  async function check() {
    try {
      email.value = (await authApi.me()).email
      status.value = 'authenticated'
    } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 401) throw e
      status.value = 'anonymous'
      email.value = ''
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

  return { status, email, check, login, register, logout, sessionEnded }
})
