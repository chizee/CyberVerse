<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppHeader from '../components/AppHeader.vue'
import { useCharacterStore } from '../stores/characters'
import { createOfflineVideo, deleteOfflineVideo, listOfflineVideos, renameOfflineVideo } from '../services/api'
import type { OfflineVideoJob } from '../types'
import { saveLaunchWorkspaceMode } from '../utils/launchModePreference'
import { formatVoiceTypeDisplay } from '../utils/voice'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const store = useCharacterStore()

const characterId = computed(() => route.params.id as string)
const pageTitle = computed(() => t('launch.workspaceTitle'))
const hasCurrentCharacter = computed(() => store.current?.id === characterId.value)
const showLoading = computed(() => loading.value && !hasCurrentCharacter.value)
const inputType = ref<'text' | 'audio'>('text')
const scriptText = ref('')
const audioFile = ref<File | null>(null)
const outputWidth = ref(1080)
const outputHeight = ref(1920)
const transparentBackground = ref(false)
const outputSettingsExpanded = ref(false)
const ttsPerson = ref('')
const ttsLan = ref('auto')
const ttsSpeed = ref(5)
const ttsVolume = ref(5)
const ttsPitch = ref(5)
const backgroundImageUrl = ref('')
const autoAnimoji = ref(false)
const jobs = ref<OfflineVideoJob[]>([])
const loading = ref(true)
const submitting = ref(false)
const renaming = ref(false)
const editingJobId = ref('')
const editingTitle = ref('')
const highlightedJobId = ref('')
const currentJobPage = ref(1)
const failedReasonJob = ref<OfflineVideoJob | null>(null)
const errorMessage = ref('')
let pollTimer: ReturnType<typeof setInterval> | null = null
let highlightTimer: ReturnType<typeof setTimeout> | null = null

const JOBS_PER_PAGE = 8
const BAIDU_XILING_COMMON_ASSETS_URL = 'https://xiling.cloud.baidu.com/open/commonAssets/list'

interface OfflineVideoSettings {
  inputType?: 'text' | 'audio'
  outputWidth?: number
  outputHeight?: number
  transparentBackground?: boolean
  outputSettingsExpanded?: boolean
  ttsPerson?: string
  ttsLan?: string
  ttsSpeed?: number
  ttsVolume?: number
  ttsPitch?: number
  backgroundImageUrl?: string
  autoAnimoji?: boolean
}

const ttsLanguageOptions = [
  'auto',
  'Chinese',
  'Chinese,Yue',
  'English',
  'Russian',
  'Spanish',
  'French',
  'Portuguese',
  'German',
  'Turkish',
  'Dutch',
  'Ukrainian',
  'Vietnamese',
  'Indonesian',
  'Japanese',
  'Italian',
  'Korean',
  'Thai',
  'Polish',
  'Romanian',
  'Greek',
  'Czech',
  'Finnish',
  'Hindi',
]

const isBaiduXilingCharacter = computed(() => store.current?.avatar_backend === 'baidu_xiling')
const hasActiveJobs = computed(() => jobs.value.some(job => job.status === 'queued' || job.status === 'running'))
const totalJobPages = computed(() => Math.max(1, Math.ceil(jobs.value.length / JOBS_PER_PAGE)))
const pagedJobs = computed(() => {
  const start = (currentJobPage.value - 1) * JOBS_PER_PAGE
  return jobs.value.slice(start, start + JOBS_PER_PAGE)
})
const failedReasonText = computed(() =>
  failedReasonJob.value?.error?.trim()
    || failedReasonJob.value?.message?.trim()
    || t('offlineVideo.failureReasonUnavailable'),
)
const characterCoverImage = computed(() => {
  const character = store.current
  if (!character) return ''
  if (character.avatar_backend === 'baidu_xiling') {
    return character.baidu_xiling?.thumbnail_url || character.baidu_xiling?.source_image_url || ''
  }
  return character.avatar_image || ''
})
const audioHint = computed(() =>
  isBaiduXilingCharacter.value ? t('offlineVideo.baiduAudioHint') : t('offlineVideo.audioHint'),
)
const audioAccept = computed(() =>
  isBaiduXilingCharacter.value ? '.wav,.mp3,.m4a,.wma,audio/*' : '.wav,.pcm,.s16le,audio/*',
)
const isTTSPersonRequired = computed(() => isBaiduXilingCharacter.value && inputType.value === 'text')
const isMissingTTSPerson = computed(() => isTTSPersonRequired.value && !ttsPerson.value.trim())
const canGenerate = computed(() => {
  if (submitting.value) return false
  if (isBaiduXilingCharacter.value && (!outputWidth.value || !outputHeight.value)) return false
  if (inputType.value === 'text') {
    if (isMissingTTSPerson.value) return false
    return scriptText.value.trim().length > 0
  }
  return !!audioFile.value
})

