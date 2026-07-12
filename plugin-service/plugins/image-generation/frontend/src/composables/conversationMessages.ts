import { authenticatedMediaUrl } from '../api/client'
import type { ChatMessage, GeneratedImage, GeneratedImagePayload, HistoryRecord, ReferenceImageRequest } from '../types'

function references(record: HistoryRecord) {
  const raw = record.request.reference_images
  const items = Array.isArray(raw) ? raw as ReferenceImageRequest[] : []
  return items.flatMap((reference, index) => {
    const original = reference.storage_key
      ? `/plugins/image-generation/api/assets/${record.id}/reference/${index}`
      : reference.data_url || reference.remote_url || ''
    if (!original) return []
    return [{
      id: `${record.id}-reference-${index}`,
      dataUrl: authenticatedMediaUrl(reference.preview_url || original),
      originalDataUrl: authenticatedMediaUrl(original),
      fileName: reference.name || `reference-${index + 1}.png`,
      mimeType: reference.mime_type || 'image/png',
    }]
  })
}

function source(image: GeneratedImagePayload): string {
  return image.url ? authenticatedMediaUrl(image.url) : ''
}

function images(record: HistoryRecord): GeneratedImage[] {
  const resultPrompt = record.result?.revised_prompt || record.prompt
  return (record.result?.images ?? []).map((image, index) => ({
    id: `${record.id}-image-${index}`,
    src: authenticatedMediaUrl(image.preview_url || source(image)),
    originalSrc: source(image),
    revisedPrompt: image.revised_prompt || resultPrompt,
    createdAt: new Date(record.updated_at).toLocaleString(),
  })).filter(image => image.src)
}

export function projectConversationMessages(records: HistoryRecord[]): ChatMessage[] {
  return [...records]
    .sort((left, right) => Date.parse(left.created_at) - Date.parse(right.created_at))
    .flatMap((record) => {
      const user: ChatMessage = {
        id: `${record.id}-user`, role: 'user', content: record.prompt,
        createdAt: new Date(record.created_at).toLocaleString(), referenceImages: references(record),
        requestSettings: [{ modelLabel: String(record.request.model ?? ''), sizeLabel: String(record.request.size ?? ''), countLabel: '数量: 1' }],
      }
      if (record.status === 'pending') return [user, { id: `${record.id}-assistant`, role: 'assistant', content: '正在生成图片，请稍候...', createdAt: new Date(record.updated_at).toLocaleString(), status: 'pending' } as ChatMessage]
      if (record.status === 'failed' || record.status === 'canceled') return [user, { id: `${record.id}-assistant`, role: 'assistant', content: record.error_message || (record.status === 'canceled' ? '生成已取消' : '图片生成失败'), createdAt: new Date(record.updated_at).toLocaleString(), status: record.status } as ChatMessage]
      const generated = images(record)
      return generated.length ? [user, { id: `${record.id}-assistant`, role: 'assistant', content: '生成结果', createdAt: new Date(record.updated_at).toLocaleString(), images: generated } as ChatMessage] : [user]
    })
}
