import { defineStore } from 'pinia'
import { ref } from 'vue'
import { projectsApi } from '@/api/client'
import type { Project, ServerEvent } from '@/api/types'

export const useProjectsStore = defineStore('projects', () => {
  const items = ref<Project[]>([])
  async function load(machine: string) { items.value = (await projectsApi.list(machine)).projects }
  function apply(event: ServerEvent) {
    if (event.type !== 'projects.changed') return
    const p = event.payload.project
    const index = items.value.findIndex((item) => item.id === p.id)
    if (index < 0) items.value.push(p)
    else items.value[index] = p
  }
  function byPath(path: string) { return items.value.find((p) => p.path === path) }
  function remember(project: Project) {
    const index = items.value.findIndex((item) => item.id === project.id)
    if (index < 0) items.value.push(project)
    else items.value[index] = project
  }
  function reset() { items.value = [] }
  return { items, load, apply, byPath, remember, reset }
})
