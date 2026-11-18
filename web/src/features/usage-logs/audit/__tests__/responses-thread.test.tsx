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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import type { AuditLog, ConversationThread } from '../api'
import { ConversationContent } from '../components/conversation-content'
import { ConversationDetailsDialog } from '../components/conversation-details-dialog'
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
    reading: {
      input: [
        {
          role: 'user',
          kind: 'text',
          origin: 'request',
          item: 0,
          part: 0,
          text: 'root question',
        },
      ],
      replies: [],
    },
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
    reading: {
      input: [],
      replies: [
        {
          role: 'assistant',
          kind: 'text',
          origin: 'response',
          item: 0,
          part: 0,
          text: 'tool branch answer',
        },
      ],
    },
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
    reading: {
      input: [
        {
          role: 'user',
          kind: 'text',
          origin: 'request',
          item: 0,
          part: 0,
          text: 'branch question',
        },
      ],
      replies: [
        {
          role: 'assistant',
          kind: 'text',
          origin: 'response',
          item: 0,
          part: 0,
          text: 'other branch answer',
        },
      ],
    },
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
  const messages = path.flatMap((turn) => responsesTurnMessages(turn))
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
      reading: undefined,
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
      reading: undefined,
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

const auditEntry: AuditLog = {
  event_id: 'fixture',
  user_id: 1,
  username: 'fixture',
  actor_role: 100,
  created_at: record.created_at,
  category: 'conversation',
  action: 'conversation.record',
  token_ref: '',
  ip: '',
  user_agent: '',
  method: 'POST',
  route: '/v1/responses',
  status: 200,
  success: true,
  request_id: 'root',
  content: '',
  other: null,
}

it('opens old records as the latest exchange and loads replay only when selected', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: String(url).endsWith('/thread')
        ? thread
        : {
            ...root,
            content: {
              ...root.content,
              output: 'latest answer',
              request: JSON.stringify({
                input: 'root question',
                instructions: 'raw context fixture',
              }),
            },
          },
    },
  }))
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ConversationDetailsDialog entry={auditEntry} />
    </QueryClientProvider>
  )
  await userEvent.click(screen.getByRole('button', { name: 'Details' }))
  expect(await screen.findByText('latest answer')).toBeVisible()
  expect(screen.queryByText(/raw context fixture/)).not.toBeInTheDocument()
  expect(get.mock.calls.map(([url]) => url)).toEqual(['/api/conversation/root'])
  await userEvent.click(
    screen.getByRole('button', { name: 'Request context and tools' })
  )
  expect(await screen.findByText(/raw context fixture/)).toBeVisible()
  await userEvent.click(
    screen.getByRole('tab', { name: 'Conversation replay' })
  )
  await waitFor(() =>
    expect(
      get.mock.calls.some(([url]) => String(url).endsWith('/thread'))
    ).toBe(true)
  )
})

it('focuses the selected turn and reveals only its own recorded context on demand', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: thread },
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ResponsesConversation requestId='tool' />
    </QueryClientProvider>
  )
  expect(await screen.findByText('tool branch answer')).toBeVisible()
  expect(screen.queryByText('root question')).not.toBeInTheDocument()
  expect(screen.queryByText('file result')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /Tool result/ }))
  expect(await screen.findByText('file result')).toBeVisible()
  await userEvent.click(
    screen.getByRole('button', { name: 'Show earlier turns' })
  )
  expect(await screen.findByText('root question')).toBeVisible()
  expect(screen.queryByText('other branch answer')).not.toBeInTheDocument()
})

