<script setup lang="ts">
import { BadgeCheckIcon, ShieldCheckIcon, UsersIcon } from 'lucide-vue-next'
import { api } from '~/lib/api'

definePageMeta({ layout: 'dashboard', middleware: 'auth' })

const { t } = useI18n()

const { data: all, pending } = await useAsyncData('stats', () => api.users({ per_page: 200 }))
const { data: recent } = await useAsyncData('recent', () => api.users({ per_page: 5, sort: '-created_at' }))

const users = computed(() => all.value?.data ?? [])
const total = computed(() => all.value?.meta.total ?? 0)

const cards = computed(() => {
  const admins = users.value.filter(user => user.role === 'admin').length
  const verified = users.value.filter(user => user.email_verified_at).length

  return [
    { key: 'totalUsers', value: total.value, icon: UsersIcon, ratio: 100 },
    { key: 'admins', value: admins, icon: ShieldCheckIcon, ratio: total.value ? (admins / total.value) * 100 : 0 },
    { key: 'verified', value: verified, icon: BadgeCheckIcon, ratio: total.value ? (verified / total.value) * 100 : 0 },
  ]
})
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="grid gap-4 sm:grid-cols-3">
      <UiCard v-for="card in cards" :key="card.key">
        <template #header>
          <div class="flex items-center justify-between">
            <p class="text-p-sm text-ink-gray-5">
              {{ t(`dashboard.${card.key}`) }}
            </p>
            <component :is="card.icon" class="size-4 text-ink-gray-5" />
          </div>
        </template>

        <div class="grid gap-3">
          <UiSkeleton v-if="pending" class="h-8 w-16" />
          <span v-else class="text-4xl font-semibold tabular text-ink-gray-9">{{ card.value }}</span>
          <UiProgress :value="card.ratio" />
        </div>
      </UiCard>
    </div>

    <UiCard :title="t('dashboard.recent')">
      <div v-if="pending" class="flex flex-col gap-3">
        <UiSkeleton v-for="index in 3" :key="index" class="h-10 w-full" />
      </div>

      <p v-else-if="!recent?.data.length" class="py-8 text-center text-base text-ink-gray-5">
        {{ t('dashboard.empty') }}
      </p>

      <ul v-else class="divide-y divide-outline-gray-1">
        <li v-for="user in recent.data" :key="user.id" class="flex items-center gap-3 py-2.5">
          <UiAvatar :name="user.name" />
          <div class="min-w-0 flex-1">
            <NuxtLink :to="`/users/${user.id}`" class="block truncate text-base font-medium text-ink-gray-9 hover:underline">
              {{ user.name }}
            </NuxtLink>
            <p class="truncate text-sm text-ink-gray-5">
              {{ user.email }}
            </p>
          </div>
          <UiBadge :variant="user.role === 'admin' ? 'brand' : 'default'">
            {{ user.role }}
          </UiBadge>
        </li>
      </ul>
    </UiCard>
  </div>
</template>
