import {
  COSYVOICE_V3_FLASH_VOICE_OPTIONS,
  COSYVOICE_V3_PLUS_VOICE_OPTIONS,
  DOUBAO_TTS_VOICE_OPTIONS,
  GROK_VOICE_OPTIONS,
  OPENAI_VOICE_OPTIONS,
  QWEN_OMNI_VOICE_OPTIONS,
  QWEN_TTS_VOICE_OPTIONS,
  VOICE_OPTIONS,
} from '../types'
import type { ComposerTranslation } from 'vue-i18n'

export const DEFAULT_OFFICIAL_VOICE = '温柔文雅'
export const DEFAULT_DOUBAO_TTS_VOICE = 'zh_female_xiaohe_uranus_bigtts'
export const DEFAULT_QWEN_TTS_VOICE = 'Momo'
export const DEFAULT_QWEN_OMNI_VOICE = 'Tina'
export const DEFAULT_GROK_VOICE = 'eve'
export const DEFAULT_COSYVOICE_V3_VOICE = 'longanyang'

type VoiceDisplayOption = {
  label: string
  value: string
  labelEn?: string
}

const officialVoiceLabelMap = new Map(
  VOICE_OPTIONS.map(option => [option.value, option.label]),
)
const doubaoTTSVoiceOptionMap = new Map(
  DOUBAO_TTS_VOICE_OPTIONS.map(option => [option.value, option]),
)
const qwenTTSVoiceLabelMap = new Map(
  QWEN_TTS_VOICE_OPTIONS.map(option => [option.value, option.label]),
)
const qwenOmniVoiceLabelMap = new Map(
  QWEN_OMNI_VOICE_OPTIONS.map(option => [option.value, option.label]),
)
const grokVoiceLabelMap = new Map(
  GROK_VOICE_OPTIONS.map(option => [option.value, option.label]),
)
const openAIVoiceLabelMap = new Map(
  OPENAI_VOICE_OPTIONS.map(option => [option.value, option.label]),
)
const cosyVoiceLabelMap = new Map(
  [
    ...COSYVOICE_V3_FLASH_VOICE_OPTIONS,
    ...COSYVOICE_V3_PLUS_VOICE_OPTIONS,
  ].map(option => [option.value, option.label]),
)

const officialVoiceEnglishLabelMap = new Map<string, string>([
  ['傲娇女友', 'Tsundere girlfriend'],
  ['冰娇姐姐', 'Cool older sister'],
  ['成熟姐姐', 'Mature older sister'],
  ['可爱女生', 'Cute girl'],
  ['暖心学姐', 'Warm senior student'],
  ['贴心女友', 'Considerate girlfriend'],
  ['温柔文雅', 'Gentle and refined'],
  ['妩媚御姐', 'Charming mature woman'],
  ['性感御姐', 'Sultry mature woman'],
  ['爱气凌人', 'Commanding voice'],
  ['傲娇公子', 'Tsundere young gentleman'],
  ['傲娇精英', 'Tsundere elite'],
  ['傲慢少爷', 'Arrogant young master'],
  ['霸道少爷', 'Dominant young master'],
  ['冰娇白莲', 'Cool and delicate voice'],
  ['不羁青年', 'Free-spirited young man'],
  ['成熟总裁', 'Mature executive'],
  ['磁性男嗓', 'Resonant male voice'],
  ['醋精男友', 'Jealous boyfriend'],
  ['风发少年', 'Energetic young man'],
  ['腹黑公子', 'Cunning young gentleman'],
])

export function isOfficialVoiceType(value: string): boolean {
  return officialVoiceLabelMap.has(value.trim())
}

export function isDoubaoTTSVoiceType(value: string): boolean {
  return doubaoTTSVoiceOptionMap.has(value.trim())
}

export function isQwenTTSVoiceType(value: string): boolean {
  return qwenTTSVoiceLabelMap.has(value.trim())
}

export function isQwenOmniVoiceType(value: string): boolean {
  return qwenOmniVoiceLabelMap.has(value.trim())
}

export function isGrokVoiceType(value: string): boolean {
  return grokVoiceLabelMap.has(value.trim())
}

export function isOpenAIVoiceType(value: string): boolean {
  return openAIVoiceLabelMap.has(value.trim())
}

export function isCosyVoiceTTSModel(model: string): boolean {
  return model.trim().toLowerCase().startsWith('cosyvoice-')
}

