<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    type: 'user' | 'ai'
    label: string
    /** Legacy: random animation when no real levels */
    active?: boolean
    /** Normalized 0–1 per band; when set, shows real mic/spectrum levels */
    levels?: number[] | null
    muted?: boolean
  }>(),
  { active: false, levels: null, muted: false },
)

const bars = ref<number[]>([2, 2, 2, 2, 2, 2, 2, 2])
let animId = 0

const useRealLevels = computed(() => props.levels != null && props.levels.length > 0)

const displayBarHeights = computed(() => {
  if (useRealLevels.value && props.levels!.length > 0) {
    if (props.muted) {
      return props.levels!.map(() => 2)
    }
    return props.levels!.map((v) => 4 + Math.min(1, v) * 20)
  }
  return bars.value
})

function animate() {
  if (!useRealLevels.value) {
    if (props.active) {
      bars.value = bars.value.map(() => Math.random() * 20 + 4)
    } else {
      bars.value = bars.value.map(() => 2)
    }
  }
  animId = requestAnimationFrame(animate)
}

watch(useRealLevels, (real) => {
  if (!real && bars.value.length !== 8) {
    bars.value = [2, 2, 2, 2, 2, 2, 2, 2]
  }
})

onMounted(() => {
  animId = requestAnimationFrame(animate)
})
onUnmounted(() => cancelAnimationFrame(animId))

const colorClass = {
  user: {
    bar: 'bg-cv-success',
    glow: 'shadow-[0_0_4px_rgba(34,197,94,0.4)]',
    text: 'text-cv-success',
    border: 'border-green-900/40',
    bg: 'bg-green-950/80',
  },
  ai: {
    bar: 'bg-cv-accent',
    glow: 'shadow-[0_0_4px_rgba(59,130,246,0.4)]',
    text: 'text-cv-accent',
    border: 'border-blue-900/40',
    bg: 'bg-blue-950/80',
  },
}

const barWidthClass = computed(() =>
  displayBarHeights.value.length > 10 ? 'w-[2px]' : 'w-[3px]',
)
</script>

<template>
  <div
    class="flex items-center gap-3 min-h-11 px-4 py-2 rounded-full backdrop-blur-sm border"
    :class="[colorClass[type].bg, colorClass[type].border]"
  >
    <svg
      v-if="type === 'user'"
      class="w-3.5 h-3.5 shrink-0"
      :class="colorClass[type].text"
      viewBox="0 0 14 14"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
    >
      <rect x="5" y="1" width="4" height="7" rx="2" />
      <path d="M3 7a4 4 0 008 0M7 11v2M5 13h4" stroke-linecap="round" />
    </svg>
    <svg
      v-else
      class="w-3.5 h-3.5 shrink-0"
      :class="colorClass[type].text"
      viewBox="0 0 14 14"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
    >
      <path d="M1 5v4M4 3v8M7 1v12M10 3v8M13 5v4" stroke-linecap="round" />
    </svg>

    <div class="flex items-center gap-px h-6 min-w-[4rem]">
      <div
        v-for="(h, i) in displayBarHeights"
        :key="i"
        class="rounded-full transition-[height] duration-75"
        :class="[colorClass[type].bar, colorClass[type].glow, barWidthClass]"
        :style="{ height: `${h}px` }"
      />
    </div>

    <span class="text-[13px] font-medium whitespace-nowrap" :class="colorClass[type].text">
      {{ label }}
    </span>
  </div>
</template>
