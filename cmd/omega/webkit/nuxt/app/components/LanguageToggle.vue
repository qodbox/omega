<script setup lang="ts">
import { LanguagesIcon } from 'lucide-vue-next'

const { locale, locales, setLocale, t } = useI18n()
</script>

<template>
  <UiDropdown>
    <template #trigger>
      <UiButton variant="ghost" size="icon" :aria-label="t('nav.language')">
        <LanguagesIcon class="size-4" />
      </UiButton>
    </template>

    <UiDropdownItem
      v-for="entry in locales"
      :key="entry.code"
      :aria-current="locale === entry.code"
      @click="setLocale(entry.code)"
    >
      {{ entry.name }}
    </UiDropdownItem>
  </UiDropdown>
</template>
