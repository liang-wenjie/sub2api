<script setup lang="ts">
import GeneratedImageCard from './GeneratedImageCard.vue'
import type { Conversation, GeneratedImage } from '../types'

defineProps<{ conversation: Conversation | null; loading?: boolean; hasOlder?: boolean }>()
defineEmits<{ reference: [image: GeneratedImage]; refine: [image: GeneratedImage]; repeat: [image: GeneratedImage, prompt: string]; retry: [messageId: string]; view: [src: string, alt: string]; loadOlder: [] }>()

function formatModelLabel(value: string): string {
  return ({ 'gpt-image-2': 'GPT Image 2', 'gpt-image-1': 'GPT Image 1', 'gemini-2.5-flash-image': 'Gemini 2.5 Flash Image' } as Record<string, string>)[value] ?? value
}

function formatSizeLabel(value: string): string {
  return value.replace('x', ' × ')
}
</script>

<template>
  <div class="chat-thread" data-testid="image-chat-thread" aria-live="polite">
    <button v-if="hasOlder" type="button" class="load-older" :disabled="loading" @click="$emit('loadOlder')">{{ loading ? '加载中...' : '加载更早消息' }}</button>
    <div v-if="!conversation?.messages.length" class="empty-state">
      <h1>No conversation yet</h1>
      <p>Send the first prompt and generated images will appear here.</p>
    </div>
    <div v-else class="message-list">
      <article v-for="message in conversation.messages" :key="message.id" class="message" :class="[`message-${message.role}`, { failed: message.status === 'failed' }]">
        <div class="avatar" aria-hidden="true">{{ message.role === 'user' ? 'U' : 'AI' }}</div>
        <div class="message-column">
          <span class="message-role">{{ message.role === 'user' ? 'Prompt' : 'Assistant' }}</span>
          <div class="message-body" :aria-busy="message.status === 'pending'">
          <div v-if="message.referenceImages?.length" class="reference-list">
            <button v-for="reference in message.referenceImages" :key="reference.id" type="button" class="reference-open" aria-label="查看参考图原图" @click="$emit('view', reference.originalDataUrl || reference.dataUrl, reference.fileName)">
            <img
              :src="reference.dataUrl"
              :alt="reference.fileName"
              data-testid="user-message-reference-image"
            ></button>
          </div>
          <div v-if="message.role === 'user'" class="user-message-details">
            <section>
              <h3>创作描述</h3>
              <p>{{ message.content }}</p>
            </section>
            <section v-if="message.requestSettings?.length">
              <h3>生成参数</h3>
              <div class="request-settings">
                <span v-for="(setting, index) in message.requestSettings" :key="`${setting.modelLabel}-${index}`">
                  {{ formatModelLabel(setting.modelLabel) }} | {{ formatSizeLabel(setting.sizeLabel) }} | {{ setting.countLabel.startsWith('数量:') ? setting.countLabel : `数量: ${setting.countLabel}` }}
                </span>
              </div>
            </section>
          </div>
          <p v-else>{{ message.content }}</p>
          <time>{{ message.createdAt }}</time>
          <div
            v-if="message.images?.length"
            class="image-grid"
            :class="{ 'single-image': message.images.length === 1 }"
            data-testid="message-attachments"
          >
            <GeneratedImageCard
              v-for="image in message.images"
              :key="image.id"
              :image="image"
              @reference="$emit('reference', $event)"
              @refine="$emit('refine', $event)"
              @repeat="$emit('repeat', $event, message.content)"
              @view="$emit('view', $event.originalSrc || $event.src, $event.revisedPrompt || '生成图片')"
            />
          </div>
          <button v-if="message.role === 'assistant' && message.status === 'failed'" type="button" class="retry-button" @click="$emit('retry', message.id)">
            重新生成
          </button>
          </div>
        </div>
      </article>
    </div>
  </div>
</template>
