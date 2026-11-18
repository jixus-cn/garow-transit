import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it } from 'vitest'

import type { ConversationMessage } from '../api'
import { MessageBubble } from '../components/message-bubble'

it('keeps client context folded and out of the user label while retaining inspectable evidence', async () => {
  const context = {
    id: 1,
    role: 'user',
    kind: 'context',
    origin: 'request',
    content: 'fixture IDE context',
    request_id: 'r',
    created_at: 1,
    item_index: 0,
    part_index: 0,
  } as ConversationMessage
  render(<MessageBubble message={context} final={false} />)
  expect(screen.queryByText('fixture IDE context')).not.toBeInTheDocument()
  expect(screen.queryByText('Client-submitted text')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /Client context/ }))
  expect(await screen.findByText('fixture IDE context')).toBeVisible()
})
