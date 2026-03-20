import { test, expect, type ConsoleMessage } from '@playwright/test';

/**
 * Tests that pages load and reload without producing JavaScript errors.
 * Catches Svelte lifecycle regressions (e.g., accessing destroyed components,
 * missing cleanup, SSE reconnection issues) that only surface in a real browser.
 */

/** Routes to test — covers all major UI pages. */
const ROUTES = ['/ui/dashboard', '/ui/detections', '/ui/analytics', '/ui/settings'];

/**
 * Known error patterns to ignore (e.g., expected fetch failures for optional
 * endpoints). Add patterns here only after confirming they are harmless.
 */
const IGNORED_ERROR_PATTERNS: RegExp[] = [
  // SSE connections fire an error event when the page unloads (reload/navigation).
  // This is expected browser behavior, not an application bug.
  /SSE connection error/,
  // Chromium-internal permissions policy warning, not from application code.
  /Permissions policy violation/,
];

function isIgnoredError(message: string): boolean {
  return IGNORED_ERROR_PATTERNS.some(pattern => pattern.test(message));
}

interface CollectedError {
  type: 'pageerror' | 'console.error';
  message: string;
}

/**
 * Helper: attach error listeners, run an action, return collected errors.
 */
async function collectErrorsDuring(
  page: import('@playwright/test').Page,
  action: () => Promise<void>
): Promise<CollectedError[]> {
  const errors: CollectedError[] = [];

  const pageErrorHandler = (error: Error) => {
    if (!isIgnoredError(error.message)) {
      errors.push({ type: 'pageerror', message: error.message });
    }
  };
  const consoleHandler = (msg: ConsoleMessage) => {
    if (msg.type() === 'error' && !isIgnoredError(msg.text())) {
      errors.push({ type: 'console.error', message: msg.text() });
    }
  };

  page.on('pageerror', pageErrorHandler);
  page.on('console', consoleHandler);

  try {
    await action();
  } finally {
    page.off('pageerror', pageErrorHandler);
    page.off('console', consoleHandler);
  }

  return errors;
}

test.describe('Console Error Regression Tests', () => {
  for (const route of ROUTES) {
    const routeName = route.replace('/ui/', '');

    test(`${routeName}: no console errors on initial load`, async ({ page }) => {
      const errors = await collectErrorsDuring(page, async () => {
        await page.goto(route);
        await page.waitForLoadState('domcontentloaded');
        // Give async effects and SSE connections time to settle
        await page.waitForTimeout(2000);
      });

      expect(errors, `Unexpected errors on initial load of ${route}`).toEqual([]);
    });

    test(`${routeName}: no console errors after reload`, async ({ page }) => {
      // Initial load
      await page.goto(route);
      await page.waitForLoadState('domcontentloaded');
      // Wait for the page to fully initialize (SSE, effects, etc.)
      await page.waitForTimeout(2000);

      // Now capture errors during reload
      const errors = await collectErrorsDuring(page, async () => {
        await page.reload();
        await page.waitForLoadState('domcontentloaded');
        // Wait for re-initialization after reload
        await page.waitForTimeout(2000);
      });

      expect(errors, `Unexpected errors after reloading ${route}`).toEqual([]);

      // Verify page still renders correctly after reload
      const mainContent = page.locator('main, [role="main"], [data-testid="main-content"]');
      await expect(mainContent.first()).toBeVisible();
    });
  }

  test('dashboard: no console errors across multiple rapid reloads', async ({ page }) => {
    await page.goto('/ui/dashboard');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForTimeout(1000);

    const errors = await collectErrorsDuring(page, async () => {
      // Rapid reload cycle — stresses cleanup/teardown paths
      for (let i = 0; i < 3; i++) {
        await page.reload();
        await page.waitForLoadState('domcontentloaded');
        await page.waitForTimeout(1000);
      }
    });

    expect(errors, 'Unexpected errors during rapid reload cycle').toEqual([]);

    // Page should still be functional
    const mainContent = page.locator('main, [role="main"], [data-testid="main-content"]');
    await expect(mainContent.first()).toBeVisible();
  });

  test('dashboard: no console errors when navigating away and back', async ({ page }) => {
    // Load dashboard
    await page.goto('/ui/dashboard');
    await page.waitForLoadState('domcontentloaded');
    await page.waitForTimeout(1000);

    const errors = await collectErrorsDuring(page, async () => {
      // Navigate away to settings
      await page.goto('/ui/settings');
      await page.waitForLoadState('domcontentloaded');
      await page.waitForTimeout(1000);

      // Navigate back to dashboard
      await page.goto('/ui/dashboard');
      await page.waitForLoadState('domcontentloaded');
      await page.waitForTimeout(2000);
    });

    expect(errors, 'Unexpected errors during navigate-away-and-back').toEqual([]);

    // Dashboard should render correctly
    const mainContent = page.locator('main, [role="main"], [data-testid="main-content"]');
    await expect(mainContent.first()).toBeVisible();
  });
});
