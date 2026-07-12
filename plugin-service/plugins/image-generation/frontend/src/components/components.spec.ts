import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import HistorySidebar from './HistorySidebar.vue'
import PromptComposer from './PromptComposer.vue'
import GeneratedImageCard from './GeneratedImageCard.vue'
import ChatThread from './ChatThread.vue'
import ImagePreviewDialog from './ImagePreviewDialog.vue'
import SidebarToggleButton from './SidebarToggleButton.vue'
import type { Conversation, ImageApiKey } from '../types'

const conversation: Conversation = {
  id: 'conversation-1',
  title: 'Lamp',
  preview: 'A blue lamp',
  lastUsedAt: 'now',
  messages: [],
  referenceImages: [],
  historyIds: ['history-1'],
}

const key: ImageApiKey = { id: 1, key: 'sk', name: 'Key', status: 'active', group: { allow_image_generation: true } }

describe('image generation components', () => {
  it('opens an original image dialog with an original download link', async () => {
    const wrapper = mount(ImagePreviewDialog, {
      props: { open: true, src: '/plugins/image-generation/api/assets/h1/result/0?token=test', alt: 'result' },
    })

    expect(wrapper.get('[role="dialog"]').attributes('aria-label')).toBe('查看原图')
    expect(wrapper.get('a[download]').attributes('href')).toContain('download=1')
    await wrapper.get('button[aria-label="关闭原图"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('moves focus into the image dialog and restores it after closing', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()
    const wrapper = mount(ImagePreviewDialog, {
      attachTo: document.body,
      props: { open: false, src: '/image.png', alt: 'result' },
    })

    await wrapper.setProps({ open: true })
    await nextTick()
    expect(document.activeElement).toBe(wrapper.get('[data-testid="image-preview-close"]').element)
    await wrapper.setProps({ open: false })
    await nextTick()
    expect(document.activeElement).toBe(trigger)

    wrapper.unmount()
    trigger.remove()
  })

  it('emits history selection and deletion from native buttons', async () => {
    const wrapper = mount(HistorySidebar, {
      props: { conversations: [conversation], activeId: '', keys: [key], selectedKeyId: 1 },
    })

    await wrapper.get('[data-testid="history-select"]').trigger('click')
    await wrapper.get('[data-testid="history-delete-button"]').trigger('click')

    expect(wrapper.emitted('select')?.[0]).toEqual([conversation.id])
    expect(wrapper.emitted('delete')?.[0]).toEqual([conversation])
  })

  it('submits on Enter and keeps Ctrl+Enter for a newline', async () => {
    const wrapper = mount(PromptComposer, {
      props: {
        prompt: 'lamp',
        model: 'gpt-image-2',
        size: '1024x1024',
        models: ['gpt-image-2'],
        busy: false,
      },
    })
    const textarea = wrapper.get('textarea')

    await textarea.trigger('keydown', { key: 'Enter' })
    await textarea.trigger('keydown', { key: 'Enter', ctrlKey: true })

    expect(wrapper.emitted('submit')).toHaveLength(1)
  })

  it('provides accessible names for icon-only composer controls', () => {
    const wrapper = mount(PromptComposer, {
      props: {
        prompt: '',
        model: 'gpt-image-2',
        size: '1024x1024',
        models: ['gpt-image-2'],
        busy: false,
      },
    })

    expect(wrapper.get('[data-testid="image-send-button"]').attributes('aria-label')).toBeTruthy()
  })

  it('renders the original Chinese generation actions', () => {
    const wrapper = mount(GeneratedImageCard, {
      props: { image: { id: 'image-1', src: 'data:image/png;base64,abc', revisedPrompt: '蓝色台灯', createdAt: 'now' } },
    })

    expect(wrapper.text()).toContain('设为参考图')
    expect(wrapper.text()).toContain('优化提示词')
    expect(wrapper.text()).toContain('再次生成')
  })

  it('renders Chinese sidebar controls and emits collapse from the pinned footer', async () => {
    const wrapper = mount(HistorySidebar, {
      props: { conversations: [conversation], activeId: conversation.id, keys: [key], selectedKeyId: 1 },
    })

    expect(wrapper.get('[data-testid="history-inline-collapse"]').attributes('aria-label')).toBe('收起侧边栏')
    expect(wrapper.get('[data-testid="history-inline-collapse"]').classes()).toContain('sidebar-toggle-button')
    expect(wrapper.get('[data-testid="history-inline-collapse"]').text()).toBe('收起侧边栏')
    expect(wrapper.get('[data-testid="sidebar-footer"]').get('[data-testid="history-inline-collapse"]').attributes('aria-label')).toBe('收起侧边栏')
    await wrapper.get('[data-testid="history-inline-collapse"]').trigger('click')
    expect(wrapper.emitted('collapse')).toHaveLength(1)
    expect(wrapper.get('[data-testid="history-delete-button"]').text()).toBe('删除')
    expect(wrapper.text()).toContain('新建会话')
  })

  it('reuses the main sidebar control for expansion', async () => {
    const wrapper = mount(SidebarToggleButton, { props: { direction: 'expand' } })

    expect(wrapper.get('button').attributes('aria-label')).toBe('展开侧边栏')
    expect(wrapper.get('button').text()).toBe('展开侧边栏')
    expect(wrapper.get('[data-testid="sidebar-toggle-icon"]').classes()).toContain('sidebar-toggle-icon-expand')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })

  it('supports multiple references, exposes overflow validation, and removes one image', async () => {
    const references = [
      { id: 'first', dataUrl: 'data:image/png;base64,first', fileName: 'first.png', mimeType: 'image/png' },
      { id: 'second', dataUrl: 'data:image/png;base64,second', fileName: 'second.png', mimeType: 'image/png' },
    ]
    const wrapper = mount(PromptComposer, {
      props: {
        prompt: 'Use both', model: 'custom-image-model', size: '1024x1024', models: ['custom-image-model'], busy: false,
        references, maxReferenceImages: 1, referenceLimitExceeded: true,
      },
    })

    expect(wrapper.findAll('[data-testid="reference-image-preview"]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="reference-limit-error"]').text()).toContain('1')
    expect(wrapper.get('[data-testid="image-send-button"]').attributes()).toHaveProperty('disabled')
    expect(wrapper.get('[data-testid="image-prompt-input"]').attributes('aria-describedby')).toBe('reference-limit-error')

    await wrapper.findAll('[data-testid="remove-reference-image"]')[1].trigger('click')
    expect(wrapper.emitted('removeReference')?.[0]).toEqual(['second'])

    await wrapper.setProps({ maxReferenceImages: 3, referenceLimitExceeded: false })
    const input = wrapper.get<HTMLInputElement>('[data-testid="reference-image-input"]')
    expect(input.attributes()).toHaveProperty('multiple')
    const files = [
      new File(['third'], 'third.png', { type: 'image/png' }),
      new File(['fourth'], 'fourth.png', { type: 'image/png' }),
    ]
    Object.defineProperty(input.element, 'files', { configurable: true, value: files })
    await input.trigger('change')
    expect(wrapper.emitted('referenceFiles')?.[0]).toEqual([files])
  })

  it('renders sent user messages with reference, description, and generation parameters', () => {
    const wrapper = mount(ChatThread, {
      props: {
        conversation: {
          ...conversation,
          messages: [{
            id: 'user-1',
            role: 'user',
            content: '生成一只小狗',
            createdAt: 'now',
            referenceImages: [{ id: 'ref-1', dataUrl: 'data:image/png;base64,abc', fileName: 'dog.png', mimeType: 'image/png' }],
            requestSettings: [{ modelLabel: 'gpt-image-2', sizeLabel: '1024x1024', countLabel: '1' }],
          }],
        },
      },
    })

    expect(wrapper.get('[data-testid="user-message-reference-image"]').attributes('src')).toContain('data:image/png')
    expect(wrapper.text()).toContain('创作描述')
    expect(wrapper.text()).toContain('生成一只小狗')
    expect(wrapper.text()).toContain('生成参数')
    expect(wrapper.text()).toContain('Prompt')
    expect(wrapper.text()).toContain('GPT Image 2 | 1024 × 1024 | 数量: 1')
  })
  it('emits a historical reference image from the action beside its thumbnail', async () => {
    const reference = { id: 'ref-1', dataUrl: '/preview.png', originalDataUrl: '/original.png', fileName: 'dog.png', mimeType: 'image/png' }
    const wrapper = mount(ChatThread, {
      props: {
        conversation: {
          ...conversation,
          messages: [{ id: 'user-1', role: 'user', content: 'dog', createdAt: 'now', referenceImages: [reference] }],
        },
      },
    })

    await wrapper.get('[data-testid="history-reference-action"]').trigger('click')
    await wrapper.get('[data-testid="history-refine-action"]').trigger('click')

    expect(wrapper.emitted('referenceImage')?.[0]).toEqual([reference])
    expect(wrapper.emitted('refinePrompt')?.[0]).toEqual(['dog'])
    expect(wrapper.get('[data-testid="history-reference-item"]').text()).toContain('设为参考图')
    expect(wrapper.get('[data-testid="history-reference-item"]').text()).toContain('优化提示词')
  })

  it('loads older messages automatically near the top without rendering a button', async () => {
    const wrapper = mount(ChatThread, { props: { conversation, hasOlder: true, loading: false } })
    const thread = wrapper.get('[data-testid="image-chat-thread"]')

    expect(wrapper.find('.load-older').exists()).toBe(false)
    Object.defineProperty(thread.element, 'scrollTop', { value: 48, writable: true })
    Object.defineProperty(thread.element, 'scrollHeight', { value: 600, configurable: true })
    await thread.trigger('scroll')

    expect(wrapper.emitted('loadOlder')).toHaveLength(1)
  })

  it('does not load older messages outside the threshold or while unavailable', async () => {
    const wrapper = mount(ChatThread, { props: { conversation, hasOlder: true, loading: false } })
    const thread = wrapper.get('[data-testid="image-chat-thread"]')
    Object.defineProperty(thread.element, 'scrollTop', { value: 49, writable: true })
    Object.defineProperty(thread.element, 'scrollHeight', { value: 600, configurable: true })

    await thread.trigger('scroll')
    await wrapper.setProps({ loading: true })
    thread.element.scrollTop = 0
    await thread.trigger('scroll')
    await wrapper.setProps({ loading: false, hasOlder: false })
    await thread.trigger('scroll')

    expect(wrapper.emitted('loadOlder')).toBeUndefined()
  })

  it('preserves the visible message position after older messages are prepended', async () => {
    const wrapper = mount(ChatThread, { props: { conversation, hasOlder: true, loading: false } })
    const thread = wrapper.get('[data-testid="image-chat-thread"]')
    let scrollHeight = 600
    Object.defineProperty(thread.element, 'scrollTop', { value: 32, writable: true })
    Object.defineProperty(thread.element, 'scrollHeight', { get: () => scrollHeight })

    await thread.trigger('scroll')
    await wrapper.setProps({ loading: true })
    scrollHeight = 840
    await wrapper.setProps({ loading: false })
    await nextTick()

    expect(thread.element.scrollTop).toBe(272)
  })

  it('positions a newly selected conversation at its latest message after loading', async () => {
    const wrapper = mount(ChatThread, { props: { conversation, hasOlder: false, loading: false } })
    const thread = wrapper.get('[data-testid="image-chat-thread"]')
    Object.defineProperty(thread.element, 'scrollTop', { value: 24, writable: true })
    Object.defineProperty(thread.element, 'scrollHeight', { value: 900, configurable: true })
    const selectedConversation = { ...conversation, id: 'conversation-2' }

    await wrapper.setProps({ conversation: selectedConversation, loading: true })
    await wrapper.setProps({ loading: false })
    await nextTick()

    expect(thread.element.scrollTop).toBe(900)
  })

  it('does not force the current conversation to the bottom when its messages change', async () => {
    const wrapper = mount(ChatThread, { props: { conversation, hasOlder: false, loading: false } })
    const thread = wrapper.get('[data-testid="image-chat-thread"]')
    Object.defineProperty(thread.element, 'scrollTop', { value: 120, writable: true })
    Object.defineProperty(thread.element, 'scrollHeight', { value: 900, configurable: true })
    await nextTick()
    thread.element.scrollTop = 120

    await wrapper.setProps({
      conversation: {
        ...conversation,
        messages: [{ id: 'new-message', role: 'assistant', content: 'new result', createdAt: 'now' }],
      },
    })
    await nextTick()

    expect(thread.element.scrollTop).toBe(120)
  })
})
