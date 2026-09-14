import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { UITurn } from '@/composables/api/useChat.types'
import { createBackgroundTaskTracker } from './background-tasks'
import { projectRuntimeTranscript } from './runtime-projection'
import { createTranscriptController } from './transcript'

vi.mock('@/store/user', () => ({
  useUserStore: () => ({ userInfo: { id: 'user-1' } }),
}))

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

function makeTranscript() {
  const backgroundTasks = createBackgroundTaskTracker()
  return createTranscriptController({
    currentBotId: ref<string | null>('bot-1'),
    sessionId: ref<string | null>('session-1'),
    rememberBackgroundTask: backgroundTasks.rememberBackgroundTask,
    applyPendingBackgroundEventsToTool: backgroundTasks.applyPendingBackgroundEventsToTool,
    bumpFsChangedAtIfFsMutation: vi.fn(),
    fetchMessages: vi.fn().mockResolvedValue([]),
    locateMessage: vi.fn(),
    isTurnLive: () => false,
  })
}

describe('render identity adoption', () => {
  // The reason adoption exists: the settled twin must inherit the live turn's
  // render key so the component is not remounted at the handover.
  it('hands the live render key to the settled twin', () => {
    const transcript = makeTranscript()
    transcript.applyRuntimeTranscript(projectRuntimeTranscript({
      run_id: 'run-1',
      turn_id: 'turn-1',
      turn_position: 1,
      generation: 'generation-1',
      status: 'completed',
      started_at: '2026-07-27T08:00:00.000Z',
      updated_at: '2026-07-27T08:00:05.000Z',
      messages: [{ id: 0, type: 'text', content: 'reply' }],
      user_turns: [{
        turn_id: 'turn-1',
        turn_position: 1,
        role: 'user',
        text: 'ask',
        timestamp: '2026-07-27T08:00:00.000Z',
      }],
    }))
    const liveIds = transcript.messages.map(turn => turn.id)

    transcript.mergeMessages([
      settledTurn('m1', 'turn-1', 1, 'user', '2026-07-27T08:00:00.000Z'),
      settledTurn('m2', 'turn-1', 1, 'assistant', '2026-07-27T08:00:01.000Z'),
    ], 'session-1')

    expect(transcript.messages.map(turn => turn.id)).toEqual(liveIds)
    expect(transcript.messages.map(turn => turn.serverId)).toEqual(['m1', 'm2'])
  })

  // A history page holds at most one turn per (turn_id, role) today, and
  // internal/agent/view/turn_identity_test.go guards that server-side. If a new
  // persistence path ever breaks it, the client must degrade to an ordering
  // question — not drop turns. The old lookup gave every twin sharing a key the
  // same render id, and the id-keyed merge then collapsed them into one.
  it('keeps every turn when history repeats a (turn_id, role)', () => {
    const transcript = makeTranscript()
    transcript.replaceMessages([
      settledTurn('m1', 'turn-1', 1, 'assistant', '2026-07-27T08:00:01.000Z'),
    ], 'session-1')

    transcript.mergeMessages([
      settledTurn('m1', 'turn-1', 1, 'assistant', '2026-07-27T08:00:01.000Z'),
      settledTurn('m2', 'turn-1', 1, 'user', '2026-07-27T08:00:02.000Z'),
      settledTurn('m3', 'turn-1', 1, 'assistant', '2026-07-27T08:00:03.000Z'),
    ], 'session-1')

    expect(transcript.messages).toHaveLength(3)
    const ids = transcript.messages.map(turn => turn.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('never issues a duplicate render key through replaceMessages either', () => {
    const transcript = makeTranscript()
    transcript.replaceMessages([
      settledTurn('m1', 'turn-1', 1, 'assistant', '2026-07-27T08:00:01.000Z'),
    ], 'session-1')

    transcript.replaceMessages([
      settledTurn('m1', 'turn-1', 1, 'assistant', '2026-07-27T08:00:01.000Z'),
      settledTurn('m3', 'turn-1', 1, 'assistant', '2026-07-27T08:00:03.000Z'),
    ], 'session-1')

    const ids = transcript.messages.map(turn => turn.id)
    expect(transcript.messages).toHaveLength(2)
    expect(new Set(ids).size).toBe(ids.length)
  })
})
