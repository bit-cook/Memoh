import { isRuntimeSteerTurnId, type ChatAssistantTurn, type ChatMessage, type ChatUserTurn } from './types'
import { isRuntimeRunActive, type RuntimeTranscriptSlice } from './runtime-projection'

type RuntimeChatTurn = ChatUserTurn | ChatAssistantTurn

export function markRuntimeTurn(
  turn: RuntimeChatTurn,
  slice: RuntimeTranscriptSlice,
  originalUser: boolean,
): RuntimeChatTurn {
  // An assistant segment keeps its own turn identity when it is provisional
  // (steer prefix) or nested under a user turn the same frame carries: that
  // is the durable turn history files the post-steer output under. Any other
  // assistant turn belongs to the run's request turn.
  const nestedAssistantSegment = turn.role === 'assistant' && Boolean(turn.turnId) && (
    isRuntimeSteerTurnId(turn.turnId)
    || slice.turns.some(other => other.role === 'user' && other.turn_id.trim() === turn.turnId)
  )
  if (originalUser || !turn.turnId || (turn.role === 'assistant' && !nestedAssistantSegment)) {
    turn.turnId = slice.turnId
    // The turn was just refiled under the run's request turn, so the run's slot
    // is the right fallback for a segment that has no number of its own. A turn
    // that arrived numbered keeps that number: the frame carries the database
    // row's position, which outranks anything derived here.
    turn.turnPosition ??= slice.turnPosition
  }
  turn.runtimeRunId = slice.runId
  if (turn.role === 'user' && slice.continuation) turn.runtimeContinuation = true
  turn.__optimistic = false
  if (turn.role === 'assistant') turn.streaming = slice.streaming
  return turn
}

// Reconciles one authoritative runtime frame without changing render
// identities already owned by optimistic or settled turns.
export function reconcileRuntimeTurns(
  existing: RuntimeChatTurn[],
  incoming: RuntimeChatTurn[],
): RuntimeChatTurn[] {
  const used = new Set<RuntimeChatTurn>()
  const resolved = incoming.map((next) => {
    const current = existing.find(turn =>
      !used.has(turn)
      && turn.role === next.role
      && turn.turnId === next.turnId,
    )
    if (!current) return next
    used.add(current)
    const renderId = current.id
    const settledPosition = current.turnPosition ?? next.turnPosition
    Object.assign(current, next, { id: renderId, turnPosition: settledPosition })
    return current
  })
  if (!incoming.some(turn => turn.role === 'assistant')) {
    const assistant = existing.find(turn => turn.role === 'assistant')
    if (assistant && !used.has(assistant)) resolved.push(assistant)
  }
  if (!incoming.some(turn => turn.role === 'user')) {
    // Admission and the first streamed frame can arrive separately. The
    // latter often carries only the assistant shell; do not remove the
    // already-rendered optimistic/request user while reconciling that frame.
    for (const user of existing) {
      if (user.role === 'user' && !used.has(user)) resolved.unshift(user)
    }
  }
  return resolved
}

// A terminal run view is not cleared when the run ends: it survives in the
// session snapshot for the whole state TTL and is replayed on every subscribe.
// Appending from one re-added a days-old round below the newest turns once its
// turn had aged out of the loaded window.
//
// The test is the run's own position against the settled history on screen, not
// merely "the run is over": a run that has just finished may legitimately own
// turns the history read has not caught up with, and that round is the newest
// thing in the session. A settled turn numbered past the run is proof the
// database has moved beyond it, so history — which did not include this run's
// turns — is authoritative and the frame has nothing left to contribute.
export function isStaleSettledRunFrame(
  messages: readonly ChatMessage[],
  slice: RuntimeTranscriptSlice,
): boolean {
  const position = slice.turnPosition
  if (position === undefined || isRuntimeRunActive(slice.status)) return false
  return messages.some(turn =>
    turn.settled === true
    && turn.turnPosition !== undefined
    && turn.turnPosition > position,
  )
}
