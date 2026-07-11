import type {
  ChatMessage,
  Conversation,
  GeneratedImage,
  GeneratedImagePayload,
  HistoryRecord,
  ReferenceImageRequest,
} from '../types'
import { authenticatedMediaUrl } from '../api/client'

type DateFormatter = (value: string) => string

function requestReferences(record: HistoryRecord) {
  const rawReferences = record.request.reference_images
  const references = Array.isArray(rawReferences) ? rawReferences as ReferenceImageRequest[] : []
  return references.flatMap((reference, index) => {
    const source = reference.storage_key
      ? `/plugins/image-generation/api/assets/${record.id}/reference/${index}`
      : reference.data_url || reference.remote_url || ''
    if (!source) return []
    return [{
      id: `${record.id}-reference-${index}`,
	      dataUrl: authenticatedMediaUrl(source),
      fileName: reference.name || `reference-${index + 1}.png`,
      mimeType: reference.mime_type || 'image/png',
    }]
  })
}

function imageSource(image: GeneratedImagePayload): string {
	if (image.url) return authenticatedMediaUrl(image.url)
  return image.b64_json ? `data:image/png;base64,${image.b64_json}` : ''
}

function resultImages(record: HistoryRecord, formatDate: DateFormatter): GeneratedImage[] {
  const displayPrompt = typeof record.request.display_prompt === 'string' ? record.request.display_prompt : record.prompt
  const resultPrompt = record.result?.revised_prompt || displayPrompt
  return (record.result?.images ?? []).map((image, index) => ({
    id: `${record.id}-image-${index}`,
    src: imageSource(image),
    revisedPrompt: image.revised_prompt || resultPrompt,
    createdAt: formatDate(record.updated_at),
  })).filter(image => image.src.length > 0)
}

function recordMessages(record: HistoryRecord, formatDate: DateFormatter): ChatMessage[] {
  const request = record.request
  const user: ChatMessage = {
    id: `${record.id}-user`,
    role: 'user',
    content: record.prompt,
    createdAt: formatDate(record.created_at),
    referenceImages: requestReferences(record),
    requestSettings: [{
      modelLabel: String(request.model ?? ''),
      sizeLabel: String(request.size ?? ''),
      countLabel: '数量: 1',
    }],
  }
  if (record.status === 'pending') {
    return [user, {
      id: `${record.id}-assistant`,
      role: 'assistant',
      content: '正在生成图片，请稍候...',
      createdAt: formatDate(record.updated_at),
      status: 'pending',
    }]
  }
  if (record.status === 'failed' || record.status === 'canceled') {
    return [user, {
      id: `${record.id}-assistant`,
      role: 'assistant',
      content: record.error_message || (record.status === 'canceled' ? '生成已取消' : '图片生成失败'),
      createdAt: formatDate(record.updated_at),
      status: record.status,
    }]
  }
  const images = resultImages(record, formatDate)
  if (images.length === 0) return [user]
  return [user, {
    id: `${record.id}-assistant`,
    role: 'assistant',
    content: images[0].revisedPrompt || record.prompt,
    createdAt: formatDate(record.updated_at),
    images,
  }]
}

export function projectHistory(
  records: HistoryRecord[],
  formatDate: DateFormatter = value => new Date(value).toLocaleString(),
): Conversation[] {
  const grouped = new Map<string, HistoryRecord[]>()
  for (const record of records) {
    const requestConversation = record.request.conversation_id
    const conversationId = record.conversation_id || (typeof requestConversation === 'string' ? requestConversation : '') || record.id
    const group = grouped.get(conversationId) ?? []
    group.push(record)
    grouped.set(conversationId, group)
  }

  return Array.from(grouped.entries()).map(([conversationId, group]) => {
    const chronological = [...group].sort((left, right) => Date.parse(left.created_at) - Date.parse(right.created_at))
    const latest = chronological[chronological.length - 1]
    const messages = chronological.flatMap(record => recordMessages(record, formatDate))
    const lastAssistant = [...messages].reverse().find(message => message.role === 'assistant')
    const latestReferences = [...messages].reverse().find(message => message.role === 'user' && message.referenceImages?.length)?.referenceImages ?? []
    return {
      id: `history-remote-${chronological.map(record => record.id).join(',')}`,
      conversationId,
      title: latest.prompt || '历史记录',
      preview: lastAssistant?.content || latest.prompt,
      lastUsedAt: formatDate(latest.updated_at),
      messages,
      referenceImages: latestReferences,
      historyIds: chronological.map(record => record.id),
    }
  }).sort((left, right) => Date.parse(right.lastUsedAt) - Date.parse(left.lastUsedAt))
}
