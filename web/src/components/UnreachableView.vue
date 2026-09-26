<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const delay = ref(1_000)
const online = ref(navigator.onLine)
let timer: ReturnType<typeof setTimeout> | undefined
let stopped = false
let running = false
let retryAgain = false

function clear() {
  if (timer) clearTimeout(timer)
  timer = undefined
}
async function retry() {
  clear()
  if (stopped) return
  if (running) { retryAgain = true; return }
  running = true
  try {
    await auth.check()
  } finally {
    running = false
    if (auth.status !== 'unreachable') stopped = true
    if (stopped) return
    if (retryAgain) {
      retryAgain = false
      void retry()
      return
    }
    timer = setTimeout(() => {
      delay.value = Math.min(delay.value * 2, 15_000)
      void retry()
    }, delay.value)
  }
}
function onOnline() { online.value = navigator.onLine; delay.value = 1_000; void retry() }
function onOffline() { online.value = false }
function onVisibility() { if (document.visibilityState === 'visible') { delay.value = 1_000; void retry() } }
onMounted(() => {
  timer = setTimeout(() => void retry(), delay.value)
  window.addEventListener('online', onOnline)
  window.addEventListener('offline', onOffline)
  document.addEventListener('visibilitychange', onVisibility)
})
onUnmounted(() => {
  stopped = true
  clear()
  window.removeEventListener('online', onOnline)
  window.removeEventListener('offline', onOffline)
  document.removeEventListener('visibilitychange', onVisibility)
})
</script>

<template>
  <main class="flex min-h-full items-center justify-center p-4">
    <section class="w-full max-w-sm rounded border border-border bg-surface p-6" aria-labelledby="unreachable-title">
      <h1 id="unreachable-title" class="mb-2 text-lg font-bold text-accent">Can't reach hostbud</h1>
      <p class="mb-2">{{ online ? "hostbud isn't responding." : "You're offline." }}</p>
      <p class="mb-4 text-muted">Check your tailnet/VPN or port forward, then try again.</p>
      <button type="button" class="touch-target rounded bg-accent px-3 py-2 font-bold text-bg" @click="retry">Try again</button>
    </section>
  </main>
</template>
