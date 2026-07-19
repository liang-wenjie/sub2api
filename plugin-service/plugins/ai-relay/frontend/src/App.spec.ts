import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import type { RelayApi } from './api'

function fakeApi(overrides: Partial<RelayApi> = {}): RelayApi {
  return {
    listPlatforms: vi.fn().mockResolvedValue([{ key: 'agnes', display_name: 'Agnes' }]),
    listRoutes: vi.fn().mockResolvedValue({ items: [], pagination: { page: 1, page_size: 20, total: 0, total_pages: 1 } }),
    createRoute: vi.fn().mockResolvedValue({}),
    updateRoute: vi.fn().mockResolvedValue({}),
    deleteRoutes: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

describe('AI Relay plugin application', () => {
  it('adds and removes path mapping rows in the create dialog', async () => {
    const wrapper = mount(App, { props: { api: fakeApi() } })
    await flushPromises()
    await wrapper.get('[data-testid="route-add"]').trigger('click')
    await wrapper.get('[data-testid="path-mapping-add"]').trigger('click')
    expect(wrapper.findAll('[data-testid="path-mapping-source"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="path-mapping-remove"]').attributes('aria-label')).toBe('Remove path mapping')
    await wrapper.get('[data-testid="path-mapping-remove"]').trigger('click')
    expect(wrapper.findAll('[data-testid="path-mapping-source"]')).toHaveLength(0)
  })

  it('submits normalized mappings and omits blank rows', async () => {
    const createRoute = vi.fn().mockResolvedValue({})
    const wrapper = mount(App, { props: { api: fakeApi({ createRoute }) } })
    await flushPromises()
    await wrapper.get('[data-testid="route-add"]').trigger('click')
    await wrapper.get('input[required]').setValue('Zhipu')
    await wrapper.findAll<HTMLInputElement>('input[required]')[1].setValue('zhipu')
    await wrapper.findAll<HTMLInputElement>('input[required]')[2].setValue('https://open.bigmodel.cn/v1')
    await wrapper.get('[data-testid="path-mapping-add"]').trigger('click')
    await wrapper.get('[data-testid="path-mapping-add"]').trigger('click')
    await wrapper.findAll<HTMLInputElement>('[data-testid="path-mapping-source"]')[0].setValue('/v1/responses/compact/')
    await wrapper.findAll<HTMLInputElement>('[data-testid="path-mapping-target"]')[0].setValue('/api/paas/v4/chat/completions/')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(createRoute).toHaveBeenCalledWith(expect.objectContaining({
      path_mappings: { 'responses/compact': 'api/paas/v4/chat/completions' },
    }))
  })

  it('shows an API error without hiding the route management surface', async () => {
    const wrapper = mount(App, { props: { api: fakeApi({ listRoutes: vi.fn().mockRejectedValue(new Error('offline')) }) } })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('offline')
    expect(wrapper.find('[data-testid="route-add"]').exists()).toBe(true)
  })
})
