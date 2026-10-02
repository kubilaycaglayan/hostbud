import { defineStore } from 'pinia'
import { ref } from 'vue'
import { queuesApi } from '@/api/client'
import type { DefaultPrompt, Queue, QueueChanged, RunChanged, ServerEvent } from '@/api/types'
import { DEFAULT_PROMPT } from '@/lib/queue'
import { describeError } from './toasts'

/** Applies a queue.changed payload: the queue after the change, or its
 * removal. A setting change (no queueId) leaves the queues alone. */
export function applyQueueChanged(queues: Queue[], change: QueueChanged): Queue[] {
  if (!change.queueId) return queues
  if (!change.queue) return queues.filter((q) => q.id !== change.queueId)
  const at = queues.findIndex((q) => q.id === change.queueId)
  if (at < 0) return [...queues, change.queue]
  const next = [...queues]
  next[at] = change.queue
  return next
}

/** Applies a run.changed payload to the item's run summary (the item's own
 * status arrives with the queue.changed that follows). A new run (a retry)
 * replaces the old summary. */
export function applyRunChanged(queues: Queue[], change: RunChanged): Queue[] {
  return queues.map((q) => q.id !== change.queueId ? q : {
    ...q,
    items: q.items.map((item) => {
      if (item.id !== change.itemId) return item
      const base = item.run?.id === change.runId ? item.run : { id: change.runId, sessionName: '', startedAt: '' }
      const tokens = change.inputTokens || change.outputTokens ? { inputTokens: change.inputTokens ?? 0, outputTokens: change.outputTokens ?? 0 } : {}
      return { ...item, run: { ...base, status: change.status, detail: change.detail, ...(Object.hasOwn(change, 'flag') ? { flag: change.flag } : {}), ...tokens } }
    }),
  })
}

/** The queues, kept current by /ws/events only (no polling). */
export const useQueuesStore = defineStore('queues', () => {
  const queues = ref<Queue[]>([])
  const loaded = ref(false)
  const loadError = ref('')
  /** The parallel-queues switch (V2-M2): several queues, parallel runs. */
  const parallelQueues = ref(false)
  /** The queue default prompt (Settings): prefills new items when enabled. */
  const defaultPrompt = ref<DefaultPrompt>({ enabled: false, text: DEFAULT_PROMPT })
  let loading: Promise<void> | null = null
  /** V2-M3: the queue item a notification asked to open (the Queue panel
   * selects its queue and highlights the item). */
  const focus = ref<{ queueId: string; itemId: string | null; at: number } | null>(null)

  async function load() {
    loading ??= (async () => {
      try {
        const list = await queuesApi.list()
        queues.value = list.queues
        parallelQueues.value = list.parallelQueues === true
        if (list.defaultPrompt) defaultPrompt.value = list.defaultPrompt
        loaded.value = true
        loadError.value = ''
      } catch (e) {
        loadError.value = describeError(e).message
      } finally {
        loading = null
      }
    })()
    return loading
  }

  /** Feeds one server event. A snapshot (every (re)connect) refetches once. */
  function apply(e: ServerEvent) {
    switch (e.type) {
      case 'snapshot':
        void load()
        break
      case 'queue.changed':
        queues.value = applyQueueChanged(queues.value, e.payload)
        if (typeof e.payload.parallelQueues === 'boolean') parallelQueues.value = e.payload.parallelQueues
        if (e.payload.defaultPrompt) defaultPrompt.value = e.payload.defaultPrompt
        break
      case 'run.changed':
        queues.value = applyRunChanged(queues.value, e.payload)
        break
    }
  }

  /** Replaces a queue with the server's answer to a request. */
  function put(queue: Queue) {
    queues.value = applyQueueChanged(queues.value, { action: 'local', queueId: queue.id, queue })
  }

  function reset() {
    queues.value = []
    loaded.value = false
    loadError.value = ''
    parallelQueues.value = false
    defaultPrompt.value = { enabled: false, text: DEFAULT_PROMPT }
  }

  function focusItem(queueId: string, itemId: string | null) {
    focus.value = { queueId, itemId, at: Date.now() }
  }

  return { queues, loaded, loadError, parallelQueues, defaultPrompt, focus, load, apply, put, reset, focusItem }
})
