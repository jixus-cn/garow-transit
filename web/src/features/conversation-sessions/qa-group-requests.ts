/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { QAObservation } from './qa-api'

export function groupConversationRequests(
  rows: QAObservation[],
  chat: boolean,
  search: boolean
) {
  if (search) return rows.map((row) => [row])
  const turns = new Map<number, QAObservation[]>()
  if (chat) {
    for (const row of rows) {
      if (!row.turn_id || !row.chat_key) continue
      const turn = turns.get(row.turn_id)
      if (turn) turn.push(row)
      else turns.set(row.turn_id, [row])
    }
  }
  const groups: QAObservation[][] = []
  let previous: QAObservation[] | undefined
  for (const row of rows) {
    const turn = row.turn_id ? turns.get(row.turn_id) : undefined
    if (turn && turn.length > 1) {
      if (turn[0] === row) groups.push(turn)
      previous = undefined
      continue
    }
    const first = previous?.[0]
    const repeat =
      !!row.prompt &&
      !!row.chat_key &&
      first?.chat_key === row.chat_key &&
      first.user_id === row.user_id &&
      first.prompt === row.prompt &&
      first.input_state === row.input_state &&
      ['text', 'legacy_input', 'client_aux'].includes(row.input_state)
    if (repeat && previous) {
      previous.push(row)
    } else {
      previous = [row]
      groups.push(previous)
    }
  }
  return groups
}
