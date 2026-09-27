<script setup lang="ts">
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuTrigger } from 'reka-ui'
const props = defineProps<{ hasSelection: boolean; canSplit: boolean; sessions: string[] }>()
const emit = defineEmits<{
  action: [name: 'search' | 'copy' | 'keyboard' | 'dictation' | 'snapshot' | 'close']
  split: [direction: 'row' | 'column', session: string | null]
}>()
const item = 'touch-target flex min-h-11 cursor-pointer items-center rounded px-3 py-1 outline-none data-disabled:text-muted data-highlighted:bg-bg'
</script>

<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger type="button" aria-label="Terminal actions" title="Terminal actions" class="touch-target rounded border border-border px-2">⋮</DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent align="end" :side-offset="4" aria-label="Terminal actions" class="z-50 max-h-[min(80dvh,36rem)] min-w-48 overflow-y-auto rounded border border-border bg-surface p-1 text-fg shadow-lg">
        <DropdownMenuItem :class="item" @select="emit('action', 'search')">Search</DropdownMenuItem>
        <DropdownMenuItem :disabled="!props.hasSelection" :class="item" @select="emit('action', 'copy')">Copy selected text</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="emit('action', 'keyboard')">Show keyboard</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="emit('action', 'dictation')">Dictation</DropdownMenuItem>
        <DropdownMenuItem :class="item" @select="emit('action', 'snapshot')">View terminal text</DropdownMenuItem>
        <template v-if="props.canSplit">
          <DropdownMenuItem v-for="session in props.sessions" :key="'right-' + session" :class="item" @select="emit('split', 'row', session)">Split right with {{ session }}</DropdownMenuItem>
          <DropdownMenuItem :class="item" @select="emit('split', 'row', null)">Split right with new session</DropdownMenuItem>
          <DropdownMenuItem v-for="session in props.sessions" :key="'down-' + session" :class="item" @select="emit('split', 'column', session)">Split down with {{ session }}</DropdownMenuItem>
          <DropdownMenuItem :class="item" @select="emit('split', 'column', null)">Split down with new session</DropdownMenuItem>
        </template>
        <DropdownMenuItem :class="item" @select="emit('action', 'close')">Close pane</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>
