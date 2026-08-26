<script setup lang="ts">
import { BoxesIcon, DatabaseIcon, ShieldCheckIcon, ZapIcon } from 'lucide-vue-next'

const { t } = useI18n()

const stack = ['Go 1.26', 'Fiber v2', 'GORM', 'REST', 'OpenAPI 3.1', 'GraphQL', 'JWT', 'Nuxt 4', 'Tailwind 4']
const featureIcons = [DatabaseIcon, ShieldCheckIcon, ZapIcon, BoxesIcon]
const features = ['f1', 'f2', 'f3', 'f4']
</script>

<template>
  <div class="mx-auto flex max-w-5xl flex-col gap-20 px-4 py-16">
    <section class="flex flex-col items-center gap-6 text-center">
      <UiBadge variant="brand" shape="pill">
        {{ t('home.badge') }}
      </UiBadge>
      <h1 class="max-w-3xl text-5xl font-semibold text-balance text-ink-gray-9 sm:text-6xl">
        {{ t('home.title') }}
      </h1>
      <p class="max-w-2xl text-p-lg text-ink-gray-6">
        {{ t('home.lead') }}
      </p>

      <div class="flex flex-wrap justify-center gap-2">
        <UiButton as="a" href="/docs" size="lg">
          {{ t('home.readDocs') }}
        </UiButton>
        <UiButton as="a" href="/api/docs" variant="outline" size="lg" target="_blank" rel="noreferrer">
          {{ t('nav.rest') }}
        </UiButton>
      </div>

      <div class="flex flex-wrap justify-center gap-1.5">
        <UiBadge v-for="item in stack" :key="item" variant="outline">
          {{ item }}
        </UiBadge>
      </div>
    </section>

    <section class="grid gap-4 sm:grid-cols-2">
      <UiCard v-for="(key, index) in features" :key="key">
        <template #header>
          <component :is="featureIcons[index]" class="size-5 text-brand" />
          <h3 class="text-lg font-semibold text-ink-gray-9">
            {{ t(`home.${key}.title`) }}
          </h3>
          <p class="text-p-sm text-ink-gray-5">
            {{ t(`home.${key}.body`) }}
          </p>
        </template>
      </UiCard>
    </section>
  </div>
</template>
