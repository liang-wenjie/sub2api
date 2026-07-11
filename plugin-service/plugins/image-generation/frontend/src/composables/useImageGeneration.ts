import { computed, ref } from 'vue'
import type { PluginApi } from '../api/client'
import { projectHistory } from './history'
import type {
  ChatMessage,
  Conversation,
  GenerateResponse,
  GeneratedImage,
  GeneratedImagePayload,
  HistoryRecord,
  ImageApiKey,
  ImageReference,
} from '../types'

type GenerationStatus = 'idle' | 'submitting' | 'polling' | 'cancelling'

interface UseImageGenerationOptions {
  api: PluginApi
  loadKeys: () => Promise<ImageApiKey[]>
  pollInterval?: number
  now?: () => Date
}

interface SubmitOptions {
  prompt?: string
  references?: ImageReference[]
}

const defaultModels = ['gpt-image-2', 'gpt-image-1', 'gemini-2.5-flash-image']

function supportsImageGeneration(key: ImageApiKey): boolean {
  if (key.status !== 'active' || !key.group?.allow_image_generation) return false
  const config = key.group.models_list_config
  if (!config?.enabled || !config.models?.length) return true
  return config.models.some(model => model.startsWith('gpt-image-') || (model.startsWith('gemini-') && model.includes('image')))
}

function sourceOf(image: GeneratedImagePayload): string {
  if (image.url) return image.url
  return image.b64_json ? `data:image/png;base64,${image.b64_json}` : ''
}

