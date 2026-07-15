<script setup lang="ts">
import type { GeneratedImage } from '../types'

defineProps<{ image: GeneratedImage }>()
defineEmits<{ reference: [image: GeneratedImage]; refine: [image: GeneratedImage]; repeat: [image: GeneratedImage]; view: [image: GeneratedImage] }>()

function fallbackToOriginal(event: Event, image: GeneratedImage) {
  const target = event.target as HTMLImageElement
  if (image.originalSrc && target.src !== image.originalSrc) target.src = image.originalSrc
}
</script>

<template>
  <figure class="generated-image">
    <button type="button" class="generated-image-open" aria-label="查看原图" @click="$emit('view', image)">
      <img :src="image.src" :alt="image.revisedPrompt || 'Generated image'" @error="fallbackToOriginal($event, image)">
    </button>
    <figcaption v-if="image.variantLabel || image.revisedPrompt">{{ image.variantLabel || image.revisedPrompt }}</figcaption>
    <div class="image-actions">
      <button type="button" @click="$emit('reference', image)">设为参考图</button>
      <button type="button" @click="$emit('refine', image)">优化提示词</button>
      <button type="button" @click="$emit('repeat', image)">再次生成</button>
      <button type="button" @click="$emit('view', image)">查看原图</button>
    </div>
  </figure>
</template>
