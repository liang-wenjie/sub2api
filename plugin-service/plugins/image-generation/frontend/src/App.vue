<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { createPluginApi, loadImageKeys, pluginApiBase } from './api/client'
import ChatThread from './components/ChatThread.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import HistorySidebar from './components/HistorySidebar.vue'
import PromptComposer from './components/PromptComposer.vue'
import { useImageGeneration } from './composables/useImageGeneration'
import type { Conversation, GeneratedImage, ImageReference } from './types'

const state = useImageGeneration({ api: createPluginApi(pluginApiBase()), loadKeys: loadImageKeys })
const loading = ref(true)
const drawerOpen = ref(false)
const sidebarCollapsed = ref(false)
const deleteTarget = ref<Conversation | null>(null)

onMounted(async () => {
  try { await state.initialize() }
  catch (error) { state.errorMessage.value = error instanceof Error ? error.message : String(error) }
  finally { loading.value = false }
})
onBeforeUnmount(state.dispose)

function useAsReference(image: GeneratedImage) {
  const reference: ImageReference = {
    id: `${image.id}-reference`, dataUrl: image.src, fileName: `${image.id}.png`, mimeType: 'image/png',
  }
  state.setReference(reference)
}

async function confirmDelete() {
  if (deleteTarget.value) await state.deleteConversation(deleteTarget.value)
  deleteTarget.value = null
}
</script>

<template>
  <main class="plugin-shell" data-testid="image-workspace">
    <button
      v-if="sidebarCollapsed || !drawerOpen"
      type="button"
      class="drawer-toggle icon-button"
      :class="{ 'collapsed-handle': sidebarCollapsed }"
      data-testid="history-drawer-toggle"
      aria-label="展开历史侧栏"
      :aria-expanded="drawerOpen"
      @click="sidebarCollapsed = false; drawerOpen = true"
    ><span class="drawer-label">侧边栏</span></button>
    <div v-if="drawerOpen" class="drawer-scrim" @click="drawerOpen = false" />
    <div class="sidebar-wrap" :class="{ open: drawerOpen, collapsed: sidebarCollapsed }">
      <button type="button" class="drawer-close icon-button" aria-label="关闭历史侧栏" @click="drawerOpen = false">‹</button>
      <HistorySidebar
        :conversations="state.conversations.value"
        :active-id="state.activeConversationId.value"
        :keys="state.keys.value"
        :selected-key-id="state.selectedKeyId.value"
        @update:selected-key-id="state.selectedKeyId.value = $event"
        @select="state.activeConversationId.value = $event; drawerOpen = false"
        @create="state.createConversation(); drawerOpen = false"
        @delete="deleteTarget = $event"
        @collapse="sidebarCollapsed = true; drawerOpen = false"
      />
    </div>
    <section class="chat-panel" data-testid="image-chat-panel">
      <div v-if="loading" class="loading-state" role="status">Loading...</div>
      <template v-else>
        <ChatThread
          :conversation="state.activeConversation.value"
          @reference="useAsReference"
          @refine="state.refineFromImage"
          @repeat="state.repeatFromImage($event, state.prompt.value)"
          @retry="state.retryMessage"
        />
        <p v-if="state.errorMessage.value" class="inline-error" role="alert">{{ state.errorMessage.value }}</p>
        <PromptComposer
          :prompt="state.prompt.value"
          :model="state.model.value"
          :size="state.size.value"
          :models="state.availableModels.value"
          :busy="state.generationStatus.value !== 'idle'"
          :reference="state.activeConversation.value?.referenceImages[0]"
          @update:prompt="state.prompt.value = $event"
          @update:model="state.model.value = $event"
          @update:size="state.size.value = $event"
          @reference="state.setReference($event)"
          @submit="state.submit()"
          @stop="state.cancelGeneration()"
        />
      </template>
    </section>
    <ConfirmDialog
      :open="Boolean(deleteTarget)"
      title="删除历史记录"
      message="确认删除当前历史记录及其生成图片吗？"
      @cancel="deleteTarget = null"
      @confirm="confirmDelete"
    />
  </main>
</template>
