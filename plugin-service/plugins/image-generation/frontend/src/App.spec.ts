import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import App from './App.vue'

vi.mock('./api/client', () => ({
  pluginApiBase: () => '/plugins/image-generation/api',
  loadImageKeys: vi.fn().mockResolvedValue([]),
  createPluginApi: () => ({
    listConversations: vi.fn().mockResolvedValue({ items: [] }),
    listConversationMessages: vi.fn().mockResolvedValue({ items: [] }),
    generate: vi.fn(), retryHistory: vi.fn(), getStatus: vi.fn(), cancel: vi.fn(), deleteConversation: vi.fn(),
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
})
