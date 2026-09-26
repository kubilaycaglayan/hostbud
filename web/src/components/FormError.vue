<script setup lang="ts">
// An API error shown inside an open dialog. (A toast would be hidden from
// assistive technology while the modal is open.)
const props = defineProps<{ id: string; title: string; message: string; hint?: string }>()
</script>

<template>
  <div
    role="alert"
    :aria-labelledby="props.id"
    class="rounded border border-danger p-2"
  >
    <p
      :id="props.id"
      class="font-bold text-danger"
    >
      {{ props.title }}
    </p>
    <p class="mt-1">
      {{ props.message }}
    </p>
    <p
      v-if="props.hint"
      class="mt-1 text-muted"
    >
      {{ props.hint }}
    </p>
  </div>
</template>