it('keeps supplied history separate from the newest user input in compact replay', () => {
  const turn = {
    ...root,
    content: {
      ...root.content,
      request: JSON.stringify({
        instructions: 'instructions fixture',
        input: [
          { role: 'user', content: 'earlier question' },
          { role: 'assistant', content: 'earlier reply' },
          { role: 'user', content: 'latest question' },
        ],
      }),
      response: '{}',
      reading: {
        input: [
          {
            role: 'user',
            kind: 'text',
            origin: 'snapshot',
            item: 0,
            part: 0,
            text: 'earlier question',
          },
          {
            role: 'assistant',
            kind: 'text',
            origin: 'snapshot',
            item: 1,
            part: 0,
            text: 'earlier reply',
          },
          {
            role: 'user',
            kind: 'text',
            origin: 'request',
            item: 2,
            part: 0,
            text: 'latest question',
          },
        ],
        replies: [
          {
            role: 'assistant',
            kind: 'text',
            origin: 'response',
            item: 0,
            part: 0,
            text: 'latest answer',
          },
        ],
      },
      output: 'latest answer',
    },
  }
  expect(
    responsesTurnMessages(turn, false).map((message) => message.body)
  ).toEqual(['latest question', 'latest answer'])
  expect(responsesTurnMessages(turn).map((message) => message.body)).toContain(
    'earlier reply'
  )
})

it('keeps a trailing client context out of compact replay and labels it in evidence', () => {
  const env = '<environment_context>fixture</environment_context>'
  const turn = {
    ...root,
    content: {
      ...root.content,
      request: JSON.stringify({
        input: [
          { role: 'user', content: 'Question' },
          { role: 'user', content: env },
        ],
      }),
      reading: {
        input: [
          {
            role: 'user',
            kind: 'text',
            origin: 'request',
            item: 0,
            part: 0,
            text: 'Question',
          },
          {
            role: 'user',
            kind: 'context',
            origin: 'request',
            item: 1,
            part: 0,
            text: env,
          },
        ],
        replies: [],
      },
      response: '{}',
      output: '',
    },
  }
  expect(responsesTurnMessages(turn, false).map((m) => m.body)).toEqual([
    'Question',
  ])
  expect(
    responsesTurnMessages(turn, true).find((m) => m.body === env)?.kind
  ).toBe('context')
})

it('previews long text without losing the full copy or keyboard expansion', async () => {
  const user = userEvent.setup()
  const value = `${'fixture text '.repeat(400)}end of fixture`
  render(<ConversationContent label='fixture' value={value} />)
  expect(screen.queryByText(value)).not.toBeInTheDocument()
  const expand = screen.getByRole('button', { name: 'Show full text' })
  expand.focus()
  await user.keyboard('{Enter}')
  expect(await screen.findByText(value)).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Show less text' })
  ).toHaveAttribute('aria-expanded', 'true')
  await user.click(screen.getByRole('button', { name: 'Copy fixture' }))
  expect(await navigator.clipboard.readText()).toBe(value)
})

it('clears conversation queries when the dialog closes and keeps chat records compatible', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...root,
        record: { ...record, kind: 'chat', truncated: true },
        content: { ...root.content, output: 'chat reply fixture' },
      },
    },
  })
  const client = new QueryClient()
  render(
    <QueryClientProvider client={client}>
      <ConversationDetailsDialog entry={auditEntry} />
    </QueryClientProvider>
  )
  await userEvent.click(screen.getByRole('button', { name: 'Details' }))
  expect(await screen.findByText('chat reply fixture')).toBeVisible()
  expect(
    screen.queryByRole('tab', { name: 'Conversation replay' })
  ).not.toBeInTheDocument()
  expect(
    screen.getByText('Recording limit reached. This record is partial.')
  ).toBeVisible()
  await userEvent.keyboard('{Escape}')
  await waitFor(() =>
    expect(
      client.getQueryCache().findAll({ queryKey: ['conversation'] })
    ).toHaveLength(0)
  )
})

it('shows a recording failure without rendering stale conversation content', async () => {
  vi.spyOn(api, 'get').mockRejectedValue(new Error('fixture failure'))
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ConversationDetailsDialog entry={auditEntry} />
    </QueryClientProvider>
  )
  await userEvent.click(screen.getByRole('button', { name: 'Details' }))
  expect(
    await screen.findByText(
      'Conversation unavailable, expired, or access denied.'
    )
  ).toBeVisible()
  expect(
    screen.queryByRole('tab', { name: 'Latest exchange' })
  ).not.toBeInTheDocument()
})