function offlineVideoSettingsKey(): string {
  return `cyberverse.offlineVideoSettings.v1:${characterId.value}`
}

function readOfflineVideoSettings(): OfflineVideoSettings {
  if (typeof window === 'undefined') return {}
  try {
    const raw = window.localStorage.getItem(offlineVideoSettingsKey())
    return raw ? JSON.parse(raw) as OfflineVideoSettings : {}
  } catch {
    return {}
  }
}

function saveOfflineVideoSettings() {
  if (typeof window === 'undefined') return
  const settings: OfflineVideoSettings = {
    inputType: inputType.value,
    outputWidth: outputWidth.value,
    outputHeight: outputHeight.value,
    transparentBackground: transparentBackground.value,
    outputSettingsExpanded: outputSettingsExpanded.value,
    ttsPerson: ttsPerson.value,
    ttsLan: ttsLan.value,
    ttsSpeed: ttsSpeed.value,
    ttsVolume: ttsVolume.value,
    ttsPitch: ttsPitch.value,
    backgroundImageUrl: backgroundImageUrl.value,
    autoAnimoji: autoAnimoji.value,
  }
  window.localStorage.setItem(offlineVideoSettingsKey(), JSON.stringify(settings))
}

function numberSetting(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function boolSetting(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

function stringSetting(value: unknown, fallback: string): string {
  return typeof value === 'string' ? value : fallback
}

function clampProgress(job: OfflineVideoJob): number {
  return Math.min(100, Math.max(0, job.progress || 0))
}

function progressStyle(job: OfflineVideoJob) {
  return { width: `${clampProgress(job)}%` }
}

function statusLabel(job: OfflineVideoJob): string {
  return t(`offlineVideo.status.${job.status}`)
}

function isActiveJob(job: OfflineVideoJob): boolean {
  return job.status === 'queued' || job.status === 'running'
}

function formatDate(value?: string): string {
  if (!value) return t('common.emptyDash')
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return date.toLocaleString()
}

async function refreshJobs() {
  if (!characterId.value) return
  const resp = await listOfflineVideos(characterId.value)
  const existingById = new Map(jobs.value.map(job => [job.id, job]))
  jobs.value = resp.videos.map(job => ({
    ...(existingById.get(job.id) || {}),
    ...job,
  }))
}

function markJobHighlighted(jobId: string) {
  highlightedJobId.value = jobId
  const index = jobs.value.findIndex(job => job.id === jobId)
  if (index >= 0) {
    currentJobPage.value = Math.floor(index / JOBS_PER_PAGE) + 1
  }
  if (highlightTimer) clearTimeout(highlightTimer)
  highlightTimer = setTimeout(() => {
    if (highlightedJobId.value === jobId) highlightedJobId.value = ''
  }, 6000)
}

function changeJobPage(nextPage: number) {
  currentJobPage.value = Math.min(totalJobPages.value, Math.max(1, nextPage))
}

function openFailedReason(job: OfflineVideoJob) {
  failedReasonJob.value = job
}

function closeFailedReason() {
  failedReasonJob.value = null
}

function handleAudioChange(event: Event) {
  const input = event.target as HTMLInputElement
  audioFile.value = input.files?.[0] || null
}

function loadOutputSettings() {
  const config = store.current?.baidu_xiling
  const settings = readOfflineVideoSettings()
  inputType.value = settings.inputType === 'audio' ? 'audio' : 'text'
  outputWidth.value = config?.width || 1080
  outputHeight.value = config?.height || 1920
  outputWidth.value = numberSetting(settings.outputWidth, outputWidth.value)
  outputHeight.value = numberSetting(settings.outputHeight, outputHeight.value)
  transparentBackground.value = boolSetting(settings.transparentBackground, false)
  outputSettingsExpanded.value = boolSetting(settings.outputSettingsExpanded, isTTSPersonRequired.value)
  ttsPerson.value = stringSetting(settings.ttsPerson, '')
  ttsLan.value = stringSetting(settings.ttsLan, 'auto')
  ttsSpeed.value = numberSetting(settings.ttsSpeed, 5)
  ttsVolume.value = numberSetting(settings.ttsVolume, 5)
  ttsPitch.value = numberSetting(settings.ttsPitch, 5)
  backgroundImageUrl.value = stringSetting(settings.backgroundImageUrl, '')
  autoAnimoji.value = boolSetting(settings.autoAnimoji, false)
}

watch(
  [
    inputType,
    outputWidth,
    outputHeight,
    transparentBackground,
    outputSettingsExpanded,
    ttsPerson,
    ttsLan,
    ttsSpeed,
    ttsVolume,
    ttsPitch,
    backgroundImageUrl,
    autoAnimoji,
  ],
  saveOfflineVideoSettings,
)

watch(jobs, () => {
  if (currentJobPage.value > totalJobPages.value) {
    currentJobPage.value = totalJobPages.value
  }
})

async function submitJob() {
  if (!canGenerate.value) return
  submitting.value = true
  errorMessage.value = ''
  try {
    const createdJob = await createOfflineVideo(characterId.value, {
      inputType: inputType.value,
      text: scriptText.value.trim(),
      audio: audioFile.value,
      width: outputWidth.value,
      height: outputHeight.value,
      transparent: transparentBackground.value,
      ttsPerson: ttsPerson.value.trim(),
      ttsLan: ttsLan.value,
      ttsSpeed: ttsSpeed.value,
      ttsVolume: ttsVolume.value,
      ttsPitch: ttsPitch.value,
      backgroundImageUrl: backgroundImageUrl.value.trim(),
      autoAnimoji: autoAnimoji.value,
    })
    if (inputType.value === 'text') {
      scriptText.value = ''
    }
    await refreshJobs()
    markJobHighlighted(createdJob.id)
  } catch (err) {
    errorMessage.value = err instanceof Error ? err.message : t('offlineVideo.createFailed')
  } finally {
    submitting.value = false
  }
}

function startRename(job: OfflineVideoJob) {
  editingJobId.value = job.id
  editingTitle.value = job.title
  errorMessage.value = ''
}

function cancelRename() {
  editingJobId.value = ''
  editingTitle.value = ''
}

async function submitRename(job: OfflineVideoJob) {
  const nextTitle = editingTitle.value.trim()
  if (!nextTitle || renaming.value) return
  renaming.value = true
  errorMessage.value = ''
  try {
    await renameOfflineVideo(characterId.value, job.id, nextTitle)
    cancelRename()
    await refreshJobs()
  } catch (err) {
    errorMessage.value = err instanceof Error ? err.message : t('offlineVideo.renameFailed')
  } finally {
    renaming.value = false
  }
}

async function removeJob(job: OfflineVideoJob) {
  if (!window.confirm(t('offlineVideo.deleteConfirm'))) return
  await deleteOfflineVideo(characterId.value, job.id)
  await refreshJobs()
}

onMounted(async () => {
  saveLaunchWorkspaceMode('offline')
  await store.fetchOne(characterId.value).catch(() => {})
  loadOutputSettings()
  await refreshJobs().catch((err) => {
    errorMessage.value = err instanceof Error ? err.message : t('offlineVideo.loadFailed')
  })
  loading.value = false
  pollTimer = setInterval(() => {
    if (hasActiveJobs.value) {
      refreshJobs().catch(() => {})
    }
  }, 4000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
  if (highlightTimer) clearTimeout(highlightTimer)
})
</script>

<template>
  <div class="offline-page flex min-h-screen flex-col bg-cv-base text-cv-text">
    <AppHeader showBack :title="pageTitle" />

    <div class="py-6 text-center">
      <div class="cv-pi-segment mx-auto h-11 w-[260px] grid-cols-2">
        <button class="cv-pi-segment-item cv-pi-segment-item--active" type="button">
          {{ t('offlineVideo.offlineMode') }}
        </button>
        <button
          class="cv-pi-segment-item"
          type="button"
          @click="router.push(`/launch/${characterId}/live`)"
        >
          {{ t('offlineVideo.liveMode') }}
        </button>
      </div>
    </div>

    <main class="mx-auto flex w-full max-w-[1100px] flex-1 flex-col gap-8 px-12 pb-24">
      <div v-if="showLoading" class="py-24 text-center text-cv-text-secondary">{{ t('common.loading') }}</div>

      <template v-else-if="store.current">
        <div class="grid gap-8 lg:grid-cols-[300px_minmax(0,1fr)]">
          <aside class="character-panel">
            <div class="avatar-shell">
              <img
                v-if="characterCoverImage"
                :src="characterCoverImage"
                :alt="store.current.name"
                class="h-full w-full object-cover"
              >
              <div v-else class="flex h-full items-center justify-center text-sm text-cv-text-muted">
                {{ t('offlineVideo.noAvatar') }}
              </div>
            </div>
            <div>
              <h2 class="truncate text-xl font-bold text-[#fbf6ef]">{{ store.current.name }}</h2>
              <p class="mt-2 text-sm leading-6 text-[#8d96a6]">{{ store.current.description || t('characterCard.noDescription') }}</p>
            </div>
            <div class="info-grid">
              <span>{{ t('launch.voice') }}</span>
              <strong>{{ formatVoiceTypeDisplay(store.current.voice_type, t, locale) }}</strong>
              <span>{{ t('offlineVideo.avatarSource') }}</span>
              <strong>{{ store.current.avatar_backend }}</strong>
            </div>
            <button
              class="cv-pi-button cv-pi-button--compact"
              type="button"
              @click="router.push(`/characters/${characterId}/edit`)"
            >
              {{ t('launch.editCharacter') }}
            </button>
          </aside>

          <section class="production-panel">
            <form class="generator-form" @submit.prevent="submitJob">
              <div v-if="errorMessage" class="notice error">{{ errorMessage }}</div>

              <div class="cv-pi-segment h-[42px] w-[260px] grid-cols-2" role="tablist" :aria-label="t('offlineVideo.inputMode')">
                <button
                  type="button"
                  class="cv-pi-segment-item"
                  :class="{ 'cv-pi-segment-item--active': inputType === 'text' }"
                  @click="inputType = 'text'"
                >
                  {{ t('offlineVideo.textInput') }}
                </button>
                <button
                  type="button"
                  class="cv-pi-segment-item"
                  :class="{ 'cv-pi-segment-item--active': inputType === 'audio' }"
                  @click="inputType = 'audio'"
                >
                  {{ t('offlineVideo.audioInput') }}
                </button>
              </div>

              <template v-if="inputType === 'text'">
                <label class="field-label" for="offline-script">{{ t('offlineVideo.script') }}</label>
                <textarea
                  id="offline-script"
                  v-model="scriptText"
                  class="script-input"
                  :placeholder="t('offlineVideo.scriptPlaceholder')"
                />
              </template>

              <template v-else>
                <label class="field-label" for="offline-audio">{{ t('offlineVideo.audioFile') }}</label>
                <input
                  id="offline-audio"
                  class="file-input"
                  type="file"
                  :accept="audioAccept"
                  @change="handleAudioChange"
                >
                <p class="field-hint">{{ audioHint }}</p>
              </template>

              <section v-if="isBaiduXilingCharacter" class="output-settings">
                <div class="settings-header">
                  <button
                    class="settings-toggle"
                    type="button"
                    @click="outputSettingsExpanded = !outputSettingsExpanded"
                  >
                    <h3>{{ t('offlineVideo.outputSettings') }}</h3>
                    <span class="settings-chevron" :class="{ expanded: outputSettingsExpanded }" aria-hidden="true" />
                  </button>
                </div>
                <template v-if="outputSettingsExpanded">
                  <div class="settings-grid">
                    <label class="settings-field">
                      <span>{{ t('offlineVideo.outputWidth') }}</span>
                      <input v-model.number="outputWidth" class="number-input" type="number" min="1" max="3840" step="1">
                    </label>
                    <label class="settings-field">
                      <span>{{ t('offlineVideo.outputHeight') }}</span>
                      <input v-model.number="outputHeight" class="number-input" type="number" min="1" max="3840" step="1">
                    </label>
                    <label class="toggle-field">
                      <input v-model="transparentBackground" type="checkbox">
                      <span>{{ t('offlineVideo.transparentBackground') }}</span>
                    </label>
                    <label class="settings-field wide-field">
                      <span>{{ t('offlineVideo.backgroundImageUrl') }}</span>
                      <input
                        v-model="backgroundImageUrl"
                        class="number-input"
                        type="url"
                        :placeholder="t('offlineVideo.backgroundImageUrlPlaceholder')"
                      >
                    </label>
                  </div>

                  <div v-if="inputType === 'text'" class="settings-section">
                    <h4 class="settings-section-title">{{ t('offlineVideo.ttsSettings') }}</h4>
                    <div class="settings-grid">
                      <div class="settings-field">
                        <div class="field-label-row">
                          <label for="offline-tts-person">{{ t('offlineVideo.ttsPerson') }}</label>
                          <span v-if="isTTSPersonRequired" class="required-mark" aria-hidden="true">*</span>
                          <span class="field-help">
                            <button
                              class="field-help-button"
                              type="button"
                              :aria-label="t('offlineVideo.ttsPersonHelpLabel')"
                            >
                              ?
                            </button>
                            <span class="field-tooltip" role="tooltip">
                              {{ t('offlineVideo.ttsPersonHelpPrefix') }}
                              <a :href="BAIDU_XILING_COMMON_ASSETS_URL" target="_blank" rel="noreferrer">
                                {{ t('offlineVideo.ttsPersonHelpLink') }}
                              </a>
                              {{ t('offlineVideo.ttsPersonHelpSuffix') }}
                            </span>
                          </span>
                        </div>
                        <input
                          id="offline-tts-person"
                          v-model="ttsPerson"
                          class="number-input"
                          type="text"
                          :required="isTTSPersonRequired"
                          :placeholder="t('offlineVideo.ttsPersonPlaceholder')"
                        >
                      </div>
                      <label class="settings-field">
                        <span>{{ t('offlineVideo.ttsLan') }}</span>
                        <select v-model="ttsLan" class="number-input">
                          <option v-for="lan in ttsLanguageOptions" :key="lan" :value="lan">{{ lan }}</option>
                        </select>
                      </label>
                      <label class="settings-field">
                        <span>{{ t('offlineVideo.ttsSpeed') }}</span>
                        <input v-model.number="ttsSpeed" class="number-input" type="number" min="0" max="15" step="1">
                      </label>
                      <label class="settings-field">
                        <span>{{ t('offlineVideo.ttsVolume') }}</span>
                        <input v-model.number="ttsVolume" class="number-input" type="number" min="0" max="15" step="1">
                      </label>
                      <label class="settings-field">
                        <span>{{ t('offlineVideo.ttsPitch') }}</span>
                        <input v-model.number="ttsPitch" class="number-input" type="number" min="0" max="15" step="1">
                      </label>
                      <label class="toggle-field">
                        <input v-model="autoAnimoji" type="checkbox">
                        <span>{{ t('offlineVideo.autoAnimoji') }}</span>
                      </label>
                    </div>
                  </div>

                  <div v-else class="settings-section">
                    <h4 class="settings-section-title">{{ t('offlineVideo.voiceDrive') }}</h4>
                    <p class="field-hint">{{ t('offlineVideo.voiceDriveHint') }}</p>
                  </div>
                </template>
              </section>

              <div class="flex justify-end">
                <button class="cv-pi-button cv-pi-button--primary" type="submit" :disabled="!canGenerate">
                  {{ submitting ? t('offlineVideo.submitting') : t('offlineVideo.generate') }}
                </button>
              </div>
            </form>
          </section>
        </div>

        <section class="jobs-section">
          <div class="jobs-header">
            <div class="jobs-title-block">
              <h2>{{ t('offlineVideo.library') }}</h2>
              <p>{{ t('offlineVideo.libraryCount', { count: jobs.length }) }}</p>
            </div>
          </div>
          <div v-if="jobs.length === 0" class="empty-jobs">{{ t('offlineVideo.empty') }}</div>
          <div v-else class="video-grid">
            <article
              v-for="job in pagedJobs"
              :key="job.id"
              class="video-card"
              :class="{
                'video-card--active': isActiveJob(job),
                'video-card--highlight': highlightedJobId === job.id,
              }"
            >
              <div class="video-preview" :class="`video-preview--${job.status}`">
                <video
                  v-if="job.status === 'completed' && job.video_url"
                  :src="job.video_url"
                  class="video-preview-media"
                  muted
                  playsinline
                  preload="metadata"
                />
                <div v-else-if="isActiveJob(job)" class="video-preview-state">
                  <span class="job-spinner" aria-hidden="true" />
                  <span>{{ statusLabel(job) }}</span>
                </div>
                <button
                  v-else-if="job.status === 'failed'"
                  class="video-preview-state video-preview-button"
                  type="button"
                  :aria-label="t('offlineVideo.viewFailureReason')"
                  @click="openFailedReason(job)"
                >
                  <span class="video-preview-mark">!</span>
                  <span>{{ t('offlineVideo.status.failed') }}</span>
                </button>
                <div v-else class="video-preview-state">
                  <span class="video-preview-mark">▶</span>
                  <span>{{ t('offlineVideo.status.completed') }}</span>
                </div>
              </div>

              <div class="video-card-body">
                <template v-if="editingJobId === job.id">
                  <input
                    v-model="editingTitle"
                    class="rename-input"
                    type="text"
                    :placeholder="t('offlineVideo.titlePlaceholder')"
                    @keydown.enter.prevent="submitRename(job)"
                    @keydown.esc.prevent="cancelRename"
                  >
                  <div class="video-card-actions">
                    <button
                      class="cv-pi-button cv-pi-button--primary cv-pi-button--compact"
                      type="button"
                      :disabled="renaming || !editingTitle.trim()"
                      @click="submitRename(job)"
                    >
                      {{ t('common.save') }}
                    </button>
                    <button class="cv-pi-button cv-pi-button--compact" type="button" @click="cancelRename">
                      {{ t('common.cancel') }}
                    </button>
                  </div>
                </template>
                <template v-else>
                  <button class="video-title" type="button" :title="job.title" @click="startRename(job)">
                    {{ job.title }}
                  </button>
                  <p class="video-date">{{ formatDate(job.created_at) }}</p>
                  <div v-if="isActiveJob(job)" class="video-progress">
                    <div class="progress-track">
                      <div class="progress-bar" :style="progressStyle(job)" />
                    </div>
                  </div>
                  <div class="video-card-actions">
                    <a
                      v-if="job.video_url"
                      class="cv-pi-button cv-pi-button--primary cv-pi-button--compact"
                      :href="job.video_url"
                      target="_blank"
                      rel="noreferrer"
                    >
                      {{ t('offlineVideo.openVideo') }}
                    </a>
                    <button
                      class="cv-pi-button cv-pi-button--compact"
                      type="button"
                      :disabled="job.status === 'queued' || job.status === 'running'"
                      @click="removeJob(job)"
                    >
                      {{ t('common.delete') }}
                    </button>
                  </div>
                </template>
              </div>
            </article>
          </div>
          <div v-if="jobs.length > JOBS_PER_PAGE" class="video-pagination">
            <span>{{ t('offlineVideo.pagination', { page: currentJobPage, total: totalJobPages }) }}</span>
            <div class="video-pagination-actions">
              <button
                class="cv-pi-button cv-pi-button--compact"
                type="button"
                :disabled="currentJobPage <= 1"
                @click="changeJobPage(currentJobPage - 1)"
              >
                {{ t('offlineVideo.previousPage') }}
              </button>
              <button
                class="cv-pi-button cv-pi-button--compact"
                type="button"
                :disabled="currentJobPage >= totalJobPages"
                @click="changeJobPage(currentJobPage + 1)"
              >
                {{ t('offlineVideo.nextPage') }}
              </button>
            </div>
          </div>
        </section>
      </template>
    </main>

    <div v-if="failedReasonJob" class="failure-modal-backdrop" @click.self="closeFailedReason">
      <section class="failure-modal" role="dialog" aria-modal="true" :aria-label="t('offlineVideo.failureReason')">
        <div class="failure-modal-header">
          <button class="failure-modal-close" type="button" :aria-label="t('offlineVideo.closeFailureReason')" @click="closeFailedReason">
            ×
          </button>
        </div>
        <pre class="failure-modal-message">{{ failedReasonText }}</pre>
      </section>
    </div>
  </div>
</template>
<style scoped>
.character-panel,
.production-panel,
.jobs-section {
  border: 1px solid var(--color-cv-border);
  border-radius: 8px;
  background: var(--color-cv-surface);
}

.character-panel {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 22px;
}

.avatar-shell {
  aspect-ratio: 3 / 4;
  overflow: hidden;
  border: 1px solid #2b3542;
  background: #0b0d12;
}

.info-grid {
  display: grid;
  grid-template-columns: 96px minmax(0, 1fr);
  gap: 10px 14px;
  font-size: 13px;
}

.info-grid span {
  color: #798394;
}

.info-grid strong {
  min-width: 0;
  overflow: hidden;
  color: #f0f4f8;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.production-panel {
  padding: 22px;
}

.generator-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.field-label {
  color: #c4ccd8;
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0;
}

.text-input,
.script-input,
.file-input {
  width: 100%;
  border: 1px solid #303a49;
  background: #0b0d12;
  color: #f4f7fb;
  font-size: 14px;
  outline: none;
}

.text-input,
.file-input {
  min-height: 44px;
  padding: 0 14px;
}

.script-input {
  min-height: 142px;
  resize: vertical;
  padding: 14px;
  line-height: 1.6;
}

.text-input:focus,
.script-input:focus,
.file-input:focus {
  border-color: #34e6f3;
}

.field-hint {
  margin-top: -6px;
  color: #798394;
  font-size: 12px;
  line-height: 18px;
}

.output-settings {
  display: flex;
  flex-direction: column;
  gap: 14px;
  border: 1px solid #242b36;
  background: #0b0d12;
  padding: 16px;
}

.settings-header {
  display: flex;
  align-items: center;
}

.settings-toggle {
  display: flex;
  min-width: 0;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  text-align: left;
}

.settings-toggle h3 {
  color: #f4f7fb;
  font-size: 14px;
  font-weight: 800;
}

.settings-chevron {
  display: inline-flex;
  width: 18px;
  height: 18px;
  flex: 0 0 18px;
  align-items: center;
  justify-content: center;
  transform: rotate(180deg);
  transition: transform 160ms ease;
}

.settings-chevron::before {
  width: 8px;
  height: 8px;
  border-bottom: 2px solid #34e6f3;
  border-right: 2px solid #34e6f3;
  content: "";
  transform: rotate(-45deg);
}

.settings-chevron.expanded {
  transform: rotate(90deg);
}

.settings-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.settings-section {
  border-top: 1px solid #242b36;
  padding-top: 14px;
}

.settings-section-title {
  margin-bottom: 12px;
  color: #f4f7fb;
  font-size: 13px;
  font-weight: 800;
}

.wide-field {
  grid-column: 1 / -1;
}

.settings-field {
  display: flex;
  min-width: 0;
  position: relative;
  flex-direction: column;
  gap: 8px;
  color: #c4ccd8;
  font-size: 12px;
  font-weight: 800;
}

.field-label-row {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
}

.required-mark {
  color: #fca5a5;
}

.field-help {
  position: relative;
  display: inline-flex;
}

.field-help-button {
  display: inline-flex;
  width: 18px;
  height: 18px;
  align-items: center;
  justify-content: center;
  border: 1px solid #303a49;
  border-radius: 999px;
  color: #c4ccd8;
  font-size: 11px;
  font-weight: 800;
  line-height: 1;
  transition: border-color 160ms ease, color 160ms ease, background 160ms ease;
}

.field-help-button:hover,
.field-help-button:focus-visible {
  border-color: #34e6f3;
  background: rgba(52, 230, 243, 0.08);
  color: #f4f7fb;
  outline: none;
}

.field-tooltip {
  position: absolute;
  left: 50%;
  bottom: calc(100% + 8px);
  z-index: 30;
  width: min(320px, calc(100vw - 48px));
  padding: 10px 12px;
  border: 1px solid #303a49;
  background: #151920;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.35);
  color: #c4ccd8;
  font-size: 12px;
  font-weight: 500;
  line-height: 18px;
  opacity: 0;
  pointer-events: none;
  transform: translate(-50%, 4px);
  transition: opacity 120ms ease, transform 120ms ease;
}

.field-help:hover .field-tooltip,
.field-help:focus-within .field-tooltip {
  opacity: 1;
  pointer-events: auto;
  transform: translate(-50%, 0);
}

.field-tooltip a {
  color: #34e6f3;
  font-weight: 700;
  text-decoration: underline;
  text-underline-offset: 3px;
}

.number-input {
  min-height: 38px;
  width: 100%;
  border: 1px solid #303a49;
  background: #07080b;
  color: #f4f7fb;
  padding: 0 10px;
  font-size: 13px;
  outline: none;
}

.number-input:focus {
  border-color: #34e6f3;
}

.toggle-field {
  display: inline-flex;
  min-height: 38px;
  align-items: center;
  gap: 10px;
  color: #c4ccd8;
  font-size: 13px;
  font-weight: 700;
}

.toggle-field input {
  width: 16px;
  height: 16px;
  accent-color: #34e6f3;
}

.notice {
  border: 1px solid;
  padding: 10px 12px;
  font-size: 13px;
  line-height: 20px;
}

.notice.warning {
  border-color: rgba(255, 179, 107, 0.3);
  background: rgba(255, 179, 107, 0.08);
  color: #ffca9a;
}

.notice.error,
.job-error {
  color: #fca5a5;
}

.notice.error {
  border-color: rgba(239, 68, 68, 0.35);
  background: rgba(239, 68, 68, 0.1);
}

.jobs-header,
.video-card-actions,
.video-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.jobs-section {
  padding: 22px;
}

.jobs-header {
  margin-bottom: 16px;
}

.jobs-title-block {
  min-width: 0;
}

.jobs-header h2 {
  color: #fbf6ef;
  font-size: 20px;
  font-weight: 800;
}

.jobs-title-block p {
  margin-top: 6px;
  color: #798394;
  font-size: 12px;
}

.empty-jobs {
  border: 1px dashed #303a49;
  padding: 40px;
  color: #798394;
  text-align: center;
}

.video-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}

.video-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  border: 1px solid #242b36;
  background: #0b0d12;
  padding: 12px;
  transition: border-color 160ms ease, background 160ms ease, box-shadow 160ms ease;
}

