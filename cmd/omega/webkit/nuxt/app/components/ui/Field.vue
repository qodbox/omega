<script setup lang="ts">
defineProps<{ label?: string, error?: string, hint?: string, for?: string }>()
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label v-if="label" :for="$props.for" class="text-sm leading-none font-medium text-ink-gray-7">
      {{ label }}
    </label>
    <slot />
    <p v-if="error" class="text-p-xs text-ink-red">
      {{ error }}
    </p>
    <p v-else-if="hint" class="text-p-xs text-ink-gray-5">
      {{ hint }}
    </p>
  </div>
</template>
