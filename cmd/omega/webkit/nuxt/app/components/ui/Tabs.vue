<script setup lang="ts">
defineProps<{ tabs: { value: string, label: string }[] }>()
const active = defineModel<string>({ required: true })
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="inline-flex w-fit items-center justify-center rounded-md bg-surface-gray-2 p-[3px]" role="tablist">
      <button
        v-for="tab in tabs"
        :key="tab.value"
        type="button"
        role="tab"
        :aria-selected="active === tab.value"
        class="inline-flex h-7 flex-1 items-center justify-center rounded-sm px-2 text-base font-medium whitespace-nowrap transition-colors"
        :class="active === tab.value
          ? 'bg-surface-base text-ink-gray-9 shadow-xs'
          : 'text-ink-gray-6 hover:text-ink-gray-9'"
        @click="active = tab.value"
      >
        {{ tab.label }}
      </button>
    </div>
    <slot />
  </div>
</template>
