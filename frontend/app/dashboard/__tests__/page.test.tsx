/**
 * Tests for the Team Lead assessment-period selection on the dashboard, added for the
 * "Take Survey" and "Post-Workshop Survey" buttons.
 *
 * Covers:
 *  - Automatic period selection is the dropdown default.
 *  - The Team Lead can select a previous period.
 *  - "Take Survey" passes the selected period into the survey via the query param.
 *  - "Post-Workshop Survey" passes the selected period (and survey type) into the survey.
 *  - Default (unchanged) behavior when the dropdown is left at auto-detection.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import DashboardPage from '../page';

const push = vi.fn();
const router = { push };

vi.mock('next/navigation', () => ({
  useRouter: () => router,
}));

vi.mock('@/lib/auth', () => ({
  getCurrentUser: () => ({
    id: 'lead-1',
    name: 'Taylor Lead',
    hierarchyLevelId: 'level-4',
    teamIds: ['team-1'],
  }),
  logout: vi.fn(),
  authenticatedFetch: vi.fn().mockResolvedValue({ ok: false, status: 404 }),
}));

vi.mock('@/lib/org-config', () => ({
  getOrgConfig: () => ({ companyName: 'Acme' }),
  getHierarchyLevel: () => ({ id: 'level-4', name: 'Team Lead' }),
  getUserPermissions: () => ({}),
}));

const getTeamInfoCached = vi.fn();
vi.mock('@/lib/api/teams', () => ({
  getTeamInfoCached: (...args: unknown[]) => getTeamInfoCached(...args),
}));

vi.mock('@/lib/api/health-checks', () => ({
  getTeamSubmissionStatus: vi.fn().mockResolvedValue({
    teamId: 'team-1',
    assessmentPeriod: '2026 H2',
    totalMembers: 1,
    submittedMembers: 0,
    allSubmitted: false,
    postWorkshopExists: false,
  }),
  getAssessmentPeriods: vi.fn().mockResolvedValue([]),
}));

vi.mock('@/components/OnboardingModal', () => ({ default: () => null }));
vi.mock('@/components/ActionItemsTab', () => ({ default: () => null }));

const TEAM_INFO = {
  id: 'team-1',
  name: 'Falcons',
  cadence: 'half-yearly',
  members: [],
};

describe('Team Lead dashboard: assessment period for Take Survey / Post-Workshop Survey', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.setSystemTime(new Date(2026, 8, 22)); // Sep 22, 2026 -> auto period "2026 H2"
    getTeamInfoCached.mockResolvedValue(TEAM_INFO);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }));
  });

  it('auto-selects the current-date period as the dropdown default', async () => {
    render(<DashboardPage />);

    await waitFor(() => expect(screen.getByTestId('take-survey-period-select')).toHaveValue('2026 H2'));
  });

  it('lets the Team Lead select a previous period (e.g. H1 2026)', async () => {
    const user = userEvent.setup({ delay: null });
    render(<DashboardPage />);

    await waitFor(() => expect(screen.getByTestId('take-survey-period-select')).toHaveValue('2026 H2'));
    await user.selectOptions(screen.getByTestId('take-survey-period-select'), '2026 H1');

    expect(screen.getByTestId('take-survey-period-select')).toHaveValue('2026 H1');
  });

  it('passes the selected period into the survey via "Take Survey"', async () => {
    const user = userEvent.setup({ delay: null });
    render(<DashboardPage />);

    await waitFor(() => expect(screen.getByTestId('take-survey-period-select')).toHaveValue('2026 H2'));
    await user.selectOptions(screen.getByTestId('take-survey-period-select'), '2026 H1');
    await user.click(screen.getByTestId('take-survey-button'));

    expect(push).toHaveBeenCalledWith('/survey?team=team-1&period=2026+H1');
  });

  it('passes the selected period and survey type into the survey via "Post-Workshop Survey"', async () => {
    const user = userEvent.setup({ delay: null });
    render(<DashboardPage />);

    await waitFor(() => expect(screen.getByTestId('take-survey-period-select')).toHaveValue('2026 H2'));
    await user.selectOptions(screen.getByTestId('take-survey-period-select'), '2026 H1');
    await user.click(screen.getByTestId('post-workshop-survey-button'));

    expect(push).toHaveBeenCalledWith('/survey?team=team-1&type=post_workshop&period=2026+H1');
  });

  it('passes the auto-detected period into both surveys when the dropdown is left unchanged (default behavior)', async () => {
    const user = userEvent.setup({ delay: null });
    render(<DashboardPage />);

    await waitFor(() => expect(screen.getByTestId('take-survey-period-select')).toHaveValue('2026 H2'));

    await user.click(screen.getByTestId('take-survey-button'));
    expect(push).toHaveBeenCalledWith('/survey?team=team-1&period=2026+H2');

    await user.click(screen.getByTestId('post-workshop-survey-button'));
    expect(push).toHaveBeenCalledWith('/survey?team=team-1&type=post_workshop&period=2026+H2');
  });
});
