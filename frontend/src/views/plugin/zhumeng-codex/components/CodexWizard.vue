<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCodexEntryStore } from '@/stores/codexEntry'
import WizardStepHeader from './WizardStepHeader.vue'
import StatusPill from './StatusPill.vue'

const { t } = useI18n()
const store = useCodexEntryStore()

const selectedReuseKey = computed(() =>
  store.availableReuseKeys.find((key) => key.id === store.selectedReuseKeyId) ?? null,
)
</script>

<template>
  <div class="codex-wizard" data-testid="codex-wizard">
    <div class="wizard-shell">
      <WizardStepHeader :current-step="store.wizardStep ?? 1" />

      <div
        v-if="store.pageState === 'onboarding_credential'"
        class="wizard-step wizard-step-credential"
        data-testid="wizard-step-1"
      >
        <div class="wizard-main-grid">
          <section class="wizard-main-column">
            <div class="field-card">
              <div class="section-header">
                <h2>{{ t('codex.wizard.step1.title') }}</h2>
                <p>{{ t('codex.wizard.step1.description') }}</p>
              </div>

              <label class="field-label" for="codex-credential-label">{{ t('codex.wizard.step1.labelTitle') }}</label>
              <input
                id="codex-credential-label"
                v-model="store.credentialLabel"
                class="text-input"
                :placeholder="t('codex.wizard.step1.labelPlaceholder')"
                data-testid="credential-label-input"
              />
              <p class="field-hint">{{ t('codex.wizard.step1.labelHint') }}</p>
            </div>

            <div class="field-card">
              <div class="section-header compact">
                <h3>{{ t('codex.wizard.step1.modeTitle') }}</h3>
                <p>{{ t('codex.wizard.step1.modeDescription') }}</p>
              </div>

              <div class="attachment-mode-selector" data-testid="attachment-mode-selector">
                <label
                  class="mode-option"
                  :class="{ selected: store.selectedAttachmentMode === 'independent_credential' }"
                >
                  <input
                    type="radio"
                    value="independent_credential"
                    :checked="store.selectedAttachmentMode === 'independent_credential'"
                    @change="store.setAttachmentMode('independent_credential')"
                    data-testid="mode-independent"
                  />
                  <div>
                    <strong>{{ t('codex.wizard.step1.modeIndependent') }}</strong>
                    <span data-testid="mode-independent-description">{{ t('codex.wizard.step1.modeIndependentDescription') }}</span>
                  </div>
                </label>

                <label
                  class="mode-option"
                  :class="{ selected: store.selectedAttachmentMode === 'reused_key' }"
                >
                  <input
                    type="radio"
                    value="reused_key"
                    :checked="store.selectedAttachmentMode === 'reused_key'"
                    @change="store.setAttachmentMode('reused_key')"
                    data-testid="mode-reused"
                  />
                  <div>
                    <strong>{{ t('codex.wizard.step1.modeReused') }}</strong>
                    <span data-testid="mode-reused-description">{{ t('codex.wizard.step1.modeReusedDescription') }}</span>
                  </div>
                </label>
              </div>
            </div>

            <div
              v-if="store.selectedAttachmentMode === 'reused_key'"
              class="field-card key-selector"
              data-testid="key-selector"
            >
              <label class="field-label" for="codex-reuse-key">{{ t('codex.wizard.step1.selectKey') }}</label>
              <select
                id="codex-reuse-key"
                class="select-input"
                :value="store.selectedReuseKeyId ?? ''"
                @change="store.selectReuseKey(Number(($event.target as HTMLSelectElement).value))"
                data-testid="key-selector-input"
              >
                <option value="" disabled>{{ t('codex.wizard.step1.selectKeyPlaceholder') }}</option>
                <option v-for="key in store.availableReuseKeys" :key="key.id" :value="key.id">
                  {{ key.name }}
                </option>
              </select>
              <p v-if="selectedReuseKey" class="field-hint">
                {{ t('codex.wizard.step1.selectedKeyHint', { name: selectedReuseKey.name }) }}
              </p>
              <p v-else class="field-hint key-selector-hint">
                {{ t('codex.wizard.step1.selectKeyHint') }}
              </p>
            </div>
          </section>

          <aside class="wizard-side-column">
            <div class="side-card" data-testid="wizard-side-card-what">
              <h4>{{ t('codex.wizard.step1.sideWhatTitle') }}</h4>
              <p>{{ t('codex.wizard.step1.sideWhatBody') }}</p>
            </div>
            <div class="side-card" data-testid="wizard-side-card-nochange">
              <h4>{{ t('codex.wizard.step1.sideNoChangeTitle') }}</h4>
              <p>{{ t('codex.wizard.step1.sideNoChangeBody') }}</p>
            </div>
            <div class="side-card" data-testid="wizard-side-card-env">
              <h4>{{ t('codex.wizard.step1.sideEnvTitle') }}</h4>
              <p>{{ t('codex.wizard.step1.sideEnvBody') }}</p>
            </div>
          </aside>
        </div>

        <div class="wizard-footer">
          <p class="footer-hint" data-testid="wizard-footer-hint">{{ t('codex.wizard.step1.footerHint') }}</p>
          <button
            class="btn btn-primary"
            :disabled="store.loading || (store.selectedAttachmentMode === 'reused_key' && !store.selectedReuseKeyId)"
            @click="store.startSetup()"
            data-testid="start-setup-btn"
          >
            {{ t('codex.wizard.step1.startSetup') }}
          </button>
        </div>
      </div>

      <div
        v-if="store.pageState === 'onboarding_attach'"
        class="wizard-step wizard-step-attach"
        data-testid="wizard-step-2"
      >
        <div class="section-header wide">
          <h2>{{ t('codex.wizard.step2.title') }}</h2>
          <p>{{ t('codex.wizard.step2.description') }}</p>
        </div>

        <div class="wizard-main-grid attach-grid">
          <section class="field-card launch-card" data-testid="attach-launch-panel">
            <div class="launch-head">
              <div>
                <p class="mini-label">{{ t('codex.wizard.step2.launchLabel') }}</p>
                <h3>{{ t('codex.wizard.step2.launchTitle') }}</h3>
              </div>
              <StatusPill status="warn" :label="t('codex.wizard.step2.pendingBadge')" />
            </div>
            <p class="launch-copy">{{ t('codex.wizard.step2.launchDescription') }}</p>
            <button
              class="btn btn-primary"
              :disabled="store.loading"
              @click="store.openLocal()"
              data-testid="open-local-btn"
            >
              {{ store.setupSession?.launch_url ? t('codex.wizard.step2.openLocal') : t('codex.wizard.step2.regenerateAndOpen') }}
            </button>

            <div
              v-if="store.setupSession?.cli_command"
              class="cli-block"
              data-testid="attach-cli-block"
            >
              <div class="cli-header">
                <span>{{ t('codex.wizard.step2.cliTitle') }}</span>
                <button class="btn btn-ghost" @click="store.copyCli()" data-testid="copy-cli-btn">
                  {{ t('codex.wizard.step2.copyCli') }}
                </button>
              </div>
              <code>{{ store.setupSession.cli_command }}</code>
            </div>

            <div class="secondary-actions">
              <button class="btn btn-secondary" @click="store.regenerateSetupSession()" data-testid="regenerate-btn">
                {{ t('codex.wizard.step2.regenerate') }}
              </button>
              <button class="btn btn-secondary" @click="store.diagnoseSetupSession()" data-testid="diagnose-session-btn">
                {{ t('codex.wizard.step2.diagnose') }}
              </button>
            </div>
          </section>

          <aside class="side-card attach-help" data-testid="attach-help-panel">
            <h4>{{ t('codex.wizard.step2.helpTitle') }}</h4>
            <ul class="help-list">
              <li>{{ t('codex.wizard.step2.helpItem1') }}</li>
              <li>{{ t('codex.wizard.step2.helpItem2') }}</li>
              <li>{{ t('codex.wizard.step2.helpItem3') }}</li>
            </ul>
            <div class="session-meta" v-if="store.setupSession">
              <div>
                <span>{{ t('codex.wizard.step2.sessionLabel') }}</span>
                <strong>{{ store.setupSession.credential_label }}</strong>
              </div>
              <div>
                <span>{{ t('codex.wizard.step2.expiresLabel') }}</span>
                <strong>{{ store.setupSession.expires_at }}</strong>
              </div>
            </div>
          </aside>
        </div>
      </div>

      <div
        v-if="store.pageState === 'onboarding_verify'"
        class="wizard-step wizard-step-verify"
        data-testid="wizard-step-3"
      >
        <div class="section-header wide">
          <h2>{{ t('codex.wizard.step3.title') }}</h2>
          <p>{{ t('codex.wizard.step3.description') }}</p>
        </div>

        <div class="verify-panel" data-testid="verify-sync-panel">
          <div class="verify-copy">
            <StatusPill status="pending" :label="t('codex.wizard.step3.verifying')" />
            <h3>{{ t('codex.wizard.step3.panelTitle') }}</h3>
            <p>{{ t('codex.wizard.step3.panelDescription') }}</p>
          </div>
          <div class="verify-actions">
            <button
              class="btn btn-primary"
              :disabled="store.loading"
              @click="store.loadSummary()"
              data-testid="refresh-verify-btn"
            >
              {{ t('codex.wizard.step3.refresh') }}
            </button>
            <button class="btn btn-secondary" @click="store.diagnoseSetupSession()" data-testid="diagnose-verify-btn">
              {{ t('codex.wizard.step3.diagnose') }}
            </button>
            <button class="btn btn-secondary" @click="store.openLocal()" data-testid="reopen-local-btn">
              {{ store.setupSession?.launch_url ? t('codex.wizard.step3.reopenLocal') : t('codex.wizard.step3.regenerateAndReopen') }}
            </button>
          </div>
        </div>

        <div class="verify-note" data-testid="verify-exit-condition">
          <strong>{{ t('codex.wizard.step3.exitTitle') }}</strong>
          <p>{{ t('codex.wizard.step3.exitDescription') }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.codex-wizard {
  padding: 26px;
}

.wizard-shell {
  border: 1px solid #e5e7eb;
  border-radius: 14px;
  padding: 22px;
  background: #fff;
}

.wizard-step {
  margin-top: 22px;
}

.wizard-main-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.45fr) minmax(280px, 0.9fr);
  gap: 20px;
}

