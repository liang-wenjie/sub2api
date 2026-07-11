import { describe, expect, it } from 'vitest'
import { projectHistory } from './history'
import type { HistoryRecord } from '../types'

const baseRecord: HistoryRecord = {
  id: 'history-1',
  conversation_id: 'conversation-1',
  user_id: 1,
  prompt: 'Create a lamp',
  status: 'succeeded',
  request: { model: 'gpt-image-2', size: '1024x1024' },
  result: { images: [{ b64_json: 'abc', revised_prompt: 'A brass lamp' }] },
  created_at: '2026-07-11T00:00:00Z',
  updated_at: '2026-07-11T00:01:00Z',
}

describe('projectHistory', () => {
  it('restores persisted reference images after reload', () => {
    const conversations = projectHistory([{
      id: 'history-ref',
      conversation_id: 'conversation-ref',
      user_id: 7,
      user_email: 'user@example.com',
      plugin_key: 'image-generation',
      prompt: 'restyle this',
      status: 'succeeded',
      request: {
        model: 'gpt-image-1',
        size: '1024x1024',
        reference_images: [{ name: 'source.png', mime_type: 'image/png', storage_key: 'stored-reference' }],
      },
      result: { images: [{ url: '/plugins/image-generation/api/assets/history-ref/result/0' }] },
      created_at: '2026-07-11T10:00:00Z',
      updated_at: '2026-07-11T10:01:00Z',
    }], value => value)

    const reference = conversations[0].messages[0].referenceImages?.[0]
    expect(reference?.dataUrl).toBe('/plugins/image-generation/api/assets/history-ref/reference/0')
    expect(reference?.fileName).toBe('source.png')
    expect(conversations[0].referenceImages[0]).toEqual(reference)
  })
  it('groups records by conversation and keeps messages chronological', () => {
    const second = { ...baseRecord, id: 'history-2', prompt: 'Make it blue', created_at: '2026-07-11T00:02:00Z' }
    const conversations = projectHistory([second, baseRecord], value => value)

    expect(conversations).toHaveLength(1)
    expect(conversations[0].conversationId).toBe('conversation-1')
    expect(conversations[0].messages.filter(message => message.role === 'user').map(message => message.content)).toEqual([
      'Create a lamp',
      'Make it blue',
    ])
  })

  it('projects a failed record into user and failed assistant messages', () => {
    const failed: HistoryRecord = { ...baseRecord, status: 'failed', result: undefined, error_message: 'quota exceeded' }
    const conversations = projectHistory([failed], value => value)

    expect(conversations[0].messages.map(message => message.status)).toEqual([undefined, 'failed'])
    expect(conversations[0].messages[1].content).toContain('quota exceeded')
  })

  it('projects pending records without inventing a completed image', () => {
    const pending: HistoryRecord = { ...baseRecord, status: 'pending', result: undefined }
    const conversations = projectHistory([pending], value => value)

    expect(conversations[0].messages[1].status).toBe('pending')
    expect(conversations[0].messages[1].images).toBeUndefined()
  })

  it('falls back to the stored display prompt when revised_prompt is empty', () => {
    const record: HistoryRecord = {
      ...baseRecord,
      request: { ...baseRecord.request, display_prompt: '一只小狗' },
      result: { images: [{ b64_json: 'abc', revised_prompt: '' }] },
    }

    const conversations = projectHistory([record], value => value)

    expect(conversations[0].messages[1].images?.[0].revisedPrompt).toBe('一只小狗')
  })
})
