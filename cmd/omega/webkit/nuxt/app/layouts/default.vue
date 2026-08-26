<script setup lang="ts">
const { t } = useI18n()
const { user } = useAuth()
</script>

<template>
  <div class="min-h-svh">
    <a href="#content" class="sr-only focus:not-sr-only focus:absolute focus:top-4 focus:left-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-3 focus:py-2 focus:text-sm">
      {{ t('nav.skip') }}
    </a>

    <header class="sticky top-0 z-30 border-b border-outline-gray-1 bg-background/85 backdrop-blur">
      <div class="mx-auto flex h-header max-w-6xl items-center gap-4 px-4">
        <NuxtLink to="/" class="flex items-center gap-2 font-semibold text-ink-gray-9">
          <span class="flex size-7 items-center justify-center rounded-md bg-primary text-sm font-bold text-primary-foreground">Ω</span>
          Omega
        </NuxtLink>

        <nav class="ml-2 hidden gap-1 sm:flex">
          <UiButton as="a" variant="ghost" size="sm" href="/docs">
            {{ t('nav.docs') }}
          </UiButton>
          <UiButton as="a" variant="ghost" size="sm" href="/api/docs" target="_blank" rel="noreferrer">
            {{ t('nav.rest') }}
          </UiButton>
          <UiButton as="a" variant="ghost" size="sm" href="/graphql" target="_blank" rel="noreferrer">
            {{ t('nav.graphql') }}
          </UiButton>
        </nav>

        <div class="ml-auto flex items-center gap-1">
          <DocsSearch />
          <LanguageToggle />
          <ThemeToggle />
          <UiButton as="a" size="sm" :href="user ? '/dashboard' : '/login'">
            {{ user ? t('nav.dashboard') : t('nav.signIn') }}
          </UiButton>
        </div>
      </div>
    </header>

    <main id="content">
      <slot />
    </main>
  </div>
</template>
