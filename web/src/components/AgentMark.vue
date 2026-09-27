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
    <img
      v-if="agent === 'codex'"
      src="/agents/codex.svg"
      alt=""
      class="size-full"
    />
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
