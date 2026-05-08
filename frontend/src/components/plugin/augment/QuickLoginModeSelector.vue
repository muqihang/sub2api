<template>
  <div class="space-y-4">
    <div class="space-y-2">
      <p class="text-sm font-medium text-gray-900 dark:text-white">
        {{ t('plugin.augment.quickLogin.modeLabel') }}
      </p>
      <div class="grid gap-3 sm:grid-cols-2">
        <button
          type="button"
          class="rounded-lg border px-4 py-3 text-left transition"
          :class="modelValue === 'official_passthrough'
            ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-200'
            : 'border-gray-200 bg-white text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200'"
          @click="$emit('update:modelValue', 'official_passthrough')"
        >
          <div class="text-sm font-semibold">
            {{ t('plugin.augment.quickLogin.modes.officialPassthrough.title') }}
          </div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('plugin.augment.quickLogin.modes.officialPassthrough.description') }}
          </div>
        </button>

        <button
          v-if="showLocalCompat"
          type="button"
          class="rounded-lg border px-4 py-3 text-left transition"
          :class="modelValue === 'local_compat'
            ? 'border-amber-500 bg-amber-50 text-amber-700 dark:border-amber-400 dark:bg-amber-900/20 dark:text-amber-200'
            : 'border-gray-200 bg-white text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200'"
          @click="$emit('update:modelValue', 'local_compat')"
        >
          <div class="text-sm font-semibold">
            {{ t('plugin.augment.quickLogin.modes.localCompat.title') }}
          </div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('plugin.augment.quickLogin.modes.localCompat.description') }}
          </div>
        </button>
      </div>
    </div>

    <div v-if="modelValue === 'official_passthrough'" class="space-y-2">
      <p class="text-sm font-medium text-gray-900 dark:text-white">
        {{ t('plugin.augment.quickLogin.sourceLabel') }}
      </p>
      <div class="flex flex-wrap gap-3">
        <button
          type="button"
          data-test="source-official_quick_login"
          class="rounded-lg border px-4 py-2 text-sm transition"
          :class="source === 'official_quick_login'
            ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-200'
            : 'border-gray-200 text-gray-700 dark:border-dark-700 dark:text-gray-200'"
          @click="$emit('update:source', 'official_quick_login')"
        >
          {{ t('plugin.augment.quickLogin.sources.official') }}
        </button>
        <button
          type="button"
          data-test="source-wukong_quick_login"
          class="rounded-lg border px-4 py-2 text-sm transition"
          :class="source === 'wukong_quick_login'
            ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/20 dark:text-primary-200'
            : 'border-gray-200 text-gray-700 dark:border-dark-700 dark:text-gray-200'"
          @click="$emit('update:source', 'wukong_quick_login')"
        >
          {{ t('plugin.augment.quickLogin.sources.wukong') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  modelValue: 'official_passthrough' | 'local_compat'
  source: 'official_quick_login' | 'wukong_quick_login'
  showLocalCompat: boolean
}>()

defineEmits<{
  'update:modelValue': [value: 'official_passthrough' | 'local_compat']
  'update:source': [value: 'official_quick_login' | 'wukong_quick_login']
}>()

const { t } = useI18n()
</script>
