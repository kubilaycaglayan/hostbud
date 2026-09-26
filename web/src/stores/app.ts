import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface SelectedSession {
  machine: string
  name: string
}

// App-wide UI state: sidebar and the session shown in the main area.
export const useAppStore = defineStore('app', () => {
  const sidebarOpen = ref(true)
  const selected = ref<SelectedSession | null>(null)

  function toggleSidebar() {
    sidebarOpen.value = !sidebarOpen.value
  }

  function select(machine: string, name: string) {
    selected.value = { machine, name }
  }

  function clearSelection() {
    selected.value = null
  }

  return { sidebarOpen, selected, toggleSidebar, select, clearSelection }
})
