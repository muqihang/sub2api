<template>
  <section class="rounded-xl border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
    <div class="flex items-start justify-between gap-4">
      <div class="space-y-1">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('plugin.augment.quickLogin.session.title') }}
        </h2>
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {{ session ? t('plugin.augment.quickLogin.session.ready') : t('plugin.augment.quickLogin.session.empty') }}
        </p>
      </div>
      <button
        v-if="session"
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="revoking"
        @click="$emit('revoke')"
      >
        {{ revoking ? t('plugin.augment.quickLogin.session.revoking') : t('plugin.augment.quickLogin.session.revoke') }}
      </button>
    </div>

    <div v-if="session" class="mt-4 grid gap-3 sm:grid-cols-2">
      <div class="space-y-1">
        <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {{ t('plugin.augment.quickLogin.session.source') }}
        </p>
        <p class="text-sm text-gray-900 dark:text-white">{{ session.source }}</p>
      </div>
      <div class="space-y-1">
        <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {{ t('plugin.augment.quickLogin.session.tenant') }}
        </p>
        <p class="text-sm text-gray-900 dark:text-white">{{ tenantHost }}</p>
      </div>
      <div class="space-y-1">
        <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {{ t('plugin.augment.quickLogin.session.status') }}
        </p>
        <p class="text-sm text-gray-900 dark:text-white">{{ session.status }}</p>
      </div>
      <div class="space-y-1">
        <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {{ t('plugin.augment.quickLogin.session.expiresAt') }}
        </p>
        <p class="text-sm text-gray-900 dark:text-white">{{ session.expires_at || '-' }}</p>
      </div>
      <div class="space-y-1 sm:col-span-2">
        <p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {{ t('plugin.augment.quickLogin.session.lastError') }}
        </p>
        <p class="text-sm text-gray-900 dark:text-white">{{ session.last_error_code || '-' }}</p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AugmentOfficialSessionView } from '@/api/augment'

const props = defineProps<{
  session: AugmentOfficialSessionView | null
  revoking?: boolean
}>()

defineEmits<{
  revoke: []
}>()

const { t } = useI18n()

const tenantHost = computed(() => {
  if (!props.session?.tenant_origin) {
    return '-'
  }
  try {
    return new URL(props.session.tenant_origin).host
  } catch {
    return props.session.tenant_origin
  }
})
</script>
