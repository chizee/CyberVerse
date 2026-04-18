<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import AppHeader from '../components/AppHeader.vue'
import AvatarUpload from '../components/AvatarUpload.vue'
import CvSelect from '../components/CvSelect.vue'
import { useCharacterStore } from '../stores/characters'
import type { CharacterForm, ImageInfo } from '../types'
import { VOICE_OPTIONS } from '../types'
import { uploadAvatar, getCharacterImages, deleteCharacterImage, activateCharacterImage } from '../services/api'

const router = useRouter()
const route = useRoute()
const store = useCharacterStore()

const isEdit = computed(() => !!route.params.id)
const characterId = computed(() => route.params.id as string)

const form = ref<CharacterForm>({
  name: '',
  description: '',
  avatar_image: '',
  use_face_crop: false,
  image_mode: 'fixed',
  voice_provider: 'doubao',
  voice_type: '温柔文雅',
  speaking_style: '',
  personality: '',
  welcome_message: '',
  system_prompt: '',
  tags: [],
})

const saving = ref(false)
const pendingFiles = ref<File[]>([])
const images = ref<ImageInfo[]>([])
const deletedImageFilenames = ref<Set<string>>(new Set())

const visibleImages = computed(() =>
  images.value.filter(img => !deletedImageFilenames.value.has(img.filename))
)

onMounted(async () => {
  if (isEdit.value) {
    await store.fetchOne(characterId.value)
    if (store.current) {
      const c = store.current
      form.value = {
        name: c.name,
        description: c.description,
        avatar_image: c.avatar_image,
        use_face_crop: c.use_face_crop,
        image_mode: c.image_mode || 'fixed',
        voice_provider: c.voice_provider,
        voice_type: c.voice_type,
        speaking_style: c.speaking_style,
        personality: c.personality,
        welcome_message: c.welcome_message,
        system_prompt: c.system_prompt,
        tags: [...c.tags],
      }
      await loadImages()
    }
  }
})

async function loadImages() {
  if (!isEdit.value) return
  try {
    images.value = await getCharacterImages(characterId.value)
  } catch {
    images.value = []
  }
}

async function handleFileSelected(file: File) {
  if (isEdit.value) {
    // Edit mode: upload immediately
    try {
      await uploadAvatar(characterId.value, file)
      await loadImages()
      await store.fetchOne(characterId.value)
      if (store.current) {
        form.value.avatar_image = store.current.avatar_image
      }
    } catch (e) {
      console.error('Upload failed:', e)
    }
  } else {
    // Create mode: queue for upload after save
    pendingFiles.value = [...pendingFiles.value, file]
  }
}

function handleDeletePending(index: number) {
  pendingFiles.value = pendingFiles.value.filter((_, i) => i !== index)
}

const activeImage = computed(() => store.current?.active_image)

async function handleActivateImage(filename: string) {
  if (!isEdit.value) return
  try {
    await activateCharacterImage(characterId.value, filename)
    await store.fetchOne(characterId.value)
    if (store.current) {
      form.value.avatar_image = store.current.avatar_image
    }
  } catch (e) {
    console.error('Activate image failed:', e)
  }
}

function handleDeleteImage(filename: string) {
  deletedImageFilenames.value = new Set([...deletedImageFilenames.value, filename])
}

async function save() {
  if (!form.value.name.trim()) return
  saving.value = true
  try {
    const payload = { ...form.value }
    if (payload.avatar_image.startsWith('blob:')) {
      payload.avatar_image = ''
    }

    let id: string
    if (isEdit.value) {
      await store.update(characterId.value, payload)
      id = characterId.value
    } else {
      const char = await store.create(payload)
      id = char.id
    }

    // Delete images marked for removal
    for (const filename of deletedImageFilenames.value) {
      await deleteCharacterImage(id, filename)
    }

    // Upload all pending files
    for (const file of pendingFiles.value) {
      await uploadAvatar(id, file)
    }

    router.push('/characters')
  } catch (e) {
    console.error('Save failed:', e)
  } finally {
    saving.value = false
  }
}

async function handleDelete() {
  if (!confirm('确定要删除这个角色吗？此操作不可撤销。')) return
  await store.remove(characterId.value)
  router.push('/characters')
}

const promptLength = computed(() => form.value.system_prompt.length)

const breadcrumb = computed(() =>
  isEdit.value ? ['角色列表', '编辑角色'] : ['角色列表', '创建角色']
)
</script>

