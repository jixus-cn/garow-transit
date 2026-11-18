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
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from '@/components/ui/collapsible'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { QAObservation } from '../qa-api'
import { ConversationContent } from './conversation-content'

export function LinkedTurn(props: {
  rows: QAObservation[]
  children: ReactNode
}) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const inputs = props.rows.filter((r) => r.prompt)
  const finals = props.rows.filter((r) => r.final)
  const conflict = props.rows.some((r) => r.basis === 'ambiguous')
  const locale = toIntlLocale(i18n?.resolvedLanguage ?? 'en')
  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className='space-y-2 rounded-lg border p-3'
    >
      {!open && (
        <article className='space-y-3'>
          {conflict && <p>{t('Conflicting evidence')}</p>}
          {inputs.map((r) => (
            <ConversationContent
              key={r.id}
              label={t('Request input')}
              value={r.prompt}
            />
          ))}
          {finals.map((r) => (
            <ConversationContent
              key={r.id}
              label={t('Final reply')}
              value={r.final}
            />
          ))}
          {inputs.length > 0 && finals.length === 0 && (
            <span className='text-muted-foreground text-sm'>
              {t('No confirmed final reply')}
            </span>
          )}
        </article>
      )}
      <CollapsibleTrigger render={<Button size='sm' variant='outline' />}>
        {t('Requests')} · {formatNumber(props.rows.length, locale)}
      </CollapsibleTrigger>
      <CollapsibleContent className='space-y-2'>
        {props.children}
      </CollapsibleContent>
    </Collapsible>
  )
}
