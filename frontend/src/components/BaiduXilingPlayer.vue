<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { BaiduXilingAudioEvent } from '../composables/useChat'
import type { BaiduXilingSessionConfig } from '../utils/sessionLaunchState'

const props = defineProps<{
  config: BaiduXilingSessionConfig
}>()

const emit = defineEmits<{
  renderFinished: [{ requestId: string }]
  renderError: [{ requestId: string; code?: number; body?: unknown }]
  stateChanged: [{ rtcReady: boolean; wsReady: boolean; wsReadyState?: number }]
}>()

const iframeRef = ref<HTMLIFrameElement | null>(null)
const iframeId = `baidu-xiling-iframe-${Math.random().toString(36).slice(2)}`
const rtcReady = ref(false)
const wsReady = ref(false)
const pendingAudio = ref<BaiduXilingAudioEvent[]>([])

const iframeSrc = computed(() => props.config.iframe_url)
const ready = computed(() => rtcReady.value && wsReady.value)

function postToIframe(type: string, content: Record<string, unknown>) {
  const win = iframeRef.value?.contentWindow
  if (!win) return false
  win.postMessage({ type, content }, props.config.origin || '*')
  return true
}

function flushPendingAudio() {
  if (!ready.value || pendingAudio.value.length === 0) return
  const events = pendingAudio.value.splice(0)
  for (const event of events) {
    sendAudioEvent(event)
  }
}

type BaiduXilingAudioDataPayload = {
  action: 'AUDIO_STREAM_RENDER'
  requestId: string
  body: string
}

function sendAudioData(payload: BaiduXilingAudioDataPayload) {
  postToIframe('audioData', {
    action: payload.action,
    requestId: payload.requestId,
    body: payload.body,
  })
}

function sendAudioEvent(event: BaiduXilingAudioEvent) {
  if (!event.requestId) return
  if (!ready.value) {
    pendingAudio.value.push(event)
    return
  }
  sendAudioData({
    action: 'AUDIO_STREAM_RENDER',
    requestId: event.requestId,
    body: JSON.stringify({
      audio: event.audio,
      first: event.first,
      last: event.last,
    }),
  })
}

function interrupt() {
  postToIframe('message', {
    action: 'TEXT_RENDER',
    body: '<interrupt></interrupt>',
    requestId: globalThis.crypto?.randomUUID?.() || `interrupt-${Date.now()}`,
  })
}

function muteAudio(mute: boolean) {
  postToIframe('command', {
    subType: 'muteAudio',
    subContent: mute,
  })
}

function playVideo(play: boolean) {
  postToIframe('command', {
    subType: 'playVideo',
    subContent: play,
  })
}

function handleIframeMessage(event: MessageEvent) {
  if (props.config.origin && event.origin !== props.config.origin) return
  const data = event.data
  if (!data || typeof data !== 'object') return
  const type = typeof data.type === 'string' ? data.type : ''
  const content = data.content && typeof data.content === 'object' ? data.content as Record<string, unknown> : {}
  const action = typeof content.action === 'string' ? content.action : ''
  const requestId = typeof content.requestId === 'string' ? content.requestId : ''

  if (type === 'rtcState' && action === 'remoteVideoConnected') {
    rtcReady.value = true
    emit('stateChanged', { rtcReady: rtcReady.value, wsReady: wsReady.value })
    flushPendingAudio()
    return
  }
  if (type === 'wsState') {
    const readyState = Number(content.readyState)
    wsReady.value = readyState === 1
    emit('stateChanged', {
      rtcReady: rtcReady.value,
      wsReady: wsReady.value,
      wsReadyState: Number.isFinite(readyState) ? readyState : undefined,
    })
    flushPendingAudio()
    return
  }
  if (type !== 'msg') return

  if (action === 'FINISHED') {
    emit('renderFinished', { requestId })
  } else if (action === 'RENDER_ERROR') {
    emit('renderError', {
      requestId,
      code: typeof content.code === 'number' ? content.code : undefined,
      body: content.body,
    })
  }
}

onMounted(() => {
  window.addEventListener('message', handleIframeMessage)
})

onUnmounted(() => {
  window.removeEventListener('message', handleIframeMessage)
})

defineExpose({
  sendAudioEvent,
  sendAudioData,
  interrupt,
  muteAudio,
  playVideo,
})
</script>

<template>
  <div class="baidu-xiling-player">
    <iframe
      :id="iframeId"
      ref="iframeRef"
      class="baidu-xiling-iframe"
      :src="iframeSrc"
      allow="autoplay; microphone"
      allowfullscreen
    />
  </div>
</template>

<style scoped>
.baidu-xiling-player {
  width: 100%;
  height: 100%;
  min-height: 0;
  background: #000;
  overflow: hidden;
}

.baidu-xiling-iframe {
  width: 100%;
  height: 100%;
  border: 0;
  display: block;
  background: #000;
}
</style>
