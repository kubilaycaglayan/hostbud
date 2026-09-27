<script setup lang="ts">
import { TabsContent, TabsList, TabsRoot, TabsTrigger } from 'reka-ui'
import { computed, ref } from 'vue'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const mode = ref<'signin' | 'register'>('signin')
const email = ref('')
const password = ref('')
const error = ref('')
const hint = ref('')
const busy = ref(false)

const MIN_PASSWORD = 10
const passwordTooShort = computed(
  () => mode.value === 'register' && password.value.length > 0 && password.value.length < MIN_PASSWORD,
)

function describe(e: unknown): [string, string] {
  if (e instanceof ApiError) {
    if (e.status === 429) {
      const wait = e.retryAfter ? ` Try again in ${e.retryAfter} seconds.` : ''
      return ['Too many attempts.' + wait, '']
    }
    return [e.message.charAt(0).toUpperCase() + e.message.slice(1) + '.', e.hint ?? '']
  }
  return ["Can't reach hostbud, check your connection and try again.", '']
}

async function submit() {
  error.value = ''
  hint.value = ''
  if (passwordTooShort.value) {
    error.value = `Use a password of at least ${MIN_PASSWORD} characters.`
    return
  }
  busy.value = true
  try {
    if (mode.value === 'signin') await auth.login(email.value, password.value)
    else await auth.register(email.value, password.value)
    password.value = ''
  } catch (e) {
    ;[error.value, hint.value] = describe(e)
  } finally {
    busy.value = false
  }
}

function switchMode(m: string | number) {
  mode.value = m === 'register' ? 'register' : 'signin'
  error.value = ''
  hint.value = ''
}
</script>

<template>
  <div class="flex min-h-full items-center justify-center p-4">
    <section
      aria-labelledby="auth-title"
      class="w-full max-w-sm rounded border border-border bg-surface p-6"
    >
      <h1
        id="auth-title"
        class="mb-4 text-lg font-bold text-accent"
      >
        hostbud
      </h1>
      <TabsRoot
        :model-value="mode"
        @update:model-value="switchMode"
      >
        <TabsList
          aria-label="Account"
          class="mb-4 flex gap-2"
        >
          <TabsTrigger
            value="signin"
            class="touch-target flex-1 rounded px-3 py-2 data-[state=active]:bg-bg data-[state=active]:text-fg text-muted"
          >
            Sign in
          </TabsTrigger>
          <TabsTrigger
            value="register"
            class="touch-target flex-1 rounded px-3 py-2 data-[state=active]:bg-bg data-[state=active]:text-fg text-muted"
          >
            Create account
          </TabsTrigger>
        </TabsList>
        <TabsContent
          v-for="m in ['signin', 'register']"
          :key="m"
          :value="m"
        >
          <form
            class="flex flex-col gap-3"
            novalidate
            @submit.prevent="submit"
          >
            <label class="flex flex-col gap-1">
              <span>Email</span>
              <input
                v-model="email"
                type="email"
                name="email"
                autocomplete="off"
                required
                class="rounded border border-border bg-bg px-2 py-2 text-base"
              >
            </label>
            <label class="flex flex-col gap-1">
              <span>Password</span>
              <input
                v-model="password"
                type="password"
                name="password"
                :autocomplete="m === 'signin' ? 'current-password' : 'new-password'"
                required
                class="rounded border border-border bg-bg px-2 py-2 text-base"
              >
            </label>
            <p
              v-if="m === 'register'"
              class="text-muted"
            >
              At least {{ MIN_PASSWORD }} characters. Your address must be approved by the owner first.
            </p>
            <div
              v-if="error"
              role="alert"
              class="text-danger"
            >
              <p>{{ error }}</p>
              <p
                v-if="hint"
                class="mt-1 text-muted"
              >
                {{ hint }}
              </p>
            </div>
            <button
              type="submit"
              :disabled="busy"
              class="touch-target rounded bg-accent px-3 py-2 font-bold text-bg disabled:opacity-60"
            >
              {{ m === 'signin' ? 'Sign in' : 'Create account' }}
            </button>
          </form>
        </TabsContent>
      </TabsRoot>
    </section>
  </div>
</template>
