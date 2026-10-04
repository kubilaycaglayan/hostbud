<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { serversApi } from '@/api/client'
import type { HostKey, Machine } from '@/api/types'
import ConfirmDialog from './ConfirmDialog.vue'
import FormError from './FormError.vue'
import { useMachinesStore } from '@/stores/machines'
import { describeError, useToastsStore } from '@/stores/toasts'

// V2-M13: other SSH servers. Adding one is two steps: scan its host keys,
// then the owner compares the fingerprints and trusts them. hostbud never
// trusts a key on first use, and removing a server never touches its tmux.
const props = defineProps<{ compact?: boolean }>()
const open = defineModel<boolean>('open', { default: false })

const machines = useMachinesStore()
const toasts = useToastsStore()

const label = ref('')
const host = ref('')
const user = ref('')
const port = ref('22')
const keys = ref<HostKey[] | null>(null)
const scanned = ref('')
const busy = ref(false)
const error = ref<{ title: string; message: string; hint?: string } | null>(null)
const removing = ref<Machine | null>(null)
const confirmOpen = ref(false)

const portNumber = computed(() => Number(port.value.trim() || '22'))
const portError = computed(() => Number.isInteger(portNumber.value) && portNumber.value >= 1 && portNumber.value <= 65535 ? '' : 'Use a port from 1 to 65535.')
const target = computed(() => `${host.value.trim()}:${portNumber.value}`)
// Keys belong to the host and port they were scanned from.
const confirmedKeys = computed(() => (keys.value && scanned.value === target.value ? keys.value : null))

watch(open, (isOpen) => {
  if (!isOpen) return
  label.value = ''
  host.value = ''
  user.value = ''
  port.value = '22'
  keys.value = null
  scanned.value = ''
  error.value = null
}, { immediate: true })

function statusText(m: Machine) {
  switch (m.status) {
    case 'ok': return 'Connected'
    case 'unknown': return 'Connecting…'
    case 'tmux_missing': return m.error || 'tmux not found'
    default: return m.error || 'Unreachable'
  }
}

async function scan() {
  error.value = null
  if (!host.value.trim()) {
    error.value = { title: "Couldn't check the host key", message: 'Enter the server\'s host name or IP address.' }
    return
  }
  if (portError.value) {
    error.value = { title: "Couldn't check the host key", message: portError.value }
    return
  }
  busy.value = true
  try {
    const res = await serversApi.scan(host.value.trim(), portNumber.value)
    keys.value = res.hostKeys
    scanned.value = target.value
  } catch (e) {
    keys.value = null
    error.value = { title: "Couldn't check the host key", ...describeError(e) }
  } finally {
    busy.value = false
  }
}

async function add() {
  const confirmed = confirmedKeys.value
  if (!confirmed) return
  error.value = null
  busy.value = true
  try {
    const m = await serversApi.add({
      label: label.value.trim(), host: host.value.trim(), port: portNumber.value, user: user.value.trim(),
      hostKeys: confirmed.map(({ type, key }) => ({ type, key })),
    })
    toasts.push({ title: 'Server added', message: `${m.label} is connecting; pick it when you create a session or add a project.`, tone: 'info' })
    label.value = ''
    host.value = ''
    user.value = ''
    port.value = '22'
    keys.value = null
    scanned.value = ''
  } catch (e) {
    error.value = { title: "Couldn't add the server", ...describeError(e) }
  } finally {
    busy.value = false
  }
}

function askRemove(m: Machine) {
  removing.value = m
  confirmOpen.value = true
}

