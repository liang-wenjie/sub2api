<script setup lang="ts">
import type { GeneratedImage } from '../types'

defineProps<{ image: GeneratedImage }>()
defineEmits<{ reference: [image: GeneratedImage]; refine: [image: GeneratedImage]; repeat: [image: GeneratedImage] }>()
</script>

<template>
  <figure class="generated-image">
    <img :src="image.src" :alt="image.revisedPrompt || 'Generated image'">
    <figcaption v-if="image.revisedPrompt">{{ image.revisedPrompt }}</figcaption>
    <div class="image-actions">
      <button type="button" @click="$emit('reference', image)">设为参考图</button>
      <button type="button" @click="$emit('refine', image)">基于此图继续优化</button>
      <button type="button" @click="$emit('repeat', image)">再次生成</button>
    </div>
  </figure>
</template>
