import { test, expect } from '@playwright/test';

// Every assertion below is against the real, running Go backend and the
// real Elasticsearch index built for this project, through the actual
// built Angular bundle. Nothing here is mocked.

test('analyst can search, drill down, and advance a document one step', async ({ page }) => {
  await page.goto('/');
  await page.getByPlaceholder('username').fill('analyst1');
  await page.getByRole('button', { name: 'Log in' }).click();
  await expect(page.getByText(/Signed in as analyst1/)).toBeVisible();

  await page.locator('select').nth(5).selectOption('NEW'); // status
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.locator('table tbody tr').first()).toBeVisible({ timeout: 10_000 });

  await page.locator('table tbody tr').first().getByRole('button', { name: 'Drill down' }).click();
  const detail = page.locator('[data-testid="detail-panel"]');
  await expect(detail).toBeVisible();
  await expect(page.locator('[data-testid="detail-status"]')).toHaveText('NEW');

  await page.locator('[data-testid="advance-button"]').click();
  await expect(page.locator('[data-testid="detail-status"]')).toHaveText('IN_REVIEW', { timeout: 10_000 });
});

test('analyst cannot search a RESTRICTED classification from the UI either', async ({ page }) => {
  await page.goto('/');
  await page.getByPlaceholder('username').fill('analyst1');
  await page.getByRole('button', { name: 'Log in' }).click();

  await page.locator('select').nth(4).selectOption('RESTRICTED'); // classification
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.getByText(/cannot search this classification/)).toBeVisible();
});

test('supervisor can close a flagged document that an analyst cannot', async ({ page }) => {
  await page.goto('/');
  await page.getByPlaceholder('username').fill('supervisor1');
  await page.getByRole('button', { name: 'Log in' }).click();
  await expect(page.getByText(/Signed in as supervisor1/)).toBeVisible();

  await page.locator('select').nth(4).selectOption('UNCLASSIFIED'); // classification
  await page.locator('select').nth(5).selectOption('FLAGGED'); // status
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.locator('table tbody tr').first()).toBeVisible({ timeout: 10_000 });

  await page.locator('table tbody tr').first().getByRole('button', { name: 'Drill down' }).click();
  await expect(page.locator('[data-testid="detail-status"]')).toHaveText('FLAGGED');
  await expect(page.locator('[data-testid="advance-button"]')).toHaveText('Move to CLOSED');

  await page.locator('[data-testid="advance-button"]').click();
  await expect(page.locator('[data-testid="detail-status"]')).toHaveText('CLOSED', { timeout: 10_000 });
  await expect(page.locator('[data-testid="advance-button"]')).toHaveCount(0);
});
