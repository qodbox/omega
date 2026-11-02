import { configureApi } from '~/lib/api'

// Only a public base URL may come from the environment: everything under
// runtimeConfig.public is shipped to every visitor. Empty means same origin.
export default defineNuxtPlugin(() => {
  configureApi({ baseUrl: useRuntimeConfig().public.apiBase })
})
