<script setup lang="ts">
import type { ImageReference } from '../types'

const props = withDefaults(defineProps<{
  prompt: string
  model: string
  size: string
  models: string[]
  busy: boolean
  references?: ImageReference[]
  maxReferenceImages?: number
  referenceLimitExceeded?: boolean
}>(), {
  references: () => [],
  maxReferenceImages: 1,
  referenceLimitExceeded: false,
})

const emit = defineEmits<{
  'update:prompt': [value: string]
  'update:model': [value: string]
  'update:size': [value: string]
  submit: []
  stop: []
  referenceFiles: [value: File[]]
  removeReference: [id: string]
}>()

function keydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.ctrlKey || event.metaKey || event.shiftKey) return
  event.preventDefault()
  emit('submit')
}

function readReference(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  if (!files.length) return
  emit('referenceFiles', files)
  input.value = ''
}

</script>

<template>
  <form class="composer" data-testid="image-chat-composer" @submit.prevent="emit('submit')">
    <div class="composer-main">
      <div v-if="references.length" class="selected-reference-list" aria-label="已选择的参考图">
        <div v-for="reference in references" :key="reference.id" class="selected-reference-item">
          <img :src="reference.dataUrl" :alt="reference.fileName" data-testid="reference-image-preview">
          <button
            type="button"
            class="remove-reference"
            data-testid="remove-reference-image"
            :aria-label="`移除参考图 ${reference.fileName}`"
            @click="emit('removeReference', reference.id)"
          >×</button>
        </div>
      </div>
      <label v-if="references.length < maxReferenceImages" class="reference-picker" data-testid="reference-upload-label" title="上传参考图">
        <span class="sr-only">上传参考图</span>
        <input data-testid="reference-image-input" type="file" multiple accept="image/png,image/jpeg,image/webp,image/gif,image/bmp,image/tiff" @change="readReference">
        <span aria-hidden="true">+</span>
      </label>
      <textarea
        :value="prompt"
        rows="3"
        data-testid="image-prompt-input"
        placeholder="描述你想生成的图片内容"
        aria-label="图片生成提示词"
        :aria-describedby="referenceLimitExceeded ? 'reference-limit-error' : undefined"
        :aria-invalid="referenceLimitExceeded || undefined"
        @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
        @keydown="keydown"
      />
    </div>
    <p v-if="references.length" class="reference-count" aria-live="polite">
      已选择 {{ references.length }} 张，当前模型最多支持 {{ maxReferenceImages }} 张参考图
    </p>
    <p v-if="referenceLimitExceeded" id="reference-limit-error" class="reference-limit-error" data-testid="reference-limit-error" role="alert">
      当前模型最多支持 {{ maxReferenceImages }} 张参考图，请删除多余图片或切换模型。
    </p>
    <div class="composer-tools">
      <label>
        <span class="sr-only">模型</span>
        <select :value="model" data-testid="image-model-select" @change="emit('update:model', ($event.target as HTMLSelectElement).value)">
          <option v-for="item in models" :key="item" :value="item">{{ item }}</option>
        </select>
      </label>
      <label>
        <span class="sr-only">尺寸</span>
        <select :value="size" @change="emit('update:size', ($event.target as HTMLSelectElement).value)">
          <option value="1024x1024">1024 × 1024</option>
          <option value="1536x1024">1536 × 1024</option>
          <option value="1024x1536">1024 × 1536</option>
        </select>
      </label>
      <button v-if="busy" type="button" class="send-button stop-button" aria-label="停止生成" @click="emit('stop')">
        <span aria-hidden="true" class="stop-icon" />
      </button>
      <button v-else type="submit" class="send-button" data-testid="image-send-button" :aria-label="referenceLimitExceeded ? `无法发送：当前模型最多支持 ${maxReferenceImages} 张参考图` : '发送图片提示词'" :disabled="!prompt.trim() || referenceLimitExceeded">
        <span aria-hidden="true">↑</span>
      </button>
    </div>
  </form>
</template>
