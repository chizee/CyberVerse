import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Character, CharacterForm } from '../types'
import * as api from '../services/api'

export const useCharacterStore = defineStore('characters', () => {
  const characters = ref<Character[]>([])
  const loading = ref(false)
  const current = ref<Character | null>(null)

  async function fetchAll() {
    loading.value = true
    try {
      characters.value = await api.getCharacters()
    } finally {
      loading.value = false
    }
  }

  async function fetchOne(id: string) {
    loading.value = true
    try {
      current.value = await api.getCharacter(id)
    } finally {
      loading.value = false
    }
  }

  async function create(form: CharacterForm) {
    const char = await api.createCharacter(form)
    characters.value.push(char)
    return char
  }

  async function update(id: string, form: CharacterForm) {
    const char = await api.updateCharacter(id, form)
    const idx = characters.value.findIndex(c => c.id === id)
    if (idx >= 0) characters.value[idx] = char
    current.value = char
    return char
  }

  async function remove(id: string) {
    await api.deleteCharacter(id)
    characters.value = characters.value.filter(c => c.id !== id)
    if (current.value?.id === id) current.value = null
  }

  return { characters, loading, current, fetchAll, fetchOne, create, update, remove }
})
