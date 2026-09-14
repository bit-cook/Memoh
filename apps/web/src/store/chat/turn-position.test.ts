import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { RuntimeCurrentRunView, UITurn } from '@/composables/api/useChat.types'
import { createBackgroundTaskTracker } from './background-tasks'
import { projectRuntimeTranscript } from './runtime-projection'
import { createTranscriptController } from './transcript'

vi.mock('@/store/user', () => ({
  useUserStore: () => ({ userInfo: { id: 'user-1' } }),
}))

function runView(overrides: Partial<RuntimeCurrentRunView> = {}): RuntimeCurrentRunView {
  return {
    run_id: 'run-1',
    turn_id: 'turn-5',
    turn_position: 5,
    generation: 'generation-1',
    status: 'running',
    started_at: '2026-07-27T08:00:00.000Z',
    updated_at: '2026-07-27T08:00:05.000Z',
    messages: [{ id: 0, type: 'text', content: 'live reply' }],
    user_turns: [{
      turn_id: 'turn-5',
      turn_position: 5,
      role: 'user',
      text: 'live ask',
      timestamp: '2026-07-27T08:00:00.000Z',
    }],
    ...overrides,
  }
}

function settledTurn(
  id: string,
  turnId: string,
  position: number,
  role: 'user' | 'assistant',
  timestamp: string,
): UITurn {
  return role === 'user'
    ? { id, turn_id: turnId, turn_position: position, role, text: `u-${id}`, timestamp, platform: 'local' }
    : { id, turn_id: turnId, turn_position: position, role, messages: [], timestamp }
}

function makeTranscript(fetchMessages = vi.fn().mockResolvedValue([])) {
  const currentBotId = ref<string | null>('bot-1')
  const sessionId = ref<string | null>('session-1')
  const backgroundTasks = createBackgroundTaskTracker()
  const transcript = createTranscriptController({
    currentBotId,
    sessionId,
    rememberBackgroundTask: backgroundTasks.rememberBackgroundTask,
    applyPendingBackgroundEventsToTool: backgroundTasks.applyPendingBackgroundEventsToTool,
    bumpFsChangedAtIfFsMutation: vi.fn(),
    fetchMessages,
    locateMessage: vi.fn(),
    isTurnLive: () => true,
  })
  return { transcript, fetchMessages }
}

describe('live turn positions', () => {
  it('numbers the request turn and its assistant segment from the run view', () => {
    const slice = projectRuntimeTranscript(runView())

    expect(slice.turnPosition).toBe(5)
    expect(slice.turns.map(turn => [turn.role, turn.turn_position]))
      .toEqual([['user', 5], ['assistant', 5]])
  })

  it('numbers a post-steer segment from the steer turn, not the run turn', () => {
    const slice = projectRuntimeTranscript(runView({
      messages: [
        { id: 0, type: 'text', content: 'before steer' },
        { id: 1, type: 'text', content: 'after steer' },
      ],
      user_turns: [
        { turn_id: 'turn-5', turn_position: 5, role: 'user', text: 'live ask', timestamp: '2026-07-27T08:00:00.000Z' },
        { turn_id: 'turn-6', turn_position: 6, role: 'user', text: 'steer me', timestamp: '2026-07-27T08:00:02.000Z' },
      ],
      steer_turns: [{
        item_id: 'item-1',
        status: 'applied',
        text: 'steer me',
        turn_id: 'turn-6',
        after_message_id: 0,
        timestamp: '2026-07-27T08:00:02.000Z',
      }],
    }))

    expect(slice.turns.map(turn => [turn.role, turn.turn_id, turn.turn_position])).toEqual([
      ['user', 'turn-5', 5],
      ['assistant', 'turn-5', 5],
      ['user', 'turn-6', 6],
      ['assistant', 'turn-6', 6],
    ])
  })

  it('leaves a claimed steer unnumbered until its step commits', () => {
    const slice = projectRuntimeTranscript(runView({
      steer_turns: [{
        item_id: 'item-1',
        status: 'claimed',
        text: 'steer me',
        after_message_id: 0,
        timestamp: '2026-07-27T08:00:02.000Z',
      }],
    }))

    // Both the provisional steer bubble and the segment named after it stay
    // unnumbered: they are the tail boundary until the step commit numbers them.
    expect(slice.turns.map(turn => [turn.role, turn.turn_id, turn.turn_position])).toEqual([
      ['user', 'turn-5', 5],
      ['assistant', 'turn-5', 5],
      ['user', 'queue-steer:item-1', undefined],
      ['assistant', 'queue-steer:item-1:assistant', undefined],
    ])
  })

  // The #1179-era regression: a channel turn persisted while a run streams has
  // a later position but an earlier timestamp than the live assistant segment.
  // Ordering by time puts the reply after a turn that came later.
  it('orders a live turn against a settled one by position, not timestamp', () => {
    const { transcript } = makeTranscript()
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView()))
    // The user scrolled up once, so refreshes take the sorting merge path.
    transcript.hasLoadedOlder.value = true

    transcript.mergeMessages([
      settledTurn('m6', 'turn-6', 6, 'user', '2026-07-27T08:00:03.000Z'),
      settledTurn('m7', 'turn-6', 6, 'assistant', '2026-07-27T08:00:04.000Z'),
    ], 'session-1')

    expect(transcript.messages.map(turn => [turn.role, turn.turnId])).toEqual([
      ['user', 'turn-5'],
      ['assistant', 'turn-5'],
      ['user', 'turn-6'],
      ['assistant', 'turn-6'],
    ])
  })

  it('numbers the optimistic pair from run_accepted', () => {
    const { transcript } = makeTranscript()
    const assistantTurn = transcript.createOptimisticAssistantTurn('invocation-1')
    const userTurn = transcript.createOptimisticUserTurn('hi', undefined, 'invocation-1')
    transcript.appendToView(userTurn, assistantTurn)

    transcript.bindRuntimeTurn('invocation-1', 'turn-5', 'run-1', 5)

    expect(transcript.messages.map(turn => turn.turnPosition)).toEqual([5, 5])
  })

  // A live turn now carries a position, so position can no longer stand in for
  // "the database can address this". Paging from one would send a render id as
  // the cursor.
  it('never pages from a live turn that has a position but no database row', async () => {
    const fetchMessages = vi.fn().mockResolvedValue([])
    const { transcript } = makeTranscript(fetchMessages)
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView()))
    fetchMessages.mockClear()

    const loaded = await transcript.loadOlderMessages()

    expect(loaded).toBe(0)
    expect(fetchMessages).not.toHaveBeenCalled()
  })

  it('pages from the oldest settled turn even when live turns precede it', async () => {
    const fetchMessages = vi.fn().mockResolvedValue([])
    const { transcript } = makeTranscript(fetchMessages)
    transcript.replaceMessages([
      settledTurn('m1', 'turn-1', 1, 'user', '2026-07-27T07:00:00.000Z'),
    ], 'session-1')
    fetchMessages.mockClear()

    await transcript.loadOlderMessages()

    expect(fetchMessages).toHaveBeenCalledWith('bot-1', 'session-1', {
      limit: 30,
      beforeMessageId: 'm1',
    })
  })
})
