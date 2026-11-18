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
import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'

import { QAExchange } from '../components/qa-exchange'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
describe('question and final reply', () => {
  it('shows only literal prompt and final from a turn even with extra legacy data', () => {
    const turn = {
      id: 1,
      user_id: 1,
      chat_key: 'chat',
      created_at: 1,
      prompt: 'actual <tag>',
      final: 'answer',
      output: 'tool text',
    }
    render(<QAExchange turn={turn} />)
    expect(screen.getByText('actual <tag>')).toBeVisible()
    expect(screen.getByText('answer')).toBeVisible()
    expect(screen.queryByText('tool text')).toBeNull()
    expect(screen.getAllByRole('button')).toHaveLength(2)
  })
  it('does not give an interrupted reply a copy button', () => {
    render(
      <QAExchange
        turn={{
          id: 2,
          user_id: 1,
          chat_key: '',
          created_at: 1,
          prompt: 'question',
          final: '',
        }}
      />
    )
    expect(screen.getByText('No confirmed final reply')).toBeVisible()
    expect(screen.getAllByRole('button')).toHaveLength(1)
  })
})
