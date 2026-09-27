<script setup lang="ts">
defineProps<{ agent: 'codex' | 'claude' }>()
</script>

<template>
  <span
    data-agent-mark
    :data-agent="agent"
    :title="agent === 'codex' ? 'Codex running' : 'Claude Code running'"
    class="inline-flex size-3.5 shrink-0 items-center justify-center"
    aria-hidden="true"
  >
    <svg
      v-if="agent === 'codex'"
      viewBox="0 0 16 16"
      class="size-full"
      fill="none"
    >
      <path
        d="M8 1.45 13.65 4.7v6.6L8 14.55 2.35 11.3V4.7L8 1.45Z"
        stroke="#10A37F"
        stroke-width="1.4"
        stroke-linejoin="round"
      />
      <path
        d="m5.05 5.05 5.9 5.9m0-5.9-5.9 5.9"
        stroke="#6B75E8"
        stroke-width="1.25"
        stroke-linecap="round"
      />
    </svg>
    <svg
      v-else
      viewBox="0 0 16 16"
      class="size-full"
      fill="none"
    >
      <path
        d="M8 1.3v3.2m0 7v3.2M1.3 8h3.2m7 0h3.2M3.25 3.25l2.27 2.27m4.96 4.96 2.27 2.27m0-9.5-2.27 2.27m-4.96 4.96-2.27 2.27"
        stroke="#D97757"
        stroke-width="1.8"
        stroke-linecap="round"
      />
      <circle
        cx="8"
        cy="8"
        r="1.25"
        fill="#D97757"
      />
    </svg>
  </span>
</template>
