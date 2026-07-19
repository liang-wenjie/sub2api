import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AIRelayView from '../AIRelayView.vue'

const { get, request, showError, showSuccess } = vi.hoisted(() => ({
  get: vi.fn(),
  request: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  default: { get, request, delete: vi.fn() },
  buildGatewayUrl: (path: string) => path,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

const SlotStub = defineComponent({ template: '<div><slot /><slot name="filters" /><slot name="table" /><slot name="pagination" /><slot name="footer" /></div>' })
const DialogStub = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})
const SelectStub = defineComponent({
  inheritAttrs: false,
  props: { modelValue: String, options: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option value="agnes">Agnes</option></select>',
})
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: '<div><slot v-if="data.length" name="cell-actions" :row="data[0]" /></div>',
})

function mountView() {
  return mount(AIRelayView, {
    global: {
      stubs: {
        AppLayout: SlotStub,
        TablePageLayout: SlotStub,
        DataTable: DataTableStub,
        Pagination: true,
        SearchInput: true,
        Select: SelectStub,
        BaseDialog: DialogStub,
        ConfirmDialog: true,
        Icon: true,
      },
    },
  })
}

describe('AI Relay administration view', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    get.mockImplementation((url: string) => {
      if (url.endsWith('/platforms')) return Promise.resolve({ data: { items: [{ key: 'agnes', display_name: 'Agnes' }] } })
      return Promise.resolve({ data: { items: [], pagination: { page: 1, page_size: 20, total: 0, total_pages: 1 } } })
    })
    request.mockResolvedValue({ data: {} })
  })

  it('adds and removes accessible path mapping rows', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('button.btn-primary').trigger('click')

    expect(wrapper.findAll('[data-testid="path-mapping-source"]')).toHaveLength(0)
    await wrapper.get('[data-testid="path-mapping-add"]').trigger('click')
    expect(wrapper.findAll('[data-testid="path-mapping-source"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="path-mapping-remove"]').attributes('aria-label')).toBe('Remove path mapping')

    await wrapper.get('[data-testid="path-mapping-remove"]').trigger('click')
    expect(wrapper.findAll('[data-testid="path-mapping-source"]')).toHaveLength(0)
  })

  it('normalizes path mappings in the create payload and omits blank rows', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('button.btn-primary').trigger('click')
    await wrapper.get('[data-testid="path-mapping-add"]').trigger('click')
    await wrapper.get('[data-testid="path-mapping-add"]').trigger('click')

    const sources = wrapper.findAll<HTMLInputElement>('[data-testid="path-mapping-source"]')
    const targets = wrapper.findAll<HTMLInputElement>('[data-testid="path-mapping-target"]')
    await sources[0].setValue('/v1/responses/compact/')
    await targets[0].setValue('/api/paas/v4/chat/completions/')
    await wrapper.get('#relay-route-form').trigger('submit')
    await flushPromises()

    expect(request).toHaveBeenCalledWith(expect.objectContaining({
      method: 'post',
      data: expect.objectContaining({
        path_mappings: { 'responses/compact': 'api/paas/v4/chat/completions' },
      }),
    }))
  })

  it('loads existing path mappings into the edit form', async () => {
    get.mockImplementation((url: string) => {
      if (url.endsWith('/platforms')) return Promise.resolve({ data: { items: [{ key: 'agnes', display_name: 'Agnes' }] } })
      return Promise.resolve({
        data: {
          items: [{
            platform: 'agnes', slug: 'zhipu', name: 'Zhipu', base_url: 'https://open.bigmodel.cn/v1',
            path_mappings: { 'responses/compact': 'api/paas/v4/chat/completions' },
          }],
          pagination: { page: 1, page_size: 20, total: 1, total_pages: 1 },
        },
      })
    })
    const wrapper = mountView()
    await flushPromises()
    const editButton = wrapper.findAll('button').find((button) => button.attributes('title') === 'Edit')
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')

    expect(wrapper.get<HTMLInputElement>('[data-testid="path-mapping-source"]').element.value).toBe('responses/compact')
    expect(wrapper.get<HTMLInputElement>('[data-testid="path-mapping-target"]').element.value).toBe('api/paas/v4/chat/completions')
  })
})
