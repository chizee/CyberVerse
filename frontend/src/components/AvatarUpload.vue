<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { ImageInfo } from '../types'

interface DisplayImage {
  key: string        // unique key for v-for
  src: string        // display URL
  filename?: string  // server filename (undefined for pending uploads)
  pending?: boolean  // true if not yet uploaded
}

const props = defineProps<{
  useFaceCrop: boolean
  images: ImageInfo[]
  characterId?: string
  pendingFiles?: File[]
  activeImage?: string
  imageMode?: string
}>()

const emit = defineEmits<{
  'update:useFaceCrop': [value: boolean]
  fileSelected: [file: File]
  deleteImage: [filename: string]
  deletePending: [index: number]
  activateImage: [filename: string]
}>()

const currentIndex = ref(0)
const dragOver = ref(false)
const showLightbox = ref(false)

// Merge server images + pending local files into a unified display list
const displayImages = computed<DisplayImage[]>(() => {
  const list: DisplayImage[] = []

  // Server images
  if (props.images) {
    for (const img of props.images) {
      list.push({
        key: 'srv-' + img.filename,
        src: img.url || (props.characterId
          ? `/api/v1/characters/${props.characterId}/images/${img.filename}`
          : ''),
        filename: img.filename,
      })
    }
  }

  // Pending local files
  if (props.pendingFiles) {
    for (let i = 0; i < props.pendingFiles.length; i++) {
      list.push({
        key: 'pending-' + i,
        src: URL.createObjectURL(props.pendingFiles[i]),
        pending: true,
      })
    }
  }

  return list
})

const totalCount = computed(() => displayImages.value.length)
const hasImages = computed(() => totalCount.value > 0)
const currentImage = computed(() => displayImages.value[currentIndex.value] || null)

// Clamp index when list shrinks
watch(totalCount, (n) => {
  if (currentIndex.value >= n && n > 0) {
    currentIndex.value = n - 1
  }
})

function prev() {
  if (currentIndex.value > 0) currentIndex.value--
}

function next() {
  if (currentIndex.value < totalCount.value - 1) currentIndex.value++
}

function handleFile(file: File) {
  if (!file.type.startsWith('image/')) return
  emit('fileSelected', file)
  // Jump to the new image (will be appended at end)
  // Use nextTick-like delay so the list updates first
  setTimeout(() => {
    currentIndex.value = displayImages.value.length - 1
  }, 50)
}

function onDrop(e: DragEvent) {
  dragOver.value = false
  const file = e.dataTransfer?.files[0]
  if (file) handleFile(file)
}

function onFileInput(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files) {
    for (const file of Array.from(input.files)) {
      handleFile(file)
    }
  }
  // Reset so same file can be re-selected
  input.value = ''
}

function handleDelete() {
  const img = currentImage.value
  if (!img) return

  if (img.pending) {
    // Find the pending index: count how many pending items are before current
    const pendingIdx = displayImages.value
      .slice(0, currentIndex.value + 1)
      .filter(d => d.pending).length - 1
    emit('deletePending', pendingIdx)
  } else if (img.filename) {
    emit('deleteImage', img.filename)
  }
}

function triggerUpload() {
  ;(document.getElementById('avatar-file-input') as HTMLInputElement)?.click()
}
</script>

