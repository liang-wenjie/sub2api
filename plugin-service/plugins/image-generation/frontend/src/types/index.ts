export type HistoryStatus = 'pending' | 'succeeded' | 'failed' | 'canceled'
export type MessageStatus = 'pending' | 'failed' | 'canceled'

export interface Principal {
  user_id: number
  role: 'admin' | 'user'
  email: string
  username: string
  plugin: string
}

export interface PluginConfig {
  plugin_key: string
  history_enabled: boolean
  user_id: number
  role: string
}

export interface ModelsListConfig {
  enabled?: boolean
  models?: string[]
}

export interface ImageKeyGroup {
  allow_image_generation?: boolean
  models_list_config?: ModelsListConfig
}

export interface ImageApiKey {
  id: number
  key: string
  name: string
  status: string
  group?: ImageKeyGroup
}

export interface ReferenceImageRequest {
  name?: string
  mime_type?: string
  data_url?: string
  remote_url?: string
}

export interface GenerateRequest {
  prompt: string
  provider_api_key: string
  model: string
  size: string
  response_format: string
  reference_images?: ReferenceImageRequest[]
  inputs?: Record<string, unknown>
}

export interface GeneratedImagePayload {
  url?: string
  b64_json?: string
  revised_prompt?: string
}

export interface GenerationResult {
  created?: number
  images?: GeneratedImagePayload[]
  revised_prompt?: string
  [key: string]: unknown
}

export interface GenerateResponse {
  job_id: string
  status: HistoryStatus
  result?: GenerationResult
}

export interface HistoryRecord {
  id: string
  conversation_id?: string
  user_id: number
  user_email?: string
  plugin_key?: string
  prompt: string
  status: HistoryStatus
  request: Record<string, unknown>
  result?: GenerationResult
  error_message?: string
  created_at: string
  updated_at: string
}

export interface HistoryList {
  items: HistoryRecord[]
}

export interface ImageReference {
  id: string
  dataUrl: string
  fileName: string
  mimeType: string
}

export interface GeneratedImage {
  id: string
  src: string
  revisedPrompt: string
  createdAt: string
}

export interface RequestSetting {
  modelLabel: string
  sizeLabel: string
  countLabel: string
}

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  createdAt: string
  status?: MessageStatus
  images?: GeneratedImage[]
  referenceImages?: ImageReference[]
  requestSettings?: RequestSetting[]
}

export interface Conversation {
  id: string
  conversationId?: string
  title: string
  preview: string
  lastUsedAt: string
  messages: ChatMessage[]
  referenceImages: ImageReference[]
  historyIds: string[]
}
