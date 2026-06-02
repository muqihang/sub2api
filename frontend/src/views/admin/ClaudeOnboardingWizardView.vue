<template>
  <AppLayout>
    <ClaudeFormalPoolOnboardingWizardV2 v-if="appStore.useNewAccountManagementUx" />
    <ClaudeFormalPoolOnboardingWizard v-else />
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
import ClaudeFormalPoolOnboardingWizard from '@/components/account/ClaudeFormalPoolOnboardingWizard.vue'
import ClaudeFormalPoolOnboardingWizardV2 from '@/components/account/ClaudeFormalPoolOnboardingWizardV2.vue'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
</script>
