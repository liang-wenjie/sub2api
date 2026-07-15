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
  ImagePresetSelection,
  EnumCapability,
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

interface ActiveGenerationTask {
  conversationId: string
  pendingId: string
  prompt: string
  label?: string
  jobId: string
  state: 'submitting' | 'polling' | 'cancelling'
  timer?: ReturnType<typeof setTimeout>
}

const defaultModels = ['gpt-image-2', 'gpt-image-1', 'gemini-2.5-flash-image']
const emptyPresetSelection = (): ImagePresetSelection => ({ styles: [], scenes: [], effects: [], angles: [] })

const presetLabels: Record<string, string> = {
  cinematic: '电影感',
  photorealistic: '写实摄影',
  anime: '动漫',
  illustration: '插画',
  product: '产品摄影',
  studio: '摄影棚',
  outdoor: '户外',
  interior: '室内',
  night: '夜景',
  minimal: '极简背景',
  soft_light: '柔和光线',
  dramatic_light: '戏剧光',
  depth_of_field: '景深',
  volumetric_light: '体积光',
  motion: '动态效果',
  front: '正面',
  back: '背面',
  left: '左侧',
  right: '右侧',
  three_quarter: '45 度',
  top: '俯视',
}

function selectSupported(current: string, descriptor?: EnumCapability): string {
  if (!descriptor) return ''
  return descriptor.values.includes(current) ? current : descriptor.default
}

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
  const quality = ref('')
  const outputFormat = ref('')
  const outputCompression = ref<number | null>(null)
  const background = ref('')
  const inputFidelity = ref('')
  const aspectRatio = ref('')
  const resolution = ref('')
  const prompt = ref('')
  const presetSelection = ref<ImagePresetSelection>(emptyPresetSelection())
  const appliedPresetText = ref('')
  const promptOptimizerModel = ref('')
  const promptOptimizerModels = ref<string[]>([])
  const optimizingPrompt = ref(false)
  const conversations = ref<Conversation[]>([])
  const activeConversationId = ref('')
  const generationStatus = ref<GenerationStatus>('idle')
  const activeJobId = ref('')
  const errorMessage = ref('')
  const modelCapabilities = ref<Record<string, ImageModelCapability>>({})
  const conversationNextCursor = ref<Record<string, string>>({})
  const loadingConversation = ref(false)
  let conversationRequestSequence = 0
  let promptModelRequestSequence = 0
  let promptOptimizationController: AbortController | null = null
  const activeTasks = new Map<string, ActiveGenerationTask>()
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
  const activeCapability = computed(() => modelCapabilities.value[model.value])
  const availableSizes = computed(() => activeCapability.value?.sizes?.values ?? [])
  const availableAspectRatios = computed(() => activeCapability.value?.aspect_ratios?.values ?? [])
  const availableResolutions = computed(() => activeCapability.value?.resolutions?.values ?? [])
  const availableQualities = computed(() => activeCapability.value?.quality?.values ?? [])
  const availableOutputFormats = computed(() => activeCapability.value?.output_formats?.values ?? [])
  const availableBackgrounds = computed(() => activeCapability.value?.background?.values ?? [])
  const availableInputFidelities = computed(() => activeCapability.value?.input_fidelity?.values ?? [])
  const compressionCapability = computed(() => activeCapability.value?.output_compression)
  const supportsOutputCompression = computed(() => Boolean(compressionCapability.value && ['jpeg', 'webp'].includes(outputFormat.value)))
  const referenceLimitExceeded = computed(() => (activeConversation.value?.referenceImages.length ?? 0) > maxReferenceImages.value)

  watch(maxOutputImages, limit => {
    outputCount.value = Math.min(Math.max(outputCount.value, 1), limit)
  })

  watch(activeCapability, (capability) => {
    size.value = capability ? selectSupported(size.value, capability.sizes) : '1024x1024'
    aspectRatio.value = selectSupported(aspectRatio.value, capability?.aspect_ratios)
    resolution.value = selectSupported(resolution.value, capability?.resolutions)
    quality.value = selectSupported(quality.value, capability?.quality)
    outputFormat.value = selectSupported(outputFormat.value, capability?.output_formats)
    background.value = selectSupported(background.value, capability?.background)
    inputFidelity.value = selectSupported(inputFidelity.value, capability?.input_fidelity)
    outputCompression.value = capability?.output_compression?.default ?? null
  }, { immediate: true })

  watch(supportsOutputCompression, supported => {
    if (supported && outputCompression.value == null) outputCompression.value = compressionCapability.value?.default ?? null
    if (!supported) outputCompression.value = null
  })

  watch(selectedKeyId, async (next, previous) => {
    if (!initializedKeySelection || next === previous) return
    try {
      await options.api.saveImageGenerationPreference(next)
    } catch (error) {
      errorMessage.value = error instanceof Error ? error.message : String(error)
    }
    await loadPromptOptimizerModels(next)
  })

  watch(promptOptimizerModels, models => {
    promptOptimizerModel.value = models.includes(promptOptimizerModel.value) ? promptOptimizerModel.value : models[0] ?? ''
  }, { immediate: true })

  function timestamp(): string {
    return now().toLocaleString()
  }

  function syncGenerationState(): void {
    const tasks = [...activeTasks.values()]
    activeJobId.value = tasks.find(task => task.jobId)?.jobId ?? ''
    if (tasks.some(task => task.state === 'cancelling')) generationStatus.value = 'cancelling'
    else if (tasks.some(task => task.state === 'polling')) generationStatus.value = 'polling'
    else if (tasks.length) generationStatus.value = 'submitting'
    else generationStatus.value = 'idle'
  }

  function finishTask(pendingId: string): void {
    const task = activeTasks.get(pendingId)
    if (task?.timer) clearTimeout(task.timer)
    activeTasks.delete(pendingId)
    syncGenerationState()
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
    appliedPresetText.value = ''
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

  async function loadPromptOptimizerModels(apiKeyID: number | null): Promise<void> {
    const sequence = ++promptModelRequestSequence
    if (!apiKeyID) {
      promptOptimizerModels.value = []
      promptOptimizerModel.value = ''
      return
    }
    try {
      const response = await options.api.listPromptModels(apiKeyID)
      if (sequence !== promptModelRequestSequence) return
      promptOptimizerModels.value = response.models
      promptOptimizerModel.value = response.models.includes(promptOptimizerModel.value) ? promptOptimizerModel.value : response.models[0] ?? ''
    } catch (error) {
      if (sequence !== promptModelRequestSequence) return
      promptOptimizerModels.value = []
      promptOptimizerModel.value = ''
      errorMessage.value = error instanceof Error ? error.message : String(error)
    }
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
    await loadPromptOptimizerModels(nextKeyID)
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
      for (const pending of page.items.filter(record => record.status === 'pending')) {
        const pendingId = `${pending.id}-assistant`
        activeTasks.set(pendingId, {
          conversationId: id,
          pendingId,
          prompt: pending.prompt,
          jobId: pending.id,
          state: 'polling',
        })
        syncGenerationState()
        schedulePoll(pendingId)
      }
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

  async function optimizePrompt(selectedModel = promptOptimizerModel.value): Promise<void> {
    const key = selectedKey.value
    const userPrompt = prompt.value.trim()
    const modelName = selectedModel.trim()
    if (!key || !userPrompt || !modelName || optimizingPrompt.value) return
    const controller = new AbortController()
    promptOptimizationController = controller
    optimizingPrompt.value = true
    errorMessage.value = ''
    try {
      const response = await options.api.optimizePrompt({
        prompt: userPrompt,
        api_key_id: key.id,
        model: modelName,
      }, controller.signal)
      const optimized = response.prompt.trim()
      if (optimized) prompt.value = optimized
    } catch (error) {
      if (controller.signal.aborted) return
      errorMessage.value = error instanceof Error ? error.message : String(error)
    } finally {
      if (promptOptimizationController === controller) {
        promptOptimizationController = null
        optimizingPrompt.value = false
      }
    }
  }

  function cancelPromptOptimization(): void {
    promptOptimizationController?.abort()
    promptOptimizationController = null
    optimizingPrompt.value = false
  }

  function requestPrompt(userPrompt: string, references: ImageReference[]): string {
    const lines = ['Follow the user request with highest priority.']
    if (references.length > 0) {
      lines.push('Use the uploaded reference image as the primary subject and preserve its identity unless the user asks to change it.')
    }
    lines.push(`User request: ${userPrompt}`)
    return lines.join('\n')
  }

  function presetDescription(selected: ImagePresetSelection): string {
    const details = [
      selected.styles.length ? `风格：${selected.styles.map(item => presetLabels[item] ?? item).join('、')}` : '',
      selected.scenes.length ? `场景：${selected.scenes.map(item => presetLabels[item] ?? item).join('、')}` : '',
      selected.effects.length ? `特效：${selected.effects.map(item => presetLabels[item] ?? item).join('、')}` : '',
      selected.angles.length ? `角度：${selected.angles.map(item => presetLabels[item] ?? item).join('、')}${selected.angles.length > 1 ? '（分别生成独立图片）' : ''}` : '',
    ].filter(Boolean)
    return details.join('\n')
  }

  function presetPrompt(userPrompt: string): string {
    const description = presetDescription(presetSelection.value)
    if (!description || userPrompt.includes(description)) return userPrompt
    return `${userPrompt}\n${description}`
  }

  function applyPresetSelection(selection: ImagePresetSelection): void {
    const previous = appliedPresetText.value
    let basePrompt = prompt.value
    if (previous) {
      basePrompt = basePrompt.replace(`\n${previous}`, '').replace(previous, '').trimEnd()
    }
    presetSelection.value = selection
    const next = presetDescription(selection)
    appliedPresetText.value = next
    prompt.value = next ? `${basePrompt.trimEnd()}\n${next}`.trimStart() : basePrompt
  }

  function angleVariants(basePrompt: string) {
    const angles = presetSelection.value.angles.slice(0, maxOutputImages.value)
    if (angles.length < 2) return undefined
    return angles.map(angle => {
      const label = presetLabels[angle] ?? angle
      return {
        label,
        prompt: `${basePrompt}\n角度：${label}。只生成这个角度的一张独立图片；保持主体身份、服装、比例、材质、场景和光线与其他角度一致。`,
      }
    })
  }

  function imagesFromResult(response: GenerateResponse, fallbackPrompt = '', fallbackLabel?: string): GeneratedImage[] {
    const resultPrompt = response.result?.revised_prompt || fallbackPrompt
    return (response.result?.images ?? []).map((image, index) => ({
      id: `${response.job_id}-image-${index}`,
      src: image.preview_url ? authenticatedMediaUrl(image.preview_url) : sourceOf(image),
      originalSrc: sourceOf(image),
      revisedPrompt: image.revised_prompt || resultPrompt,
      variantLabel: image.variant_label || fallbackLabel,
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

  function activeParameterSummary(hasReferences: boolean): string {
    const details = [
      aspectRatio.value ? `比例: ${aspectRatio.value}` : '',
      resolution.value ? `分辨率: ${resolution.value}` : '',
      quality.value ? `画质: ${quality.value}` : '',
      outputFormat.value ? `格式: ${outputFormat.value}` : '',
      supportsOutputCompression.value && outputCompression.value != null ? `压缩: ${outputCompression.value}%` : '',
      background.value ? `背景: ${background.value}` : '',
      hasReferences && inputFidelity.value ? `保真度: ${inputFidelity.value}` : '',
    ]
    return details.filter(Boolean).join(' | ')
  }

  async function submit(submitOptions: SubmitOptions = {}): Promise<void> {
    if (generationStatus.value !== 'idle') return
    const conversation = activeConversation.value
    const key = selectedKey.value
    const userPrompt = (submitOptions.prompt ?? prompt.value).trim()
    const references = submitOptions.references ?? conversation?.referenceImages ?? []
    const composedPrompt = presetPrompt(userPrompt)
    const variants = angleVariants(composedPrompt)
    const requestedOutputCount = variants?.length ?? Math.min(Math.max(submitOptions.outputCount ?? outputCount.value, 1), maxOutputImages.value)
    if (!conversation || !key || !userPrompt || references.length > maxReferenceImages.value) return

    const createdAt = timestamp()
    const userId = `user-${now().getTime()}`
    const independentGPTTasks = model.value.startsWith('gpt-image-') && requestedOutputCount > 1
    const descriptors = independentGPTTasks
      ? (variants ?? Array.from({ length: requestedOutputCount }, (_, index) => ({
          label: `图片 ${index + 1}`,
          prompt: composedPrompt,
        })))
      : [{ label: variants?.[0]?.label, prompt: variants?.[0]?.prompt ?? composedPrompt }]
    const pendingMessages: ChatMessage[] = descriptors.map((descriptor, index) => ({
      id: `assistant-pending-${now().getTime()}-${index}`,
      role: 'assistant',
      content: descriptor.label ? `正在生成图片（${descriptor.label}），请稍候...` : '正在生成图片，请稍候...',
      createdAt,
      status: 'pending',
    }))
    const userMessage: ChatMessage = {
      id: userId,
      role: 'user',
      content: userPrompt,
      createdAt,
      referenceImages: references.map(reference => ({ ...reference })),
      requestSettings: [{
        modelLabel: model.value,
        sizeLabel: size.value || aspectRatio.value,
        countLabel: `数量: ${requestedOutputCount}`,
        detailsLabel: activeParameterSummary(references.length > 0),
      }],
    }
    updateConversation(conversation.id, current => ({
      ...current,
      title: current.messages.length === 0 ? userPrompt.slice(0, 24) : current.title,
      preview: pendingMessages[0].content,
      lastUsedAt: createdAt,
      messages: [...current.messages, userMessage, ...pendingMessages],
    }))
    promoteConversation(conversation.id)
    prompt.value = ''
    errorMessage.value = ''

    const commonRequest = {
        api_key_id: key.id,
        model: model.value,
        size: size.value,
        response_format: 'b64_json',
        ...(quality.value ? { quality: quality.value } : {}),
        ...(outputFormat.value ? { output_format: outputFormat.value } : {}),
        ...(supportsOutputCompression.value && outputCompression.value != null ? { output_compression: outputCompression.value } : {}),
        ...(background.value ? { background: background.value } : {}),
        ...(references.length > 0 && inputFidelity.value ? { input_fidelity: inputFidelity.value } : {}),
        ...(aspectRatio.value ? { aspect_ratio: aspectRatio.value } : {}),
        ...(resolution.value ? { resolution: resolution.value } : {}),
        reference_images: referencesToRequest(references),
        ...(!independentGPTTasks && variants ? {
          variants: variants.map(variant => ({ ...variant, prompt: requestPrompt(variant.prompt, references) })),
        } : {}),
        inputs: {
          display_prompt: userPrompt,
          conversation_id: conversation.conversationId || conversation.id,
        },
    }

    const results = await Promise.all(descriptors.map(async (descriptor, index) => {
      const pendingId = pendingMessages[index].id
      const task: ActiveGenerationTask = {
        conversationId: conversation.id,
        pendingId,
        prompt: userPrompt,
        label: descriptor.label,
        jobId: '',
        state: 'submitting',
      }
      activeTasks.set(pendingId, task)
      syncGenerationState()
      try {
        const response = await options.api.generate({
          ...commonRequest,
          prompt: requestPrompt(descriptor.prompt, references),
          output_count: independentGPTTasks ? 1 : requestedOutputCount,
        })
        if (!activeTasks.has(pendingId)) return false
        task.jobId = response.job_id
        if (response.status === 'pending') {
          task.state = 'polling'
          syncGenerationState()
          schedulePoll(pendingId)
          return true
        }
        const message = terminalMessage(response, userPrompt, descriptor.label)
        replacePending(conversation.id, pendingId, message)
        finishTask(pendingId)
        return message.status !== 'failed' && message.status !== 'canceled'
      } catch (error) {
        if (!activeTasks.has(pendingId)) return false
        const message = error instanceof Error ? error.message : String(error)
        replacePending(conversation.id, pendingId, {
          id: `assistant-failed-${pendingId}`,
          role: 'assistant',
          content: `图片生成失败${descriptor.label ? `（${descriptor.label}）` : ''}\n${message}`,
          createdAt: timestamp(),
          status: 'failed',
        })
        finishTask(pendingId)
        return false
      }
    }))
    if (results.every(result => !result)) errorMessage.value = '所有图片生成任务均失败'
  }

  function terminalMessage(record: GenerateResponse, fallbackPrompt = '', fallbackLabel?: string): ChatMessage {
    if (record.status === 'failed' || record.status === 'canceled') {
      return {
        id: `${record.job_id}-assistant`,
        role: 'assistant',
        content: record.error_message || (record.status === 'canceled' ? '生成已取消' : '图片生成失败'),
        createdAt: timestamp(),
        status: record.status,
      }
    }
    const images = imagesFromResult(record, fallbackPrompt, fallbackLabel)
    return {
      id: `${record.job_id}-assistant`,
      role: 'assistant',
      content: images.length ? '生成结果' : '图片生成未返回可显示的图片',
      createdAt: timestamp(),
      status: images.length ? undefined : 'failed',
      images: images.length ? images : undefined,
    }
  }

  function schedulePoll(pendingId: string): void {
    const task = activeTasks.get(pendingId)
    if (!task?.jobId) return
    if (task.timer) clearTimeout(task.timer)
    task.timer = setTimeout(async () => {
      const current = activeTasks.get(pendingId)
      if (!current?.jobId) return
      try {
        const record = await options.api.getStatus(current.jobId)
        if (record.status === 'pending') {
          schedulePoll(pendingId)
          return
        }
        replacePending(current.conversationId, pendingId, terminalMessage(record, current.prompt, current.label))
        finishTask(pendingId)
      } catch (error) {
        errorMessage.value = error instanceof Error ? error.message : String(error)
        schedulePoll(pendingId)
      }
    }, pollInterval)
  }

  async function cancelGeneration(): Promise<void> {
    const tasks = [...activeTasks.values()]
    if (!tasks.length) return
    for (const task of tasks) {
      task.state = 'cancelling'
      if (task.timer) clearTimeout(task.timer)
    }
    syncGenerationState()
    await Promise.all(tasks.map(async task => {
      let message: ChatMessage
      if (task.jobId) {
        try {
          message = terminalMessage(await options.api.cancel(task.jobId), task.prompt, task.label)
        } catch (error) {
          message = {
            id: `${task.pendingId}-canceled`, role: 'assistant', status: 'canceled', createdAt: timestamp(),
            content: error instanceof Error ? error.message : '生成已取消',
          }
        }
      } else {
        message = { id: `${task.pendingId}-canceled`, role: 'assistant', status: 'canceled', createdAt: timestamp(), content: '生成已取消' }
      }
      replacePending(task.conversationId, task.pendingId, message)
      finishTask(task.pendingId)
    }))
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
    for (const task of activeTasks.values()) {
      if (task.timer) clearTimeout(task.timer)
    }
    activeTasks.clear()
    syncGenerationState()
    cancelPromptOptimization()
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
    quality,
    outputFormat,
    outputCompression,
    background,
    inputFidelity,
    aspectRatio,
    resolution,
    availableSizes,
    availableAspectRatios,
    availableResolutions,
    availableQualities,
    availableOutputFormats,
    availableBackgrounds,
    availableInputFidelities,
    compressionCapability,
    supportsOutputCompression,
    outputCount,
    prompt,
    presetSelection,
    applyPresetSelection,
    promptOptimizerModel,
    promptOptimizerModels,
    optimizingPrompt,
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
    optimizePrompt,
    cancelPromptOptimization,
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
