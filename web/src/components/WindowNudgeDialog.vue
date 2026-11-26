<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { useWindowNudgeStore } from '@/stores/windowNudge'
import ServerPicker from './ServerPicker.vue'
import FileBrowser from './FileBrowser.vue'
import type { WindowNudge } from '@/api/types'

const props = defineProps<{ compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const store = useWindowNudgeStore()
const schedules = ['01-06-11-16-21', '02-07-12-17-22', '03-08-13-18-23', '04-09-14-19-00', '05-10-15-20-00']
const machineId = ref('host')
const directory = ref('')
const provider = ref<'claude' | 'codex'>('codex')
const schedule = ref(schedules[0]!)
const enabled = ref(false)
const skipFirst = ref(false)
const choosingDirectory = ref(false)
const confirmEnable = ref(false)
const message = ref('')
const settings = computed(() => store.settings)

watch(open, async (shown) => {
  if (!shown) return
  message.value = ''
  confirmEnable.value = false
  choosingDirectory.value = false
  await store.load()
  const current = store.settings
  machineId.value = current?.machineId || 'host'
  directory.value = current?.directory || ''
  provider.value = current?.provider || 'codex'
  schedule.value = current?.schedule || schedules[0]!
  enabled.value = current?.enabled || false
  skipFirst.value = false
}, { immediate: true })

function selected(path: string) {
  directory.value = path
  choosingDirectory.value = false
}

async function save(allowEnable = false) {
  if (enabled.value && !settings.value?.enabled && !allowEnable) {
    confirmEnable.value = true
    return
  }
  const ok = await store.save({ enabled: enabled.value, machineId: machineId.value, directory: directory.value, provider: provider.value, schedule: schedule.value, skipFirst: skipFirst.value })
  if (ok) { open.value = false; confirmEnable.value = false }
  else message.value = store.loadError || 'Could not save window nudge settings.'
}

const nextRun = computed(() => {
  if (!settings.value?.nextRunAt) return ''
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Etc/GMT-3' }).format(new Date(settings.value.nextRunAt)) + ' (UTC+03:00)'
})
function outcome(value?: WindowNudge['lastOutcome']) {
  return value ? value.charAt(0).toUpperCase() + value.slice(1) : 'No runs yet'
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent
        class="fixed z-40 flex max-h-[min(44rem,90dvh)] w-[min(38rem,calc(100vw-1rem))] flex-col overflow-y-auto rounded border border-border bg-surface text-fg"
        :class="props.compact ? 'inset-x-0 bottom-0 top-auto w-full rounded-t-2xl pb-[env(safe-area-inset-bottom)]' : 'left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2'"
      >
        <div class="flex items-start justify-between border-b border-border px-3 py-2">
          <div>
            <DialogTitle class="font-bold">
              Window nudge
            </DialogTitle>
            <DialogDescription class="sr-only">
              Configure the recurring session replacement.
            </DialogDescription>
          </div>
          <DialogClose
            aria-label="Close window nudge settings"
            class="touch-target min-h-8 min-w-8 rounded border border-border"
          >
            ×
          </DialogClose>
        </div>
        <div
          v-if="choosingDirectory"
          class="flex min-h-0 flex-1 flex-col gap-2 p-3"
        >
          <ServerPicker v-model="machineId" />
          <FileBrowser
            :key="machineId"
            :machine="machineId"
            select-directory
            class="min-h-0 flex-1 overflow-y-auto"
            @selected="selected"
          />
          <button
            type="button"
            class="touch-target self-start rounded border border-border px-3 py-1"
            @click="choosingDirectory = false"
          >
            Cancel
          </button>
        </div>
        <form
          v-else
          class="flex flex-col gap-3 p-3"
          @submit.prevent="save()"
        >
          <ServerPicker v-model="machineId" />
          <div class="flex flex-wrap items-center gap-2">
            <span
              class="min-w-0 flex-1 truncate rounded border border-border px-2 py-2"
              aria-live="polite"
            >{{ directory || 'Choose a directory' }}</span>
            <button
              type="button"
              class="touch-target rounded border border-border px-3 py-2"
              @click="choosingDirectory = true"
            >
              Browse
            </button>
          </div>
          <label class="flex flex-col gap-1">Provider
            <select
              v-model="provider"
              autocomplete="off"
              class="rounded border border-border bg-bg px-2 py-2 text-base"
            ><option value="claude">Claude</option><option value="codex">Codex</option></select>
          </label>
          <label class="flex flex-col gap-1">Daily UTC+03:00 times
            <select
              v-model="schedule"
              autocomplete="off"
              class="rounded border border-border bg-bg px-2 py-2 text-base"
            ><option
              v-for="item in schedules"
              :key="item"
              :value="item"
            >{{ item.split('-').join(' · ') }}</option></select>
          </label>
          <p
            v-if="settings?.enabled && nextRun"
            class="text-sm"
          >
            Next run: {{ nextRun }}
          </p>
          <p
            v-if="settings?.lastOutcome"
            class="text-sm"
          >
            Last run: {{ outcome(settings.lastOutcome) }}{{ settings.lastDetail ? ` — ${settings.lastDetail}` : '' }}
          </p>
          <p
            v-if="message || store.loadError"
            role="alert"
            class="text-sm text-danger"
          >
            {{ message || store.loadError }}
          </p>
          <div class="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
            <label class="flex items-center gap-2"><input
              v-model="enabled"
              type="checkbox"
              autocomplete="off"
            > Active</label>
            <div class="flex gap-2">
              <button
                type="button"
                class="touch-target rounded border border-border px-3 py-2"
                @click="open = false"
              >
                Cancel
              </button><button
                type="submit"
                :disabled="store.saving || !directory"
                class="touch-target rounded bg-accent px-3 py-2 font-bold text-bg"
              >
                Save
              </button>
            </div>
          </div>
          <label class="flex items-center gap-2"><input
            v-model="skipFirst"
            type="checkbox"
            autocomplete="off"
          > Skip the next scheduled time once</label>
          <section
            v-if="confirmEnable"
            role="alertdialog"
            aria-label="Confirm recurring session replacement"
            class="rounded border-2 border-orange-500 bg-orange-500/10 p-3"
          >
            <p class="font-bold">
              Every scheduled occurrence will terminate whatever is running in the exact tmux session <code>window-nudge-chore</code>, then replace it.
            </p>
            <p class="mt-1 text-sm">
              The first run waits for the next scheduled time. Missed times are skipped.
            </p>
            <div class="mt-3 flex justify-end gap-2">
              <button
                type="button"
                class="touch-target rounded border border-border px-3 py-2"
                @click="confirmEnable = false"
              >
                Cancel
              </button><button
                type="button"
                class="touch-target rounded bg-orange-500 px-3 py-2 font-bold text-white"
                :disabled="store.saving"
                @click="save(true)"
              >
                Confirm and enable
              </button>
            </div>
          </section>
        </form>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
