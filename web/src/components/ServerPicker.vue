<script setup lang="ts">
import { computed } from 'vue'
import { useMachinesStore } from '@/stores/machines'

// V2-M13: which server a new session or project goes to. Shown only once a
// server has been added (the host alone needs no choice).
const model = defineModel<string>({ required: true })
const machines = useMachinesStore()
const choices = computed(() => machines.machines)
</script>

<template>
  <label v-if="choices.length > 1" class="flex flex-col gap-1">
    <span>Server</span>
    <select
      v-model="model"
      name="server"
      autocomplete="off"
      class="rounded border border-border bg-bg px-2 py-2 text-base"
    >
      <option v-for="m in choices" :key="m.id" :value="m.id">
        {{ m.label }}{{ m.source === 'custom' ? '' : ' (this host)' }}{{ m.status === 'ok' ? '' : ' — not connected' }}
      </option>
    </select>
  </label>
</template>
