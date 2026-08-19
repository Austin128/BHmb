<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import ThemeSwitch from '@/components/ThemeSwitch.vue'
import { ENV } from '@/config/env'
import { CODE_NEED_2FA } from '@/config/errorCode'
import { useUserStore } from '@/store/modules/user'
import { BizError } from '@/types/api'
import type { TwoFAChallenge } from '@/types/user'
import { errorText as translateError } from '@/utils/errorText'
import { toast } from '@/utils/feedback'

const user = useUserStore()
const router = useRouter()
const route = useRoute()
const { t } = useI18n()

const form = reactive({ username: '', password: '', remember: false })
const twoFAForm = reactive({ code: '', method: 'totp' as 'totp' | 'recovery' })

const loading = ref(false)
const errorText = ref('')
// 非空表示进入 2FA 阶段：此时 accessToken 尚未下发
const challenge = ref<TwoFAChallenge | null>(null)

// 切语言后校验文案需跟着变，所以用 computed 而不是常量
const rules = computed(() => ({
  username: [{ required: true, message: t('login.usernameRequired') }],
  password: [{ required: true, message: t('login.passwordRequired') }],
}))

const methodOptions = computed(() => {
  const methods = challenge.value?.methods ?? ['totp']
  return methods.map((m) => ({
    value: m,
    label: m === 'recovery' ? 'login.twoFAMethodRecovery' : 'login.twoFAMethodTotp',
  }))
})

function messageOf(err: unknown): string {
  if (err instanceof BizError) return translateError(err.code, err.message)
  return t('login.failed')
}

async function onLogin() {
  if (!form.username || !form.password) return
  loading.value = true
  errorText.value = ''
  try {
    const res = await user.login({ ...form })
    await afterLogin(res.user.mustChangePassword)
  } catch (err) {
    // 110009 不是错误终态，而是转入两步验证
    if (err instanceof BizError && err.code === CODE_NEED_2FA) {
      challenge.value = err.data as TwoFAChallenge
      twoFAForm.method = (challenge.value?.methods?.[0] as 'totp' | 'recovery') ?? 'totp'
      twoFAForm.code = ''
    } else {
      errorText.value = messageOf(err)
    }
  } finally {
    loading.value = false
  }
}

async function onVerify() {
  if (!challenge.value || !twoFAForm.code) return
  loading.value = true
  errorText.value = ''
  try {
    const res = await user.verifyTwoFA({
      twoFAToken: challenge.value.twoFAToken,
      method: twoFAForm.method,
      code: twoFAForm.code,
    })
    await afterLogin(res.user.mustChangePassword)
  } catch (err) {
    errorText.value = messageOf(err)
  } finally {
    loading.value = false
  }
}

async function afterLogin(mustChangePassword: boolean) {
  // 菜单与完整权限只认 profile，登录响应不足以驱动导航
  await user.fetchProfile()
  toast(t('login.success'), 'success')
  if (mustChangePassword) toast(t('login.mustChangePassword'), 'warning')
  const redirect = route.query.redirect
  await router.replace(typeof redirect === 'string' && redirect.startsWith('/') ? redirect : '/')
}

function backToPassword() {
  challenge.value = null
  errorText.value = ''
}
</script>

<template>
  <div class="login">
    <div class="login__toolbar">
      <ThemeSwitch />
    </div>

    <a-card class="login__card" :bordered="false">
      <div class="login__brand">
        <icon-storage class="login__logo" />
        <h1 class="login__title">{{ challenge ? $t('login.twoFATitle') : $t('login.title') }}</h1>
        <p class="login__subtitle">
          {{ challenge ? $t('login.twoFATip') : $t('login.subtitle') }}
        </p>
      </div>

      <a-alert v-if="errorText" type="error" class="login__alert">{{ errorText }}</a-alert>

      <a-form
        v-if="!challenge"
        :model="form"
        :rules="rules"
        layout="vertical"
        @submit-success="onLogin"
      >
        <a-form-item field="username" :label="$t('login.username')" hide-asterisk>
          <a-input
            v-model="form.username"
            size="large"
            allow-clear
            autocomplete="username"
            :placeholder="$t('login.usernamePlaceholder')"
          >
            <template #prefix><icon-user /></template>
          </a-input>
        </a-form-item>

        <a-form-item field="password" :label="$t('login.password')" hide-asterisk>
          <a-input-password
            v-model="form.password"
            size="large"
            autocomplete="current-password"
            :placeholder="$t('login.passwordPlaceholder')"
          >
            <template #prefix><icon-lock /></template>
          </a-input-password>
        </a-form-item>

        <div class="login__row">
          <a-checkbox v-model="form.remember">{{ $t('login.remember') }}</a-checkbox>
        </div>

        <a-button type="primary" size="large" long html-type="submit" :loading="loading">
          {{ $t('login.submit') }}
        </a-button>
      </a-form>

      <a-form v-else :model="twoFAForm" layout="vertical" @submit-success="onVerify">
        <a-form-item field="method" :label="$t('login.twoFACode')">
          <a-radio-group v-model="twoFAForm.method" type="button">
            <a-radio v-for="opt in methodOptions" :key="opt.value" :value="opt.value">
              {{ $t(opt.label) }}
            </a-radio>
          </a-radio-group>
        </a-form-item>

        <a-form-item field="code" hide-label>
          <a-input
            v-model="twoFAForm.code"
            size="large"
            allow-clear
            autocomplete="one-time-code"
            :max-length="32"
            placeholder="000000"
          >
            <template #prefix><icon-safe /></template>
          </a-input>
        </a-form-item>

        <a-button type="primary" size="large" long html-type="submit" :loading="loading">
          {{ $t('login.twoFASubmit') }}
        </a-button>
        <a-button type="text" long class="login__back" @click="backToPassword">
          {{ $t('login.twoFABack') }}
        </a-button>
      </a-form>

      <p class="login__footer">{{ ENV.appTitle }}</p>
    </a-card>
  </div>
</template>

<style lang="scss" scoped>
.login {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100%;
  padding: 24px;
  background: var(--nova-login-bg);

  &__toolbar {
    position: fixed;
    top: 16px;
    right: 16px;
  }

  &__card {
    width: 100%;
    max-width: 400px;
    background-color: var(--nova-login-card-bg);
    border-radius: 12px;
    box-shadow: 0 12px 32px rgb(0 0 0 / 12%);
    backdrop-filter: blur(6px);
  }

  &__brand {
    margin-bottom: 20px;
    text-align: center;
  }

  &__logo {
    font-size: 34px;
    color: rgb(var(--primary-6));
  }

  &__title {
    margin: 8px 0 4px;
    font-size: 20px;
    font-weight: 600;
    color: var(--color-text-1);
  }

  &__subtitle {
    margin: 0;
    font-size: 13px;
    color: var(--color-text-3);
  }

  &__alert {
    margin-bottom: 16px;
  }

  &__row {
    display: flex;
    justify-content: space-between;
    margin-bottom: 16px;
  }

  &__back {
    margin-top: 8px;
  }

  &__footer {
    margin: 20px 0 0;
    font-size: 12px;
    color: var(--color-text-4);
    text-align: center;
  }
}
</style>
