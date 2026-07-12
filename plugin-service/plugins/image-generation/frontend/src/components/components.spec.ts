import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
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

  it('labels reference upload and replacement in Chinese', () => {
    const wrapper = mount(PromptComposer, {
      props: {
        prompt: '', model: 'gpt-image-2', size: '1024x1024', models: ['gpt-image-2'], busy: false,
        reference: { id: 'ref', dataUrl: 'data:image/png;base64,abc', fileName: 'ref.png', mimeType: 'image/png' },
      },
    })

    expect(wrapper.get('[data-testid="reference-upload-label"]').attributes('title')).toBe('再次上传参考图')
    expect(wrapper.get('.clear-reference').attributes('aria-label')).toBe('清除参考图')
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
})
