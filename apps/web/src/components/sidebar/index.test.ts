// @vitest-environment jsdom
import { createApp, h, nextTick } from 'vue'
import { createPinia, defineStore } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => ({
    'chat.activityBar.sessions': 'Chat',
    'chat.activityBar.files': 'Files',
    'chat.activityBar.schedule': 'Schedule',
    'supermarket.title': 'Supermarket',
  }[key] ?? key) }),
}))
vi.mock('@/store/settings', () => ({
  useSettingsStore: defineStore('settings-test', { state: () => ({ uiFontSizePx: 16 }) }),
}))
vi.mock('@/store/chat-list', () => ({
  useChatStore: defineStore('chat-test', { state: () => ({
    currentBotId: 'test-bot',
    bots: [{ id: 'test-bot', current_user_permissions: ['workspace_read', 'manage'] }],
  }) }),
}))
vi.mock('@/store/workspace-tabs', () => ({
  useWorkspaceTabsStore: defineStore('tabs-test', {
    state: () => ({ sidebarView: 'sessions', sidebarWidth: 220, workbenchOpen: true, dirtyFileCount: 0 }),
    actions: { selectSidebarView(view: string) { this.sidebarView = view } },
  }),
}))
vi.mock('./bot-switcher.vue', () => ({ default: { render: () => null } }))
vi.mock('./user-menu.vue', () => ({ default: { render: () => null } }))
vi.mock('./update-chip.vue', () => ({ default: { render: () => null } }))
vi.mock('./panel-sessions.vue', () => ({ default: { render: () => null } }))
vi.mock('./panel-files.vue', () => ({ default: { render: () => null } }))
vi.mock('./panel-schedule.vue', () => ({ default: { render: () => null } }))
vi.mock('./panel-supermarket.vue', () => ({ default: { render: () => null } }))
vi.mock('./session-search-dialog.vue', () => ({ default: { render: () => null } }))

describe('Sidebar navigation', () => {
  let app: ReturnType<typeof createApp> | undefined
  let root: HTMLDivElement | undefined
  afterEach(() => {
    app?.unmount()
    root?.remove()
  })

  it('renders and switches every view without an ancestor TooltipProvider', async () => {
    // 保留真实 Tooltip 组件，避免测试环境补齐 Provider 后掩盖生产注入缺失。
    const Sidebar = (await import('./index.vue')).default
    const onError = vi.fn()
    root = document.createElement('div')
    document.body.append(root)
    app = createApp({ render: () => h(Sidebar) }).use(createPinia())
    app.config.errorHandler = onError
    app.mount(root)
    await nextTick()

    expect(onError).not.toHaveBeenCalled()
    for (const label of ['Chat', 'Files', 'Schedule', 'Supermarket']) {
      const button = root.querySelector<HTMLButtonElement>(`nav button[aria-label="${label}"]`)
      expect(button).not.toBeNull()
      expect(button?.querySelector('svg')).not.toBeNull()
      button!.click()
      await nextTick()
      expect(button?.getAttribute('aria-pressed')).toBe('true')
      expect(button?.textContent?.trim()).toBe(label === 'Supermarket' ? '' : label)
    }
    expect(onError).not.toHaveBeenCalled()
  })
})
