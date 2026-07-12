<script setup lang="ts">
import type { ImageReference } from '../types'

const props = defineProps<{
  prompt: string
  model: string
  size: string
  models: string[]
  busy: boolean
  reference?: ImageReference
}>()

const emit = defineEmits<{
  'update:prompt': [value: string]
  'update:model': [value: string]
  'update:size': [value: string]
  submit: []
  stop: []
  reference: [value?: ImageReference]
  referenceFile: [value: File]
}>()

function keydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.ctrlKey || event.metaKey || event.shiftKey) return
  event.preventDefault()
  emit('submit')
}

function readReference(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  emit('referenceFile', file)
  input.value = ''
}

</script>

<template>
  <form class="composer" data-testid="image-chat-composer" @submit.prevent="emit('submit')">
    <div class="composer-main">
      <label class="reference-picker" data-testid="reference-upload-label" :title="reference ? '再次上传参考图' : '上传参考图'">
        <span class="sr-only">{{ reference ? '再次上传参考图' : '上传参考图' }}</span>
        <input data-testid="reference-image-input" type="file" accept="image/png,image/jpeg,image/webp,image/gif,image/bmp,image/tiff" @change="readReference">
        <img v-if="reference" :src="reference.dataUrl" alt="已选择的参考图" data-testid="reference-image-preview">
        <span v-else aria-hidden="true">+</span>
      </label>
      <button v-if="reference" type="button" class="icon-button clear-reference" aria-label="清除参考图" @click="emit('reference')">×</button>
      <textarea
        :value="prompt"
        rows="3"
        data-testid="image-prompt-input"
        placeholder="描述你想生成的图片内容"
        aria-label="图片生成提示词"
        @input="emit('update:prompt', ($event.target as HTMLTextAreaElement).value)"
        @keydown="keydown"
      />
    </div>
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
      <button v-else type="submit" class="send-button" data-testid="image-send-button" aria-label="发送图片提示词" :disabled="!prompt.trim()">
        <span aria-hidden="true">↑</span>
      </button>
    </div>
  </form>
</template>
