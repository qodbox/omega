<script setup lang="ts">
import { ChevronLeftIcon, ChevronRightIcon, ListIcon } from 'lucide-vue-next'
import { docs, sections } from '~/lib/docs'

const { t, loadDocs } = useI18n()

await loadDocs()
const route = useRoute()

const current = computed(() => {
  const slug = route.params.slug as string | undefined
  return docs.find(page => page.slug === slug) ?? docs[0]!
})

const sectionTitle = computed(() =>
  sections.find(section => section.key === current.value.section)?.title ?? current.value.section)

const index = computed(() => docs.indexOf(current.value))
const previous = computed(() => docs[index.value - 1])
const next = computed(() => docs[index.value + 1])
const menu = ref(false)

const grouped = computed(() =>
  sections
    .map(section => ({ section, pages: docs.filter(page => page.section === section.key) }))
    .filter(entry => entry.pages.length > 0),
)

useHead({ title: () => `${t(`doc.${current.value.slug}.title`, current.value.title)} · Omega` })
</script>

<template>
  <div class="mx-auto flex max-w-reading gap-10 px-4 py-10">
    <nav class="hidden w-56 shrink-0 lg:block">
      <div class="sticky top-20 flex flex-col gap-5">
        <div v-for="entry in grouped" :key="entry.section.key" class="flex flex-col gap-0.5">
          <p class="px-2 py-1 text-xs font-medium text-ink-gray-5">
            {{ t(`docSection.${entry.section.key}`, entry.section.title) }}
          </p>
          <NuxtLink
            v-for="page in entry.pages"
            :key="page.slug"
            :to="`/docs/${page.slug}`"
            class="rounded-md px-2 py-1 text-sm text-ink-gray-7 hover:bg-surface-gray-2 hover:text-ink-gray-9"
            active-class="bg-surface-gray-2 font-medium text-ink-gray-9"
          >
            {{ t(`doc.${page.slug}.title`, page.title) }}
          </NuxtLink>
        </div>
      </div>
    </nav>

    <article class="flex min-w-0 flex-1 flex-col gap-8">
      <UiButton variant="outline" size="sm" class="w-fit lg:hidden" @click="menu = true">
        <ListIcon />
        {{ t('nav.docs') }}
      </UiButton>

      <header class="flex flex-col gap-3">
        <UiBadge variant="outline" class="w-fit">
          {{ t(`docSection.${current.section}`, sectionTitle) }}
        </UiBadge>
        <h1 class="text-4xl font-semibold text-ink-gray-9">
          {{ t(`doc.${current.slug}.title`, current.title) }}
        </h1>
        <p class="text-p-lg text-ink-gray-6">
          {{ t(`doc.${current.slug}.lead`, current.lead) }}
        </p>
      </header>

      <DocBlock
        v-for="(block, position) in current.blocks"
        :key="position"
        :block="block"
        :slug="current.slug"
        :index="position"
      />

      <UiSeparator />

      <nav class="flex justify-between gap-2">
        <UiButton v-if="previous" as="a" variant="ghost" :href="`/docs/${previous.slug}`">
          <ChevronLeftIcon />
          {{ t(`doc.${previous.slug}.title`, previous.title) }}
        </UiButton>
        <span v-else />
        <UiButton v-if="next" as="a" variant="ghost" :href="`/docs/${next.slug}`">
          {{ t(`doc.${next.slug}.title`, next.title) }}
          <ChevronRightIcon />
        </UiButton>
      </nav>
    </article>

    <UiDialog v-model:open="menu" :title="t('nav.docs')" :description="t('docs.browse')">
      <div class="flex max-h-96 flex-col gap-4 overflow-y-auto">
        <div v-for="entry in grouped" :key="entry.section.key" class="flex flex-col gap-0.5">
          <p class="px-2 py-1 text-xs font-medium text-ink-gray-5">
            {{ t(`docSection.${entry.section.key}`, entry.section.title) }}
          </p>
          <NuxtLink
            v-for="page in entry.pages"
            :key="page.slug"
            :to="`/docs/${page.slug}`"
            class="rounded-md px-2 py-1 text-sm text-ink-gray-7 hover:bg-surface-gray-2"
            @click="menu = false"
          >
            {{ t(`doc.${page.slug}.title`, page.title) }}
          </NuxtLink>
        </div>
      </div>
    </UiDialog>
  </div>
</template>
