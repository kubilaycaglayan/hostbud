import { defineStore } from 'pinia'
import { ref } from 'vue'

const sidebarStorageKey = 'hostbud.sidebarOpen'

function readSidebarOpen(): boolean {
  try {
    const saved = window.localStorage.getItem(sidebarStorageKey)
    return saved === null ? true : saved === 'true'
  } catch {
    return true
  }
}

// App-wide UI state. The open terminals live in the layout store.
export const useAppStore = defineStore('app', () => {
  const sidebarOpen = ref(readSidebarOpen())
  // Narrow screens show the session list or the terminals, one at a time.
  const terminalShown = ref(false)

  function toggleSidebar() {
    sidebarOpen.value = !sidebarOpen.value
    try {
      window.localStorage.setItem(sidebarStorageKey, String(sidebarOpen.value))
    } catch {
      // Storage can be disabled; the in-memory toggle still works.
    }
  }

  function showTerminal() {
    terminalShown.value = true
  }

  function showList() {
    terminalShown.value = false
  }

  return { sidebarOpen, terminalShown, toggleSidebar, showTerminal, showList }
})
