<script setup lang="ts">
const { restore: restoreTheme } = useTheme()
const { restore: restoreAuth } = useAuth()
const { restore: restoreLocale } = useI18n()

onMounted(() => {
  restoreTheme()
  restoreAuth()
  restoreLocale()
})
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
  <UiToaster />
</template>