async function remove() {
  const m = removing.value
  if (!m) return
  error.value = null
  try {
    await serversApi.remove(m.id)
    toasts.push({ title: 'Server removed', message: `${m.label} is no longer tracked; its tmux sessions keep running.`, tone: 'info' })
  } catch (e) {
    error.value = { title: "Couldn't remove the server", ...describeError(e) }
  } finally {
    removing.value = null
  }
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-overlay" />
      <DialogContent
        class="fixed z-40 flex flex-col overflow-hidden border border-border bg-surface text-fg"
        :class="props.compact ? 'inset-0 h-dvh w-full pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)]' : 'left-1/2 top-1/2 max-h-[min(44rem,90vh)] w-[min(34rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded'"
      >
        <div class="flex items-start justify-between gap-2 border-b border-border px-3 py-2">
          <div class="min-w-0">
            <DialogTitle class="text-base font-bold">
              Servers
            </DialogTitle>
            <DialogDescription class="text-sm text-muted">
              Other machines hostbud reaches over SSH with the key in its agent.
            </DialogDescription>
          </div>
          <DialogClose aria-label="Close servers" title="Close" class="touch-target inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-border">
            ×
          </DialogClose>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto p-3">
          <ul aria-label="Servers" class="mb-4 flex flex-col gap-2">
            <li v-for="m in machines.machines" :key="m.id" data-testid="server-row" class="flex items-center gap-2 rounded border border-border px-2 py-1.5">
              <span
                class="size-2 shrink-0 rounded-full"
                :class="m.status === 'ok' ? 'bg-ok' : m.status === 'unknown' ? 'bg-muted' : 'bg-danger'"
                aria-hidden="true"
              />
              <div class="min-w-0 flex-1">
                <p class="truncate font-bold">
                  {{ m.label }} <span v-if="m.source !== 'custom'" class="font-normal text-muted">(this host)</span>
                </p>
                <p class="truncate text-sm text-muted">
                  <span v-if="m.address">{{ m.address }} · </span>{{ statusText(m) }}
                </p>
                <p v-if="m.source === 'custom' && m.status !== 'ok' && m.hint" class="text-sm text-muted">
                  {{ m.hint }}
                </p>
              </div>
              <button
                v-if="m.source === 'custom'"
                type="button"
                class="touch-target min-h-11 shrink-0 rounded border border-border px-2 text-sm"
                :aria-label="`Remove ${m.label}`"
                @click="askRemove(m)"
              >
                Remove
              </button>
            </li>
          </ul>
          <FormError v-if="error" id="servers-error" :title="error.title" :message="error.message" :hint="error.hint" />
          <form class="flex flex-col gap-3" aria-label="Add server" novalidate @submit.prevent="confirmedKeys ? add() : scan()">
            <h2 class="font-bold">
              Add server
            </h2>
            <label class="flex flex-col gap-1">
              <span>Nickname</span>
              <input v-model="label" name="label" autocomplete="off" spellcheck="false" maxlength="40" placeholder="e.g. build-box" class="rounded border border-border bg-bg px-2 py-2 text-base">
            </label>
            <div class="flex gap-2">
              <label class="flex min-w-0 flex-1 flex-col gap-1">
                <span>Host</span>
                <input v-model="host" name="host" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="server-a.example.com" class="rounded border border-border bg-bg px-2 py-2 text-base">
              </label>
              <label class="flex w-24 flex-col gap-1">
                <span>Port</span>
                <input v-model="port" name="port" inputmode="numeric" autocomplete="off" class="rounded border border-border bg-bg px-2 py-2 text-base">
              </label>
            </div>
            <label class="flex flex-col gap-1">
              <span>User</span>
              <input v-model="user" name="user" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="dev" class="rounded border border-border bg-bg px-2 py-2 text-base">
            </label>
            <p class="text-sm text-muted">
              The server must accept hostbud's agent key for this user (its public key in <code>~/.ssh/authorized_keys</code>).
            </p>
            <section v-if="confirmedKeys" aria-label="Host key fingerprints" class="rounded border border-border p-2 text-sm">
              <p class="font-bold">
                Host keys of {{ scanned }}
              </p>
              <ul class="my-1">
                <li v-for="k in confirmedKeys" :key="k.key" data-testid="fingerprint" class="break-all font-mono">
                  {{ k.type }} {{ k.fingerprint }}
                </li>
              </ul>
              <p class="text-muted">
                Compare them with <code>ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub</code> on the server. Trust them only if they match.
              </p>
            </section>
            <div class="flex justify-end gap-2">
              <button v-if="confirmedKeys" type="button" class="touch-target min-h-11 rounded border border-border px-3" :disabled="busy" @click="scan">
                Check again
              </button>
              <button type="submit" :disabled="busy" class="touch-target min-h-11 rounded bg-accent px-3 font-bold text-bg disabled:opacity-60">
                {{ confirmedKeys ? 'Trust and add' : 'Check host key' }}
              </button>
            </div>
          </form>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
  <ConfirmDialog
    v-model:open="confirmOpen"
    :title="`Remove ${removing?.label ?? 'the server'}?`"
    body="hostbud stops tracking it and forgets its pinned host key. Its tmux sessions keep running. Remove its projects first."
    action="Remove server"
    danger
    :compact="props.compact"
    @confirm="remove"
  />
</template>
