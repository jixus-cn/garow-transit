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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { DetailSection } from '@/features/usage-logs/components/dialogs/log-detail-layout'

export function ConversationText(props: { value: string; label: string }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const text = props.value
  if (!text) {
    return (
      <p className='text-muted-foreground text-sm'>{t('No recorded text')}</p>
    )
  }
  const long = text.length > 4000
  return (
    <div className='min-w-0 space-y-2'>
      <CopyButton
        value={text}
        size='sm'
        aria-label={t('Copy {{label}}', { label: props.label })}
      />
      <pre className='max-h-80 min-w-0 overflow-y-auto font-sans text-sm leading-relaxed break-words whitespace-pre-wrap'>
        {long && !expanded ? `${text.slice(0, 4000)}…` : text}
      </pre>
      {long && (
        <Button
          variant='outline'
          size='sm'
          aria-expanded={expanded}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? t('Show less text') : t('Show full text')}
        </Button>
      )}
    </div>
  )
}

export function ConversationContent(props: { label: string; value: string }) {
  return (
    <DetailSection label={props.label}>
      <ConversationText value={props.value} label={props.label} />
    </DetailSection>
  )
}
