import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import ChatThread from './components/ChatThread.vue'
import PromptComposer from './components/PromptComposer.vue'

const generate = vi.fn()

vi.mock('./api/client', () => ({
  pluginApiBase: () => '/plugins/image-generation/api',
  loadImageKeys: vi.fn().mockResolvedValue([]),
  createPluginApi: () => ({
    listConversations: vi.fn().mockResolvedValue({ items: [] }),
    listConversationMessages: vi.fn().mockResolvedValue({ items: [] }),
    generate, retryHistory: vi.fn(), getStatus: vi.fn(), cancel: vi.fn(), deleteConversation: vi.fn(),
  }),
  authenticatedMediaUrl: (value: string) => value,
}))

describe('image generation app sidebar', () => {
  it('collapses immediately from the pinned footer and exposes the expand button', async () => {
    const wrapper = mount(App)
    await wrapper.get('[data-testid="history-inline-collapse"]').trigger('click')

    expect(wrapper.get('.sidebar-wrap').classes()).toContain('collapsed')
    expect(wrapper.get('[data-testid="history-drawer-toggle"]').attributes('aria-label')).toBe('展开侧边栏')
  })
  it('sets a historical message image as the active reference', async () => {
    const wrapper = mount(App)
    const reference = { id: 'history-ref', dataUrl: '/preview.png', originalDataUrl: '/original.png', fileName: 'history.png', mimeType: 'image/png' }

    wrapper.getComponent(ChatThread).vm.$emit('referenceImage', reference)
    await wrapper.vm.$nextTick()

    expect(wrapper.getComponent(PromptComposer).props('reference')).toEqual(reference)
  })

  it('copies a historical creation description into the prompt without submitting', async () => {
    generate.mockClear()
    const wrapper = mount(App)

    wrapper.getComponent(ChatThread).vm.$emit('refinePrompt', 'Improve the old dog prompt')
    await wrapper.vm.$nextTick()

    expect(wrapper.getComponent(PromptComposer).props('prompt')).toBe('Improve the old dog prompt')
    expect(generate).not.toHaveBeenCalled()
  })
})
