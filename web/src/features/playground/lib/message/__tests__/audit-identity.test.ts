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
import { expect, it } from 'vitest'

import { messagesSchema } from '../../storage/storage-schema'
import {
  appendUserMessagePair,
  applyMessageEdit,
  createRegeneratedMessages,
} from '../conversation-message-utils'

it('keeps the chat across storage while assigning each explicit send its own identity', () => {
  const one = appendUserMessagePair([], 'continue')
  const restored = messagesSchema.parse(JSON.parse(JSON.stringify(one)))
  const two = appendUserMessagePair(restored, 'continue')
  expect(one[0].chatId).toBeTruthy()
  expect(two[2].chatId).toBe(one[0].chatId)
  expect(two[2].submissionId).not.toBe(one[0].submissionId)
  expect(appendUserMessagePair([], 'continue')[0].chatId).not.toBe(
    one[0].chatId
  )
  const retry = createRegeneratedMessages(two, two[3].key)
  expect(retry?.[2].submissionId).toBe(two[2].submissionId)
  const edited = applyMessageEdit(two, two[2].key, 'changed', true)
  expect(edited?.messages[2].chatId).toBe(two[2].chatId)
  expect(edited?.messages[2].submissionId).not.toBe(two[2].submissionId)
})
