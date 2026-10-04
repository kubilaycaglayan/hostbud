import { defineStore } from 'pinia'
import { ref } from 'vue'
import { projectsApi } from '@/api/client'
import type { Project, ServerEvent } from '@/api/types'

export const useProjectsStore = defineStore('projects', () => {
  const items = ref<Project[]>([])
  const loaded = ref(false)
  /** Loads one machine's projects, or every machine's ('*', the default). */
  async function load(machine = '*') {
    items.value = (await projectsApi.list(machine)).projects
    loaded.value = true
  }
  function apply(event: ServerEvent) {
    if (event.type !== 'projects.changed') return
    const p = event.payload.project
    const index = items.value.findIndex((item) => item.id === p.id)
    if (event.payload.action === 'deleted') {
      if (index >= 0) items.value.splice(index, 1)
      return
    }
    if (index < 0) items.value.push(p)
    else items.value[index] = p
  }
  function byPath(path: string, machine = 'host') { return items.value.find((p) => p.path === path && p.machineId === machine) }
  function remember(project: Project) {
    const index = items.value.findIndex((item) => item.id === project.id)
    if (index < 0) items.value.push(project)
    else items.value[index] = project
  }
  function reset() { items.value = []; loaded.value = false }
  return { items, loaded, load, apply, byPath, remember, reset }
})
