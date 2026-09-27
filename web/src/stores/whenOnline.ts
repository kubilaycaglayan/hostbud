import { watch } from 'vue'
import { useLiveStore } from './live'

/**
 * Runs `send` now while the events connection is open (or was never
 * started), otherwise once it reopens: a save during an app restart or a
 * network cut would only fail. Returns a function that cancels a deferred send.
 */
export function whenOnline(send: () => void): () => void {
  const live = useLiveStore()
  const online = () => live.state === 'open' || live.state === 'idle'
  if (online()) {
    send()
    return () => {}
  }
  const stop = watch(() => live.state, () => {
    if (!online()) return
    stop()
    send()
  })
  return stop
}
