import { computed, nextTick, ref, watch } from 'vue'
import { authenticatedMediaUrl, type PluginApi } from '../api/client'
import { projectConversationMessages } from './conversationMessages'
import type {
  ChatMessage,
  Conversation,
  GenerateResponse,
  GeneratedImage,
  GeneratedImagePayload,
  HistoryRecord,
  ImageApiKey,
  ImageModelCapability,
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
  outputCount?: number
}

const defaultModels = ['gpt-image-2', 'gpt-image-1', 'gemini-2.5-flash-image']

function supportsImageGeneration(key: ImageApiKey): boolean {
  if (key.status !== 'active' || !key.group?.allow_image_generation) return false
  const config = key.group.models_list_config
  if (!config?.enabled || !config.models?.length) return true
  return config.models.some(model => model.startsWith('gpt-image-') || (model.startsWith('gemini-') && model.includes('image')))
}

function sourceOf(image: GeneratedImagePayload): string {
	return image.url ? authenticatedMediaUrl(image.url) : ''
}

export function useImageGeneration(options: UseImageGenerationOptions) {
  const now = options.now ?? (() => new Date())
  const pollInterval = options.pollInterval ?? 1500
  const keys = ref<ImageApiKey[]>([])
  const selectedKeyId = ref<number | null>(null)
  const model = ref(defaultModels[0])
  const size = ref('1024x1024')
  const outputCount = ref(1)
  const prompt = ref('')
  const conversations = ref<Conversation[]>([])
  const activeConversationId = ref('')
  const generationStatus = ref<GenerationStatus>('idle')
  const activeJobId = ref('')
  const errorMessage = ref('')
  const modelCapabilities = ref<Record<string, ImageModelCapability>>({})
  const conversationNextCursor = ref<Record<string, string>>({})
  const loadingConversation = ref(false)
  let conversationRequestSequence = 0
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let initializedKeySelection = false

  const selectedKey = computed(() => keys.value.find(key => key.id === selectedKeyId.value) ?? null)
  const activeConversation = computed(() => conversations.value.find(item => item.id === activeConversationId.value) ?? null)
  const hasOlderMessages = computed(() => Boolean(conversationNextCursor.value[activeConversationId.value]))
  const availableModels = computed(() => {
    const config = selectedKey.value?.group?.models_list_config
    if (!config?.enabled || !config.models?.length) return defaultModels
    return config.models.filter(value => defaultModels.includes(value) || value.startsWith('gpt-image-') || value.includes('image'))
  })
  const maxReferenceImages = computed(() => modelCapabilities.value[model.value]?.max_reference_images ?? 1)
  const maxOutputImages = computed(() => modelCapabilities.value[model.value]?.max_output_images ?? 1)
  const referenceLimitExceeded = computed(() => (activeConversation.value?.referenceImages.length ?? 0) > maxReferenceImages.value)

  watch(maxOutputImages, limit => {
    outputCount.value = Math.min(Math.max(outputCount.value, 1), limit)
  })

  watch(selectedKeyId, async (next, previous) => {
    if (!initializedKeySelection || next === previous) return
    try {
      await options.api.saveImageGenerationPreference(next)
    } catch (error) {
      errorMessage.value = error instanceof Error ? error.message : String(error)
    }
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

  function promoteConversation(id: string): void {
    const conversation = conversations.value.find(item => item.id === id)
    if (!conversation || conversations.value[0]?.id === id) return
    conversations.value = [conversation, ...conversations.value.filter(item => item.id !== id)]
  }

  async function initialize(): Promise<void> {
    if (!activeConversation.value) createConversation()
    const configPromise = options.api.getConfig().then((config) => {
      modelCapabilities.value = config.image_model_capabilities ?? {}
    })
    const keysPromise = options.loadKeys()
    const preferencePromise = options.api.getImageGenerationPreference()
    const summaries = await options.api.listConversations()
    if (summaries.items.length > 0) {
      conversations.value = summaries.items.map(item => ({
        id: item.id, conversationId: item.id, title: item.title, preview: item.preview,
        lastUsedAt: new Date(item.updated_at).toLocaleString(), messages: [], referenceImages: [], historyIds: [],
      }))
      await selectConversation(summaries.items[0].id)
    }
    const [loadedKeys, preference] = await Promise.all([keysPromise, preferencePromise])
    keys.value = loadedKeys.filter(supportsImageGeneration)
    const savedKeyID = preference.last_api_key_id
    const nextKeyID = keys.value.some(key => key.id === savedKeyID) ? savedKeyID : keys.value[0]?.id ?? null
    selectedKeyId.value = nextKeyID
    if (nextKeyID !== savedKeyID) {
      try {
        await options.api.saveImageGenerationPreference(nextKeyID)
      } catch (error) {
        errorMessage.value = error instanceof Error ? error.message : String(error)
      }
    }
    await nextTick()
    initializedKeySelection = true
    await configPromise
  }

  async function loadConversation(id: string, before = ''): Promise<void> {
    const sequence = ++conversationRequestSequence
    loadingConversation.value = true
    try {
      const page = await options.api.listConversationMessages(id, before)
      if (sequence !== conversationRequestSequence) return
      const messages = projectConversationMessages(page.items)
      updateConversation(id, conversation => ({
        ...conversation,
        messages: before ? [...messages, ...conversation.messages] : messages,
        referenceImages: before
          ? conversation.referenceImages
          : [...messages].reverse().find(message => message.role === 'user' && message.referenceImages?.length)?.referenceImages ?? conversation.referenceImages,
        historyIds: before ? [...page.items.map(item => item.id), ...conversation.historyIds] : page.items.map(item => item.id),
      }))
      conversationNextCursor.value = { ...conversationNextCursor.value, [id]: page.next_cursor || '' }
      const pending = page.items.find(record => record.status === 'pending')
      if (pending) { activeJobId.value = pending.id; generationStatus.value = 'polling'; schedulePoll() }
    } finally {
      if (sequence === conversationRequestSequence) loadingConversation.value = false
    }
  }

  async function selectConversation(id: string): Promise<void> {
    activeConversationId.value = id
    const conversation = conversations.value.find(item => item.id === id)
    if (conversation && conversation.messages.length === 0) await loadConversation(id)
  }

  async function loadOlderMessages(): Promise<void> {
    const id = activeConversationId.value
    const cursor = conversationNextCursor.value[id]
    if (id && cursor && !loadingConversation.value) await loadConversation(id, cursor)
  }

  function referencesToRequest(references: ImageReference[]) {
    return references.filter(reference => reference.dataUrl).map(reference => ({
      name: reference.fileName,
      mime_type: reference.mimeType,
      data_url: reference.storageKey ? undefined : reference.uploadDataUrl || reference.originalDataUrl || reference.dataUrl,
      storage_key: reference.storageKey,
      preview_storage_key: reference.previewStorageKey,
      preview_url: reference.dataUrl,
    }))
  }

  async function uploadReference(files: File[]): Promise<void> {
    errorMessage.value = ''
    const remainingCapacity = Math.max(0, maxReferenceImages.value - (activeConversation.value?.referenceImages.length ?? 0))
    const acceptedFiles = files.slice(0, remainingCapacity)
    const errors: string[] = []
    if (acceptedFiles.length < files.length) {
      errors.push(`当前模型最多支持 ${maxReferenceImages.value} 张参考图`)
    }
    for (const file of acceptedFiles) {
      try {
        const uploaded = await options.api.uploadReference(file)
        setReference({
          id: uploaded.storage_key,
          dataUrl: authenticatedMediaUrl(uploaded.preview_url),
          originalDataUrl: authenticatedMediaUrl(uploaded.original_url),
          storageKey: uploaded.storage_key,
          previewStorageKey: uploaded.preview_storage_key,
          fileName: uploaded.name || file.name,
          mimeType: uploaded.mime_type || file.type || 'image/png',
        })
      } catch (error) {
        errors.push(`${file.name}: ${error instanceof Error ? error.message : String(error)}`)
      }
    }
    errorMessage.value = errors.join('\n')
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
      src: image.preview_url ? authenticatedMediaUrl(image.preview_url) : sourceOf(image),
      originalSrc: sourceOf(image),
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
    const requestedOutputCount = Math.min(Math.max(submitOptions.outputCount ?? outputCount.value, 1), maxOutputImages.value)
    if (!conversation || !key || !userPrompt || references.length > maxReferenceImages.value) return

    const createdAt = timestamp()
    const userId = `user-${now().getTime()}`
    const pendingId = `assistant-pending-${now().getTime()}`
    const userMessage: ChatMessage = {
      id: userId,
      role: 'user',
      content: userPrompt,
      createdAt,
      referenceImages: references.map(reference => ({ ...reference })),
      requestSettings: [{ modelLabel: model.value, sizeLabel: size.value, countLabel: `数量: ${requestedOutputCount}` }],
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
    promoteConversation(conversation.id)
    prompt.value = ''
    generationStatus.value = 'submitting'
    errorMessage.value = ''

    try {
      const response = await options.api.generate({
        prompt: requestPrompt(userPrompt, references),
        api_key_id: key.id,
        model: model.value,
        size: size.value,
        response_format: 'b64_json',
        output_count: requestedOutputCount,
        reference_images: referencesToRequest(references),
        inputs: {
          display_prompt: userPrompt,
          conversation_id: conversation.conversationId || conversation.id,
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
      replacePending(conversation.id, pendingId, {
        id: `assistant-${now().getTime()}`,
        role: 'assistant',
        content: '生成结果',
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

  function terminalMessage(record: GenerateResponse, fallbackPrompt = ''): ChatMessage {
    if (record.status === 'failed' || record.status === 'canceled') {
      return {
        id: `${record.job_id}-assistant`,
        role: 'assistant',
        content: record.error_message || (record.status === 'canceled' ? '生成已取消' : '图片生成失败'),
        createdAt: timestamp(),
        status: record.status,
      }
    }
    const images = imagesFromResult(record, fallbackPrompt)
    return {
      id: `${record.job_id}-assistant`,
      role: 'assistant',
      content: images.length ? '生成结果' : '图片生成未返回可显示的图片',
      createdAt: timestamp(),
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

  function promptBeforePending(conversationId: string, messageId: string): string {
    const conversation = conversations.value.find(item => item.id === conversationId)
    const pendingIndex = conversation?.messages.findIndex(item => item.id === messageId) ?? -1
    if (!conversation || pendingIndex < 1) return ''
    for (let index = pendingIndex - 1; index >= 0; index -= 1) {
      if (conversation.messages[index].role === 'user') return conversation.messages[index].content
    }
    return ''
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
        if (location) replacePending(location.conversationId, location.messageId, terminalMessage(record, promptBeforePending(location.conversationId, location.messageId)))
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
      originalDataUrl: image.originalSrc,
      fileName: `${image.id}.png`,
      mimeType: 'image/png',
    }
    const conversation = activeConversation.value
    const assistantIndex = conversation?.messages.findIndex(message => message.images?.some(item => item.id === image.id)) ?? -1
    const sourceMessage = assistantIndex > 0 ? conversation?.messages.slice(0, assistantIndex).reverse().find(message => message.role === 'user') : undefined
    await submit({ prompt: repeatPrompt || image.revisedPrompt, references: [reference], outputCount: messageOutputCount(sourceMessage) })
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
    await submit({ prompt: userMessage.content, references: userMessage.referenceImages ?? [], outputCount: messageOutputCount(userMessage) })
  }

  function messageOutputCount(message?: ChatMessage): number {
    const label = message?.requestSettings?.[0]?.countLabel ?? ''
    const parsed = Number(label.match(/\d+/)?.[0])
    return Number.isInteger(parsed) && parsed > 0 ? parsed : 1
  }

  function setReference(reference?: ImageReference): void {
    const conversation = activeConversation.value
    if (!conversation) return
    updateConversation(conversation.id, current => {
      if (!reference) return { ...current, referenceImages: [] }
      const identity = reference.storageKey || reference.id
      const exists = current.referenceImages.some(item => (item.storageKey || item.id) === identity)
      return exists ? current : { ...current, referenceImages: [...current.referenceImages, reference] }
    })
  }

  function removeReference(id: string): void {
    const conversation = activeConversation.value
    if (!conversation) return
    updateConversation(conversation.id, current => ({
      ...current,
      referenceImages: current.referenceImages.filter(reference => reference.id !== id),
    }))
  }

  function clearReferences(): void {
    setReference()
  }

  async function deleteConversation(conversation: Conversation): Promise<void> {
    if (conversation.conversationId) await options.api.deleteConversation(conversation.conversationId)
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
    maxReferenceImages,
    referenceLimitExceeded,
    model,
    size,
    outputCount,
    prompt,
    conversations,
    activeConversationId,
    activeConversation,
    hasOlderMessages,
    generationStatus,
    activeJobId,
    errorMessage,
    maxOutputImages,
    loadingConversation,
    initialize,
    createConversation,
    selectConversation,
    loadOlderMessages,
    submit,
    cancelGeneration,
    repeatFromImage,
    refineFromImage,
    retryMessage,
    setReference,
    removeReference,
    clearReferences,
    uploadReference,
    deleteConversation,
    dispose,
  }
}