.wizard-main-column,
.wizard-side-column {
  display: grid;
  gap: 16px;
}

.field-card,
.side-card,
.verify-panel,
.verify-note {
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  background: #fff;
}

.field-card {
  padding: 18px;
}

.side-card,
.verify-note {
  padding: 16px;
  background: #f9fafb;
}

.section-header h2,
.section-header h3,
.side-card h4,
.verify-copy h3,
.launch-head h3 {
  margin: 0;
}

.section-header p,
.side-card p,
.launch-copy,
.verify-copy p,
.verify-note p,
.field-hint {
  margin: 6px 0 0;
  color: #4b5563;
  line-height: 1.65;
}

.section-header.compact p {
  font-size: 13px;
}

.field-label,
.mini-label,
.cli-header span,
.session-meta span {
  display: block;
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 8px;
}

.text-input,
.select-input {
  width: 100%;
  border: 1px solid #d1d5db;
  border-radius: 10px;
  padding: 11px 12px;
  font-size: 14px;
  color: #111827;
  background: #fff;
}

.attachment-mode-selector {
  display: grid;
  gap: 10px;
}

.mode-option {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 14px;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
}

.mode-option.selected {
  border-color: #4338ca;
  background: #eef2ff;
}

.mode-option strong,
.mode-option span {
  display: block;
}

