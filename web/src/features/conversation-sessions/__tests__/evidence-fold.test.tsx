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
import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'

import { EvidenceList } from '../components/qa-evidence-list'
import type { QAObservation } from '../qa-api'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: { children: React.ReactNode }) => <span>{props.children}</span>,
}))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
it('keeps every request and its own result when expanding two separate repeated runs', () => {
  const rows: QAObservation[] = Array.from({ length: 58 }, (_, i) => ({
    id: i + 1,
    request_id: `r${i}`,
    user_id: 14,
    created_at: 1,
    chat_key: i < 29 ? 'a' : 'b',
    prompt: 'title',
    final: i === 28 ? 'one terminal' : '',
    http_status: i === 28 ? 200 : 503,
    outcome: i === 28 ? 'final' : 'http_error',
    input_state: 'text',
    basis: 'direct',
    chat_basis: 'client',
  }))
  render(<EvidenceList rows={rows} />)
  expect(screen.getAllByText('Repeated requests')).toHaveLength(2)
  fireEvent.click(screen.getByRole('button', { name: 'Expand all' }))
  expect(screen.getAllByRole('article')).toHaveLength(58)
  expect(screen.getByText('one terminal')).toBeVisible()
})

it('reads six linked questions while retaining all forty request IDs', () => {
  const rows: QAObservation[] = []
  for (const [index, count] of [7, 7, 9, 5, 7, 5].entries()) {
    for (let j = 0; j < count; j++) {
      rows.push({
        id: rows.length + 1,
        request_id: `request-${rows.length}`,
        user_id: 14,
        turn_id: index + 1,
        chat_key: 'chat',
        chat_basis: 'client',
        basis: 'direct',
        created_at: 100 + rows.length,
        prompt: j === 0 ? `Q${index}` : '',
        final: j === count - 1 ? `A${index}` : '',
        outcome: j === count - 1 ? 'final' : 'continuation',
        input_state: j === 0 ? 'text' : 'no_input',
      })
    }
  }
  // Two turns overlap. Expanding all must restore the original request order.
  const arrivalOrder = [rows[0], rows[7], ...rows.slice(1, 7), ...rows.slice(8)]
  render(<EvidenceList rows={arrivalOrder} chat />)
  expect(screen.getAllByRole('article')).toHaveLength(6)
  expect(screen.getByText('A5')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Expand all' }))
  expect(screen.getAllByRole('article').map((article) => article.id)).toEqual(
    arrivalOrder.map((row) => `qa-evidence-${row.id}`)
  )
})
