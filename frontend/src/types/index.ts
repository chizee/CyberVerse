// Character image info
export interface ImageInfo {
  filename: string
  orig_name: string
  added_at: string
  url?: string
}

// Character data model
export interface Character {
  id: string
  name: string
  description: string
  avatar_image: string
  idle_video_url?: string
  idle_video_urls?: string[]
  use_face_crop: boolean
  voice_provider: string
  voice_type: string
  speaking_style: string
  personality: string
  welcome_message: string
  system_prompt: string
  tags: string[]
  images: ImageInfo[]
  active_image: string
  image_mode: string
  created_at: string
  updated_at: string
}

export type CharacterForm = Omit<Character, 'id' | 'created_at' | 'updated_at' | 'images' | 'active_image'>

// Settings
export interface DoubaoSettings {
  access_token: string
  app_id: string
  ws_url: string
}

export interface LiveKitSettings {
  url: string
  api_key: string
  api_secret: string
}

export interface LLMSettings {
  api_key: string
  model: string
  temperature: number
}

export interface TTSSettings {
  model: string
  voice: string
}

export interface ASRSettings {
  model_size: string
  language: string
  device: string
}

export interface InferenceSettings {
  grpc_addr: string
}

export interface Settings {
  doubao: DoubaoSettings
  livekit: LiveKitSettings
  llm: LLMSettings
  tts: TTSSettings
  asr: ASRSettings
  inference: InferenceSettings
}

// Launch config
export interface ConfigParam {
  name: string
  path: string
  value: string | number
  readonly: boolean
  requires_restart: boolean
  options?: string[]
}

export interface ConfigSection {
  title: string
  badge: 'restart' | 'configurable'
  params: ConfigParam[]
  collapsed?: boolean
}

export interface LaunchConfig {
  active_model: string
  configured_default_model: string
  config_status: AvatarModelConfigStatus
  sections: ConfigSection[]
}

export interface LaunchConfigUpdate {
  model: string
  params: Array<{ path: string; value: string | number }>
}

export interface AvatarModelConfigStatus {
  has_infer_params: boolean
  config_sections_available: string[]
}

export interface AvatarModelDescriptor {
  name: string
  display_name: string
  is_active: boolean
  is_configured_default: boolean
  config_status: AvatarModelConfigStatus
}

export interface AvatarModelInfo {
  active_model: string
  configured_default_model: string
  models: AvatarModelDescriptor[]
  config_status: AvatarModelConfigStatus
}

// Voice types
export interface VoiceOption {
  label: string
  value: string
}

// SC2.0 official voices — values match SC20_VOICES keys in doubao_config.py
export const VOICE_OPTIONS: VoiceOption[] = [
  // Female
  { label: '傲娇女友', value: '傲娇女友' },
  { label: '冰娇姐姐', value: '冰娇姐姐' },
  { label: '成熟姐姐', value: '成熟姐姐' },
  { label: '可爱女生', value: '可爱女生' },
  { label: '暖心学姐', value: '暖心学姐' },
  { label: '贴心女友', value: '贴心女友' },
  { label: '温柔文雅', value: '温柔文雅' },
  { label: '妩媚御姐', value: '妩媚御姐' },
  { label: '性感御姐', value: '性感御姐' },
  // Male
  { label: '爱气凌人', value: '爱气凌人' },
  { label: '傲娇公子', value: '傲娇公子' },
  { label: '傲娇精英', value: '傲娇精英' },
  { label: '傲慢少爷', value: '傲慢少爷' },
  { label: '霸道少爷', value: '霸道少爷' },
  { label: '冰娇白莲', value: '冰娇白莲' },
  { label: '不羁青年', value: '不羁青年' },
  { label: '成熟总裁', value: '成熟总裁' },
  { label: '磁性男嗓', value: '磁性男嗓' },
  { label: '醋精男友', value: '醋精男友' },
  { label: '风发少年', value: '风发少年' },
  { label: '腹黑公子', value: '腹黑公子' },
]
