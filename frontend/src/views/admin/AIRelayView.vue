<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap-reverse items-start justify-between gap-3">
          <div class="flex flex-wrap items-center gap-3">
            <SearchInput v-model="search" placeholder="Search configurations" class="w-full sm:w-64" @search="reload" />
            <Select v-model="platform" class="w-40" :options="platformOptions" />
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" class="btn btn-secondary btn-icon" :disabled="loading" title="Refresh" @click="reload">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreate">Add configuration</button>
          </div>
        </div>
      </template>

      <template #table>
        <div v-if="selectedRoutes.length" class="flex items-center justify-between border-b border-primary-100 bg-primary-50 px-4 py-3 text-sm dark:border-primary-900/40 dark:bg-primary-900/20">
          <span class="font-medium text-primary-700 dark:text-primary-300">{{ selectedRoutes.length }} selected</span>
          <div class="flex items-center gap-2">
            <button type="button" class="btn btn-ghost btn-sm" @click="clearSelection">Clear selection</button>
            <button type="button" class="btn btn-danger btn-sm" @click="openBatchDelete">Delete selected</button>
          </div>
        </div>
        <DataTable
          :columns="columns"
          :data="routes"
          :loading="loading"
          row-key="key"
          :selectable="true"
          :selected-keys="selectedKeys"
          @update:selected-keys="updateSelection"
        >
          <template #cell-name="{ row }"><span class="font-medium text-gray-900 dark:text-white">{{ row.name || row.slug }}</span></template>
          <template #cell-platform="{ value }"><span class="text-gray-600 dark:text-gray-300">{{ value }}</span></template>
          <template #cell-base_url="{ value }"><span class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ value }}</span></template>
          <template #cell-plugin_url="{ row }"><span class="font-mono text-xs text-gray-500 dark:text-gray-400">{{ relayURL(row) }}</span></template>
          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button type="button" class="btn btn-ghost btn-icon h-8 w-8 p-0" title="Copy URL" @click="copyURL(row)"><Icon name="copy" size="sm" /></button>
              <button type="button" class="btn btn-ghost btn-icon h-8 w-8 p-0" title="Edit" @click="openEdit(row)"><Icon name="edit" size="sm" /></button>
              <button type="button" class="btn btn-ghost btn-icon h-8 w-8 p-0 text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-900/20" title="Delete" @click="openDelete([row])"><Icon name="trash" size="sm" /></button>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="changePage" @update:pageSize="changePageSize" />
      </template>
    </TablePageLayout>

    <BaseDialog :show="showEditor" :title="editing ? 'Edit configuration' : 'Add configuration'" width="normal" @close="showEditor = false">
      <form id="relay-route-form" class="grid gap-4 sm:grid-cols-2" @submit.prevent="saveRoute">
        <label class="input-label">Name<input v-model.trim="form.name" class="input" required maxlength="80" /></label>
        <label class="input-label">Platform<Select v-model="form.platform" :disabled="!!editing" :options="platformOptions.slice(1)" /></label>
        <label class="input-label">Slug<input v-model.trim="form.slug" class="input" required :disabled="!!editing" pattern="[a-z0-9][a-z0-9_-]{0,63}" /></label>
        <label class="input-label">Target Base URL<input v-model.trim="form.base_url" class="input" type="url" required /></label>
        <div class="sm:col-span-2">
          <div class="mb-2 flex items-center justify-between gap-3">
            <div>
              <p class="input-label">Path mappings</p>
              <p class="text-sm text-gray-500 dark:text-gray-400">Replace matching upstream paths while keeping the target host.</p>
            </div>
            <button type="button" class="btn btn-secondary btn-sm shrink-0" data-testid="path-mapping-add" @click="addPathMapping">
              <Icon name="plus" size="sm" />
              Add mapping
            </button>
          </div>
          <div v-if="pathMappingRows.length" class="space-y-2">
            <div v-for="(mapping, index) in pathMappingRows" :key="mapping.id" class="grid grid-cols-1 items-center gap-2 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto]">
              <label class="sr-only" :for="`path-mapping-source-${mapping.id}`">Source path</label>
              <input
                :id="`path-mapping-source-${mapping.id}`"
                v-model="mapping.source"
                class="input font-mono text-sm"
                placeholder="responses/compact"
                data-testid="path-mapping-source"
              />
              <Icon name="arrowRight" size="sm" class="hidden text-gray-400 sm:block" aria-hidden="true" />
              <label class="sr-only" :for="`path-mapping-target-${mapping.id}`">Target path</label>
              <input
                :id="`path-mapping-target-${mapping.id}`"
                v-model="mapping.target"
                class="input font-mono text-sm"
                placeholder="api/paas/v4/chat/completions"
                data-testid="path-mapping-target"
              />
              <button
                type="button"
                class="btn btn-ghost btn-icon size-9 justify-self-end p-0 text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-900/20"
                aria-label="Remove path mapping"
                data-testid="path-mapping-remove"
                @click="removePathMapping(index)"
              >
                <Icon name="trash" size="sm" aria-hidden="true" />
              </button>
            </div>
          </div>
        </div>
        <p v-if="formError" class="sm:col-span-2 text-sm text-red-600 dark:text-red-400">{{ formError }}</p>
      </form>
      <template #footer><button type="button" class="btn btn-secondary" @click="showEditor = false">Cancel</button><button form="relay-route-form" type="submit" class="btn btn-primary">Save configuration</button></template>
    </BaseDialog>

    <ConfirmDialog :show="showDeleteDialog" title="Delete configuration?" :message="deleteMessage" confirm-text="Delete" cancel-text="Cancel" :danger="true" @confirm="confirmDelete" @cancel="showDeleteDialog = false" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useAppStore } from '@/stores/app'
