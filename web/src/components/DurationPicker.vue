<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown } from 'lucide-vue-next'
import { SelectContent, SelectItem, SelectItemText, SelectPortal, SelectRoot, SelectTrigger, SelectValue, SelectViewport } from 'reka-ui'

// A simple duration picker: hours and minutes selects, so the value can't be
// malformed. The model is seconds; seconds set elsewhere (the API) are kept
// until the owner picks a new value. The option lists are Reka UI selects
// whose popups scroll inside a bounded height instead of filling the screen.
const props = withDefaults(defineProps<{ label: string; maxHours?: number }>(), { maxHours: 48 })
const seconds = defineModel<number>({ required: true })

const hours = computed(() => Math.min(Math.floor(seconds.value / 3600), props.maxHours))
const minutes = computed(() => Math.floor((seconds.value % 3600) / 60))
const hourOptions = computed(() => Array.from({ length: props.maxHours + 1 }, (_, h) => h))
const minuteOptions = Array.from({ length: 60 }, (_, m) => m)
const pad = (m: number) => String(m).padStart(2, '0')

function set(h: number, m: number) {
  seconds.value = Math.min(h * 3600 + m * 60, props.maxHours * 3600)
}
</script>

<template>
  <span role="group" :aria-label="label" class="mt-1 flex items-center gap-1">
    <template v-for="part in (['Hours', 'Minutes'] as const)" :key="part">
      <SelectRoot
        :model-value="part === 'Hours' ? hours : minutes"
        @update:model-value="(v) => part === 'Hours' ? set(Number(v), minutes) : set(hours, Number(v))"
      >
        <SelectTrigger
          :aria-label="part"
          class="inline-flex min-h-11 items-center gap-1 rounded border border-border bg-bg px-2 font-mono text-base"
        >
          <SelectValue>{{ part === 'Hours' ? hours : pad(minutes) }}</SelectValue>
          <ChevronDown :size="14" aria-hidden="true" />
        </SelectTrigger>
        <SelectPortal>
          <SelectContent
            position="popper"
            :side-offset="4"
            class="z-50 max-h-[min(16rem,var(--reka-select-content-available-height))] min-w-[var(--reka-select-trigger-width)] overflow-hidden rounded border border-border bg-surface text-fg shadow-lg"
          >
            <SelectViewport class="p-1">
              <SelectItem
                v-for="n in (part === 'Hours' ? hourOptions : minuteOptions)"
                :key="n"
                :value="n"
                class="flex min-h-9 cursor-pointer select-none items-center rounded px-2 font-mono outline-none data-[highlighted]:bg-bg data-[state=checked]:font-bold data-[state=checked]:text-accent"
              >
                <SelectItemText>{{ part === 'Hours' ? n : pad(n) }}</SelectItemText>
              </SelectItem>
            </SelectViewport>
          </SelectContent>
        </SelectPortal>
      </SelectRoot>
      <span aria-hidden="true">{{ part === 'Hours' ? 'h' : 'm' }}</span>
    </template>
  </span>
</template>
