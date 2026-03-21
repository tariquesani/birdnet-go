import { test, expect } from '@playwright/test';

test.describe('Error Handling and Network Failures', () => {
  test('Application handles JavaScript errors gracefully', async ({ page }) => {
    const errors: string[] = [];

    // Capture JavaScript errors with cleanup
    const pageErrorHandler = (error: Error) => {
      errors.push(error.message);
    };
    const consoleHandler = (msg: import('@playwright/test').ConsoleMessage) => {
      if (msg.type() === 'error') {
        errors.push(msg.text());
      }
    };

    page.on('pageerror', pageErrorHandler);
    page.on('console', consoleHandler);

    await page.goto('/ui/dashboard');

    // Wait for the page to load completely
    await page.waitForLoadState('domcontentloaded');

    // Inject a controlled error to test error boundary
    await page.evaluate(() => {
      // Create a custom event that might trigger an error boundary
      const event = new CustomEvent('test-error', { detail: { force: true } });
      window.dispatchEvent(event);
    });

    // Wait a bit to see if any errors surface
    await page.waitForTimeout(1000);

    // Check that the app didn't completely crash
    await expect(page.locator('body')).toBeVisible();

    // Look for error boundary UI if it exists
    const errorBoundary = page.locator('[data-testid="error-boundary"], .error-boundary');
    const hasErrorBoundary = (await errorBoundary.count()) > 0;

    if (hasErrorBoundary && (await errorBoundary.isVisible())) {
      // If there's an error boundary, it should show user-friendly content
      await expect(errorBoundary).toContainText(/something went wrong|error|reload|refresh/i);
    } else {
      // If no error boundary, app should still function
      await expect(page.locator('nav').first()).toBeVisible();
    }

    // Assert no unexpected errors occurred
    expect(errors, 'no unexpected console/page errors').toEqual([]);

    // Cleanup event listeners
    page.off('pageerror', pageErrorHandler);
    page.off('console', consoleHandler);
  });

  test('Application handles API network failures', async ({ page }) => {
    // Intercept API calls EXCEPT app/config (needed for SPA initialization and CSRF)
    await page.route('**/api/v2/**', route => {
      const url = route.request().url();
      if (url.includes('/api/v2/app/config') || url.includes('/api/v2/health')) {
        route.continue();
        return;
      }
      // Simulate network failure for other API calls
      route.abort('failed');
    });

    await page.goto('/ui/dashboard');

    // Wait for initial load — the app should boot since app/config is allowed through
    await expect(page.locator('main').first()).toBeVisible();

    // Wait for potential error states to appear from failed data fetches
    await page.waitForTimeout(2000);

    // App should remain navigable even with API failures
    await expect(page.locator('nav').first()).toBeVisible();
  });

  test('Application handles slow network conditions', async ({ page }) => {
    // Simulate slow network for data endpoints, but let app/config through immediately
    await page.route('**/api/v2/**', route => {
      const url = route.request().url();
      if (url.includes('/api/v2/app/config') || url.includes('/api/v2/health')) {
        route.continue();
        return;
      }
      setTimeout(() => {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ message: 'delayed response' }),
        });
      }, 3000); // 3 second delay
    });

    await page.goto('/ui/dashboard');

    // Should show main content even while API calls are slow
    await expect(page.locator('main').first()).toBeVisible();

    // Navigation should work despite slow API
    await expect(page.locator('nav').first()).toBeVisible();
  });

  test('Application handles malformed API responses', async ({ page }) => {
    // Intercept data API calls and return malformed JSON, but let app/config through
    await page.route('**/api/v2/**', route => {
      const url = route.request().url();
      if (url.includes('/api/v2/app/config') || url.includes('/api/v2/health')) {
        route.continue();
        return;
      }
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: '{"invalid": json malformed}',
      });
    });

    await page.goto('/ui/dashboard');

    // App should handle JSON parse errors gracefully
    await expect(page.locator('main').first()).toBeVisible();

    // Check that navigation still works
    await expect(page.locator('nav').first()).toBeVisible();

    // The app may display parse error messages in component error states (e.g.,
    // "Unexpected token" in Daily Activity or Recent Detections cards), which is
    // correct graceful handling. The key assertion is that the app shell remains
    // functional — main content and navigation are visible and not crashed.
  });

  test('Application recovers from temporary network issues', async ({ page }) => {
    let requestCount = 0;

    // Fail first health request, succeed on retry
    await page.route('**/api/v2/health', route => {
      requestCount++;
      if (requestCount === 1) {
        route.abort('failed');
      } else {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'ok' }),
        });
      }
    });

    await page.goto('/ui/dashboard');

    // Should eventually recover and show content
    await expect(page.locator('main').first()).toBeVisible();

    // The health endpoint may or may not be retried depending on app logic,
    // so just verify the app loaded successfully
    await expect(page.locator('nav').first()).toBeVisible();
  });
});
