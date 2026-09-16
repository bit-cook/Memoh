import { describe, expect, it } from 'vitest'
import { firstAvatarCharacter } from './useAvatarInitials'

describe('firstAvatarCharacter', () => {
  it.each(['😁', '👍🏽', '👨‍👩‍👧‍👦', '🇨🇳', 'é', '中', 'A'])(
    'keeps the complete first character in %s', (character) => {
      expect(firstAvatarCharacter(`${character}bot`)).toBe(character)
    },
  )
  it('handles an empty name', () => {
    expect(firstAvatarCharacter('')).toBe('')
  })
})
