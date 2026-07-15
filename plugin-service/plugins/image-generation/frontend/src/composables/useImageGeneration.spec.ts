import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { useImageGeneration } from './useImageGeneration'
import type { GenerateResponse, HistoryRecord, ImageApiKey } from '../types'

const key: ImageApiKey = {
  id: 7,
  key: 'sk-image',
  name: 'Image key',
  status: 'active',
  group: { allow_image_generation: true },
}

function completedResponse(): GenerateResponse {
  return {
    job_id: 'job-1',
    status: 'succeeded',
    result: { images: [{ url: '/plugins/image-generation/api/assets/job-1/result/0', preview_url: '/plugins/image-generation/api/assets/job-1/result/0/preview', revised_prompt: 'A blue lamp' }] },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function createApi(generateResult: GenerateResponse = completedResponse()) {
  return {
    getConfig: vi.fn().mockResolvedValue({
      image_model_capabilities: {
        'gpt-image-2': {
          max_reference_images: 16, max_output_images: 10,
          sizes: { values: ['1024x1024', '1536x1024', '1024x1536'], default: '1024x1024' },
          quality: { values: ['auto', 'low', 'medium', 'high'], default: 'auto' },
          output_formats: { values: ['png', 'jpeg', 'webp'], default: 'png' },
          output_compression: { min: 0, max: 100, default: 100 },
          background: { values: ['auto', 'transparent', 'opaque'], default: 'auto' },
          input_fidelity: { values: ['low', 'high'], default: 'high' },
        },
        'gpt-image-1': {
          max_reference_images: 16, max_output_images: 10,
          sizes: { values: ['1024x1024', '1536x1024', '1024x1536'], default: '1024x1024' },
          quality: { values: ['auto', 'low', 'medium', 'high'], default: 'auto' },
          output_formats: { values: ['png', 'jpeg', 'webp'], default: 'png' },
          output_compression: { min: 0, max: 100, default: 100 },
          background: { values: ['auto', 'transparent', 'opaque'], default: 'auto' },
          input_fidelity: { values: ['low', 'high'], default: 'high' },
        },
        'gemini-2.5-flash-image': {
          max_reference_images: 10, max_output_images: 4,
          aspect_ratios: { values: ['1:1', '2:3', '3:2', '3:4', '4:3', '4:5', '5:4', '9:16', '16:9', '21:9'], default: '1:1' },
          resolutions: { values: ['1K', '2K', '4K'], default: '1K' },
        },
      },
    }),
    getImageGenerationPreference: vi.fn().mockResolvedValue({ last_api_key_id: null }),
    saveImageGenerationPreference: vi.fn().mockResolvedValue({ last_api_key_id: null }),
    uploadReference: vi.fn(),
    listPromptModels: vi.fn().mockResolvedValue({ models: ['gpt-5.1'] }),
    listConversations: vi.fn().mockResolvedValue({ items: [] }),
    listConversationMessages: vi.fn().mockResolvedValue({ items: [] }),
    optimizePrompt: vi.fn().mockResolvedValue({ prompt: 'Optimized prompt', model: 'gpt-5.1' }),
    generate: vi.fn().mockResolvedValue(generateResult),
    retryHistory: vi.fn(),
    getStatus: vi.fn(),
    cancel: vi.fn(),
    deleteConversation: vi.fn(),
  }
}

describe('useImageGeneration', () => {
  it('restores a saved usable key without rewriting the preference', async () => {
    const api = createApi()
    api.getImageGenerationPreference.mockResolvedValue({ last_api_key_id: 8 })
    const savedKey = { ...key, id: 8 }
    const state = useImageGeneration({ api, loadKeys: async () => [key, savedKey] })

    await state.initialize()

    expect(state.selectedKeyId.value).toBe(8)
    expect(api.saveImageGenerationPreference).not.toHaveBeenCalled()
  })

  it('falls back to the first usable key and persists it when the saved key is unavailable', async () => {
    const api = createApi()
    api.getImageGenerationPreference.mockResolvedValue({ last_api_key_id: 99 })
    const state = useImageGeneration({ api, loadKeys: async () => [key] })

    await state.initialize()

    expect(state.selectedKeyId.value).toBe(7)
    expect(api.saveImageGenerationPreference).toHaveBeenCalledWith(7)
  })

  it('falls back when the saved key is filtered out of image generation', async () => {
    const api = createApi()
    api.getImageGenerationPreference.mockResolvedValue({ last_api_key_id: 8 })
    const unavailable = { ...key, id: 8, status: 'disabled' }
    const state = useImageGeneration({ api, loadKeys: async () => [unavailable, key] })

    await state.initialize()

    expect(state.selectedKeyId.value).toBe(7)
    expect(api.saveImageGenerationPreference).toHaveBeenCalledWith(7)
  })

  it('persists a user key selection after initialization', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key, { ...key, id: 8 }] })
    await state.initialize()

    state.selectedKeyId.value = 8
    await nextTick()
    await Promise.resolve()

    expect(api.saveImageGenerationPreference).toHaveBeenCalledWith(8)
  })

  it('persists null for an empty usable key list and retains user selection on save failure', async () => {
    const api = createApi()
    api.getImageGenerationPreference.mockResolvedValue({ last_api_key_id: 99 })
    const empty = useImageGeneration({ api, loadKeys: async () => [] })
    await empty.initialize()
    expect(api.saveImageGenerationPreference).toHaveBeenCalledWith(null)

    const changing = createApi()
    changing.saveImageGenerationPreference.mockRejectedValue(new Error('sync failed'))
    const state = useImageGeneration({ api: changing, loadKeys: async () => [key, { ...key, id: 8 }] })
    await state.initialize()
    state.selectedKeyId.value = 8
    await nextTick()
    await Promise.resolve()
    expect(state.selectedKeyId.value).toBe(8)
    expect(state.errorMessage.value).toBe('sync failed')
  })
  it('appends uploaded references, ignores duplicates, and removes one by id', async () => {
    const api = createApi()
    api.uploadReference
      .mockResolvedValueOnce({
        name: 'first.png', mime_type: 'image/png', storage_key: 'uploads/first/original',
        preview_storage_key: 'uploads/first/preview', original_url: '/first/original', preview_url: '/first/preview',
      })
      .mockResolvedValueOnce({
        name: 'second.png', mime_type: 'image/png', storage_key: 'uploads/second/original',
        preview_storage_key: 'uploads/second/preview', original_url: '/second/original', preview_url: '/second/preview',
      })
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()

    await state.uploadReference([
      new File(['first'], 'first.png', { type: 'image/png' }),
      new File(['second'], 'second.png', { type: 'image/png' }),
    ])
    state.setReference({
      id: 'uploads/second/original', dataUrl: '/second/preview', storageKey: 'uploads/second/original',
      fileName: 'second.png', mimeType: 'image/png',
    })

    expect(state.activeConversation.value?.referenceImages.map(item => item.fileName)).toEqual(['first.png', 'second.png'])
    state.removeReference('uploads/second/original')
    expect(state.activeConversation.value?.referenceImages.map(item => item.fileName)).toEqual(['first.png'])
    state.clearReferences()
    expect(state.activeConversation.value?.referenceImages).toEqual([])
  })

  it('clamps output count to the selected model and sends it', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()
    state.outputCount.value = 8
    state.model.value = 'gemini-2.5-flash-image'
    await nextTick()
    expect(state.maxOutputImages.value).toBe(4)
    expect(state.outputCount.value).toBe(4)
    state.prompt.value = 'four variations'
    await state.submit()
    expect(api.generate).toHaveBeenCalledWith(expect.objectContaining({ output_count: 4 }))
  })

  it('derives parameter options and defaults from the selected model', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key] })

    await state.initialize()

    expect(state.availableSizes.value).toEqual(['1024x1024', '1536x1024', '1024x1536'])
    expect(state.availableQualities.value).toEqual(['auto', 'low', 'medium', 'high'])
    expect(state.quality.value).toBe('auto')
    expect(state.outputFormat.value).toBe('png')
    expect(state.background.value).toBe('auto')
  })

  it('uses all advertised Gemini ratios and clears GPT-only settings', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()
    state.quality.value = 'high'

    state.model.value = 'gemini-2.5-flash-image'
    await nextTick()

    expect(state.availableAspectRatios.value).toEqual(['1:1', '2:3', '3:2', '3:4', '4:3', '4:5', '5:4', '9:16', '16:9', '21:9'])
    expect(state.aspectRatio.value).toBe('1:1')
    expect(state.resolution.value).toBe('1K')
    expect(state.quality.value).toBe('')
    expect(state.size.value).toBe('')
  })

  it('hides advanced settings and omits stale values for an unconfigured model', async () => {
    const customKey: ImageApiKey = {
      ...key,
      group: { allow_image_generation: true, models_list_config: { enabled: true, models: ['gpt-image-custom'] } },
    }
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [customKey] })
    await state.initialize()
    state.quality.value = 'high'
    state.model.value = 'gpt-image-custom'
    await nextTick()
    state.prompt.value = 'draw a lamp'

    await state.submit()

    expect(state.availableQualities.value).toEqual([])
    expect(state.quality.value).toBe('')
    const request = api.generate.mock.calls[0][0] as Record<string, unknown>
    expect(request).not.toHaveProperty('quality')
    expect(request).not.toHaveProperty('aspect_ratio')
  })

  it('serializes only active model parameters', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()
    state.outputFormat.value = 'webp'
    await nextTick()
    state.quality.value = 'high'
    state.outputCompression.value = 82
    state.background.value = 'transparent'
    state.prompt.value = 'draw a lamp'

    await state.submit()

    expect(api.generate).toHaveBeenCalledWith(expect.objectContaining({
      size: '1024x1024', quality: 'high', output_format: 'webp', output_compression: 82, background: 'transparent',
    }))
    expect(state.activeConversation.value?.messages[0].requestSettings?.[0].detailsLabel).toContain('画质: high')
    expect(state.activeConversation.value?.messages[0].requestSettings?.[0].detailsLabel).toContain('格式: webp')
    expect(state.activeConversation.value?.messages[0].requestSettings?.[0].detailsLabel).toContain('压缩: 82%')
  })

  it('submits the selected API key id without the key secret or duplicate metadata', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()
    state.prompt.value = 'draw a lamp'

    await state.submit()

    const request = api.generate.mock.calls[0][0] as Record<string, unknown>
    expect(request.api_key_id).toBe(key.id)
    expect(request).not.toHaveProperty('provider_api_key')
    expect(request.inputs).not.toHaveProperty('api_key_id')
  })

  it('uploads only files within the remaining model capacity', async () => {
    const api = createApi()
    api.getConfig.mockResolvedValue({
      image_model_capabilities: { 'gpt-image-2': { max_reference_images: 1 } },
    })
    api.uploadReference.mockResolvedValue({
      name: 'first.png', mime_type: 'image/png', storage_key: 'uploads/first/original',
      preview_storage_key: 'uploads/first/preview', original_url: '/first/original', preview_url: '/first/preview',
    })
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()

    await state.uploadReference([
      new File(['first'], 'first.png', { type: 'image/png' }),
      new File(['second'], 'second.png', { type: 'image/png' }),
    ])

    expect(api.uploadReference).toHaveBeenCalledTimes(1)
    expect(state.activeConversation.value?.referenceImages).toHaveLength(1)
    expect(state.errorMessage.value).toContain('最多支持 1 张参考图')
  })

  it('keeps references and blocks submission when the selected model limit is exceeded', async () => {
    const api = createApi()
    const limitedKey: ImageApiKey = {
      ...key,
      group: { allow_image_generation: true, models_list_config: { enabled: true, models: ['gpt-image-2', 'custom-image-model'] } },
    }
    const state = useImageGeneration({ api, loadKeys: async () => [limitedKey] })
    await state.initialize()
    state.setReference({ id: 'first', dataUrl: '/first', fileName: 'first.png', mimeType: 'image/png' })
    state.setReference({ id: 'second', dataUrl: '/second', fileName: 'second.png', mimeType: 'image/png' })

    state.model.value = 'custom-image-model'
    state.prompt.value = 'Edit both images'
    await state.submit()

    expect(state.activeConversation.value?.referenceImages).toHaveLength(2)
    expect(state.maxReferenceImages.value).toBe(1)
    expect(state.referenceLimitExceeded.value).toBe(true)
    expect(api.generate).not.toHaveBeenCalled()
  })

  it('serializes every selected reference for a compatible model', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()
    state.setReference({ id: 'first', dataUrl: '/first', storageKey: 'uploads/first/original', fileName: 'first.png', mimeType: 'image/png' })
    state.setReference({ id: 'second', dataUrl: '/second', storageKey: 'uploads/second/original', fileName: 'second.png', mimeType: 'image/png' })
    state.prompt.value = 'Use both references'

    await state.submit()

    expect(api.generate).toHaveBeenCalledWith(expect.objectContaining({
      reference_images: [
        expect.objectContaining({ storage_key: 'uploads/first/original' }),
        expect.objectContaining({ storage_key: 'uploads/second/original' }),
      ],
    }))
  })

  it('uploads a reference before generation and submits storage metadata without base64', async () => {
    const api = createApi()
    api.uploadReference.mockResolvedValue({
      name: 'reference.png', mime_type: 'image/png',
      storage_key: 'image-generation/uploads/7/upload-1/original',
      preview_storage_key: 'image-generation/uploads/7/upload-1/preview',
      original_url: '/plugins/image-generation/api/references/upload-1/original',
      preview_url: '/plugins/image-generation/api/references/upload-1/preview',
    })
    const state = useImageGeneration({ api, loadKeys: async () => [key] })
    await state.initialize()

    await state.uploadReference([new File(['png-bytes'], 'reference.png', { type: 'image/png' })])
    state.prompt.value = 'Edit this image'
    await state.submit()

    expect(api.generate).toHaveBeenCalledWith(expect.objectContaining({
      reference_images: [expect.objectContaining({
        storage_key: 'image-generation/uploads/7/upload-1/original',
        preview_storage_key: 'image-generation/uploads/7/upload-1/preview',
        data_url: undefined,
      })],
    }))
  })

  it('loads summaries before the selected conversation details', async () => {
    const api = createApi()
    api.listConversations.mockResolvedValue({ items: [{ id: 'conversation-1', title: 'Lamp', preview: 'Latest', status: 'succeeded', updated_at: '2026-07-11T10:00:00Z' }] })
    api.listConversationMessages.mockResolvedValue({ items: [{
      id: 'history-1', conversation_id: 'conversation-1', user_id: 1, prompt: 'Create a lamp', status: 'succeeded',
      request: { model: 'gpt-image-2', size: '1024x1024', output_count: 3 }, result: { images: [{ url: '/original.png', preview_url: '/preview.jpg' }] },
      created_at: '2026-07-11T09:59:00Z', updated_at: '2026-07-11T10:00:00Z',
    }] })
    const state = useImageGeneration({ api, loadKeys: async () => [key] })

    await state.initialize()
    expect(state.conversations.value[0].messages[0].requestSettings?.[0].countLabel).toBe('数量: 3')

    expect(api.listConversationMessages).toHaveBeenCalledWith('conversation-1', '')
    expect(state.conversations.value[0].messages).toHaveLength(2)
    expect(state.conversations.value[0].messages[1].content).toBe('生成结果')
    expect(state.conversations.value[0].messages[1].images?.[0].src).toContain('/preview.jpg')
    expect(state.conversations.value[0].messages[1].images?.[0].revisedPrompt).toBe('Create a lamp')
  })

  it('keeps the latest reference when older messages are loaded', async () => {
    const api = createApi()
    api.listConversations.mockResolvedValue({ items: [{ id: 'conversation-1', title: 'Lamp', preview: 'Latest', status: 'succeeded', updated_at: '2026-07-11T10:00:00Z' }] })
    api.listConversationMessages
      .mockResolvedValueOnce({
        items: [{
          id: 'history-new', conversation_id: 'conversation-1', user_id: 1, prompt: 'New prompt', status: 'succeeded',
          request: { model: 'gpt-image-2', size: '1024x1024', reference_images: [{ name: 'new-reference.png', data_url: 'data:image/png;base64,new' }] },
          result: { images: [] }, created_at: '2026-07-11T09:59:00Z', updated_at: '2026-07-11T10:00:00Z',
        }],
        next_cursor: 'older-cursor',
      })
      .mockResolvedValueOnce({
        items: [{
          id: 'history-old', conversation_id: 'conversation-1', user_id: 1, prompt: 'Old prompt', status: 'succeeded',
          request: { model: 'gpt-image-2', size: '1024x1024', reference_images: [{ name: 'old-reference.png', data_url: 'data:image/png;base64,old' }] },
          result: { images: [] }, created_at: '2026-07-10T09:59:00Z', updated_at: '2026-07-10T10:00:00Z',
        }],
        next_cursor: '',
      })
    const state = useImageGeneration({ api, loadKeys: async () => [key] })

    await state.initialize()
    await state.loadOlderMessages()

    expect(api.listConversationMessages).toHaveBeenLastCalledWith('conversation-1', 'older-cursor')
    expect(state.activeConversation.value?.messages).toHaveLength(2)
    expect(state.activeConversation.value?.referenceImages[0]?.fileName).toBe('new-reference.png')
  })

  it('creates a renderable conversation before initialization requests finish', async () => {
    let resolveHistory!: (value: { items: [] }) => void
    const api = createApi()
    api.listConversations.mockReturnValue(new Promise(resolve => { resolveHistory = resolve }))
    const state = useImageGeneration({ api, loadKeys: async () => [key] })

    const initialization = state.initialize()

    expect(state.activeConversation.value).not.toBeNull()
    resolveHistory({ items: [] })
    await initialization
  })

  it('replaces the optimistic assistant message with generated images', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = 'Create a lamp'

    await state.submit()

    expect(state.activeConversation.value?.messages.map(message => message.role)).toEqual(['user', 'assistant'])
    expect(state.activeConversation.value?.messages[1].content).toBe('生成结果')
    expect(state.activeConversation.value?.messages[1].images?.[0].src).toContain('/preview')
  })

  it('falls back to the sent prompt when the result has no revised prompt', async () => {
    const api = createApi({
      job_id: 'job-1',
      status: 'succeeded',
      result: { images: [{ url: '/plugins/image-generation/api/assets/job-1/result/0', revised_prompt: '' }] },
    })
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = '一只坐在客厅地毯上的小狗'

    await state.submit()

    expect(state.activeConversation.value?.messages[1].images?.[0].revisedPrompt).toBe('一只坐在客厅地毯上的小狗')
    expect(state.activeConversation.value?.messages[1].content).toBe('生成结果')
  })

  it('renders a failed message when a completed response has no image', async () => {
    const api = createApi({ job_id: 'job-1', status: 'succeeded', result: { images: [] } })
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = 'Create a lamp'

    await state.submit()

    expect(state.activeConversation.value?.messages[1].status).toBe('failed')
    expect(state.activeConversation.value?.messages[1].content).toContain('图片生成未返回可显示的图片')
  })

  it('polls a pending job and can cancel it', async () => {
    const api = createApi({ job_id: 'history-1', status: 'pending' })
    api.getStatus.mockResolvedValue({ job_id: 'history-1', status: 'pending' })
    api.cancel.mockResolvedValue({ job_id: 'history-1', status: 'canceled', error_message: 'stopped' })
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 60_000 })
    await state.initialize()
    state.prompt.value = 'Create a lamp'
    await state.submit()

    expect(state.generationStatus.value).toBe('polling')
    await state.cancelGeneration()
    expect(api.cancel).toHaveBeenCalledWith('history-1')
    expect(state.activeConversation.value?.messages[1].status).toBe('canceled')
    state.dispose()
  })

  it('uses a generated image as reference when generating again', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.outputCount.value = 3
    state.prompt.value = 'Create a lamp'
    await state.submit()
    const image = state.activeConversation.value!.messages[1].images![0]
    state.outputCount.value = 1

    await state.repeatFromImage(image, 'Try another')

    expect(api.generate.mock.calls.slice(-3)).toHaveLength(3)
    for (const [request] of api.generate.mock.calls.slice(-3)) {
      expect(request).toEqual(expect.objectContaining({
        output_count: 1,
        reference_images: [expect.objectContaining({ data_url: image.originalSrc })],
      }))
    }
  })

  it('submits selected angle variants as independent single-image tasks', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = '红色夹克角色'
    state.presetSelection.value = {
      styles: ['cinematic'],
      scenes: ['studio'],
      effects: ['dramatic_light'],
      angles: ['front', 'back'],
    }

    await state.submit()

    expect(api.generate).toHaveBeenCalledTimes(2)
    expect(api.generate).toHaveBeenNthCalledWith(1, expect.objectContaining({
      output_count: 1,
      prompt: expect.stringContaining('角度：正面'),
    }))
    expect(api.generate).toHaveBeenNthCalledWith(2, expect.objectContaining({
      output_count: 1,
      prompt: expect.stringContaining('角度：背面'),
    }))
    expect(api.generate.mock.calls[0][0]).not.toHaveProperty('variants')
    expect(api.generate.mock.calls[1][0]).not.toHaveProperty('variants')
    expect(state.activeConversation.value?.messages.filter(message => message.role === 'assistant')).toHaveLength(2)
  })

  it('shows an independently completed GPT image while a sibling task is still running', async () => {
    const first = deferred<GenerateResponse>()
    const second = deferred<GenerateResponse>()
    const api = createApi()
    api.generate.mockImplementationOnce(() => first.promise).mockImplementationOnce(() => second.promise)
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.outputCount.value = 2
    state.prompt.value = 'Create two lamps'

    const submission = state.submit()
    await vi.waitFor(() => expect(api.generate).toHaveBeenCalledTimes(2))
    first.resolve({
      job_id: 'job-first', status: 'succeeded',
      result: { images: [{ url: '/plugins/image-generation/api/assets/job-first/result/0', preview_url: '/plugins/image-generation/api/assets/job-first/result/0/preview' }] },
    })

    await vi.waitFor(() => {
      const assistants = state.activeConversation.value?.messages.filter(message => message.role === 'assistant') ?? []
      expect(assistants.some(message => message.images?.[0].src.includes('job-first'))).toBe(true)
      expect(assistants.some(message => message.status === 'pending')).toBe(true)
    })

    second.resolve({
      job_id: 'job-second', status: 'succeeded',
      result: { images: [{ url: '/plugins/image-generation/api/assets/job-second/result/0', preview_url: '/plugins/image-generation/api/assets/job-second/result/0/preview' }] },
    })
    await submission
  })

  it('keeps a successful GPT image when a sibling task fails', async () => {
    const api = createApi()
    api.generate
      .mockResolvedValueOnce({
        job_id: 'job-success', status: 'succeeded',
        result: { images: [{ url: '/plugins/image-generation/api/assets/job-success/result/0', preview_url: '/plugins/image-generation/api/assets/job-success/result/0/preview' }] },
      })
      .mockRejectedValueOnce(new Error('second image failed'))
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.outputCount.value = 2
    state.prompt.value = 'Create two lamps'

    await state.submit()

    const assistants = state.activeConversation.value?.messages.filter(message => message.role === 'assistant') ?? []
    expect(assistants.some(message => message.images?.[0].src.includes('job-success'))).toBe(true)
    expect(assistants.some(message => message.status === 'failed' && message.content.includes('second image failed'))).toBe(true)
  })

  it('cancels every unfinished GPT image task and preserves completed results', async () => {
    const api = createApi({ job_id: 'unused', status: 'pending' })
    api.generate
      .mockResolvedValueOnce({ job_id: 'job-complete', status: 'succeeded', result: { images: [{ url: '/plugins/image-generation/api/assets/job-complete/result/0' }] } })
      .mockResolvedValueOnce({ job_id: 'job-pending-1', status: 'pending' })
      .mockResolvedValueOnce({ job_id: 'job-pending-2', status: 'pending' })
    api.cancel.mockImplementation(async (id: string) => ({ job_id: id, status: 'canceled' as const, error_message: 'stopped' }))
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 60_000 })
    await state.initialize()
    state.outputCount.value = 3
    state.prompt.value = 'Create three lamps'

    await state.submit()
    await state.cancelGeneration()

    expect(api.cancel).toHaveBeenCalledTimes(2)
    expect(api.cancel).toHaveBeenCalledWith('job-pending-1')
    expect(api.cancel).toHaveBeenCalledWith('job-pending-2')
    const assistants = state.activeConversation.value?.messages.filter(message => message.role === 'assistant') ?? []
    expect(assistants.some(message => message.images?.[0].src.includes('job-complete'))).toBe(true)
    expect(assistants.filter(message => message.status === 'canceled')).toHaveLength(2)
  })

  it('applies presets visibly to the prompt and replaces the previous preset text', async () => {
    const state = useImageGeneration({ api: createApi(), loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = '红色夹克角色'

    state.applyPresetSelection({ styles: ['cinematic'], scenes: [], effects: [], angles: ['front', 'back'] })
    expect(state.prompt.value).toContain('风格：电影感')
    expect(state.prompt.value).toContain('角度：正面、背面（分别生成独立图片）')

    state.applyPresetSelection({ styles: ['anime'], scenes: ['night'], effects: [], angles: [] })
    expect(state.prompt.value).toBe('红色夹克角色\n风格：动漫\n场景：夜景')
    expect(state.prompt.value).not.toContain('电影感')
  })

  it('copies the revised prompt back for continued refinement', async () => {
    const state = useImageGeneration({ api: createApi(), loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()

    state.refineFromImage({ id: 'image-1', src: 'data:image/png;base64,abc', revisedPrompt: '蓝色玻璃台灯', createdAt: 'now' })

    expect(state.prompt.value).toBe('蓝色玻璃台灯')
  })

  it('optimizes the current prompt with a model loaded from the selected key', async () => {
    const api = createApi()
    api.listPromptModels.mockResolvedValue({ models: ['gpt-5.1', 'claude-sonnet-4-5'] })
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = 'orange cat'

    await state.optimizePrompt('gpt-5.1')

    expect(api.listPromptModels).toHaveBeenCalledWith(7)
    expect(state.promptOptimizerModels.value).toEqual(['gpt-5.1', 'claude-sonnet-4-5'])
    expect(api.optimizePrompt).toHaveBeenCalledWith(
      { prompt: 'orange cat', api_key_id: 7, model: 'gpt-5.1' },
      expect.any(AbortSignal),
    )
    expect(state.prompt.value).toBe('Optimized prompt')
  })

  it('cancels an in-flight prompt optimization without showing an error', async () => {
    const api = createApi()
    api.optimizePrompt.mockImplementation((_request, signal) => new Promise((_resolve, reject) => {
      signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })
    }))
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = 'orange cat'

    const pending = state.optimizePrompt('gpt-5.1')
    expect(state.optimizingPrompt.value).toBe(true)
    state.cancelPromptOptimization()
    await pending

    expect(state.optimizingPrompt.value).toBe(false)
    expect(state.prompt.value).toBe('orange cat')
    expect(state.errorMessage.value).toBe('')
  })

  it('resubmits the user prompt when retrying a failed assistant message', async () => {
    const api = createApi({ job_id: 'job-1', status: 'succeeded', result: { images: [] } })
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.outputCount.value = 3
    state.prompt.value = '生成一盏台灯'
    await state.submit()
    const failedId = state.activeConversation.value!.messages[1].id
    api.generate.mockResolvedValue(completedResponse())
    state.outputCount.value = 1

    await state.retryMessage(failedId)

    for (const [request] of api.generate.mock.calls.slice(-3)) {
      expect(request).toEqual(expect.objectContaining({
        output_count: 1,
        inputs: expect.objectContaining({ display_prompt: '生成一盏台灯' }),
      }))
    }
    expect(state.activeConversation.value?.messages.at(-1)?.images).toHaveLength(1)
  })
})