.video-card--active {
  border-color: rgba(52, 230, 243, 0.42);
}

.video-card--highlight {
  border-color: rgba(52, 230, 243, 0.76);
  background: rgba(52, 230, 243, 0.035);
  box-shadow: 0 0 0 1px rgba(52, 230, 243, 0.18), 0 0 24px rgba(52, 230, 243, 0.08);
}

.video-preview {
  position: relative;
  aspect-ratio: 16 / 10;
  overflow: hidden;
  border: 1px solid #242b36;
  background: #07080b;
}

.video-preview-media {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.video-preview-state {
  display: flex;
  width: 100%;
  height: 100%;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #8d96a6;
  font-size: 12px;
  font-weight: 800;
}

.video-preview-button {
  border: 0;
  background: transparent;
  cursor: pointer;
  outline: none;
  transition: background 160ms ease, color 160ms ease;
}

.video-preview-button:hover,
.video-preview-button:focus-visible {
  background: rgba(239, 68, 68, 0.08);
  color: #f3b5b5;
}

.video-preview--queued,
.video-preview--running {
  background:
    linear-gradient(135deg, rgba(52, 230, 243, 0.1), transparent 42%),
    repeating-linear-gradient(135deg, rgba(52, 230, 243, 0.06) 0 1px, transparent 1px 11px),
    #07080b;
}

.video-preview--failed {
  border-color: #303a49;
  background:
    linear-gradient(135deg, rgba(255, 179, 107, 0.06), transparent 42%),
    #07080b;
}

.video-preview-mark {
  display: inline-flex;
  width: 32px;
  height: 32px;
  align-items: center;
  justify-content: center;
  border: 1px solid #303a49;
  color: #f4f7fb;
  font-size: 14px;
  font-weight: 800;
}

.job-spinner {
  width: 16px;
  height: 16px;
  flex: 0 0 16px;
  border: 2px solid rgba(52, 230, 243, 0.2);
  border-top-color: #34e6f3;
  border-radius: 999px;
  animation: job-spin 800ms linear infinite;
}

.rename-input {
  min-width: 0;
  height: 36px;
  width: 100%;
  border: 1px solid #303a49;
  background: #07080b;
  color: #f4f7fb;
  padding: 0 10px;
  font-size: 13px;
  outline: none;
}

.rename-input:focus {
  border-color: #34e6f3;
}

@keyframes job-spin {
  to {
    transform: rotate(360deg);
  }
}

.video-card-body {
  display: flex;
  min-width: 0;
  min-height: 128px;
  flex: 1 1 auto;
  flex-direction: column;
  padding-top: 12px;
}

.video-title {
  overflow: hidden;
  color: #f4f7fb;
  font-size: 14px;
  font-weight: 800;
  line-height: 20px;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: color 160ms ease;
}

.video-title:hover {
  color: #34e6f3;
}

.video-date {
  margin-top: 4px;
  color: #798394;
  font-size: 11px;
}

.video-progress {
  margin-top: 10px;
}

.progress-track {
  height: 5px;
  overflow: hidden;
  background: #1b222c;
}

.progress-bar {
  height: 100%;
  background: #34e6f3;
  transition: width 200ms ease;
}

.video-card-actions {
  margin-top: auto;
  justify-content: flex-start;
  flex-wrap: wrap;
  gap: 8px;
  padding-top: 12px;
}

.video-pagination {
  margin-top: 18px;
  border-top: 1px solid #242b36;
  padding-top: 16px;
  color: #798394;
  font-size: 12px;
}

.video-pagination-actions {
  display: flex;
  gap: 10px;
}

.failure-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.68);
  padding: 24px;
}

