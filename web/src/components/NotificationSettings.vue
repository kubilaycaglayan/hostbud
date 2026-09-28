<script setup lang="ts">
import { computed, ref } from 'vue'
import { notificationsApi } from '@/api/client'
import type { NotificationSettings } from '@/api/types'
import FormError from './FormError.vue'
import { currentPermission } from '@/lib/notifications'
import { currentDevice, DEVICE_TEXT, deviceState, markDeviceSetUp } from '@/lib/notificationDevice'
import { pushSupported, subscribeDevice, unsubscribeDevice, pushEndpoint } from '@/lib/push'
import { hasPushSubscription, useNotificationsStore } from '@/stores/notifications'
import { describeError } from '@/stores/toasts'

// Settings → Notifications (V2-M3): the account's switch and event choices,
// this device's state, and a test. The browser's permission prompt appears
// only after a click here.
const store = useNotificationsStore()
const busy = ref(false)
const error = ref<{ title: string; message: string; hint?: string } | null>(null)
const note = ref('')
const permission = ref(currentPermission())

const settings = computed(() => store.settings)
const state = computed(() => deviceState(currentDevice(permission.value, store.pushSubscribed)))
const deviceText = computed(() => store.revoked ? 'Notifications are blocked on this device.' : DEVICE_TEXT[state.value])
const events = [
  { key: 'onDone', label: 'An item is done' },
  { key: 'onAttention', label: 'An item needs attention' },
  { key: 'onFinished', label: 'A queue has finished' },
] as const

async function run(title: string, fn: () => Promise<void>) {
  busy.value = true
  error.value = null
  note.value = ''
  try {
    await fn()
  } catch (e) {
    error.value = { title, ...describeError(e) }
  } finally {
    busy.value = false
    permission.value = currentPermission()
  }
}

async function save(prefs: Partial<Pick<NotificationSettings, 'enabled' | 'onDone' | 'onAttention' | 'onFinished'>>) {
  store.settings = await notificationsApi.save(prefs)
}

/** Asks for permission (this click is the only place that does) and sets
 * this device up: push when available, else in-app. False: not granted. */
async function setUpDevice(): Promise<boolean> {
  if (state.value === 'ios-install' || state.value === 'unsupported') return false
  permission.value = await Notification.requestPermission()
  if (permission.value !== 'granted') return false
  markDeviceSetUp(true)
  store.revoked = false
  const s = store.settings
  if (s?.push?.available && s.vapidPublicKey && await pushSupported()) {
    try {
      await subscribeDevice(s.vapidPublicKey)
    } catch (e) {
      note.value = `Push couldn't be set up here (${describeError(e).message}); you'll get notifications while hostbud is open.`
    }
  }
  store.pushSubscribed = await hasPushSubscription()
  return true
}

function toggle(box: HTMLInputElement) {
  const on = box.checked
  return run("Couldn't change notifications", async () => {
    if (!on) {
      await save({ enabled: false })
      await unsubscribeDevice()
      markDeviceSetUp(false)
      store.pushSubscribed = false
      store.revoked = false
      return
    }
    if (!await setUpDevice()) return // stays off; the device text says why
    await save({ enabled: true })
  }).finally(() => {
    box.checked = store.settings?.enabled === true // a refusal leaves it as it was
  })
}

function choose(key: 'onDone' | 'onAttention' | 'onFinished', on: boolean) {
  void run("Couldn't save the choice", () => save({ [key]: on }))
}

function setUpThisDevice() {
  void run("Couldn't set up this device", async () => { await setUpDevice() })
}

function sendTest() {
  void run("Couldn't send a test notification", async () => {
    const endpoint = store.pushSubscribed ? await pushEndpoint() : null
    if (endpoint) {
      await notificationsApi.test(endpoint)
      note.value = 'Test sent through push to this device.'
      return
    }
    store.showLocal({ title: 'hostbud: test notification', body: 'Notifications reach this device while hostbud is open.', tag: `test:${Date.now()}` })
    note.value = 'Test shown on this device.'
  })
}
</script>

<template>
  <section class="flex flex-col gap-2" aria-labelledby="notifications-heading" data-testid="notification-settings">
    <h2 id="notifications-heading" class="font-bold">
      Notifications
    </h2>
    <p class="text-sm text-muted">
      Tell you when an item is done, needs attention, or a queue has finished, on this account's devices. Only the project name, item number and outcome are shown.
    </p>
    <FormError v-if="error" id="notifications-error" :title="error.title" :message="error.message" :hint="error.hint" />
    <p v-if="!settings" class="text-sm text-muted">
      Loading…
    </p>
    <template v-else>
      <label class="flex min-h-11 items-center gap-2">
        <input
          type="checkbox"
          data-testid="notifications-toggle"
          autocomplete="off"
          class="size-4"
          :checked="settings.enabled"
          :disabled="busy"
          @change="toggle($event.target as HTMLInputElement)"
        >
        Notify me
      </label>
      <p data-testid="notification-device" role="status" class="text-sm" :class="store.revoked || state === 'blocked' ? 'text-danger' : 'text-muted'">
        {{ deviceText }}
      </p>
      <button
        v-if="settings.enabled && (state === 'not-set-up' || store.revoked) && permission !== 'denied'"
        type="button"
        class="touch-target min-h-11 self-start rounded border border-border px-3"
        :disabled="busy"
        @click="setUpThisDevice"
      >
        Set up this device
      </button>
      <p v-if="!settings.push?.available" data-testid="push-unavailable" class="text-sm text-muted">
        {{ settings.push?.reason }}
      </p>
      <fieldset class="flex flex-col gap-1" :disabled="busy">
        <legend class="text-sm text-muted">
          Notify when
        </legend>
        <label v-for="e in events" :key="e.key" class="flex min-h-11 items-center gap-2">
          <input
            type="checkbox"
            autocomplete="off"
            class="size-4"
            :checked="settings[e.key]"
            @change="choose(e.key, ($event.target as HTMLInputElement).checked)"
          >
          {{ e.label }}
        </label>
      </fieldset>
      <button
        type="button"
        class="touch-target min-h-11 self-start rounded border border-border px-3"
        :disabled="busy || permission !== 'granted'"
        @click="sendTest"
      >
        Send test notification
      </button>
      <p v-if="note" role="status" class="text-sm text-ok">
        {{ note }}
      </p>
    </template>
  </section>
</template>
