<script setup lang="ts">
import type { Block } from '~/lib/docs'

const props = defineProps<{ block: Block, slug: string, index: number }>()

const { t } = useI18n()
const key = computed(() => `doc.${props.slug}.b${props.index}`)

const heading = computed(() =>
  props.block.heading ? t(`${key.value}.heading`, props.block.heading) : undefined,
)
const body = computed(() =>
  'body' in props.block ? t(`${key.value}.body`, props.block.body) : '',
)
</script>

<template>
  <UiAlert v-if="block.kind === 'note'" :title="heading">
    {{ body }}
  </UiAlert>

  <section v-else class="flex flex-col gap-3">
    <h2 v-if="heading" class="text-3xl font-semibold text-ink-gray-9">
      {{ heading }}
    </h2>

    <p v-if="block.kind === 'text'" class="text-p-base text-ink-gray-7">
      {{ body }}
    </p>

    <pre
      v-else-if="block.kind === 'code'"
      class="overflow-x-auto rounded-lg border border-outline-gray-1 bg-surface-gray-1 p-4 font-mono text-sm leading-relaxed text-ink-gray-8"
    ><code>{{ body }}</code></pre>

    <UiTable v-else-if="block.kind === 'table'" class="rounded-md border border-outline-gray-1">
      <thead>
        <tr>
          <th v-for="(column, position) in block.columns" :key="column">
            {{ t(`${key}.c${position}`, column) }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, position) in block.rows" :key="position">
          <td
            v-for="(cell, column) in row"
            :key="column"
            :class="column === 0 ? 'font-mono text-xs' : ''"
          >
            {{ column === 0 ? cell : t(`${key}.r${position}c${column}`, cell) }}
          </td>
        </tr>
      </tbody>
    </UiTable>
  </section>
</template>