.mode-option span {
  margin-top: 4px;
  font-size: 13px;
  color: #4b5563;
  line-height: 1.6;
}

.wizard-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid #eef0f3;
}

.footer-hint {
  margin: 0;
  color: #6b7280;
  font-size: 13px;
}

.launch-card,
.attach-help {
  min-height: 100%;
}

.attach-grid {
  align-items: start;
}

.launch-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.cli-block {
  margin-top: 18px;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  background: #111827;
  color: #f9fafb;
  padding: 14px;
}

.cli-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}

.cli-block code {
  display: block;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  line-height: 1.7;
}

.secondary-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 16px;
}

.help-list {
  margin: 10px 0 0;
  padding-left: 18px;
  color: #4b5563;
  line-height: 1.7;
}

.session-meta {
  display: grid;
  gap: 12px;
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid #e5e7eb;
}

.session-meta strong {
  display: block;
  color: #111827;
  font-size: 13px;
}

.verify-panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 18px;
  padding: 18px;
  background: #eef2ff;
  border-color: #c7d2fe;
}

.verify-copy {
  display: grid;
  gap: 10px;
}

.verify-actions {
  display: flex;
  gap: 10px;
  align-items: center;
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
  line-height: 1;
}

.btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
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

.btn-ghost {
  background: transparent;
  color: #fff;
  padding: 0;
}

@media (max-width: 960px) {
  .wizard-main-grid,
  .verify-panel {
    grid-template-columns: 1fr;
  }

  .wizard-footer,
  .launch-head {
    align-items: flex-start;
    flex-direction: column;
  }

  .verify-actions {
    justify-content: flex-start;
  }
}

@media (max-width: 720px) {
  .codex-wizard {
    padding: 18px;
  }

  .wizard-shell {
    padding: 16px;
  }
}
</style>
