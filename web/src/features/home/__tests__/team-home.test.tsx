import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
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
import { render, screen, cleanup } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { TeamHome } from '../components/sections/team-home'

afterEach(cleanup)

function renderHome(authenticated: boolean) {
  const root = createRootRoute()
  const index = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => <TeamHome isAuthenticated={authenticated} />,
  })
  const router = createRouter({
    routeTree: root.addChildren([index]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return render(<RouterProvider router={router} />)
}

it('directs signed-out members to sign in from each workspace entry', async () => {
  renderHome(false)
  expect(
    await screen.findByRole('button', { name: 'Member sign in' })
  ).toHaveAttribute('href', '/sign-in')
  for (const label of ['Work overview', 'Review activity', 'Personal space']) {
    expect(
      screen.getByRole('link', { name: new RegExp(label) })
    ).toHaveAttribute('href', '/sign-in')
  }
  expect(
    screen.getByRole('button', { name: 'Explore the workspace' })
  ).toHaveAttribute('href', '#team-workflows')
})

it('directs members to workspace, activity and their profile', async () => {
  renderHome(true)
  expect(
    await screen.findByRole('button', { name: 'Enter workspace' })
  ).toHaveAttribute('href', '/dashboard')
  expect(screen.getByRole('link', { name: /Review activity/ })).toHaveAttribute(
    'href',
    '/usage-logs'
  )
  expect(screen.getByRole('link', { name: /Personal space/ })).toHaveAttribute(
    'href',
    '/profile'
  )
  expect(
    screen.queryByText(/pricing|API gateway|buy now/i)
  ).not.toBeInTheDocument()
})
