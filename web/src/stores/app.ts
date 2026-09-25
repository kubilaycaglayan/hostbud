import { defineStore } from 'pinia'
import { ref } from 'vue'

// Placeholder app-wide store; machines/sessions stores arrive with T13.
export const useAppStore = defineStore('app', () => {
  const sidebarOpen = ref(true)

  function toggleSidebar() {
    sidebarOpen.value = !sidebarOpen.value
  }

  return { sidebarOpen, toggleSidebar }
})
