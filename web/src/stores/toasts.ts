import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError } from '@/api/client'

export interface Toast {
  id: number
  title: string
  message: string
  hint?: string
  tone?: 'error' | 'info' | 'success'
  placement?: 'top-right' | 'bottom-right'
}

let next = 1

/** Error notifications carrying the backend's actionable {error, hint}. */
export const useToastsStore = defineStore('toasts', () => {
  const toasts = ref<Toast[]>([])

  function push(t: Omit<Toast, 'id'>, ttlMs = 10_000) {
    const toast = { ...t, id: next++ }
    toasts.value = [...toasts.value, toast]
    if (ttlMs > 0) setTimeout(() => dismiss(toast.id), ttlMs)
    return toast.id
  }

  function error(title: string, e: unknown) {
    return push({ title, ...describeError(e), tone: 'error' })
  }

  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  return { toasts, push, error, dismiss }
})

/** The user-facing message and hint of a failed request. */
export function describeError(e: unknown): { message: string; hint?: string } {
  if (e instanceof ApiError) {
    const message = e.message.charAt(0).toUpperCase() + e.message.slice(1)
    return { message: message.endsWith('.') ? message : message + '.', hint: e.hint }
  }
  return { message: "Can't reach hostbud. Check your connection and try again." }
}
