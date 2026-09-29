<script setup lang="ts">
import { computed, ref } from 'vue'

// A simple duration picker: hours and minutes selects, so the value can't be
// malformed. The model is seconds; seconds set elsewhere (the API) are kept
// until the owner picks a new value.
const props = withDefaults(defineProps<{ label: string; maxHours?: number }>(), { maxHours: 720 })
const seconds = defineModel<number>({ required: true })

const hours = computed(() => Math.min(Math.floor(seconds.value / 3600), props.maxHours))
const minutes = computed(() => Math.floor((seconds.value % 3600) / 60))
const hourOptions = computed(() => Array.from({ length: props.maxHours + 1 }, (_, h) => h))
const minuteOptions = Array.from({ length: 60 }, (_, m) => m)

// Read both selects: the model may not have re-rendered between changes.
const hoursBox = ref<HTMLSelectElement>()
const minutesBox = ref<HTMLSelectElement>()
function update() {
  const h = Number(hoursBox.value?.value ?? hours.value)
  const m = Number(minutesBox.value?.value ?? minutes.value)
  seconds.value = Math.min(h * 3600 + m * 60, props.maxHours * 3600)
}
</script>

<template>
  <span role="group" :aria-label="label" class="mt-1 flex items-center gap-1">
    <select
      ref="hoursBox"
      :value="hours"
      autocomplete="off"
      aria-label="Hours"
      class="min-h-11 rounded border border-border bg-bg px-1 font-mono text-base"
      @change="update"
    >
      <option v-for="h in hourOptions" :key="h" :value="h">{{ h }}</option>
    </select>
    <span aria-hidden="true">h</span>
    <select
      ref="minutesBox"
      :value="minutes"
      autocomplete="off"
      aria-label="Minutes"
      class="min-h-11 rounded border border-border bg-bg px-1 font-mono text-base"
      @change="update"
    >
      <option v-for="m in minuteOptions" :key="m" :value="m">{{ String(m).padStart(2, '0') }}</option>
    </select>
    <span aria-hidden="true">m</span>
  </span>
</template>
