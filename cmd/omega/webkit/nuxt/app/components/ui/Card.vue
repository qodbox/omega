<script setup lang="ts">
defineProps<{ title?: string, description?: string }>()
</script>

<template>
  <div class="flex flex-col gap-4 overflow-hidden rounded-lg border border-outline-gray-1 bg-card py-4 text-base text-card-foreground shadow-xs">
    <div v-if="title || description || $slots.header" class="grid gap-1 px-4">
      <slot name="header">
        <h3 v-if="title" class="text-lg leading-snug font-semibold text-ink-gray-9">
          {{ title }}
        </h3>
        <p v-if="description" class="text-p-sm text-ink-gray-5">
          {{ description }}
        </p>
      </slot>
    </div>
    <div v-if="$slots.default" class="px-4">
      <slot />
    </div>
    <div v-if="$slots.footer" class="flex items-center border-t border-outline-gray-1 bg-surface-gray-1 p-4">
      <slot name="footer" />
    </div>
  </div>
</template>
