<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useCodexEntryStore } from '@/stores/codexEntry'
import type { CodexDeviceDTO } from '@/types'

const { t } = useI18n()
const store = useCodexEntryStore()

defineProps<{
  device: CodexDeviceDTO
}>()
</script>

<template>
  <section class="console-card troubleshoot-card" data-testid="troubleshoot-card">
    <div class="card-head">
      <div>
        <h3>{{ t('codex.console.troubleshoot') }}</h3>
        <p class="card-sub">{{ t('codex.console.troubleshootDescription') }}</p>
      </div>
    </div>

    <div class="troubleshoot-actions">
      <button
        class="btn btn-primary"
        @click="store.repairDevice(device.device_id)"
        data-testid="troubleshoot-repair-btn"
      >
        {{ t('codex.console.repair') }}
      </button>
      <button
        class="btn btn-secondary"
        @click="store.diagnoseDevice(device.device_id)"
        data-testid="troubleshoot-diagnose-btn"
      >
        {{ t('codex.console.diagnose') }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.console-card {
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  background: #fff;
  padding: 16px;
}

.card-head h3 { margin: 0; }
.card-sub { margin: 6px 0 0; color: #6b7280; font-size: 13px; }
.troubleshoot-actions { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 12px; }
.btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 10px 14px; border-radius: 10px; border: 1px solid transparent; cursor: pointer; font-size: 14px; }
.btn-primary { background: #111827; color: #fff; }
.btn-secondary { background: #fff; border-color: #d1d5db; color: #111827; }
</style>
