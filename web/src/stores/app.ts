import { defineStore } from 'pinia'
import { ref } from 'vue'

// App-wide UI state. The open terminals live in the layout store.
export const useAppStore = defineStore('app', () => {
  const sidebarOpen = ref(true)
  // Narrow screens show the session list or the terminals, one at a time.
  const terminalShown = ref(false)

  function toggleSidebar() {
    sidebarOpen.value = !sidebarOpen.value
  }

  function showTerminal() {
    terminalShown.value = true
  }

  function showList() {
    terminalShown.value = false
  }

  return { sidebarOpen, terminalShown, toggleSidebar, showTerminal, showList }
})