export function useImageGeneration(options: UseImageGenerationOptions) {
  const now = options.now ?? (() => new Date())
  const pollInterval = options.pollInterval ?? 1500
  const keys = ref<ImageApiKey[]>([])
  const selectedKeyId = ref<number | null>(null)
  const model = ref(defaultModels[0])
  const size = ref('1024x1024')
  const prompt = ref('')
  const conversations = ref<Conversation[]>([])
  const activeConversationId = ref('')
  const generationStatus = ref<GenerationStatus>('idle')
  const activeJobId = ref('')
  const errorMessage = ref('')
  let pollTimer: ReturnType<typeof setTimeout> | undefined

  const selectedKey = computed(() => keys.value.find(key => key.id === selectedKeyId.value) ?? null)
  const activeConversation = computed(() => conversations.value.find(item => item.id === activeConversationId.value) ?? null)
  const availableModels = computed(() => {
    const config = selectedKey.value?.group?.models_list_config
    if (!config?.enabled || !config.models?.length) return defaultModels
    return config.models.filter(value => defaultModels.includes(value) || value.startsWith('gpt-image-') || value.includes('image'))
  })

  function timestamp(): string {
    return now().toLocaleString()
  }

  function createConversation(): Conversation {
    const id = `conversation-live-${now().getTime()}`
    const conversation: Conversation = {
      id,
      title: '新会话',
      preview: '',
      lastUsedAt: timestamp(),
      messages: [],
      referenceImages: [],
      historyIds: [],
    }
    conversations.value.unshift(conversation)
    activeConversationId.value = id
    prompt.value = ''
    return conversation
  }

  function updateConversation(id: string, update: (conversation: Conversation) => Conversation): void {
    conversations.value = conversations.value.map(conversation => conversation.id === id ? update(conversation) : conversation)
  }

  async function initialize(): Promise<void> {
    const [, , history, loadedKeys] = await Promise.all([
      options.api.getMe(),
      options.api.getConfig(),
      options.api.listHistory(),
      options.loadKeys(),
    ])
    keys.value = loadedKeys.filter(supportsImageGeneration)
    selectedKeyId.value = keys.value[0]?.id ?? null
    const remote = projectHistory(history.items)
    conversations.value = remote
    if (remote.length > 0) activeConversationId.value = remote[0].id
    else createConversation()

    const pending = history.items.find(record => record.status === 'pending')
    if (pending) {
      activeJobId.value = pending.id
      generationStatus.value = 'polling'
      schedulePoll()
    }
  }

  function referencesToRequest(references: ImageReference[]) {
    return references.filter(reference => reference.dataUrl).slice(0, 1).map(reference => ({
      name: reference.fileName,
      mime_type: reference.mimeType,
      data_url: reference.dataUrl,
    }))
  }

  function requestPrompt(userPrompt: string, references: ImageReference[]): string {
    const lines = ['Follow the user request with highest priority.']
    if (references.length > 0) {
      lines.push('Use the uploaded reference image as the primary subject and preserve its identity unless the user asks to change it.')
    }
    lines.push(`User request: ${userPrompt}`)
    return lines.join('\n')
  }

  function imagesFromResult(response: GenerateResponse, fallbackPrompt = ''): GeneratedImage[] {
    const resultPrompt = response.result?.revised_prompt || fallbackPrompt
    return (response.result?.images ?? []).map((image, index) => ({
      id: `${response.job_id}-image-${index}`,
      src: sourceOf(image),
      revisedPrompt: image.revised_prompt || resultPrompt,
      createdAt: response.result?.created
        ? new Date(response.result.created * 1000).toLocaleString()
        : timestamp(),
    })).filter(image => image.src)
  }

  function replacePending(conversationId: string, pendingId: string, message: ChatMessage): void {
    updateConversation(conversationId, conversation => ({
      ...conversation,
      preview: message.content,
      lastUsedAt: message.createdAt,
      messages: conversation.messages.map(item => item.id === pendingId ? message : item),
    }))
  }

  async function submit(submitOptions: SubmitOptions = {}): Promise<void> {
    if (generationStatus.value !== 'idle') return
    const conversation = activeConversation.value
    const key = selectedKey.value
    const userPrompt = (submitOptions.prompt ?? prompt.value).trim()
    const references = submitOptions.references ?? conversation?.referenceImages ?? []
    if (!conversation || !key || !userPrompt) return

    const createdAt = timestamp()
    const userId = `user-${now().getTime()}`
    const pendingId = `assistant-pending-${now().getTime()}`
    const userMessage: ChatMessage = {
      id: userId,
      role: 'user',
      content: userPrompt,
      createdAt,
      referenceImages: references.map(reference => ({ ...reference })),
      requestSettings: [{ modelLabel: model.value, sizeLabel: size.value, countLabel: '数量: 1' }],
    }
    const pendingMessage: ChatMessage = {
      id: pendingId,
      role: 'assistant',
      content: '正在生成图片，请稍候...',
      createdAt,
      status: 'pending',
    }
    updateConversation(conversation.id, current => ({
      ...current,
      title: current.messages.length === 0 ? userPrompt.slice(0, 24) : current.title,
      preview: pendingMessage.content,
      lastUsedAt: createdAt,
      messages: [...current.messages, userMessage, pendingMessage],
    }))
    prompt.value = ''
    generationStatus.value = 'submitting'
    errorMessage.value = ''

    try {
      const response = await options.api.generate({
        prompt: requestPrompt(userPrompt, references),
        provider_api_key: key.key,
        model: model.value,
        size: size.value,
        response_format: 'b64_json',
        reference_images: referencesToRequest(references),
        inputs: {
          display_prompt: userPrompt,
          conversation_id: conversation.conversationId || conversation.id,
          api_key_id: key.id,
          api_key_name: key.name,
        },
      })
      activeJobId.value = response.job_id
      if (response.status === 'pending') {
        generationStatus.value = 'polling'
        schedulePoll(conversation.id, pendingId)
        return
      }
      const images = imagesFromResult(response, userPrompt)
      if (images.length === 0) throw new Error('图片生成未返回可显示的图片')
      const content = images[0].revisedPrompt || '生成结果'
      replacePending(conversation.id, pendingId, {
        id: `assistant-${now().getTime()}`,
        role: 'assistant',
        content,
        createdAt: images[0].createdAt,
        images,
      })
      generationStatus.value = 'idle'
      activeJobId.value = ''
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      errorMessage.value = message
      replacePending(conversation.id, pendingId, {
        id: `assistant-failed-${now().getTime()}`,
        role: 'assistant',
        content: `图片生成失败\n${message}`,
        createdAt: timestamp(),
        status: 'failed',
      })
      generationStatus.value = 'idle'
      activeJobId.value = ''
    }
  }

  function terminalMessage(record: HistoryRecord): ChatMessage {
    if (record.status === 'failed' || record.status === 'canceled') {
      return {
        id: `${record.id}-assistant`,
        role: 'assistant',
        content: record.error_message || (record.status === 'canceled' ? '生成已取消' : '图片生成失败'),
        createdAt: new Date(record.updated_at || now()).toLocaleString(),
        status: record.status,
      }
    }
    const response: GenerateResponse = { job_id: record.id, status: record.status, result: record.result }
    const requestDisplayPrompt = typeof record.request?.display_prompt === 'string' ? record.request.display_prompt : record.prompt
    const images = imagesFromResult(response, requestDisplayPrompt)
    return {
      id: `${record.id}-assistant`,
      role: 'assistant',
      content: images[0]?.revisedPrompt || (images.length ? '生成结果' : '图片生成未返回可显示的图片'),
      createdAt: new Date(record.updated_at || now()).toLocaleString(),
      status: images.length ? undefined : 'failed',
      images: images.length ? images : undefined,
    }
  }

  function findPendingLocation(): { conversationId: string; messageId: string } | null {
    for (const conversation of conversations.value) {
      const message = [...conversation.messages].reverse().find(item => item.status === 'pending')
      if (message) return { conversationId: conversation.id, messageId: message.id }
    }
    return null
  }

  function schedulePoll(conversationId?: string, pendingId?: string): void {
    clearTimeout(pollTimer)
    pollTimer = setTimeout(async () => {
      if (!activeJobId.value) return
      try {
        const record = await options.api.getStatus(activeJobId.value)
        if (record.status === 'pending') {
          schedulePoll(conversationId, pendingId)
          return
        }
        const location = conversationId && pendingId ? { conversationId, messageId: pendingId } : findPendingLocation()
        if (location) replacePending(location.conversationId, location.messageId, terminalMessage(record))
        generationStatus.value = 'idle'
        activeJobId.value = ''
      } catch (error) {
        errorMessage.value = error instanceof Error ? error.message : String(error)
        schedulePoll(conversationId, pendingId)
      }
    }, pollInterval)
  }

  async function cancelGeneration(): Promise<void> {
    if (!activeJobId.value) return
    generationStatus.value = 'cancelling'
    clearTimeout(pollTimer)
    try {
      const record = await options.api.cancel(activeJobId.value)
      const location = findPendingLocation()
      if (location) replacePending(location.conversationId, location.messageId, terminalMessage(record))
    } finally {
      generationStatus.value = 'idle'
      activeJobId.value = ''
    }
  }

  async function repeatFromImage(image: GeneratedImage, repeatPrompt: string): Promise<void> {
    const reference: ImageReference = {
      id: `${image.id}-repeat-reference`,
      dataUrl: image.src,
      fileName: `${image.id}.png`,
      mimeType: 'image/png',
    }
    await submit({ prompt: repeatPrompt || image.revisedPrompt, references: [reference] })
  }

  function refineFromImage(image: GeneratedImage): void {
    prompt.value = image.revisedPrompt || prompt.value
  }

  async function retryMessage(messageId: string): Promise<void> {
    const conversation = activeConversation.value
    if (!conversation) return
    const failedIndex = conversation.messages.findIndex(message => message.id === messageId)
    if (failedIndex < 0) return
    const userMessage = conversation.messages.slice(0, failedIndex).reverse().find(message => message.role === 'user')
    if (!userMessage) return
    updateConversation(conversation.id, current => ({
      ...current,
      messages: current.messages.filter(message => message.id !== messageId && message.id !== userMessage.id),
    }))
    await submit({ prompt: userMessage.content, references: userMessage.referenceImages ?? [] })
  }

  function setReference(reference?: ImageReference): void {
    const conversation = activeConversation.value
    if (!conversation) return
    updateConversation(conversation.id, current => ({ ...current, referenceImages: reference ? [reference] : [] }))
  }

  async function deleteConversation(conversation: Conversation): Promise<void> {
    await Promise.all(conversation.historyIds.map(id => options.api.deleteHistory(id)))
    conversations.value = conversations.value.filter(item => item.id !== conversation.id)
    if (activeConversationId.value === conversation.id) {
      activeConversationId.value = conversations.value[0]?.id ?? ''
      if (!activeConversationId.value) createConversation()
    }
  }

  function dispose(): void {
    clearTimeout(pollTimer)
  }

  return {
    keys,
    selectedKeyId,
    selectedKey,
    availableModels,
    model,
    size,
    prompt,
    conversations,
    activeConversationId,
    activeConversation,
    generationStatus,
    activeJobId,
    errorMessage,
    initialize,
    createConversation,
    submit,
    cancelGeneration,
    repeatFromImage,
    refineFromImage,
    retryMessage,
    setReference,
    deleteConversation,
    dispose,
  }
}
