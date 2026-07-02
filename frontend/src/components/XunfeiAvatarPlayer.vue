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
  stateChanged: [{ streamReady: boolean; autoplayBlocked: boolean }]
}>()

const { t } = useI18n()

const videoRef = ref<HTMLVideoElement | null>(null)
const streamReady = ref(false)
const autoplayBlocked = ref(false)
const statusText = ref('')
const errorText = ref('')

type PlayerStats = {
  speed?: number
  decodedFrames?: number
  droppedFrames?: number
  loaderType?: string
}

type VideoQuality = {
  totalVideoFrames?: number
  droppedVideoFrames?: number
}

const LIVE_STASH_INITIAL_SIZE = 1536 * 1024
const LIVE_LATENCY_CATCHUP_THRESHOLD_SECONDS = 8
const LIVE_LATENCY_TARGET_REMAIN_SECONDS = 1.5
const LIVE_BUFFER_TARGET_SECONDS = 3
const LIVE_BUFFER_LOW_SECONDS = 1.5
const LIVE_BUFFER_CRITICAL_SECONDS = 0.75
const LIVE_BUFFER_TARGET_RATE = 0.98
const LIVE_BUFFER_LOW_RATE = 0.94
const LIVE_BUFFER_CRITICAL_RATE = 0.88
const STALL_RECOVERY_DELAY_MS = 3000
const STALL_RECOVERY_MIN_INTERVAL_MS = 12000
const STALL_RECOVERY_RELOAD_BUFFER_SECONDS = 0.75
const RENDER_FREEZE_THRESHOLD_MS = 450

let player: ReturnType<typeof mpegts.createPlayer> | null = null
let playbackStatsTimer: ReturnType<typeof window.setInterval> | null = null
let stallRecoveryTimer: ReturnType<typeof window.setTimeout> | null = null
let frameProbeHandle: number | null = null
let frameProbeActive = false
let playbackWaitingCount = 0
let playbackStalledCount = 0
let playbackRecoveryCount = 0
let renderFreezeCount = 0
let lastFrameWallTimeMs = 0
let lastRenderGapMs = 0
let maxRenderGapMs = 0
let lastRecoveryAtMs = 0
let latestPlayerStats: PlayerStats = {}

const streamURL = computed(() => props.config.playback_url || props.config.stream_url || '')
const canUseFlv = computed(() => {
  const url = streamURL.value.toLowerCase()
  return props.config.protocol === 'flv' || url.includes('.flv') || url.includes('format=flv')
})

function emitState() {
  emit('stateChanged', {
    streamReady: streamReady.value,
    autoplayBlocked: autoplayBlocked.value,
  })
}

function setupPlayer() {
  streamReady.value = false
  autoplayBlocked.value = false
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
    stashInitialSize: LIVE_STASH_INITIAL_SIZE,
    liveBufferLatencyChasing: false,
    lazyLoad: false,
    deferLoadAfterSourceOpen: false,
    statisticsInfoReportInterval: 1000,
    autoCleanupSourceBuffer: true,
    autoCleanupMaxBackwardDuration: 10,
    autoCleanupMinBackwardDuration: 4,
  })
  player.on(mpegts.Events.ERROR, (_type, _detail, info) => {
    markPlaybackError(t('session.xunfeiPlaybackError'))
    emit('renderError', { message: errorText.value, body: info })
  })
  player.on(mpegts.Events.MEDIA_INFO, info => {
    lastFrameWallTimeMs = 0
    console.info('[xunfei-player] media_info', info)
  })
  player.on(mpegts.Events.STATISTICS_INFO, (info: PlayerStats) => {
    latestPlayerStats = info || {}
  })
  player.attachMediaElement(video)
  bindPlaybackDiagnostics(video)
  player.load()
  void playVideo(true)
  emitState()
}

function markPlaybackReady() {
  streamReady.value = true
  autoplayBlocked.value = false
  errorText.value = ''
  statusText.value = ''
  emitState()
}

function markPlaybackError(message: string) {
  streamReady.value = false
  autoplayBlocked.value = false
  errorText.value = message
  emitState()
}

