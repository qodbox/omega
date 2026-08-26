<script setup lang="ts">
import { PanelLeftIcon } from 'lucide-vue-next'

const { t } = useI18n()
const route = useRoute()
const { toggle, mobileOpen, restore } = useSidebar()

const heading = computed(() =>
  route.path.startsWith('/users') ? t('nav.users') : t('nav.dashboard'),
)

useHead({ title: () => `${heading.value} · Omega` })

onMounted(restore)
</script>

<template>
  <div class="flex min-h-svh items-start">
    <AppSidebar />

    <div class="flex min-w-0 flex-1 flex-col">
      <header class="sticky top-0 z-10 flex h-header items-center gap-2 border-b border-outline-gray-1 bg-background/85 px-4 backdrop-blur-md">
        <UiButton
          variant="ghost"
          size="icon-sm"
          class="-ml-1 md:hidden"
          :aria-label="t('nav.menu')"
          @click="mobileOpen = true"
        >
          <PanelLeftIcon />
        </UiButton>

        <UiButton
          variant="ghost"
          size="icon-sm"
          class="-ml-1 hidden md:inline-flex"
          :aria-label="t('nav.menu')"
          @click="toggle"
        >
          <PanelLeftIcon />
        </UiButton>

        <UiSeparator orientation="vertical" class="mr-2 h-4" />

        <h1 class="text-base font-medium text-ink-gray-9">
          {{ heading }}
        </h1>

        <div class="ml-auto flex items-center gap-1">
          <LanguageToggle />
          <ThemeToggle />
        </div>
      </header>

      <main id="content" class="flex-1 px-4 pt-5 pb-10 sm:px-6">
        <slot />
      </main>
    </div>
  </div>
</template>
