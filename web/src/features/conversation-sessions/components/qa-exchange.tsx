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
import { useTranslation } from 'react-i18next'

import { ConversationContent } from '@/features/usage-logs/audit/components/conversation-content'

import type { QATurn } from '../qa-api'

export function QAExchange(props: { turn: QATurn }) {
  const { t } = useTranslation()
  return (
    <article className='min-w-0 space-y-4 rounded-lg border p-4'>
      <ConversationContent label={t('User prompt')} value={props.turn.prompt} />
      {props.turn.final ? (
        <ConversationContent
          label={t('Final reply')}
          value={props.turn.final}
        />
      ) : (
        <p className='text-muted-foreground text-sm'>
          {t('No confirmed final reply')}
        </p>
      )}
    </article>
  )
}