.failure-modal {
  position: relative;
  width: min(560px, 100%);
  border: 1px solid #303a49;
  background: #11141b;
  box-shadow: 0 24px 80px rgba(0, 0, 0, 0.38);
  padding: 24px 54px 24px 24px;
}

.failure-modal-header {
  position: absolute;
  top: 12px;
  right: 12px;
}

.failure-modal-close {
  display: inline-flex;
  width: 24px;
  height: 24px;
  flex: 0 0 24px;
  align-items: center;
  justify-content: center;
  border: 1px solid transparent;
  color: #c4ccd8;
  font-size: 18px;
  line-height: 1;
}

.failure-modal-close:hover {
  border-color: #303a49;
  color: #34e6f3;
}

.failure-modal-message {
  max-height: 260px;
  margin-top: 0;
  overflow: auto;
  color: #d7dde8;
  padding: 0;
  font-family: inherit;
  font-size: 14px;
  line-height: 1.65;
  white-space: pre-wrap;
  word-break: break-word;
}

@media (max-width: 860px) {
  .settings-grid {
    grid-template-columns: 1fr;
  }

  .jobs-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .video-grid {
    grid-template-columns: 1fr;
  }

  .video-pagination {
    align-items: flex-start;
    flex-direction: column;
  }
}

@media (max-width: 1180px) and (min-width: 861px) {
  .video-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 960px) and (min-width: 681px) {
  .video-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
