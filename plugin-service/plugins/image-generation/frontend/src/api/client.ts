import type {
  GenerateRequest,
  GenerateResponse,
  HistoryList,
  HistoryRecord,
  PluginConfig,
  Principal,
  ImageApiKey,
} from '../types'

export interface PluginApi {
  getMe(): Promise<Principal>
  getConfig(): Promise<PluginConfig>
  listHistory(): Promise<HistoryList>
  generate(request: GenerateRequest): Promise<GenerateResponse>
  retryHistory(id: string): Promise<GenerateResponse>
  getStatus(id: string): Promise<HistoryRecord>
  cancel(id: string): Promise<HistoryRecord>
  deleteHistory(id: string): Promise<void>
}

async function readResponse<T>(response: Response): Promise<T> {
  const contentType = response.headers.get('content-type') ?? ''
  const body: unknown = contentType.includes('application/json')
    ? await response.json()
    : await response.text()
  if (!response.ok) {
    if (typeof body === 'object' && body !== null) {
      const record = body as Record<string, unknown>
      throw new Error(String(record.error ?? record.message ?? response.status))
    }
    throw new Error(String(body || response.status))
  }
  return body as T
}

export function createPluginApi(base: string, fetcher: typeof fetch = window.fetch.bind(window)): PluginApi {
  const apiBase = base.replace(/\/+$/, '')
  const request = async <T>(path: string, init: RequestInit = {}): Promise<T> => {
    const response = await fetcher(`${apiBase}${path}`, {
      credentials: 'same-origin',
      ...init,
      headers: { ...init.headers },
    })
    if (response.status === 204) return undefined as T
    return readResponse<T>(response)
  }
  const historyPath = (id: string, action = '') => `/history/${encodeURIComponent(id)}${action}`

  return {
    getMe: () => request('/me'),
    getConfig: () => request('/config'),
    listHistory: () => request('/history'),
    generate: payload => request('/generate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    }),
    retryHistory: id => request(historyPath(id, '/retry'), { method: 'POST' }),
    getStatus: id => request(historyPath(id, '/status')),
    cancel: id => request(historyPath(id, '/cancel'), { method: 'POST' }),
    deleteHistory: id => request(historyPath(id), { method: 'DELETE' }),
  }
}

export function pluginApiBase(): string {
  const root = document.getElementById('app')
  return root?.dataset.pluginApiBase?.replace(/\/+$/, '') || '/plugins/image-generation/api'
}

export function authenticatedMediaUrl(rawUrl: string): string {
  if (!rawUrl || rawUrl.startsWith('data:') || rawUrl.startsWith('blob:')) return rawUrl
  const params = new URLSearchParams(window.location.search)
  let token = params.get('token') || params.get('session') || ''
  try { token ||= window.localStorage.getItem('auth_token') || '' } catch { token = token || '' }
  if (!token) return rawUrl
  const url = new URL(rawUrl, window.location.origin)
  if (!url.searchParams.has('token') && !url.searchParams.has('session')) url.searchParams.set('token', token)
  return url.origin === window.location.origin ? `${url.pathname}${url.search}${url.hash}` : url.toString()
}

export async function loadImageKeys(fetcher: typeof fetch = window.fetch.bind(window)): Promise<ImageApiKey[]> {
  let token = ''
  try { token = window.localStorage.getItem('auth_token') || '' } catch { token = '' }
  const response = await fetcher('/api/v1/keys?page=1&page_size=100', {
    credentials: 'same-origin',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  const payload = await readResponse<unknown>(response)
  if (typeof payload !== 'object' || payload === null) return []
  const record = payload as Record<string, unknown>
  const data = record.code === 0 && typeof record.data === 'object' && record.data !== null
    ? record.data as Record<string, unknown>
    : record
  return Array.isArray(data.items) ? data.items as ImageApiKey[] : []
}
