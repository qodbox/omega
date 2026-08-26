<script setup lang="ts">
const { t } = useI18n()
</script>

<template>
  <div class="mx-auto flex max-w-md flex-col items-center gap-4 px-4 py-32 text-center">
    <p class="text-6xl font-semibold text-ink-gray-3">
      404
    </p>
    <h1 class="text-2xl font-semibold text-ink-gray-9">
      {{ t('notFound.title') }}
    </h1>
    <p class="text-p-base text-ink-gray-6">
      {{ t('notFound.body') }}
    </p>
    <UiButton as="a" href="/">
      {{ t('notFound.home') }}
    </UiButton>
  </div>
</template>
