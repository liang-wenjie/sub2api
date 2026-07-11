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
    result: { images: [{ b64_json: 'abc', revised_prompt: 'A blue lamp' }] },
  }
}

function createApi(generateResult: GenerateResponse = completedResponse()) {
  return {
    getMe: vi.fn().mockResolvedValue({ user_id: 1, role: 'user', email: '', username: '', plugin: 'image-generation' }),
    getConfig: vi.fn().mockResolvedValue({ plugin_key: 'image-generation', history_enabled: true, user_id: 1, role: 'user' }),
    listHistory: vi.fn().mockResolvedValue({ items: [] }),
    generate: vi.fn().mockResolvedValue(generateResult),
    retryHistory: vi.fn(),
    getStatus: vi.fn(),
    cancel: vi.fn(),
    deleteHistory: vi.fn(),
  }
}

describe('useImageGeneration', () => {
  it('replaces the optimistic assistant message with generated images', async () => {
    const api = createApi()
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = 'Create a lamp'

    await state.submit()

    expect(state.activeConversation.value?.messages.map(message => message.role)).toEqual(['user', 'assistant'])
    expect(state.activeConversation.value?.messages[1].images?.[0].src).toBe('data:image/png;base64,abc')
  })

  it('uses the display prompt when the provider omits revised_prompt', async () => {
    const api = createApi({
      job_id: 'job-1',
      status: 'succeeded',
      result: { images: [{ b64_json: 'abc', revised_prompt: '' }] },
    })
    const state = useImageGeneration({ api, loadKeys: async () => [key], pollInterval: 1 })
    await state.initialize()
    state.prompt.value = '一只坐在客厅地毯上的小狗'

    await state.submit()

    expect(state.activeConversation.value?.messages[1].images?.[0].revisedPrompt).toBe('一只坐在客厅地毯上的小狗')
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
      reference_images: [expect.objectContaining({ data_url: image.src })],
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
