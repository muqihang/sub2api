<template>
  <div class="zhumeng-agent-setup-panel" data-testid="zhumeng-agent-setup-panel">
    <div class="panel-header">
      <span class="panel-icon">⚡</span>
      <span class="panel-title">{{ t('keys.zhumengAgent.title') }}</span>
    </div>

    <p class="panel-description">{{ t('keys.zhumengAgent.quickDescription') }}</p>

    <!-- Quick setup: reuse current key -->
    <button
      class="quick-setup-btn"
      :disabled="isSetupDisabled || isCreating"
      @click="handleQuickSetup"
      data-testid="quick-setup-btn"
    >
      {{ isCreating ? t('keys.zhumengAgent.creating') : t('keys.zhumengAgent.quickSetup') }}
    </button>

    <!-- Link to the dedicated Codex entry page -->
    <router-link
      :to="{ name: 'CodexEntry' }"
      class="go-to-entry-link"
      data-testid="go-to-codex-entry-link"
    >
      {{ t('keys.zhumengAgent.goToEntryPage') }}
    </router-link>

    <span v-if="isSetupDisabled" class="hint-text" data-testid="key-required-hint">
      {{ t('keys.zhumengAgent.keyIdRequired') }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { createCodexSetupGrant } from '@/api/zhumengAgent'

const { t } = useI18n()

const props = defineProps<{
  apiKeyId: number | null
}>()

const isCreating = ref(false)
const isSetupDisabled = computed(() => props.apiKeyId == null)

async function handleQuickSetup() {
  if (props.apiKeyId == null) return
  isCreating.value = true
  try {
    const result = await createCodexSetupGrant(props.apiKeyId)
    if (result.deeplink) {
      window.open(result.deeplink, '_self')
    }
  } catch (e) {
    // Silently fail; user can use the dedicated page.
  } finally {
    isCreating.value = false
  }
}
</script>
