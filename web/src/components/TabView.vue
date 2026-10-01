<script setup lang="ts">
import { computed } from 'vue'
import LayoutNodeView from '@/components/LayoutNodeView.vue'
import TerminalView from '@/components/TerminalView.vue'
import { panesOf, type Tab } from '@/lib/layout'
import { useLayoutStore } from '@/stores/layout'

// One terminal layout. Wide screens show its split tree; narrow ones show only
// the focused pane, full size, with a "Pane n of m" switcher (the layout
// itself is unchanged, so a wide screen shows the split again).
const props = defineProps<{ tab: Tab; active: boolean; narrow: boolean; focusInitialTerminal?: boolean }>()
const emit = defineEmits<{ initialFocus: [] }>()

const layout = useLayoutStore()
const panes = computed(() => panesOf(props.tab.root))
</script>

<template>
  <LayoutNodeView
    v-if="!props.narrow"
    :node="props.tab.root"
    :tab="props.tab"
    :active="props.active"
    :focus-initial-terminal="props.focusInitialTerminal"
    @initial-focus="emit('initialFocus')"
  />
  <template v-else>
    <div
      v-for="(p, i) in panes"
      v-show="p.id === props.tab.focusedPane"
      :key="p.id"
      class="h-full"
    >
      <TerminalView
        :pane-id="p.id"
        :machine="p.machine"
        :session="p.session"
        :active="props.active && p.id === props.tab.focusedPane"
        :focused="p.id === props.tab.focusedPane"
        :focus-initial-terminal="props.focusInitialTerminal"
        :pane-index="i + 1"
        :pane-count="panes.length"
        narrow
        @focus="layout.focusPane(props.tab.id, p.id)"
        @initial-focus="emit('initialFocus')"
        @close="layout.closePane(p.id)"
        @cycle-pane="layout.cycleFocus(props.tab.id)"
      />
    </div>
  </template>
</template>