function destroyPlayer() {
  unbindPlaybackDiagnostics()
  clearStallRecovery()
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
  playbackRecoveryCount = 0
  renderFreezeCount = 0
  lastFrameWallTimeMs = 0
  lastRenderGapMs = 0
  maxRenderGapMs = 0
  lastRecoveryAtMs = 0
  latestPlayerStats = {}
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

function bufferedEnd(video: HTMLVideoElement): number {
  const { buffered } = video
  if (!buffered.length) return 0
  return buffered.end(buffered.length - 1)
}

function videoPlaybackQuality(video: HTMLVideoElement): VideoQuality {
  const qualityVideo = video as HTMLVideoElement & {
    getVideoPlaybackQuality?: () => VideoQuality
    webkitDecodedFrameCount?: number
    webkitDroppedFrameCount?: number
  }
  const quality = qualityVideo.getVideoPlaybackQuality?.() || {}
  return {
    totalVideoFrames: quality.totalVideoFrames ?? qualityVideo.webkitDecodedFrameCount,
    droppedVideoFrames: quality.droppedVideoFrames ?? qualityVideo.webkitDroppedFrameCount,
  }
}

function maybeCatchUpLiveLatency(video: HTMLVideoElement) {
  const end = bufferedEnd(video)
  if (!end) return
  const remain = end - video.currentTime
  if (remain <= LIVE_LATENCY_CATCHUP_THRESHOLD_SECONDS) return

  const target = Math.max(0, end - LIVE_LATENCY_TARGET_REMAIN_SECONDS)
  if (target <= video.currentTime) return
  console.info('[xunfei-player] catch_up_live_latency', {
    currentTime: Number(video.currentTime.toFixed(2)),
    target: Number(target.toFixed(2)),
    remain: Number(remain.toFixed(2)),
  })
  video.currentTime = target
}

function targetPlaybackRate(bufferAhead: number): number {
  if (bufferAhead < LIVE_BUFFER_CRITICAL_SECONDS) return LIVE_BUFFER_CRITICAL_RATE
  if (bufferAhead < LIVE_BUFFER_LOW_SECONDS) return LIVE_BUFFER_LOW_RATE
  if (bufferAhead < LIVE_BUFFER_TARGET_SECONDS) return LIVE_BUFFER_TARGET_RATE
  return 1
}

function adjustLivePlaybackRate(video: HTMLVideoElement, bufferAhead: number) {
  const nextRate = targetPlaybackRate(bufferAhead)
  if (Math.abs(video.playbackRate - nextRate) < 0.005) return

  video.playbackRate = nextRate
  console.info('[xunfei-player] adjust_playback_rate', {
    playbackRate: nextRate,
    bufferedAhead: Number(bufferAhead.toFixed(2)),
    targetBuffer: LIVE_BUFFER_TARGET_SECONDS,
  })
}

function logPlaybackStats(video: HTMLVideoElement) {
  maybeCatchUpLiveLatency(video)
  const ahead = bufferedAhead(video)
  adjustLivePlaybackRate(video, ahead)
  const quality = videoPlaybackQuality(video)
  console.debug('[xunfei-player]', {
    readyState: video.readyState,
    networkState: video.networkState,
    paused: video.paused,
    playbackRate: video.playbackRate,
    currentTime: Number(video.currentTime.toFixed(2)),
    bufferedAhead: Number(ahead.toFixed(2)),
    decodedFrames: quality.totalVideoFrames ?? latestPlayerStats.decodedFrames,
    droppedFrames: quality.droppedVideoFrames ?? latestPlayerStats.droppedFrames,
    transmuxSpeedKBps: latestPlayerStats.speed,
    loaderType: latestPlayerStats.loaderType,
    waitingCount: playbackWaitingCount,
    stalledCount: playbackStalledCount,
    recoveryCount: playbackRecoveryCount,
    renderFreezeCount,
    lastRenderGapMs,
    maxRenderGapMs,
  })
}

function onPlaybackWaiting() {
  playbackWaitingCount += 1
  scheduleStallRecovery('waiting')
}

function onPlaybackStalled() {
  playbackStalledCount += 1
  scheduleStallRecovery('stalled')
}

function onPlaybackPlaying() {
  clearStallRecovery()
  markPlaybackReady()
}

function onNativePlaybackError() {
  markPlaybackError(t('session.xunfeiPlaybackError'))
}

function clearStallRecovery() {
  if (stallRecoveryTimer) {
    window.clearTimeout(stallRecoveryTimer)
    stallRecoveryTimer = null
  }
}

function recoverStalledPlayback(reason: string) {
  const video = videoRef.value
  if (!video || !player || video.paused) return
  const ahead = bufferedAhead(video)
  if (ahead >= STALL_RECOVERY_RELOAD_BUFFER_SECONDS || video.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) {
    console.info('[xunfei-player] skip_stall_recovery_buffered', {
      reason,
      readyState: video.readyState,
      currentTime: Number(video.currentTime.toFixed(2)),
      bufferedAhead: Number(ahead.toFixed(2)),
    })
    return
  }

  const now = Date.now()
  if (now - lastRecoveryAtMs < STALL_RECOVERY_MIN_INTERVAL_MS) return
  lastRecoveryAtMs = now
  playbackRecoveryCount += 1
  console.warn('[xunfei-player] recovering stalled live stream', {
    reason,
    readyState: video.readyState,
    currentTime: Number(video.currentTime.toFixed(2)),
    bufferedAhead: Number(ahead.toFixed(2)),
    recoveryCount: playbackRecoveryCount,
  })

  try {
    lastFrameWallTimeMs = 0
    player.unload()
    player.load()
    void playVideo(true)
  } catch (err) {
    console.warn('[xunfei-player] recovery failed', err)
  }
}

function scheduleStallRecovery(reason: string) {
  clearStallRecovery()
  stallRecoveryTimer = window.setTimeout(() => {
    stallRecoveryTimer = null
    recoverStalledPlayback(reason)
  }, STALL_RECOVERY_DELAY_MS)
}

function scheduleFrameProbe(video: HTMLVideoElement) {
  const probeVideo = video as HTMLVideoElement & {
    requestVideoFrameCallback?: (callback: (now: number, metadata: VideoFrameCallbackMetadata) => void) => number
    cancelVideoFrameCallback?: (handle: number) => void
  }
  if (!probeVideo.requestVideoFrameCallback) return

  frameProbeActive = true
  const onFrame = (now: number) => {
    if (!frameProbeActive) return
    if (lastFrameWallTimeMs > 0) {
      const gap = Math.round(now - lastFrameWallTimeMs)
      lastRenderGapMs = gap
      if (gap > maxRenderGapMs) maxRenderGapMs = gap
      if (gap > RENDER_FREEZE_THRESHOLD_MS) {
        renderFreezeCount += 1
        const quality = videoPlaybackQuality(video)
        console.warn('[xunfei-player] render_frame_gap', {
          gapMs: gap,
          freezeCount: renderFreezeCount,
          readyState: video.readyState,
          networkState: video.networkState,
          playbackRate: video.playbackRate,
          currentTime: Number(video.currentTime.toFixed(2)),
          bufferedAhead: Number(bufferedAhead(video).toFixed(2)),
          decodedFrames: quality.totalVideoFrames ?? latestPlayerStats.decodedFrames,
          droppedFrames: quality.droppedVideoFrames ?? latestPlayerStats.droppedFrames,
        })
      }
    }
    lastFrameWallTimeMs = now
    frameProbeHandle = probeVideo.requestVideoFrameCallback?.(onFrame) ?? null
  }

  frameProbeHandle = probeVideo.requestVideoFrameCallback(onFrame)
}

function cancelFrameProbe(video: HTMLVideoElement | null) {
  frameProbeActive = false
  if (frameProbeHandle == null || !video) {
    frameProbeHandle = null
    return
  }
  const probeVideo = video as HTMLVideoElement & {
    cancelVideoFrameCallback?: (handle: number) => void
  }
  probeVideo.cancelVideoFrameCallback?.(frameProbeHandle)
  frameProbeHandle = null
}

function bindPlaybackDiagnostics(video: HTMLVideoElement) {
  video.addEventListener('waiting', onPlaybackWaiting)
  video.addEventListener('stalled', onPlaybackStalled)
  video.addEventListener('playing', onPlaybackPlaying)
  video.addEventListener('error', onNativePlaybackError)
  scheduleFrameProbe(video)
  playbackStatsTimer = window.setInterval(() => logPlaybackStats(video), 1000)
}

function unbindPlaybackDiagnostics() {
  const video = videoRef.value
  if (video) {
    video.removeEventListener('waiting', onPlaybackWaiting)
    video.removeEventListener('stalled', onPlaybackStalled)
    video.removeEventListener('playing', onPlaybackPlaying)
    video.removeEventListener('error', onNativePlaybackError)
  }
  cancelFrameProbe(video)
  if (playbackStatsTimer) {
    window.clearInterval(playbackStatsTimer)
    playbackStatsTimer = null
  }
  clearStallRecovery()
}

function interrupt() {
}

function muteAudio(mute: boolean) {
  const video = videoRef.value
  if (video) video.muted = mute
}

async function playVideo(play: boolean): Promise<boolean> {
  const video = videoRef.value
  if (!video) return false
  if (play) {
    try {
      await video.play()
      autoplayBlocked.value = false
      emitState()
      return true
    } catch {
      streamReady.value = false
      autoplayBlocked.value = true
      errorText.value = t('session.xunfeiAutoplayBlocked')
      emitState()
      return false
    }
  }
  video.pause()
  return true
}

onMounted(setupPlayer)
onUnmounted(() => {
  streamReady.value = false
  autoplayBlocked.value = false
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