<template>
  <div class="bg-cv-surface border border-cv-border rounded-cv-lg p-6">
    <!-- Carousel or empty upload -->
    <div v-if="hasImages"
         class="relative w-full aspect-square rounded-cv-lg overflow-hidden group">
      <!-- Current image -->
      <img :src="currentImage?.src" class="w-full h-full object-cover transition-opacity duration-200 cursor-pointer" @click="showLightbox = true" />

      <!-- Pending badge -->
      <div v-if="currentImage?.pending"
           class="absolute bottom-3 left-3 px-2.5 py-1 bg-cv-accent/80 text-white text-[11px] font-medium rounded-full backdrop-blur-sm">
        待上传
      </div>

      <!-- Active image badge -->
      <div v-else-if="currentImage?.filename && currentImage.filename === activeImage && imageMode !== 'random'"
           class="absolute bottom-3 left-3 px-2.5 py-1 bg-emerald-500/80 text-white text-[11px] font-medium rounded-full backdrop-blur-sm flex items-center gap-1">
        <svg class="w-3 h-3" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M3 8.5l3.5 3.5L13 5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        当前头像
      </div>

      <!-- Set as active button -->
      <button v-else-if="currentImage?.filename && imageMode !== 'random'"
              @click.stop="emit('activateImage', currentImage!.filename!)"
              class="absolute bottom-3 left-3 px-2.5 py-1 bg-black/60 text-white/80 text-[11px] font-medium rounded-full opacity-0 group-hover:opacity-100 transition-all backdrop-blur-sm flex items-center gap-1 cursor-pointer hover:bg-cv-accent/80">
        <svg class="w-3 h-3" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M3 8.5l3.5 3.5L13 5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        设为头像
      </button>

      <!-- Delete button (top-right) -->
      <button @click.stop="handleDelete"
              class="absolute top-3 right-3 w-8 h-8 flex items-center justify-center rounded-full bg-black/60 text-white/80 hover:bg-red-600/80 transition-all opacity-0 group-hover:opacity-100 cursor-pointer backdrop-blur-sm">
        <svg class="w-4 h-4" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M4 4l8 8M12 4l-8 8" stroke-linecap="round" />
        </svg>
      </button>

      <!-- Add more button (top-left, on hover) -->
      <button @click.stop="triggerUpload"
              class="absolute top-3 left-3 px-2.5 py-1 bg-black/60 text-white/80 text-[11px] rounded-full opacity-0 group-hover:opacity-100 transition-all backdrop-blur-sm flex items-center gap-1 cursor-pointer hover:bg-black/80">
        <svg class="w-3 h-3" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M8 3v10M3 8h10" stroke-linecap="round" />
        </svg>
        添加图片
      </button>

      <!-- Left arrow -->
      <button v-if="currentIndex > 0"
              @click.stop="prev"
              class="absolute left-2 top-1/2 -translate-y-1/2 w-8 h-8 flex items-center justify-center rounded-full bg-black/50 text-white hover:bg-black/70 transition-all cursor-pointer backdrop-blur-sm">
        <svg class="w-4 h-4" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M10 3L5 8l5 5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>

      <!-- Right arrow -->
      <button v-if="currentIndex < totalCount - 1"
              @click.stop="next"
              class="absolute right-2 top-1/2 -translate-y-1/2 w-8 h-8 flex items-center justify-center rounded-full bg-black/50 text-white hover:bg-black/70 transition-all cursor-pointer backdrop-blur-sm">
        <svg class="w-4 h-4" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M6 3l5 5-5 5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>

      <!-- Dots indicator -->
      <div v-if="totalCount > 1"
           class="absolute bottom-3 left-1/2 -translate-x-1/2 flex items-center gap-1.5">
        <button v-for="(_, i) in displayImages" :key="i"
                @click.stop="currentIndex = i"
                class="w-2 h-2 rounded-full transition-all cursor-pointer"
                :class="i === currentIndex ? 'bg-white w-4' : 'bg-white/50 hover:bg-white/70'" />
      </div>
    </div>

    <!-- Empty state: upload placeholder -->
    <div v-else
         class="relative w-full aspect-square rounded-cv-lg bg-cv-elevated border-2 border-dashed border-cv-border flex flex-col items-center justify-center cursor-pointer hover:border-cv-accent hover:bg-cv-accent/5 transition-all"
         :class="{ 'border-cv-accent bg-cv-accent/5': dragOver }"
         @dragover.prevent="dragOver = true"
         @dragleave="dragOver = false"
         @drop.prevent="onDrop"
         @click="triggerUpload">
      <div class="w-12 h-12 rounded-full bg-cv-hover flex items-center justify-center mb-3">
        <svg class="w-5 h-5 text-cv-text-secondary" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M10 4v12M4 10h12" stroke-linecap="round" />
        </svg>
      </div>
      <p class="text-sm font-medium text-cv-text-secondary">上传角色头像</p>
      <p class="text-xs text-cv-text-muted mt-1">支持 PNG、JPG，建议 512x512</p>
    </div>

    <!-- Counter -->
    <div v-if="hasImages" class="mt-3 text-center">
      <span class="text-[12px] text-cv-text-muted">{{ currentIndex + 1 }} / {{ totalCount }}</span>
    </div>

    <!-- Hidden file input -->
    <input id="avatar-file-input" type="file" accept="image/*" multiple class="hidden" @change="onFileInput" />

    <!-- Face crop toggle -->
    <div class="mt-4 pt-4 border-t border-cv-border-subtle">
      <div class="flex items-center justify-between">
        <span class="text-[13px] text-cv-text-secondary">是否裁剪人脸</span>
        <button @click="emit('update:useFaceCrop', !useFaceCrop)"
                class="relative w-11 h-6 rounded-full transition-colors cursor-pointer"
                :class="useFaceCrop ? 'bg-cv-accent' : 'bg-cv-elevated'">
          <span class="absolute top-0.5 left-0.5 w-5 h-5 rounded-full transition-transform duration-200"
                :class="useFaceCrop ? 'translate-x-5 bg-white' : 'translate-x-0 bg-cv-text-muted'" />
        </button>
      </div>
      <p class="text-[11px] text-cv-text-muted mt-2 leading-4">开启后将自动检测并裁剪图片中的人脸区域</p>
    </div>
  </div>

  <!-- Lightbox modal -->
  <Teleport to="body">
    <Transition name="lightbox">
      <div v-if="showLightbox" class="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm" @click="showLightbox = false">
        <button class="absolute top-4 right-4 w-10 h-10 flex items-center justify-center rounded-full bg-white/10 text-white hover:bg-white/20 transition-colors cursor-pointer"
                @click.stop="showLightbox = false">
          <svg class="w-5 h-5" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M4 4l8 8M12 4l-8 8" stroke-linecap="round" />
          </svg>
        </button>
        <img :src="currentImage?.src" class="max-w-[90vw] max-h-[90vh] object-contain rounded-lg shadow-2xl" @click.stop />
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.lightbox-enter-active,
.lightbox-leave-active {
  transition: opacity 0.2s ease;
}
.lightbox-enter-from,
.lightbox-leave-to {
  opacity: 0;
}
</style>
