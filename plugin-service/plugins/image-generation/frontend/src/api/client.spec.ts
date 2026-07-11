import { describe, expect, it, vi } from 'vitest'
import { createPluginApi } from './client'
import type { GenerateRequest } from '../types'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('plugin api client', () => {
  it('submits generation through the configured plugin base', async () => {
    const fetchSpy = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ job_id: 'job-1', status: 'pending' }, 201))
    const client = createPluginApi('/plugins/image-generation/api/', fetchSpy)
    const request = { prompt: 'lamp', model: 'gpt-image-2' } as GenerateRequest

    await client.generate(request)

    expect(fetchSpy).toHaveBeenCalledWith(
      '/plugins/image-generation/api/generate',
      expect.objectContaining({
        method: 'POST',
        credentials: 'same-origin',
        body: JSON.stringify(request),
      }),
    )
  })

  it('exposes history task actions', async () => {
    const fetchSpy = vi.fn<typeof fetch>().mockImplementation(async () => jsonResponse({ id: 'history-1', status: 'pending' }))
    const client = createPluginApi('/plugins/image-generation/api', fetchSpy)

    await client.getStatus('history-1')
    await client.cancel('history-1')
    await client.deleteHistory('history-1')

    expect(fetchSpy).toHaveBeenNthCalledWith(1, '/plugins/image-generation/api/history/history-1/status', expect.objectContaining({ credentials: 'same-origin' }))
    expect(fetchSpy).toHaveBeenNthCalledWith(2, '/plugins/image-generation/api/history/history-1/cancel', expect.objectContaining({ method: 'POST' }))
    expect(fetchSpy).toHaveBeenNthCalledWith(3, '/plugins/image-generation/api/history/history-1', expect.objectContaining({ method: 'DELETE' }))
  })

  it('uses a structured API error message', async () => {
    const fetchSpy = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ error: 'provider unavailable' }, 502))
    const client = createPluginApi('/plugins/image-generation/api', fetchSpy)

    await expect(client.getConfig()).rejects.toThrow('provider unavailable')
  })
})
