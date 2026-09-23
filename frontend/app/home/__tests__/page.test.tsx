/**
 * Tests for the "Select assessment period" panel on the Member Home page.
 *
 * Covers:
 *  - Automatic period selection based on the current date.
 *  - Rendering the bordered "Select assessment period" panel inside the blue Current Period card.
 *  - Opening/using the dropdown and selecting a previous period (e.g. H1 2026).
 *  - The selected period is passed into the survey via the `?period=` query param on "Take Survey".
 *  - Default behavior (auto period passed through) when the user does not change the dropdown.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import MemberHomePage from '../page';

const push = vi.fn();
const router = { push };

vi.mock('next/navigation', () => ({
  useRouter: () => router,
}));

vi.mock('@/lib/auth', () => ({
  getCurrentUser: () => ({
    id: 'user-1',
    username: 'demo',
    fullName: 'Demo User',
    hierarchyLevel: 'level-5',
    hierarchyLevelId: 'level-5',
    teamIds: ['team-1'],
  }),
  logout: vi.fn(),
  authenticatedFetch: vi.fn().mockResolvedValue({ ok: true, json: async () => ({ surveyHistory: [] }) }),
}));

vi.mock('@/lib/org-config', () => ({
  getOrgConfig: () => ({}),
  getHierarchyLevel: () => ({ id: 'level-5', name: 'Team Member' }),
}));

const getTeamInfoCached = vi.fn();
vi.mock('@/lib/api/teams', () => ({
  getTeamInfoCached: (...args: unknown[]) => getTeamInfoCached(...args),
}));

vi.mock('@/components/OnboardingModal', () => ({
  default: () => null,
}));

const TEAM_INFO = {
  id: 'team-1',
  name: 'Falcons',
  cadence: 'half-yearly',
  members: [],
};

describe('Member Home: Select assessment period panel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    vi.setSystemTime(new Date(2026, 8, 22)); // Sep 22, 2026 -> auto period "2026 H2"
    getTeamInfoCached.mockResolvedValue(TEAM_INFO);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }));
  });

  it('auto-selects the current-date period and renders it as the dropdown default', async () => {
    render(<MemberHomePage />);

    await waitFor(() => expect(screen.getByTestId('assessment-period-select')).toHaveValue('2026 H2'));
  });

  it('displays the auto-detected period in the Current Period label', async () => {
    render(<MemberHomePage />);

    await waitFor(() => expect(screen.getByTestId('current-period')).toHaveTextContent('Current Period: 2026 H2'));
  });

  it('keeps showing the auto-detected period in the Current Period label after the user overrides the dropdown', async () => {
    const user = userEvent.setup({ delay: null });
    render(<MemberHomePage />);

    await waitFor(() => expect(screen.getByTestId('assessment-period-select')).toHaveValue('2026 H2'));
    await user.selectOptions(screen.getByTestId('assessment-period-select'), '2026 H1');

    // The dropdown reflects the override...
    expect(screen.getByTestId('assessment-period-select')).toHaveValue('2026 H1');
    // ...but the Current Period label must remain the auto-detected value, never the selection
    expect(screen.getByTestId('current-period')).toHaveTextContent('Current Period: 2026 H2');
  });

  it('renders the bordered "Select assessment period" panel with a labeled dropdown and helper text', async () => {
    render(<MemberHomePage />);

    await waitFor(() => expect(screen.getByTestId('assessment-period-panel')).toBeInTheDocument());
    expect(screen.getByText('Select assessment period')).toBeInTheDocument();
    expect(screen.getByLabelText('Assessment period')).toBeInTheDocument();
    expect(screen.getByText(/selected automatically based on today's date/i)).toBeInTheDocument();
    expect(screen.getByTestId('take-survey-btn')).toBeInTheDocument();
  });

  it('lets the user select a previous period (e.g. H1 2026) from the dropdown', async () => {
    const user = userEvent.setup({ delay: null });
    render(<MemberHomePage />);

    await waitFor(() => expect(screen.getByTestId('assessment-period-select')).toHaveValue('2026 H2'));

    await user.selectOptions(screen.getByTestId('assessment-period-select'), '2026 H1');
    expect(screen.getByTestId('assessment-period-select')).toHaveValue('2026 H1');
  });

  it('passes the selected (overridden) period into the survey via the query param on "Take Survey"', async () => {
    const user = userEvent.setup({ delay: null });
    render(<MemberHomePage />);

    await waitFor(() => expect(screen.getByTestId('assessment-period-select')).toHaveValue('2026 H2'));
    await user.selectOptions(screen.getByTestId('assessment-period-select'), '2026 H1');

    await user.click(screen.getByTestId('take-survey-btn'));

    expect(push).toHaveBeenCalledWith('/survey?period=2026%20H1');
  });

  it('passes the auto-detected period into the survey when the dropdown is left unchanged (default behavior)', async () => {
    const user = userEvent.setup({ delay: null });
    render(<MemberHomePage />);

    await waitFor(() => expect(screen.getByTestId('assessment-period-select')).toHaveValue('2026 H2'));

    await user.click(screen.getByTestId('take-survey-btn'));

    expect(push).toHaveBeenCalledWith('/survey?period=2026%20H2');
  });
});
