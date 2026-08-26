<script setup lang="ts">
import { MoonIcon, SunIcon } from 'lucide-vue-next'

const { theme, set } = useTheme()
const { t } = useI18n()

const options = ['light', 'dark', 'system'] as const
</script>

<template>
  <UiDropdown>
    <template #trigger>
      <UiButton variant="ghost" size="icon" :aria-label="t('nav.theme')">
        <SunIcon class="size-4 dark:hidden" />
        <MoonIcon class="hidden size-4 dark:block" />
      </UiButton>
    </template>

    <UiDropdownItem
      v-for="option in options"
      :key="option"
      :aria-current="theme === option"
      @click="set(option)"
    >
      {{ t(`nav.${option}`) }}
    </UiDropdownItem>
  </UiDropdown>
</template>
