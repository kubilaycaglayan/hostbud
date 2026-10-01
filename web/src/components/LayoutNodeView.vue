<script setup lang="ts">
import { Pane as SplitPane, Splitpanes, type SplitpanesResizedPayload } from 'splitpanes'
import 'splitpanes/dist/splitpanes.css'
import { computed, inject } from 'vue'
import TerminalView from '@/components/TerminalView.vue'
import { MAX_TAB_PANES, MIN_PANE_SIZE, panesOf, type LayoutNode, type SplitDir, type Tab } from '@/lib/layout'
import { useLayoutStore } from '@/stores/layout'
import { NEW_SESSION_FOR_SPLIT } from './layoutKeys'

// One node of a tab's layout tree (M3 T8): a terminal pane, or a split
// rendered with splitpanes whose children are rendered recursively.
const props = defineProps<{ node: LayoutNode; tab: Tab; active: boolean; focusInitialTerminal?: boolean }>()
const emit = defineEmits<{ initialFocus: [] }>()

const layout = useLayoutStore()
const newSessionFor = inject(NEW_SESSION_FOR_SPLIT, () => {})
const panes = computed(() => panesOf(props.tab.root))

function split(paneId: string, dir: SplitDir, session: string | null) {
  if (session === null) newSessionFor(paneId, dir)
  else layout.split(paneId, dir, panes.value.find((p) => p.id === paneId)!.machine, session)
}

/** A divider drag ended: keep the new sizes. (splitpanes also fires this
 * after adding or removing a pane, without an event; the model already
 * has those sizes.) */
function resized(splitId: string, e: SplitpanesResizedPayload) {
  if (e.event) layout.setSizes(splitId, e.panes.map((p) => p.size))
}
</script>

<template>
  <TerminalView
    v-if="props.node.type === 'pane'"
    :key="props.node.id"
    :pane-id="props.node.id"
    :machine="props.node.machine"
    :session="props.node.session"
    :active="props.active"
    :focus-initial-terminal="props.focusInitialTerminal"
    :focused="props.tab.focusedPane === props.node.id"
    :pane-index="panes.findIndex((p) => p.id === props.node.id) + 1"
    :pane-count="panes.length"
    :can-split="panes.length < MAX_TAB_PANES"
    @focus="layout.focusPane(props.tab.id, props.node.id)"
    @initial-focus="emit('initialFocus')"
    @split="(dir, s) => split(props.node.id, dir, s)"
    @close="layout.closePane(props.node.id)"
  />
  <Splitpanes
    v-else
    :horizontal="props.node.dir === 'column'"
    :maximize-panes="false"
    :data-split="props.node.dir"
    class="h-full"
    @resized="(e: SplitpanesResizedPayload) => resized(props.node.id, e)"
  >
    <SplitPane
      v-for="(child, i) in props.node.children"
      :key="child.id"
      :size="props.node.sizes[i]"
      :min-size="MIN_PANE_SIZE"
    >
      <LayoutNodeView
        :node="child"
        :tab="props.tab"
        :active="props.active"
        :focus-initial-terminal="props.focusInitialTerminal"
        @initial-focus="emit('initialFocus')"
      />
    </SplitPane>
  </Splitpanes>
</template>
