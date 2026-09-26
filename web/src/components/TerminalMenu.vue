<script setup lang="ts">
import type { Terminal } from '@xterm/xterm'
import {
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuPortal,
  ContextMenuRoot,
  ContextMenuTrigger,
} from 'reka-ui'
import { ref } from 'vue'
import { copySelection, pasteClipboard } from '@/lib/clipboard'

// The terminal's context menu: right-click (or a long press on touch
// screens). When the program captures the mouse (tmux `mouse on`), a plain
// right-click belongs to it; Shift (Option on macOS) + right-click opens this
// menu anyway.
const props = defineProps<{ term: Terminal | undefined }>()

const ITEM =
  'cursor-default select-none rounded px-3 py-2 outline-none data-disabled:text-muted data-highlighted:bg-border'

const hasSelection = ref(false)

function onContextMenu(ev: MouseEvent) {
  const captured = (props.term?.modes.mouseTrackingMode ?? 'none') !== 'none'
  // Prevented: no browser menu over the program's, and Reka stays closed.
  if (captured && !ev.shiftKey && !ev.altKey) ev.preventDefault()
}

function onOpenChange(open: boolean) {
  if (open) hasSelection.value = props.term?.hasSelection() ?? false
  else props.term?.focus()
}

function copy() {
  if (props.term) void copySelection(props.term)
}
function paste() {
  if (props.term) void pasteClipboard(props.term)
}
function selectAll() {
  props.term?.selectAll()
}
</script>

<template>
  <ContextMenuRoot @update:open="onOpenChange">
    <div
      class="flex min-h-0 flex-1 flex-col"
      @contextmenu="onContextMenu"
    >
      <ContextMenuTrigger as-child>
        <slot />
      </ContextMenuTrigger>
    </div>
    <ContextMenuPortal>
      <ContextMenuContent
        aria-label="Terminal menu"
        class="z-50 min-w-40 rounded border border-border bg-surface p-1 text-fg shadow-lg"
      >
        <ContextMenuItem
          :disabled="!hasSelection"
          :class="ITEM"
          @select="copy"
        >
          Copy
        </ContextMenuItem>
        <ContextMenuItem
          :class="ITEM"
          @select="paste"
        >
          Paste
        </ContextMenuItem>
        <ContextMenuItem
          :class="ITEM"
          @select="selectAll"
        >
          Select all
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenuPortal>
  </ContextMenuRoot>
</template>