import apiClient, { buildGatewayUrl } from '@/api/client'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

interface RelayRoute { key: string; platform: string; slug: string; name: string; base_url: string; path_mappings: Record<string, string> }
interface Platform { key: string; display_name: string }
interface Page { page: number; page_size: number; total: number; total_pages: number }
interface MappingRow { id: number; source: string; target: string }

const appStore = useAppStore()
const pluginAPIBase = buildGatewayUrl('/plugins/ai-relay/api')
const loading = ref(false)
const routes = ref<RelayRoute[]>([])
const platforms = ref<Platform[]>([])
const search = ref('')
const platform = ref('')
const pagination = reactive<Page>({ page: 1, page_size: 20, total: 0, total_pages: 1 })
const selectedKeys = ref<Array<string | number>>([])
const selectedRouteMap = ref(new Map<string, RelayRoute>())
const showEditor = ref(false)
const showDeleteDialog = ref(false)
const editing = ref<RelayRoute | null>(null)
const deleting = ref<RelayRoute[]>([])
const formError = ref('')
const form = reactive({ name: '', platform: '', slug: '', base_url: 'https://apihub.agnes-ai.com/v1' })
const pathMappingRows = ref<MappingRow[]>([])
let nextMappingID = 1

// Agnes remains selectable while the optional platform discovery endpoint is unavailable.
const fallbackPlatforms: Platform[] = [{ key: 'agnes', display_name: 'Agnes' }]
const availablePlatforms = computed(() => platforms.value.length > 0 ? platforms.value : fallbackPlatforms)
const platformOptions = computed(() => [{ value: '', label: 'All platforms' }, ...availablePlatforms.value.map(item => ({ value: item.key, label: item.display_name }))])
const columns = [
  { key: 'name', label: 'Name', sortable: false },
  { key: 'platform', label: 'Platform', sortable: false },
  { key: 'base_url', label: 'Target URL', sortable: false },
  { key: 'plugin_url', label: 'Plugin URL', sortable: false },
  { key: 'actions', label: 'Actions', sortable: false }
]
const selectedRoutes = computed(() => Array.from(selectedRouteMap.value.values()))
const deleteMessage = computed(() => deleting.value.length === 1 ? `Delete ${deleting.value[0].name || deleting.value[0].slug}?` : `Delete ${deleting.value.length} selected configurations?`)

