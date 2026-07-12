import { describe, expect, it, vi } from 'vitest'
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

function createApi(generateResult: GenerateResponse = completedResponse()) {
  return {
    uploadReference: vi.fn(),
    listConversations: vi.fn().mockResolvedValue({ items: [] }),
    listConversationMessages: vi.fn().mockResolvedValue({ items: [] }),
    generate: vi.fn().mockResolvedValue(generateResult),
    retryHistory: vi.fn(),
    getStatus: vi.fn(),
    cancel: vi.fn(),
    deleteConversation: vi.fn(),
  }
}

describe('useImageGeneration', () => {
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

    await state.uploadReference(new File(['png-bytes'], 'reference.png', { type: 'image/png' }))
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
      request: { model: 'gpt-image-2', size: '1024x1024' }, result: { images: [{ url: '/original.png', preview_url: '/preview.jpg' }] },
      created_at: '2026-07-11T09:59:00Z', updated_at: '2026-07-11T10:00:00Z',
    }] })
    const state = useImageGeneration({ api, loadKeys: async () => [key] })

    await state.initialize()

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
    api.getStatus.mockResolvedValue({ id: 'history-1', status: 'pending' } as HistoryRecord)
    api.cancel.mockResolvedValue({ id: 'history-1', status: 'canceled', error_message: 'stopped' } as HistoryRecord)
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
    state.prompt.value = 'Create a lamp'
    await state.submit()
    const image = state.activeConversation.value!.messages[1].images![0]

    await state.repeatFromImage(image, 'Try another')

    expect(api.generate).toHaveBeenLastCalledWith(expect.objectContaining({
      reference_images: [expect.objectContaining({ data_url: image.originalSrc })],
    }))
  })

  it('copies the revised prompt back for continued refinement', async () => {
    const state = useImageGeneration({ api: createApi(), loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()

    state.refineFromImage({ id: 'image-1', src: 'data:image/png;base64,abc', revisedPrompt: '蓝色玻璃台灯', createdAt: 'now' })

    expect(state.prompt.value).toBe('蓝色玻璃台灯')
  })

  it('resubmits the user prompt when retrying a failed assistant message', async () => {
    const api = createApi({ job_id: 'job-1', status: 'succeeded', result: { images: [] } })
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = '生成一盏台灯'
    await state.submit()
    const failedId = state.activeConversation.value!.messages[1].id
    api.generate.mockResolvedValue(completedResponse())

    await state.retryMessage(failedId)

    expect(api.generate).toHaveBeenLastCalledWith(expect.objectContaining({
      inputs: expect.objectContaining({ display_prompt: '生成一盏台灯' }),
    }))
    expect(state.activeConversation.value?.messages.at(-1)?.images).toHaveLength(1)
  })
})
