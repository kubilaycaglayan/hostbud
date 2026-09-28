import { defineStore } from 'pinia'
import { ref } from 'vue'
import { queuesApi } from '@/api/client'
import type { Queue, QueueChanged, RunChanged, ServerEvent } from '@/api/types'
import { describeError } from './toasts'

/** Applies a queue.changed payload: the queue after the change, or its removal. */
export function applyQueueChanged(queues: Queue[], change: QueueChanged): Queue[] {
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
      return { ...item, run: { ...base, status: change.status, detail: change.detail } }
    }),
  })
}

/** The queues, kept current by /ws/events only (no polling). */
export const useQueuesStore = defineStore('queues', () => {
  const queues = ref<Queue[]>([])
  const loaded = ref(false)
  const loadError = ref('')
  /** HOSTBUD_PARALLEL_QUEUES (V2-M2): several queues, parallel runs. */
  const parallelQueues = ref(false)
  let loading: Promise<void> | null = null

  async function load() {
    loading ??= (async () => {
      try {
        const list = await queuesApi.list()
        queues.value = list.queues
        parallelQueues.value = list.parallelQueues === true
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
  }

  return { queues, loaded, loadError, parallelQueues, load, apply, put, reset }
})
