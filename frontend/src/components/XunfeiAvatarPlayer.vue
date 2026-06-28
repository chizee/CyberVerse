<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import mpegts from 'mpegts.js'
import type { XunfeiSessionConfig } from '../utils/sessionLaunchState'

const props = defineProps<{
  config: XunfeiSessionConfig
}>()

const emit = defineEmits<{
  renderError: [{ message?: string; body?: unknown }]
  stateChanged: [{ streamReady: boolean }]
}>()

const { t } = useI18n()

const videoRef = ref<HTMLVideoElement | null>(null)
const streamReady = ref(false)
const statusText = ref('')
const errorText = ref('')

let player: ReturnType<typeof mpegts.createPlayer> | null = null
let playbackStatsTimer: ReturnType<typeof window.setInterval> | null = null
let playbackWaitingCount = 0
let playbackStalledCount = 0

const streamURL = computed(() => props.config.playback_url || props.config.stream_url || '')
const canUseFlv = computed(() => {
  const url = streamURL.value.toLowerCase()
  return props.config.protocol === 'flv' || url.includes('.flv') || url.includes('format=flv')
})

function emitState() {
  emit('stateChanged', { streamReady: streamReady.value })
}

function setupPlayer() {
  errorText.value = ''
  statusText.value = ''
  resetPlaybackStats()

  if (!streamURL.value) {
    errorText.value = t('session.xunfeiRenderError')
    emit('renderError', { message: errorText.value })
    emitState()
    return
  }
  if (!canUseFlv.value) {
    errorText.value = t('session.xunfeiUnsupportedProtocol', { protocol: props.config.protocol })
    emit('renderError', { message: errorText.value, body: { streamURL: streamURL.value } })
    emitState()
    return
  }
  if (!mpegts.isSupported()) {
    errorText.value = t('session.xunfeiFlvUnsupported')
    emit('renderError', { message: errorText.value })
    emitState()
    return
  }

  destroyPlayer()
  const video = videoRef.value
  if (!video) return

  player = mpegts.createPlayer({
    type: 'flv',
    isLive: true,
    url: streamURL.value,
    hasAudio: true,
    hasVideo: true,
  }, {
    enableStashBuffer: true,
    stashInitialSize: 384 * 1024,
    liveBufferLatencyChasing: true,
    liveBufferLatencyMaxLatency: 3,
    liveBufferLatencyMinRemain: 1,
    autoCleanupSourceBuffer: true,
    autoCleanupMaxBackwardDuration: 30,
    autoCleanupMinBackwardDuration: 10,
  })
  player.on(mpegts.Events.ERROR, (_type, _detail, info) => {
    streamReady.value = false
    errorText.value = t('session.xunfeiPlaybackError')
    emit('renderError', { message: errorText.value, body: info })
    emitState()
  })
  player.attachMediaElement(video)
  bindPlaybackDiagnostics(video)
  player.load()
  void video.play().catch(() => {
    errorText.value = t('session.xunfeiAutoplayBlocked')
  })

  streamReady.value = true
  statusText.value = t('session.xunfeiReady')
  emitState()
}

function destroyPlayer() {
  unbindPlaybackDiagnostics()
  if (!player) return
  try {
    player.pause()
    player.unload()
    player.detachMediaElement()
    player.destroy()
  } catch {}
  player = null
}

function resetPlaybackStats() {
  playbackWaitingCount = 0
  playbackStalledCount = 0
}

function bufferedAhead(video: HTMLVideoElement): number {
  const { buffered, currentTime } = video
  for (let i = 0; i < buffered.length; i += 1) {
    if (buffered.start(i) <= currentTime && buffered.end(i) >= currentTime) {
      return buffered.end(i) - currentTime
    }
  }
  return 0
}

function logPlaybackStats(video: HTMLVideoElement) {
  console.debug('[xunfei-player]', {
    readyState: video.readyState,
    networkState: video.networkState,
    paused: video.paused,
    currentTime: Number(video.currentTime.toFixed(2)),
    bufferedAhead: Number(bufferedAhead(video).toFixed(2)),
    waitingCount: playbackWaitingCount,
    stalledCount: playbackStalledCount,
  })
}

function onPlaybackWaiting() {
  playbackWaitingCount += 1
}

function onPlaybackStalled() {
  playbackStalledCount += 1
}

function bindPlaybackDiagnostics(video: HTMLVideoElement) {
  video.addEventListener('waiting', onPlaybackWaiting)
  video.addEventListener('stalled', onPlaybackStalled)
  playbackStatsTimer = window.setInterval(() => logPlaybackStats(video), 5000)
}

function unbindPlaybackDiagnostics() {
  const video = videoRef.value
  if (video) {
    video.removeEventListener('waiting', onPlaybackWaiting)
    video.removeEventListener('stalled', onPlaybackStalled)
  }
  if (playbackStatsTimer) {
    window.clearInterval(playbackStatsTimer)
    playbackStatsTimer = null
  }
}

function interrupt() {
}

function muteAudio(mute: boolean) {
  const video = videoRef.value
  if (video) video.muted = mute
}

function playVideo(play: boolean) {
  const video = videoRef.value
  if (!video) return
  if (play) {
    void video.play().catch(() => {
      errorText.value = t('session.xunfeiAutoplayBlocked')
    })
  } else {
    video.pause()
  }
}

onMounted(setupPlayer)
onUnmounted(() => {
  streamReady.value = false
  emitState()
  destroyPlayer()
})

defineExpose({
  interrupt,
  muteAudio,
  playVideo,
})
</script>

<template>
  <div class="xunfei-avatar-player">
    <video
      ref="videoRef"
      class="xunfei-avatar-video"
      autoplay
      playsinline
    />
    <div v-if="statusText || errorText" class="xunfei-avatar-status" :class="{ error: !!errorText }">
      {{ errorText || statusText }}
    </div>
  </div>
</template>

<style scoped>
.xunfei-avatar-player {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 0;
  background: #000;
  overflow: hidden;
}

.xunfei-avatar-video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  object-position: center center;
  display: block;
  background: #000;
}

.xunfei-avatar-status {
  position: absolute;
  left: 16px;
  bottom: 16px;
  max-width: min(80%, 28rem);
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.72);
  color: #fff;
  font-size: 13px;
  line-height: 1.35;
}

.xunfei-avatar-status.error {
  color: #fecaca;
}
</style>
