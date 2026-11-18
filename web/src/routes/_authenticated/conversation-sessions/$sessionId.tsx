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
import { createFileRoute, Navigate, redirect } from '@tanstack/react-router'

import { SectionPageLayout } from '@/components/layout'
import { QAReader } from '@/features/conversation-sessions/components/qa-reader'
import { SessionReader } from '@/features/conversation-sessions/components/session-reader'
import { useStatus } from '@/hooks/use-status'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute(
  '/_authenticated/conversation-sessions/$sessionId'
)({
  beforeLoad: ({ params }) => {
    const { auth } = useAuthStore.getState()
    if (auth.user?.role !== ROLE.SUPER_ADMIN) {
      throw redirect({ to: '/403' })
    }
    if (!/^(?:[a-f0-9]{64}|[1-9][0-9]{0,17})$/.test(params.sessionId)) {
      throw redirect({ to: '/conversation-sessions' })
    }
  },
  component: SessionReaderPage,
})

function SessionReaderPage() {
  const { sessionId } = Route.useParams()
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Content>
        {sessionId.length === 64 ? (
          <QAReader chat={sessionId} />
        ) : (
          <LegacyReader id={sessionId} />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
function LegacyReader(props: { id: string }) {
  const { status } = useStatus()
  return status?.conversation_qa === false ? (
    <SessionReader sessionId={Number(props.id)} />
  ) : (
    <Navigate to='/conversation-sessions' />
  )
}
