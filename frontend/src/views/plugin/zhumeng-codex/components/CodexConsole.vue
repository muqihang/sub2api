<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCodexEntryStore } from '@/stores/codexEntry'
import StatusBar from './StatusBar.vue'
import DeviceListCard from './DeviceListCard.vue'
import TroubleshootCard from './TroubleshootCard.vue'
import DiagnoseDialog from './DiagnoseDialog.vue'
import ModelCatalogCard from './ModelCatalogCard.vue'
import BillingCard from './BillingCard.vue'
import StatusPill from './StatusPill.vue'

const { t } = useI18n()
const store = useCodexEntryStore()

const focusStatePill = computed(() => {
  const state = store.focusDevice?.device_state
  if (state === 'healthy') return { status: 'ok' as const, label: t('codex.console.stateHealthy') }
  if (state === 'catalog_stale') return { status: 'warn' as const, label: t('codex.console.stateCatalogStale') }
  if (state === 'credential_revoked' || state === 'client_outdated') return { status: 'fail' as const, label: t('codex.console.stateAttention') }
  if (state === 'device_offline') return { status: 'warn' as const, label: t('codex.console.stateOffline') }
  return { status: 'pending' as const, label: t('codex.console.statePending') }
})
</script>

<template>
  <div class="codex-console" data-testid="codex-console">
    <div class="console-hero" data-testid="console-hero">
      <div>
        <h2>{{ t('codex.console.title') }}</h2>
        <p>{{ t('codex.console.description', { count: store.connectedDeviceCount }) }}</p>
      </div>
      <div class="hero-actions">
        <StatusPill :status="focusStatePill.status" :label="focusStatePill.label" />
        <button
          class="btn btn-primary"
          @click="store.startSetup()"
          data-testid="hero-add-device-btn"
        >
          {{ t('codex.console.addDevice') }}
        </button>
      </div>
    </div>

    <div
      v-if="store.setupSessionPresentation === 'console_banner' && store.setupSession"
      class="console-banner"
      data-testid="console-setup-banner"
    >
      <div>
        <p class="banner-label">{{ t('codex.console.bannerLabel') }}</p>
        <strong>{{ t('codex.console.newDeviceInProgress') }}</strong>
        <p>{{ t('codex.console.bannerDescription') }}</p>
      </div>
      <div class="banner-actions">
        <button class="btn btn-primary" @click="store.openLocal()" data-testid="banner-open-local-btn">
          {{ t('codex.console.openLocal') }}
        </button>
        <button class="btn btn-secondary" @click="store.copyCli()" data-testid="banner-copy-cli-btn">
          {{ t('codex.console.copyCli') }}
        </button>
      </div>
    </div>

    <StatusBar
      v-if="store.focusDevice"
      :device="store.focusDevice"
      data-testid="console-status-bar"
    />

    <div class="console-grid">
      <div class="console-main-column">
        <DeviceListCard
          :devices="store.devices"
          data-testid="console-device-list"
        />

        <TroubleshootCard
          v-if="store.focusDevice && store.focusDevice.device_state === 'device_offline'"
          :device="store.focusDevice"
          data-testid="console-troubleshoot"
        />
      </div>

      <div class="console-side-column">
        <ModelCatalogCard data-testid="model-catalog-card" />
        <BillingCard data-testid="billing-card" />
      </div>
    </div>

    <DiagnoseDialog
      v-if="store.lastDiagnose"
      :report="store.lastDiagnose"
      @close="store.lastDiagnose = null"
      data-testid="diagnose-dialog"
    />
  </div>
</template>

<style scoped>
.codex-console {
  padding: 26px;
}

.console-hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 20px;
  align-items: end;
  padding-bottom: 18px;
  border-bottom: 1px solid #eef0f3;
}

.console-hero h2 {
  margin: 0;
  font-size: 24px;
}

.console-hero p {
  margin: 8px 0 0;
  color: #4b5563;
  line-height: 1.7;
}

.hero-actions {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.console-banner,
.console-grid > * > :deep(.console-card) {
  border-radius: 12px;
}

.console-banner {
  margin-top: 18px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 16px;
  align-items: center;
  padding: 16px 18px;
  border: 1px solid #c7d2fe;
  background: #eef2ff;
}

.banner-label {
  margin: 0 0 4px;
  font-size: 12px;
  color: #4338ca;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.console-banner strong {
  display: block;
  font-size: 16px;
  color: #111827;
}

.console-banner p:last-child {
  margin: 6px 0 0;
  color: #4b5563;
}

.banner-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.console-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(300px, 1fr);
  gap: 16px;
  margin-top: 16px;
}

.console-main-column,
.console-side-column {
  display: grid;
  gap: 16px;
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

.btn-primary {
  background: #111827;
  color: #fff;
}

.btn-secondary {
  background: #fff;
  border-color: #d1d5db;
  color: #111827;
}

@media (max-width: 960px) {
  .console-hero,
  .console-banner,
  .console-grid {
    grid-template-columns: 1fr;
  }

  .hero-actions,
  .banner-actions {
    justify-content: flex-start;
  }
}

@media (max-width: 720px) {
  .codex-console {
    padding: 18px;
  }
}
</style>