function normalizeRoute(route: Omit<RelayRoute, 'key'>): RelayRoute { return { ...route, path_mappings: route.path_mappings || {}, key: `${route.platform}:${route.slug}` } }
function relayURL(route: RelayRoute) { return `${window.location.protocol}//plugin-server:8091/plugins/ai-relay/${route.platform}/${route.slug}` }
function clearSelection() { selectedKeys.value = []; selectedRouteMap.value = new Map() }
function updateSelection(keys: Array<string | number>) {
  const next = new Map(selectedRouteMap.value)
  const currentKeys = new Set(keys.map(String))
  routes.value.forEach(route => { if (currentKeys.has(route.key)) next.set(route.key, route); else next.delete(route.key) })
  selectedKeys.value = Array.from(next.keys())
  selectedRouteMap.value = next
}
async function loadPlatforms() {
  try {
    const { data } = await apiClient.get<{ items: Platform[] }>(`${pluginAPIBase}/platforms`)
    platforms.value = Array.isArray(data.items) ? data.items : []
  } catch {
    // Do not block route management or show a failure toast for optional discovery.
    platforms.value = []
  }
}
async function reload() {
  loading.value = true
  try {
    const { data } = await apiClient.get<{ items: Array<Omit<RelayRoute, 'key'>>; pagination: Page }>(`${pluginAPIBase}/routes`, { params: { page: pagination.page, page_size: pagination.page_size, platform: platform.value || undefined, search: search.value || undefined } })
    routes.value = (data.items || []).map(normalizeRoute)
    Object.assign(pagination, data.pagination)
  } catch (error: any) {
    appStore.showError(error?.message || 'Failed to load configurations')
  } finally { loading.value = false }
}
function changePage(page: number) { pagination.page = page; reload() }
function changePageSize(pageSize: number) { pagination.page = 1; pagination.page_size = pageSize; reload() }
function canonicalPath(value: string) {
  const trimmed = value.trim().replace(/^\/+|\/+$/g, '')
  return trimmed.startsWith('v1/') ? trimmed.slice(3) : trimmed
}
function mappingRowsFromRecord(record: Record<string, string>): MappingRow[] {
  return Object.entries(record || {}).map(([source, target]) => ({ id: nextMappingID++, source, target }))
}
function mappingRecordFromRows(rows: MappingRow[]): Record<string, string> {
  const mappings: Record<string, string> = {}
  rows.forEach(({ source, target }) => {
    const normalizedSource = canonicalPath(source)
    const normalizedTarget = target.trim().replace(/^\/+|\/+$/g, '')
    if (normalizedSource && normalizedTarget) mappings[normalizedSource] = normalizedTarget
  })
  return mappings
}
function addPathMapping() { pathMappingRows.value.push({ id: nextMappingID++, source: '', target: '' }) }
function removePathMapping(index: number) { pathMappingRows.value.splice(index, 1) }
function openCreate() { editing.value = null; formError.value = ''; pathMappingRows.value = []; Object.assign(form, { name: '', platform: availablePlatforms.value[0]?.key || '', slug: Date.now().toString(), base_url: 'https://apihub.agnes-ai.com/v1' }); showEditor.value = true }
function openEdit(route: RelayRoute) { editing.value = route; formError.value = ''; pathMappingRows.value = mappingRowsFromRecord(route.path_mappings); Object.assign(form, route); showEditor.value = true }
async function saveRoute() {
  formError.value = ''
  try {
    const path = editing.value ? `${pluginAPIBase}/routes/${editing.value.platform}/${editing.value.slug}` : `${pluginAPIBase}/routes`
    await apiClient.request({ method: editing.value ? 'put' : 'post', url: path, data: { ...form, slug: form.slug.toLowerCase(), path_mappings: mappingRecordFromRows(pathMappingRows.value) } })
    showEditor.value = false; pagination.page = 1; await reload(); appStore.showSuccess('Configuration saved')
  } catch (error: any) { formError.value = error?.message || 'Failed to save configuration' }
}
function openDelete(routesToDelete: RelayRoute[]) { deleting.value = routesToDelete; showDeleteDialog.value = true }
function openBatchDelete() { openDelete(selectedRoutes.value) }
async function confirmDelete() {
  try { await apiClient.delete(`${pluginAPIBase}/routes`, { data: { items: deleting.value.map(({ platform, slug }) => ({ platform, slug })) } }); clearSelection(); showDeleteDialog.value = false; pagination.page = 1; await reload(); appStore.showSuccess('Configuration deleted') }
  catch (error: any) { appStore.showError(error?.message || 'Failed to delete configuration') }
}
async function copyURL(route: RelayRoute) { await navigator.clipboard.writeText(relayURL(route)); appStore.showSuccess('Plugin URL copied') }
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(search, () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => { pagination.page = 1; reload() }, 180) })
watch(platform, () => { pagination.page = 1; reload() })
onMounted(async () => { await Promise.all([loadPlatforms(), reload()]) })
</script>
