<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getHealth } from '../services/api'

const router = useRouter()
const search = ref('')
const serviceConnected = ref(false)

onMounted(async () => {
  try {
    const h = await getHealth()
    serviceConnected.value = h.inference_connected
  } catch {
    serviceConnected.value = false
  }
})

withDefaults(defineProps<{
  showBack?: boolean
  breadcrumb?: string[]
  logoTo?: string
}>(), {
  logoTo: '/characters',
})
</script>

<template>
  <header class="h-14 bg-cv-surface border-b border-cv-border-subtle flex items-center px-12 shrink-0">
    <!-- Left -->
    <div class="flex items-center gap-3">
      <button v-if="showBack" @click="router.back()"
              class="text-cv-text-secondary hover:text-cv-text text-sm cursor-pointer transition-colors">
        ← 返回
      </button>
      <span v-if="showBack" class="text-cv-border">|</span>
      <span class="text-lg font-bold text-cv-text tracking-[-0.5px] cursor-pointer" @click="router.push(logoTo)">
        CyberVerse
      </span>
    </div>

    <!-- Center: Search -->
    <div class="flex-1 flex justify-center" v-if="!breadcrumb">
      <div class="relative w-[280px]">
        <input
          v-model="search"
          type="text"
          placeholder="搜索角色..."
          class="w-full h-9 bg-cv-elevated border border-cv-border rounded-cv-md px-4 pr-8 text-sm text-cv-text placeholder:text-cv-text-muted focus:border-cv-accent focus:outline-none focus:shadow-[0_0_0_2px_rgba(59,130,246,0.15)] transition-all"
        />
      </div>
    </div>

    <!-- Center: Breadcrumb -->
    <div class="flex-1 flex justify-center" v-if="breadcrumb">
      <div class="flex items-center gap-2 text-sm">
        <template v-for="(item, i) in breadcrumb" :key="i">
          <span v-if="i < breadcrumb.length - 1"
                class="text-cv-accent cursor-pointer hover:text-cv-accent-hover transition-colors"
                @click="router.push(i === 0 ? '/characters' : '')">
            {{ item }}
          </span>
          <span v-if="i < breadcrumb.length - 1" class="text-cv-text-muted">/</span>
          <span v-if="i === breadcrumb.length - 1" class="text-cv-text-secondary">{{ item }}</span>
        </template>
      </div>
    </div>

    <!-- Right: Status + Settings -->
    <div class="flex items-center gap-4">
      <div class="flex items-center gap-2 text-[13px]">
        <span class="w-2 h-2 rounded-full" :class="serviceConnected ? 'bg-cv-success' : 'bg-cv-danger'" />
        <span class="text-cv-text-secondary">{{ serviceConnected ? '推理服务已连接' : '推理服务未连接' }}</span>
      </div>
      <button @click="router.push('/settings')"
              class="w-8 h-8 flex items-center justify-center rounded-cv-md text-cv-text-secondary hover:text-cv-text hover:bg-cv-hover transition-all cursor-pointer">
        <svg class="w-[18px] h-[18px]" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5">
          <circle cx="10" cy="10" r="3" />
          <path d="M10 1v2M10 17v2M1 10h2M17 10h2M3.5 3.5l1.5 1.5M15 15l1.5 1.5M16.5 3.5L15 5M5 15l-1.5 1.5" stroke-linecap="round" />
        </svg>
      </button>
    </div>
  </header>
</template>
