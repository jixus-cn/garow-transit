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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import type { ConversationThread } from '../api'
import { ResponsesConversation } from '../components/responses-conversation'
import {
  responsesThreadPath,
  responsesTurnMessages,
} from '../lib/responses-thread'

const record = {
  request_id: 'root',
  user_id: 1,
  created_at: 1794760000,
  model: 'fixture',
  response_model: 'actual',
  token_id: 1,
  token_name: 'team',
  protocol: 'http',
  kind: 'responses',
  outcome: 'completed',
  truncated: false,
}
const root = {
  record,
  parent_request_id: '',
  content: {
    request: JSON.stringify({
      input: [
        {
          role: 'user',
          content: [{ type: 'input_text', text: 'root question' }],
        },
      ],
    }),
    response: JSON.stringify({
      output: [
        {
          type: 'function_call',
          name: 'read_file',
          call_id: 'call-1',
          arguments: '{"path":"README.md"}',
        },
      ],
    }),
    latest_input: 'root question',
    output: '',
  },
}
const tool = {
  record: { ...record, request_id: 'tool' },
  parent_request_id: 'root',
  content: {
    request: JSON.stringify({
      previous_response_id: 'resp-root',
      input: [
        {
          type: 'function_call_output',
          call_id: 'call-1',
          output: 'file result',
        },
      ],
    }),
    response: JSON.stringify({
      output: [
        {
          type: 'message',
          role: 'assistant',
          content: [{ type: 'output_text', text: 'tool branch answer' }],
        },
      ],
    }),
    latest_input: '',
    output: 'tool branch answer',
  },
}
const branch = {
  record: { ...record, request_id: 'branch' },
  parent_request_id: 'root',
  content: {
    request: JSON.stringify({ input: 'branch question' }),
    response: '{}',
    latest_input: 'branch question',
    output: 'other branch answer',
  },
}
const thread: ConversationThread = {
  selected_request_id: 'tool',
  missing_previous: false,
  truncated: false,
  turns: [root, tool, branch],
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('keeps tool calls and results in order and follows only the selected branch', () => {
  const path = responsesThreadPath(thread, 'tool')
  expect(path.map((turn) => turn.record.request_id)).toEqual(['root', 'tool'])
  const messages = path.flatMap(responsesTurnMessages)
  expect(messages.map((message) => message.body)).toEqual([
    'root question',
    '{"path":"README.md"}',
    'file result',
    'tool branch answer',
  ])
  expect(messages[1].name).toContain('call-1')
  expect(messages[2].role).toBe('tool')
})

it('reconstructs the full supplied history from a standalone context snapshot', () => {
  const turn = {
    ...root,
    content: {
      ...root.content,
      request: JSON.stringify({
        instructions: 'team instructions',
        input: [
          { role: 'user', content: 'first' },
          { role: 'assistant', content: 'previous reply' },
          { role: 'user', content: 'next' },
        ],
      }),
      response: '{}',
      output: 'latest reply',
    },
  }
  expect(responsesTurnMessages(turn).map((message) => message.body)).toEqual([
    'team instructions',
    'first',
    'previous reply',
    'next',
    'latest reply',
  ])
})

it('uses complete output items without duplicating delta text and omits encrypted reasoning', () => {
  const turn = {
    ...root,
    content: {
      ...root.content,
      response: JSON.stringify([
        {
          type: 'response.completed',
          response: {
            output: [
              {
                type: 'reasoning',
                summary: [{ text: 'visible summary' }],
                encrypted_content: '[omitted]',
              },
              {
                type: 'message',
                role: 'assistant',
                content: [{ type: 'output_text', text: 'complete reply' }],
              },
            ],
          },
        },
      ]),
      output: 'complete reply',
    },
  }
  const messages = responsesTurnMessages(turn)
  expect(
    messages.filter((message) => message.body === 'complete reply')
  ).toHaveLength(1)
  expect(messages.some((message) => message.body === 'visible summary')).toBe(
    true
  )
  expect(
    messages.some((message) => message.body.includes('encrypted_content'))
  ).toBe(false)
})

it('switches branches by keyboard without mixing their assistant replies', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: thread },
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ResponsesConversation requestId='tool' />
    </QueryClientProvider>
  )
  expect(await screen.findByText('tool branch answer')).toBeVisible()
  expect(screen.queryByText('other branch answer')).not.toBeInTheDocument()
  const turn = within(
    screen.getByRole('navigation', { name: 'Conversation turns' })
  ).getByRole('button', { name: /Turn 3/ })
  turn.focus()
  await userEvent.keyboard('{Enter}')
  expect(turn).toHaveAttribute('aria-pressed', 'true')
  expect(await screen.findByText('other branch answer')).toBeVisible()
  expect(screen.queryByText('tool branch answer')).not.toBeInTheDocument()
})

it('labels missing context and omitted bodies instead of inventing messages', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...thread,
        missing_previous: true,
        truncated: true,
        turns: [{ ...tool, content: null }],
      },
    },
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ResponsesConversation requestId='tool' />
    </QueryClientProvider>
  )
  expect(
    await screen.findByText(
      'Earlier context is missing, expired, or ambiguous. Only recorded content is shown.'
    )
  ).toBeVisible()
  expect(
    screen.getByText('This turn is partial or its body is unavailable.')
  ).toBeVisible()
  expect(screen.queryByText('tool branch answer')).not.toBeInTheDocument()
})
