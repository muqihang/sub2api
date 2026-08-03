<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useCodexEntryStore } from '@/stores/codexEntry'
import type { CodexDeviceDTO } from '@/types'
import StatusPill from './StatusPill.vue'

const { t } = useI18n()
const store = useCodexEntryStore()
const appStore = useAppStore()

const props = defineProps<{
  device: CodexDeviceDTO
}>()

const statusMeta = computed(() => {
  switch (props.device.device_state) {
    case 'healthy':
      return {
        tone: 'ok',
        title: t('codex.console.deviceHealthyTitle'),
        description: t('codex.console.deviceHealthyBody'),
        label: t('codex.console.stateHealthy'),
      }
    case 'catalog_stale':
      return {
        tone: 'warn',
        title: t('codex.console.deviceCatalogStaleTitle'),
        description: t('codex.console.deviceCatalogStaleBody'),
        label: t('codex.console.stateCatalogStale'),
      }
    case 'device_offline':
      return {
        tone: 'warn',
        title: t('codex.console.deviceOfflineTitle'),
        description: t('codex.console.deviceOfflineBody'),
        label: t('codex.console.stateOffline'),
      }
    case 'credential_revoked':
      return {
        tone: 'fail',
        title: t('codex.console.deviceCredentialRevokedTitle'),
        description: t('codex.console.deviceCredentialRevokedBody'),
        label: t('codex.console.stateAttention'),
      }
    default:
      return {
        tone: 'fail',
        title: t('codex.console.deviceClientOutdatedTitle'),
        description: t('codex.console.deviceClientOutdatedBody'),
        label: t('codex.console.stateAttention'),
      }
  }
})

function openUpgradeDocs() {
  const docsUrl = appStore.cachedPublicSettings?.doc_url || appStore.docUrl
  const upgradeUrl = docsUrl ? `${docsUrl.replace(/\/$/, '')}/codex` : 'https://docs.example.com'
  window.open(upgradeUrl, '_blank')
}
</script>

<template>
  <div class="status-bar" :class="`tone-${statusMeta.tone}`" data-testid="status-bar">
    <div class="status-left">
      <div class="status-title-row">
        <div>
          <p class="status-caption">{{ device.device_name }}</p>
          <h3>{{ statusMeta.title }}</h3>
        </div>
        <StatusPill :status="statusMeta.tone as any" :label="statusMeta.label" />
      </div>
      <p class="status-description" data-testid="status-bar-state">{{ statusMeta.description }}</p>
    </div>

    <div class="status-actions">
      <template v-if="device.device_state === 'catalog_stale'">
        <button class="btn btn-primary" @click="store.resyncDevice(device.device_id)" data-testid="status-bar-resync-btn">
          {{ t('codex.console.resync') }}
        </button>
        <button class="btn btn-secondary" @click="store.diagnoseDevice(device.device_id)" data-testid="status-bar-diagnose-btn">
          {{ t('codex.console.diagnose') }}
        </button>
      </template>

      <template v-else-if="device.device_state === 'device_offline'">
        <button class="btn btn-primary" @click="store.repairDevice(device.device_id)" data-testid="status-bar-repair-btn">
          {{ t('codex.console.repair') }}
        </button>
        <button class="btn btn-secondary" @click="store.diagnoseDevice(device.device_id)" data-testid="status-bar-diagnose-btn">
          {{ t('codex.console.diagnose') }}
        </button>
        <button class="btn btn-danger" @click="store.removeDevice(device.device_id)" data-testid="status-bar-remove-btn">
          {{ t('codex.console.removeDevice') }}
        </button>
      </template>

      <template v-else-if="device.device_state === 'credential_revoked'">
        <button class="btn btn-primary" @click="store.reAttachDevice(device.device_id)" data-testid="status-bar-reattach-btn">
          {{ t('codex.console.reattach') }}
        </button>
        <button class="btn btn-secondary" @click="store.removeDevice(device.device_id)" data-testid="status-bar-remove-btn">
          {{ t('codex.console.removeDeviceOnly') }}
        </button>
      </template>

      <template v-else-if="device.device_state === 'client_outdated'">
        <button class="btn btn-primary" @click="openUpgradeDocs" data-testid="status-bar-upgrade-btn">
          {{ t('codex.console.upgradeClient') }}
        </button>
        <button class="btn btn-secondary" @click="store.diagnoseDevice(device.device_id)" data-testid="status-bar-diagnose-btn">
          {{ t('codex.console.diagnose') }}
        </button>
      </template>

      <template v-else>
        <button class="btn btn-secondary" @click="store.resyncDevice(device.device_id)" data-testid="status-bar-resync-btn">
          {{ t('codex.console.resync') }}
        </button>
        <button class="btn btn-secondary" @click="store.diagnoseDevice(device.device_id)" data-testid="status-bar-diagnose-btn">
          {{ t('codex.console.diagnose') }}
        </button>
      </template>
    </div>
  </div>
</template>

<style scoped>
.status-bar {
  margin-top: 18px;
  padding: 18px;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 16px;
  align-items: center;
}

.status-bar.tone-ok {
  background: #ecfdf5;
  border-color: #bbf7d0;
}

.status-bar.tone-warn {
  background: #fffbeb;
  border-color: #fde68a;
}

.status-bar.tone-fail {
  background: #fef2f2;
  border-color: #fecaca;
}

.status-title-row {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: flex-start;
}

.status-caption {
  margin: 0 0 4px;
  font-size: 12px;
  color: #6b7280;
}

.status-title-row h3 {
  margin: 0;
  font-size: 17px;
}

.status-description {
  margin: 8px 0 0;
  color: #4b5563;
  line-height: 1.65;
}

.status-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 14px;
  border-radius: 10px;
  border: 1px solid transparent;
  cursor: pointer;
  font-size: 14px;
}

.btn-primary { background: #111827; color: #fff; }
.btn-secondary { background: #fff; border-color: #d1d5db; color: #111827; }
.btn-danger { background: #fff; border-color: #fecaca; color: #b91c1c; }

@media (max-width: 960px) {
  .status-bar {
    grid-template-columns: 1fr;
  }

  .status-actions {
    justify-content: flex-start;
  }
}
</style>
