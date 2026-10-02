import { onScopeDispose, ref, type Ref } from 'vue'

/** Compact age for tree rows: "now", "4m", "3h", "2d". Empty when unknown. */
export function relativeTime(iso: string, now: number): string {
  const at = Date.parse(iso)
  if (!iso || Number.isNaN(at) || at <= 0) return ''
  const minutes = Math.floor(Math.max(0, now - at) / 60_000)
  if (minutes < 1) return 'now'
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

const now = ref(Date.now())
let users = 0
let timer: ReturnType<typeof setInterval> | undefined

/** A shared clock that ticks every 30 s while any component uses it. */
export function useNow(): Ref<number> {
  users++
  now.value = Date.now()
  if (!timer) {
    timer = setInterval(() => { now.value = Date.now() }, 30_000)
  }
  onScopeDispose(() => {
    users--
    if (users === 0 && timer) {
      clearInterval(timer)
      timer = undefined
    }
  })
  return now
}
