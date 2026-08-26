<script setup lang="ts">
import { EyeIcon, EyeOffIcon } from 'lucide-vue-next'
import { ApiError } from '~/lib/api'

definePageMeta({ layout: 'default' })

const { t } = useI18n()
const { signIn, signUp } = useAuth()
const toast = useToast()

const tab = ref('signin')
const pending = ref(false)
const fields = ref<Record<string, string>>({})
const revealed = ref(false)

const form = reactive({ name: '', email: 'admin@omega.test', password: '' })

async function run(action: () => Promise<void>) {
  pending.value = true
  fields.value = {}
  try {
    await action()
    await navigateTo('/dashboard')
  }
  catch (error) {
    if (error instanceof ApiError) {
      fields.value = error.fields ?? {}
      toast.error(error.message)
    }
    else {
      toast.error(t('auth.unreachable'))
    }
  }
  finally {
    pending.value = false
  }
}
</script>

<template>
  <div class="flex min-h-svh items-center justify-center p-4">
    <UiCard class="w-full max-w-md" :title="t('auth.title')">
      <UiTabs
        v-model="tab"
        :tabs="[{ value: 'signin', label: t('auth.signIn') }, { value: 'signup', label: t('auth.signUp') }]"
      >
        <form class="flex flex-col gap-4" @submit.prevent="run(() => tab === 'signin'
          ? signIn(form.email, form.password)
          : signUp(form.name, form.email, form.password))"
        >
          <UiField v-if="tab === 'signup'" :label="t('auth.name')" for="name" :error="fields.name">
            <UiInput id="name" v-model="form.name" :placeholder="t('auth.namePlaceholder')" autocomplete="name" :invalid="!!fields.name" />
          </UiField>

          <UiField :label="t('auth.email')" for="email" :error="fields.email">
            <UiInput id="email" v-model="form.email" type="email" :placeholder="t('auth.emailPlaceholder')" autocomplete="email" :invalid="!!fields.email" />
          </UiField>

          <UiField :label="t('auth.password')" for="password" :error="fields.password">
            <div class="relative">
              <UiInput
                id="password"
                v-model="form.password"
                :type="revealed ? 'text' : 'password'"
                :placeholder="t('auth.passwordPlaceholder')"
                :autocomplete="tab === 'signin' ? 'current-password' : 'new-password'"
                :invalid="!!fields.password"
                class="pr-9"
              />
              <UiButton
                variant="ghost"
                size="icon-sm"
                type="button"
                tabindex="-1"
                class="absolute inset-y-0 right-1 my-auto text-ink-gray-5"
                :aria-pressed="revealed"
                :aria-label="revealed ? t('auth.hidePassword') : t('auth.showPassword')"
                @click="revealed = !revealed"
              >
                <EyeOffIcon v-if="revealed" />
                <EyeIcon v-else />
              </UiButton>
            </div>
          </UiField>

          <UiButton type="submit" :disabled="pending" class="w-full">
            <UiSpinner v-if="pending" />
            {{ tab === 'signin'
              ? (pending ? t('auth.signingIn') : t('auth.signIn'))
              : (pending ? t('auth.creating') : t('auth.signUp')) }}
          </UiButton>
        </form>
      </UiTabs>
    </UiCard>
  </div>
</template>
