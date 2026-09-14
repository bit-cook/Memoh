<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check } from 'lucide-vue-next'
import {
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  Spinner,
} from '@felinic/ui'
import type { ModelsGetResponse, ProvidersGetResponse } from '@memohai/sdk'
import ModelOptions from '@/pages/bots/components/model-options.vue'
import ModelDescriptionTooltip from '@/components/model-description-tooltip/index.vue'
import { EFFORT_LABELS, REASONING_EFFORT_DISABLE, selectableEfforts } from '@/pages/bots/components/reasoning-effort'

const props = defineProps<{
  modelValue: string
  modelLabel: string
  defaultModelId: string
  reasoningEffort: string
  reasoningOptions?: Array<{ value: string, label: string, description?: string }>
  models: ModelsGetResponse[]
  providers: ProvidersGetResponse[]
  noneLabel?: string
  showReasoning: boolean
  loading: boolean
  error?: string
  authRequired: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:reasoningEffort': [value: string]
  retry: []
  settings: []
}>()
const { t } = useI18n()
const modelOpen = ref(false)
const modelOptions = ref<InstanceType<typeof ModelOptions>>()
const activeModel = computed(() => {
  const id = props.modelValue || props.defaultModelId
  return props.models.find(model => (model.id || model.model_id) === id)
})
const efforts = computed(() => props.reasoningOptions !== undefined
  ? props.reasoningOptions.flatMap(option => {
      const value = option.value.trim()
      return value ? [{ value, label: option.label.trim() || value, description: option.description?.trim() }] : []
    })
  : selectableEfforts(activeModel.value?.reasoning).map(value => ({
      value,
      label: t(EFFORT_LABELS[value] ?? 'chat.reasoningOff'),
      description: undefined,
    })))
const currentEffort = computed(() => props.reasoningOptions !== undefined
  ? props.reasoningEffort
  : efforts.value.length ? props.reasoningEffort || REASONING_EFFORT_DISABLE : REASONING_EFFORT_DISABLE)
const effortLabel = computed(() => efforts.value.find(option => option.value === currentEffort.value)?.label
  || (currentEffort.value ? t(EFFORT_LABELS[currentEffort.value] ?? 'chat.modelDefault') : t('chat.modelDefault')))

function focusModelSearch() {
  void nextTick(() => modelOptions.value?.focusSearch())
}

function onModelKeydown(event: KeyboardEvent) {
  // The listbox owns search, caret movement and virtual row navigation. Let
  // Escape/Tab reach the containing menu so dismissal still uses its contract.
  const backToParent = event.key === 'ArrowLeft' && !(event.target instanceof HTMLInputElement)
  if (event.key !== 'Escape' && event.key !== 'Tab' && !backToParent) event.stopPropagation()
}
</script>

<template>
  <DropdownMenuSub v-model:open="modelOpen">
    <DropdownMenuSubTrigger>
      <span>{{ t('chat.modelOverride') }}</span>
      <span
        class="ml-auto min-w-0 flex-1 truncate text-right text-muted-foreground"
        :title="modelLabel"
      >{{ modelLabel }}</span>
    </DropdownMenuSubTrigger>
    <DropdownMenuSubContent
      position-strategy="absolute"
      :scrollable="false"
      :collision-padding="8"
      :avoid-collisions="false"
      class="w-80 min-w-0 max-w-(--reka-dropdown-menu-content-available-width)"
      @entry-focus.prevent="focusModelSearch"
    >
      <DropdownMenuItem
        v-if="loading"
        disabled
      >
        <Spinner />
        {{ t('common.loading') }}
      </DropdownMenuItem>
      <template v-else-if="error">
        <DropdownMenuLabel class="whitespace-normal">
          {{ error }}
        </DropdownMenuLabel>
        <DropdownMenuItem @select.prevent="authRequired ? emit('settings') : emit('retry')">
          {{ t(authRequired ? 'bots.agent.openSettings' : 'common.retry') }}
        </DropdownMenuItem>
      </template>
      <div
        v-else
        class="contents"
        @keydown="onModelKeydown"
      >
        <ModelOptions
          ref="modelOptions"
          :model-value="modelValue"
          :models="models"
          :providers="providers"
          :none-label="noneLabel"
          model-type="chat"
          :open="modelOpen"
          @update:model-value="emit('update:modelValue', $event)"
        />
      </div>
    </DropdownMenuSubContent>
  </DropdownMenuSub>
  <DropdownMenuSub v-if="showReasoning">
    <DropdownMenuSubTrigger :disabled="loading || !!error || !efforts.length">
      <span>{{ t('chat.reasoningEffort') }}</span>
      <span
        class="ml-auto min-w-0 flex-1 truncate text-right text-muted-foreground"
      >{{ effortLabel }}</span>
    </DropdownMenuSubTrigger>
    <DropdownMenuSubContent
      position-strategy="absolute"
      :collision-padding="8"
      :avoid-collisions="false"
      class="min-w-0 w-40 max-w-(--reka-dropdown-menu-content-available-width)"
    >
      <ModelDescriptionTooltip
        v-for="option in efforts"
        :key="option.value"
        :description="option.description"
      >
        <DropdownMenuItem @select.prevent="emit('update:reasoningEffort', option.value)">
          <span class="min-w-0 flex-1 truncate">{{ option.label }}</span>
          <Check v-if="currentEffort === option.value" />
        </DropdownMenuItem>
      </ModelDescriptionTooltip>
    </DropdownMenuSubContent>
  </DropdownMenuSub>
</template>