<template>
  <div class="min-h-screen bg-cv-base flex flex-col">
    <AppHeader showBack :breadcrumb="breadcrumb" />

    <!-- Page title -->
    <div class="text-center py-8">
      <h1 class="text-2xl font-bold text-cv-text">{{ isEdit ? '编辑角色' : '创建新角色' }}</h1>
    </div>

    <!-- Content -->
    <main class="flex-1 max-w-[1100px] mx-auto w-full px-12 pb-24 flex gap-8">
      <!-- Left column: Avatar -->
      <div class="w-[300px] shrink-0">
        <AvatarUpload
          :use-face-crop="form.use_face_crop"
          :images="visibleImages"
          :character-id="isEdit ? characterId : undefined"
          :pending-files="pendingFiles"
          :active-image="activeImage"
          :image-mode="form.image_mode"
          @update:use-face-crop="v => form.use_face_crop = v"
          @file-selected="handleFileSelected"
          @delete-image="handleDeleteImage"
          @delete-pending="handleDeletePending"
          @activate-image="handleActivateImage"
        />

        <!-- Image mode toggle -->
        <div v-if="isEdit && visibleImages.length > 1"
             class="mt-4 bg-cv-surface border border-cv-border rounded-cv-lg p-4">
          <div class="flex items-center justify-between">
            <div>
              <span class="text-[13px] font-medium text-cv-text-secondary">随机切换头像</span>
              <p class="text-[11px] text-cv-text-muted mt-1">开启后每次进入会话时随机选择一张头像</p>
            </div>
            <button @click="form.image_mode = form.image_mode === 'random' ? 'fixed' : 'random'"
                    class="relative w-11 h-6 rounded-full transition-colors cursor-pointer"
                    :class="form.image_mode === 'random' ? 'bg-cv-accent' : 'bg-cv-elevated'">
              <span class="absolute top-0.5 left-0.5 w-5 h-5 rounded-full transition-transform duration-200"
                    :class="form.image_mode === 'random' ? 'translate-x-5 bg-white' : 'translate-x-0 bg-cv-text-muted'" />
            </button>
          </div>
        </div>
      </div>

      <!-- Right column: Form -->
      <div class="flex-1 flex flex-col gap-6">
        <!-- Section 1: 基本信息 -->
        <section class="bg-cv-surface border border-cv-border rounded-cv-lg p-6">
          <h2 class="text-base font-semibold text-cv-text mb-5">基本信息</h2>

          <label class="block mb-4">
            <span class="text-[13px] font-medium text-cv-text-secondary">角色名称 <span class="text-cv-danger">*</span></span>
            <input v-model="form.name" type="text" placeholder="输入角色名称..."
                   class="mt-1.5 w-full h-[42px] bg-cv-elevated border border-cv-border rounded-cv-md px-4 text-sm text-cv-text placeholder:text-cv-text-muted focus:border-cv-accent focus:outline-none focus:shadow-[0_0_0_2px_rgba(59,130,246,0.15)] transition-all" />
          </label>

          <label class="block">
            <span class="text-[13px] font-medium text-cv-text-secondary">角色描述</span>
            <textarea v-model="form.description" placeholder="简要描述该角色的特点和用途..."
                      class="mt-1.5 w-full h-20 bg-cv-elevated border border-cv-border rounded-cv-md px-4 py-3 text-sm text-cv-text placeholder:text-cv-text-muted resize-y focus:border-cv-accent focus:outline-none focus:shadow-[0_0_0_2px_rgba(59,130,246,0.15)] transition-all" />
          </label>
        </section>

        <!-- Section 2: 语音配置 -->
        <section class="bg-cv-surface border border-cv-border rounded-cv-lg p-6">
          <h2 class="text-base font-semibold text-cv-text mb-1">语音配置</h2>
          <p class="text-[13px] text-cv-text-muted mb-5">为此角色设置交互时使用的语音供应商和声线。</p>

          <div class="grid grid-cols-2 gap-4">
            <label class="block">
              <span class="text-[11px] font-medium text-cv-text-muted uppercase tracking-wide">语音 / 供应商</span>
              <CvSelect
                v-model="form.voice_provider"
                :options="[{ label: '豆包语音', value: 'doubao' }]"
                class="mt-1.5"
              />
            </label>
            <label class="block">
              <span class="text-[11px] font-medium text-cv-text-muted uppercase tracking-wide">语音 / 声线</span>
              <CvSelect
                v-model="form.voice_type"
                :options="VOICE_OPTIONS"
                class="mt-1.5"
              />
            </label>
          </div>
        </section>

        <!-- Section 3: 人设与风格 -->
        <section class="bg-cv-surface border border-cv-border rounded-cv-lg p-6">
          <h2 class="text-base font-semibold text-cv-text mb-5">人设与风格</h2>

          <label class="block mb-4">
            <span class="text-[13px] font-medium text-cv-text-secondary">说话风格</span>
            <input v-model="form.speaking_style" type="text" placeholder="温柔、专业"
                   class="mt-1.5 w-full h-[42px] bg-cv-elevated border border-cv-border rounded-cv-md px-4 text-sm text-cv-text placeholder:text-cv-text-muted focus:border-cv-accent focus:outline-none focus:shadow-[0_0_0_2px_rgba(59,130,246,0.15)] transition-all" />
            <p class="text-[11px] text-cv-text-muted mt-1">描述角色的语言风格，如"幽默风趣"、"严谨专业"</p>
          </label>

          <label class="block mb-4">
            <span class="text-[13px] font-medium text-cv-text-secondary">角色性格</span>
            <textarea v-model="form.personality" placeholder="温柔体贴、善于倾听，喜欢用比喻来解释复杂的概念..."
                      class="mt-1.5 w-full h-20 bg-cv-elevated border border-cv-border rounded-cv-md px-4 py-3 text-sm text-cv-text placeholder:text-cv-text-muted resize-y focus:border-cv-accent focus:outline-none focus:shadow-[0_0_0_2px_rgba(59,130,246,0.15)] transition-all" />
            <p class="text-[11px] text-cv-text-muted mt-1">描述角色的性格特征，将融入 system_prompt 生成</p>
          </label>

          <label class="block">
            <span class="text-[13px] font-medium text-cv-text-secondary">欢迎语</span>
            <textarea v-model="form.welcome_message" placeholder="你好，我是小雪，有什么可以帮助你的吗？"
                      class="mt-1.5 w-full h-[60px] bg-cv-elevated border border-cv-border rounded-cv-md px-4 py-3 text-sm text-cv-text placeholder:text-cv-text-muted resize-y focus:border-cv-accent focus:outline-none focus:shadow-[0_0_0_2px_rgba(59,130,246,0.15)] transition-all" />
            <p class="text-[11px] text-cv-text-muted mt-1">角色开场白，连接后自动播放</p>
          </label>
        </section>

        <!-- Section 4: 系统提示词 -->
        <section class="bg-cv-surface border border-cv-border rounded-cv-lg p-6">
          <h2 class="text-base font-semibold text-cv-text mb-5">系统提示词</h2>

          <textarea v-model="form.system_prompt"
                    placeholder="你是一个友善的数字人助手。请用简洁清晰的方式回答用户的问题，保持友好的语气。"
                    class="w-full h-40 bg-cv-elevated border border-cv-border rounded-cv-md px-4 py-3 text-[13px] text-cv-text placeholder:text-cv-text-muted resize-y leading-[22px] focus:border-cv-accent focus:outline-none focus:shadow-[0_0_0_2px_rgba(59,130,246,0.15)] transition-all" />
          <p class="text-right text-[11px] text-cv-text-muted mt-1">{{ promptLength }} / 2000</p>
        </section>
      </div>
    </main>

    <!-- Bottom action bar -->
    <div class="fixed bottom-0 left-0 right-0 bg-cv-surface border-t border-cv-border-subtle px-12 py-4 z-20">
      <div class="max-w-[1100px] mx-auto flex items-center justify-between">
        <button v-if="isEdit" @click="handleDelete"
                class="text-cv-danger text-sm hover:bg-cv-danger-muted px-3 py-1.5 rounded-cv-md transition-colors cursor-pointer">
          删除角色
        </button>
        <div v-else />
        <div class="flex items-center gap-3">
          <button @click="router.back()"
                  class="px-5 py-2.5 border border-cv-border text-cv-text-secondary text-sm rounded-cv-md hover:bg-cv-hover hover:text-cv-text transition-all cursor-pointer">
            取消
          </button>
          <button @click="save" :disabled="saving || !form.name.trim()"
                  :class="{ 'opacity-40 cursor-not-allowed': saving || !form.name.trim() }"
                  class="px-6 py-2.5 bg-cv-accent text-white text-sm font-medium rounded-cv-md hover:bg-cv-accent-hover transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed">
            {{ saving ? '保存中...' : '保存角色' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
