// What a family member does, in a browser: a tap answers at once and the
// page stays where it was (no reload: a mark set on the window survives),
// a refusal is said on the page, a note is written, the page follows a
// change made elsewhere. No script error on any page.
const { test, expect } = require('@playwright/test');

let errors;
test.beforeEach(async ({ page, request }) => {
  errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  await request.get('/demo/fehler?status=0');
  await request.get('/demo/setze?id=light.flur&state=off&name=Flurlicht');
});
test.afterEach(() => expect(errors, 'script errors').toEqual([]));

const stays = (page) => page.evaluate(() => window.hausBleibt === 1);
const mark = (page) => page.evaluate(() => { window.hausBleibt = 1; });

test('a switch flips at once and the page stays', async ({ page }) => {
  await page.goto('/raeume');
  await mark(page);
  const flur = page.getByRole('switch', { name: 'Flurlicht' });
  await expect(flur).toHaveAttribute('aria-checked', 'false');
  await flur.click();
  await expect(page.getByRole('switch', { name: 'Flurlicht' })).toHaveAttribute('aria-checked', 'true');
  expect(await stays(page), 'no reload').toBe(true);
  await expect(page).toHaveURL(/\/raeume/);
});

test('a refusal is said on the page and the switch goes back', async ({ page, request }) => {
  await request.get('/demo/fehler?status=503');
  await page.goto('/raeume');
  await mark(page);
  await page.getByRole('switch', { name: 'Flurlicht' }).click();
  await expect(page.locator('#haus-hinweis')).toContainText('Das hat nicht geklappt');
  await expect(page.getByRole('switch', { name: 'Flurlicht' })).toHaveAttribute('aria-checked', 'false');
  expect(await stays(page)).toBe(true);
});

test('the page follows a change made elsewhere', async ({ page, request }) => {
  await page.goto('/raeume');
  await mark(page);
  await request.get('/demo/setze?id=light.flur&state=on&name=Flurlicht');
  await expect(page.getByRole('switch', { name: 'Flurlicht' })).toHaveAttribute('aria-checked', 'true', { timeout: 25_000 });
  expect(await stays(page), 'refreshed in place').toBe(true);
});

test('a player pauses in place', async ({ page }) => {
  await page.goto('/medien');
  await mark(page);
  await page.getByRole('button', { name: 'Pause' }).first().click();
  await expect(page.getByRole('button', { name: 'Weiter' }).first()).toBeVisible();
  expect(await stays(page)).toBe(true);
});

test('a note is written and changed', async ({ page }) => {
  await page.goto('/notizen');
  await expect(page.getByText('Merk dir')).toBeVisible();
  await page.getByText('Neue Notiz').click();
  await page.getByLabel('Titel').fill('Wasserzähler');
  await page.getByLabel('Text', { exact: true }).fill('Nummer 1234');
  await mark(page);
  await page.getByRole('button', { name: 'Anlegen' }).click();
  await expect(page.getByRole('status')).toContainText('Notiz angelegt.');
  await expect(page).toHaveURL(/\/notizen\/wasserzaehler\.md\?gesagt=angelegt/);
  expect(await stays(page)).toBe(true);

  await page.getByRole('link', { name: 'Bearbeiten' }).click();
  const field = page.locator('textarea[name=text]');
  await field.fill('# Wasserzähler\n\nNummer 1234, im Keller links');
  await page.getByRole('button', { name: 'Speichern' }).click();
  await expect(page.getByRole('status')).toContainText('Gespeichert.');
  await expect(page.getByText('im Keller links')).toBeVisible();
});

// Pictures of the main pages as a phone shows them, for a look at the
// run (the CI keeps them).
for (const path of ['/start', '/raeume', '/medien', '/notizen', '/mehr']) {
  test(`picture of ${path}`, async ({ page }, info) => {
    await page.goto(path);
    await page.screenshot({ path: info.outputPath(path.slice(1) + '.png'), fullPage: true });
  });
}
