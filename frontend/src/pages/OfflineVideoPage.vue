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
const errorMessage = ref('')
let pollTimer: ReturnType<typeof setInterval> | null = null

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
const canGenerate = computed(() => {
  if (submitting.value) return false
  if (isBaiduXilingCharacter.value && (!outputWidth.value || !outputHeight.value)) return false
  if (inputType.value === 'text') return scriptText.value.trim().length > 0
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

function progressStyle(job: OfflineVideoJob) {
  return { width: `${Math.min(100, Math.max(0, job.progress || 0))}%` }
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

function jobMeta(job: OfflineVideoJob): string {
  if (job.width && job.height && job.fps) {
    return `${job.width}x${job.height} · ${job.fps} FPS`
  }
  return job.message || t('offlineVideo.waiting')
}

async function refreshJobs() {
  if (!characterId.value) return
  const resp = await listOfflineVideos(characterId.value)
  jobs.value = resp.videos
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
  outputSettingsExpanded.value = boolSetting(settings.outputSettingsExpanded, false)
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

async function submitJob() {
  if (!canGenerate.value) return
  submitting.value = true
  errorMessage.value = ''
  try {
    await createOfflineVideo(characterId.value, {
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
                      <label class="settings-field">
                        <span>{{ t('offlineVideo.ttsPerson') }}</span>
                        <input v-model="ttsPerson" class="number-input" type="text" :placeholder="t('offlineVideo.ttsPersonPlaceholder')">
                      </label>
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
            <h2>{{ t('offlineVideo.library') }}</h2>
            <span>{{ t('offlineVideo.libraryCount', { count: jobs.length }) }}</span>
          </div>
          <div v-if="jobs.length === 0" class="empty-jobs">{{ t('offlineVideo.empty') }}</div>
          <div v-else class="jobs-list">
            <article v-for="job in jobs" :key="job.id" class="job-row">
              <div class="job-main">
                <div class="job-title-row">
                  <div class="job-title-left">
                    <span v-if="isActiveJob(job)" class="job-spinner" aria-hidden="true" />
                    <template v-if="editingJobId === job.id">
                      <input
                        v-model="editingTitle"
                        class="rename-input"
                        type="text"
                        :placeholder="t('offlineVideo.titlePlaceholder')"
                        @keydown.enter.prevent="submitRename(job)"
                        @keydown.esc.prevent="cancelRename"
                      >
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
                    </template>
                    <template v-else>
                      <h3>{{ job.title }}</h3>
                      <button class="cv-pi-button cv-pi-button--compact" type="button" @click="startRename(job)">
                        {{ t('offlineVideo.rename') }}
                      </button>
                    </template>
                  </div>
                  <span class="status-pill" :class="`status-${job.status}`">{{ statusLabel(job) }}</span>
                </div>
                <p class="job-meta">{{ jobMeta(job) }}</p>
                <div class="progress-track">
                  <div class="progress-bar" :style="progressStyle(job)" />
                </div>
                <p v-if="job.error" class="job-error">{{ job.error }}</p>
              </div>
              <div class="job-side">
                <span>{{ formatDate(job.created_at) }}</span>
                <div class="job-actions">
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
              </div>
            </article>
          </div>
        </section>
      </template>
    </main>
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
  flex-direction: column;
  gap: 8px;
  color: #c4ccd8;
  font-size: 12px;
  font-weight: 800;
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
.job-title-row,
.job-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.jobs-section {
  padding: 22px;
}

.jobs-header {
  margin-bottom: 18px;
}

.jobs-header h2 {
  color: #fbf6ef;
  font-size: 20px;
  font-weight: 800;
}

.jobs-header span,
.job-side {
  color: #798394;
  font-size: 12px;
}

.empty-jobs {
  border: 1px dashed #303a49;
  padding: 40px;
  color: #798394;
  text-align: center;
}

.jobs-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.job-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 230px;
  gap: 18px;
  border: 1px solid #242b36;
  background: #0b0d12;
  padding: 16px;
}

.job-main {
  min-width: 0;
}

.job-title-row h3 {
  min-width: 0;
  overflow: hidden;
  color: #f4f7fb;
  font-size: 15px;
  font-weight: 800;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.job-title-left {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  align-items: center;
  gap: 10px;
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
  flex: 1 1 auto;
  height: 36px;
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

.status-pill {
  flex: 0 0 auto;
  border: 1px solid #303a49;
  padding: 3px 10px;
  color: #9da6b5;
  font-size: 12px;
}

.status-completed {
  border-color: rgba(34, 197, 94, 0.45);
  color: #86efac;
}

.status-failed {
  border-color: rgba(239, 68, 68, 0.45);
  color: #fca5a5;
}

.status-running,
.status-queued {
  border-color: rgba(52, 230, 243, 0.45);
  color: #8fe8ef;
}

.job-meta {
  margin-top: 7px;
  color: #8d96a6;
  font-size: 13px;
}

.progress-track {
  margin-top: 12px;
  height: 6px;
  overflow: hidden;
  background: #1b222c;
}

.progress-bar {
  height: 100%;
  background: #34e6f3;
  transition: width 200ms ease;
}

.job-error {
  margin-top: 8px;
  font-size: 12px;
}

.job-side {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
}

@media (max-width: 860px) {
  .settings-grid {
    grid-template-columns: 1fr;
  }

  .job-row {
    grid-template-columns: 1fr;
  }

  .job-side {
    align-items: flex-start;
  }
}
</style>
