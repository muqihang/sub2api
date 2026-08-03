<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useCodexEntryStore } from '@/stores/codexEntry'
import type { CodexDeviceDTO } from '@/types'
import StatusPill from './StatusPill.vue'

const { t } = useI18n()
const store = useCodexEntryStore()

defineProps<{
  devices: CodexDeviceDTO[]
}>()

function stateTone(state: CodexDeviceDTO['device_state']) {
  switch (state) {
    case 'healthy':
      return 'ok'
    case 'catalog_stale':
    case 'device_offline':
      return 'warn'
    case 'credential_revoked':
    case 'client_outdated':
      return 'fail'
    default:
      return 'pending'
  }
}
</script>

<template>
  <section class="console-card device-list-card" data-testid="device-list-card">
    <div class="card-head">
      <div>
        <h3>{{ t('codex.console.devices') }}</h3>
        <p class="card-sub">{{ t('codex.console.devicesDescription') }}</p>
      </div>
    </div>

    <div v-if="devices.length > 0" class="device-list">
      <article
        v-for="device in devices"
        :key="device.device_id"
        class="device-row"
        :data-testid="`device-row-${device.device_id}`"
      >
        <div class="device-info">
          <div class="device-text">
            <strong class="device-name">{{ device.device_name }}</strong>
            <span class="device-meta">{{ t(`codex.console.deviceStateLabels.${device.device_state}`) }}</span>
          </div>
          <StatusPill :status="stateTone(device.device_state) as any" :label="t(`codex.console.deviceStateLabels.${device.device_state}`)" />
        </div>
        <div class="device-actions">
          <button
            v-if="device.attachment_mode === 'independent_credential' && device.device_state !== 'credential_revoked'"
            class="btn btn-danger"
            @click="store.revokeAttachment(device.device_id)"
            data-testid="device-revoke-btn"
          >
            {{ t('codex.console.revokeCredential') }}
          </button>
          <button
            v-if="device.attachment_mode === 'reused_key'"
            class="btn btn-secondary"
            @click="store.removeDevice(device.device_id)"
            data-testid="device-disconnect-btn"
          >
            {{ t('codex.console.disconnectDevice') }}
          </button>
        </div>
      </article>
    </div>

    <p v-else class="empty-state no-devices">
      {{ t('codex.console.noDevices') }}
    </p>
  </section>
</template>

<style scoped>
.console-card {
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  background: #fff;
  padding: 16px;
}

.card-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.card-head h3 {
  margin: 0;
}

.card-sub {
  margin: 6px 0 0;
  color: #6b7280;
  font-size: 13px;
}

.device-list {
  display: grid;
  gap: 10px;
}

.device-row {
  border: 1px solid #eef0f3;
  border-radius: 10px;
  padding: 14px;
  display: grid;
  gap: 12px;
}

.device-info {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.device-name {
  display: block;
  font-size: 14px;
}

.device-meta {
  display: block;
  margin-top: 5px;
  color: #6b7280;
  font-size: 12px;
}

.device-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.empty-state {
  margin: 0;
  padding: 12px;
  border-radius: 10px;
  background: #f9fafb;
  color: #6b7280;
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

.btn-secondary { background: #fff; border-color: #d1d5db; color: #111827; }
.btn-danger { background: #fff; border-color: #fecaca; color: #b91c1c; }

@media (max-width: 720px) {
  .device-info {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
