export type HistoryStatus = 'pending' | 'succeeded' | 'failed' | 'canceled'
export type MessageStatus = 'pending' | 'failed' | 'canceled'

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

export interface ImageModelCapability {
  max_reference_images: number
  max_output_images: number
}

export interface PluginConfig {
  image_model_capabilities: Record<string, ImageModelCapability>
}

export interface ReferenceImageRequest {
  name?: string
  mime_type?: string
  data_url?: string
  remote_url?: string
  storage_key?: string
  preview_url?: string
  preview_storage_key?: string
}

export interface UploadedReference extends ReferenceImageRequest {
  storage_key: string
  preview_storage_key: string
  original_url: string
  preview_url: string
}

export interface GenerateRequest {
  prompt: string
  provider_api_key: string
  model: string
  size: string
  response_format: string
  output_count?: number
  reference_images?: ReferenceImageRequest[]
  inputs?: Record<string, unknown>
}

export interface GeneratedImagePayload {
  url?: string
  preview_url?: string
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

export interface ConversationSummary {
  id: string
  title: string
  preview: string
  status: HistoryStatus
  updated_at: string
}

export interface ConversationList {
  items: ConversationSummary[]
  next_cursor?: string
}

export interface ConversationMessages {
  items: HistoryRecord[]
  next_cursor?: string
}

export interface ImageReference {
  id: string
  dataUrl: string
  originalDataUrl?: string
  uploadDataUrl?: string
  storageKey?: string
  previewStorageKey?: string
  fileName: string
  mimeType: string
}

export interface GeneratedImage {
  id: string
  src: string
  originalSrc?: string
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