export function isCosyVoiceCloneOnlyModel(model: string): boolean {
  return model.trim().toLowerCase().startsWith('cosyvoice-v3.5-')
}

export function isCosyVoiceBuiltinModel(model: string): boolean {
  return model.trim().toLowerCase().startsWith('cosyvoice-v3-')
}

export function cosyVoiceBuiltinVoiceOptions(model: string): VoiceDisplayOption[] {
  const normalized = model.trim().toLowerCase()
  if (normalized === 'cosyvoice-v3-plus') return COSYVOICE_V3_PLUS_VOICE_OPTIONS
  if (normalized === 'cosyvoice-v3-flash') return COSYVOICE_V3_FLASH_VOICE_OPTIONS
  return []
}

export function isCosyVoiceBuiltinVoice(model: string, voice: string): boolean {
  const normalizedVoice = voice.trim()
  return cosyVoiceBuiltinVoiceOptions(model).some(option => option.value === normalizedVoice)
}

export function isCosyVoiceKnownBuiltinVoice(voice: string): boolean {
  return cosyVoiceLabelMap.has(voice.trim())
}

function hasCJK(value: string): boolean {
  return /[\u3400-\u9fff]/.test(value)
}

function slashEnglishLabel(label: string): string {
  const parts = label.split('/')
  if (parts.length < 2) return ''
  const candidate = parts[parts.length - 1]?.trim() || ''
  return candidate && /[A-Za-z]/.test(candidate) && !hasCJK(candidate) ? candidate : ''
}

function englishLabelFromCurrentLabel(
  label: string,
  value: string,
  labelEn?: string,
): string {
  if (labelEn) return labelEn
  const officialLabel = officialVoiceEnglishLabelMap.get(value)
  if (officialLabel) return officialLabel
  const match = label.match(/\(([^)]+)\)\s*$/)
  if (match?.[1]) return match[1]
  const slashLabel = slashEnglishLabel(label)
  if (slashLabel) return slashLabel
  const cleaned = chineseLabelFromCurrentLabel(label)
  if (!hasCJK(cleaned)) return cleaned
  return value
}

function chineseLabelFromCurrentLabel(label: string): string {
  const cleaned = label.replace(/\s*\([^)]+\)\s*$/, '').trim()
  const parts = cleaned.split('/')
  if (parts.length < 2) return cleaned
  const left = parts[0]?.trim() || ''
  const right = parts[parts.length - 1]?.trim() || ''
  if (!left || !hasCJK(left) || hasCJK(right)) return cleaned
  const version = right.match(/\s+(\d+(?:\.\d+)?)$/)?.[1]
  return version ? `${left} ${version}` : left
}

export function localizedVoiceOptions(
  options: VoiceDisplayOption[],
  locale: string,
): VoiceDisplayOption[] {
  const useEnglish = locale.toLowerCase().startsWith('en')
  return options.map((option) => ({
    value: option.value,
    label: useEnglish
      ? englishLabelFromCurrentLabel(option.label, option.value, option.labelEn)
      : chineseLabelFromCurrentLabel(option.label),
  }))
}

export function formatVoiceTypeDisplay(
  value: string,
  t?: ComposerTranslation,
  locale: string = 'zh-CN',
): string {
  const trimmed = value.trim()
  if (!trimmed) return '—'
  const doubaoOption = doubaoTTSVoiceOptionMap.get(trimmed)
  if (doubaoOption) {
    return locale.toLowerCase().startsWith('en')
      ? englishLabelFromCurrentLabel(doubaoOption.label, trimmed, doubaoOption.labelEn)
      : chineseLabelFromCurrentLabel(doubaoOption.label)
  }
  const label = qwenTTSVoiceLabelMap.get(trimmed)
    ?? qwenOmniVoiceLabelMap.get(trimmed)
    ?? grokVoiceLabelMap.get(trimmed)
    ?? openAIVoiceLabelMap.get(trimmed)
    ?? cosyVoiceLabelMap.get(trimmed)
    ?? officialVoiceLabelMap.get(trimmed)
  if (label) {
    return locale.toLowerCase().startsWith('en')
      ? englishLabelFromCurrentLabel(label, trimmed)
      : chineseLabelFromCurrentLabel(label)
  }
  return t ? t('voices.cloned', { id: trimmed }) : `Cloned voice · ${trimmed}`
}
