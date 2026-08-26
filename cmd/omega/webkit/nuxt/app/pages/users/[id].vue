<script setup lang="ts">
import { ArrowLeftIcon } from 'lucide-vue-next'
import { api } from '~/lib/api'

definePageMeta({ layout: 'dashboard', middleware: 'auth' })

const { t } = useI18n()
const route = useRoute()

const { data, pending, error } = await useAsyncData(
  () => `user-${route.params.id}`,
  () => api.user(route.params.id as string),
)

const user = computed(() => data.value?.data)
</script>

<template>
  <div class="flex flex-col gap-4">
    <UiButton as="a" variant="ghost" size="sm" href="/users" class="w-fit">
      <ArrowLeftIcon />
      {{ t('users.title') }}
    </UiButton>

    <UiSkeleton v-if="pending" class="h-48 w-full" />

    <UiAlert v-else-if="error || !user" variant="destructive" :title="t('users.missing')">
      {{ t('users.missingBody') }}
    </UiAlert>

    <UiCard v-else>
      <template #header>
        <div class="flex items-center gap-3">
          <UiAvatar :name="user.name" class="size-10" />
          <div>
            <h2 class="text-xl font-semibold text-ink-gray-9">
              {{ user.name }}
            </h2>
            <p class="text-p-sm text-ink-gray-5">
              {{ user.email }}
            </p>
          </div>
        </div>
      </template>

      <dl class="divide-y divide-outline-gray-1">
        <div v-for="row in [
          { label: t('users.role'), value: user.role },
          { label: t('users.verified'), value: user.email_verified_at ?? '—' },
          { label: t('users.created'), value: user.created_at },
        ]" :key="row.label" class="flex justify-between gap-4 py-2.5"
        >
          <dt class="text-base text-ink-gray-6">
            {{ row.label }}
          </dt>
          <dd class="text-base text-ink-gray-9 tabular">
            {{ row.value }}
          </dd>
        </div>
      </dl>
    </UiCard>
  </div>
</template>
