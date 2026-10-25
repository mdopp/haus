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

test('the page follows a change made elsewhere at once (push)', async ({ page, request }) => {
  await page.goto('/raeume');
  await mark(page);
  await page.waitForTimeout(500); // the stream is open
  await request.get('/demo/setze?id=light.flur&state=on&name=Flurlicht');
  await expect(page.getByRole('switch', { name: 'Flurlicht' })).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });
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
  await expect(page.getByText('Merk dir').first()).toBeVisible();
  await page.locator('summary', { hasText: 'Neue Notiz' }).click();
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

test('a note gets a kind and tags and is found by its tag', async ({ page }) => {
  await page.goto('/notizen');
  await page.locator('summary', { hasText: 'Neue Notiz' }).click();
  await page.getByLabel('Titel').fill('Lasagne');
  await page.getByLabel('Text', { exact: true }).fill('Nudeln, Tomaten');
  await page.getByLabel('Art').selectOption('rezept');
  await page.getByLabel('#kochen').check();
  await page.getByLabel('Weitere Tags, mit Komma getrennt').fill('italienisch');
  await page.getByRole('button', { name: 'Anlegen' }).click();
  await expect(page.getByRole('status')).toContainText('Notiz angelegt.');
  await expect(page.locator('.kopf')).toContainText('Rezept');
  await page.locator('.kopf a', { hasText: '#italienisch' }).click();
  await expect(page).toHaveURL(/tag=italienisch/);
  await expect(page.getByRole('link', { name: 'Lasagne' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Einkaufsliste' })).toHaveCount(0);
});

test('a recipe link becomes a recipe note', async ({ page, baseURL }) => {
  await page.goto('/notizen');
  await page.getByLabel('Link zu einem Rezept, einer Rezension, einer Reise …').fill(baseURL + '/demo/rezept');
  await page.getByLabel('Dazu, wenn du magst').fill('#herbst');
  await page.getByRole('button', { name: 'Merken' }).click();
  await expect(page.getByRole('status')).toContainText('Gemerkt.');
  await expect(page).toHaveURL(/kuerbissuppe\.md/);
  await expect(page.locator('.kopf')).toContainText('Rezept');
  await expect(page.getByText('1 l Gemüsebrühe')).toBeVisible();
  await expect(page.locator('.kopf a', { hasText: '#herbst' })).toBeVisible();
});

test('a podcast is subscribed by its link and can be ended', async ({ page, baseURL }) => {
  await page.goto('/podcasts');
  await page.getByLabel('Link zum Podcast').fill(baseURL + '/demo/feed.xml');
  await page.getByRole('button', { name: 'Abonnieren' }).click();
  await expect(page.getByRole('status')).toContainText('Abonniert: „Demo-Podcast“');
  await expect(page.locator('#k-meine')).toBeVisible();
  page.once('dialog', (d) => d.accept());
  await page.getByRole('button', { name: 'Abo beenden' }).click();
  await expect(page.getByRole('status')).toContainText('Abo beendet');
});

test('a line ticked on one phone is ticked on the other at once', async ({ page }) => {
  const other = await page.context().newPage();
  other.on('pageerror', (e) => errors.push(String(e)));
  await page.goto('/einkauf');
  await other.goto('/einkauf');
  await mark(other);
  const item = 'Hafermilch ' + Date.now();
  await page.getByLabel('Was fehlt?').fill(item);
  await page.getByRole('button', { name: 'Dazu' }).click();
  await expect(page.getByRole('status')).toContainText('1 Zeile neu auf der Liste');
  await expect(other.getByRole('switch', { name: item }), 'pushed to the other phone').toBeVisible({ timeout: 5_000 });
  const mine = page.getByRole('switch', { name: item });
  await mine.click();
  await expect(page.getByRole('switch', { name: item })).toHaveAttribute('aria-checked', 'true');
  await expect(other.getByRole('switch', { name: item })).toHaveAttribute('aria-checked', 'true', { timeout: 5_000 });
  expect(await stays(other), 'the other page swapped in place').toBe(true);
  page.once('dialog', (d) => d.accept());
  await page.getByRole('button', { name: 'Abgehakte entfernen' }).click();
  await expect(page.getByRole('status')).toContainText('entfernt');
  await expect(other.getByRole('switch', { name: item })).toHaveCount(0, { timeout: 5_000 });
  await other.close();
});

test('a recipe goes on the list for more portions, without what one has', async ({ page }) => {
  await page.goto('/notizen/rezepte/lasagne.md');
  await page.getByRole('link', { name: 'Auf die Einkaufsliste' }).click();
  await expect(page.getByLabel('Für')).toHaveValue('4');
  await page.getByLabel('Für').fill('8');
  await page.getByRole('button', { name: 'Zutaten zeigen' }).click();
  await expect(page.getByText('1 kg Hackfleisch')).toBeVisible();
  await page.getByLabel('2 Dosen Tomaten').uncheck();
  await page.getByRole('button', { name: 'Auf die Einkaufsliste' }).click();
  await expect(page).toHaveURL(/\/einkauf$/);
  await expect(page.getByRole('status')).toContainText('auf der Liste');
  await expect(page.getByRole('switch', { name: '500 g Lasagneplatten' })).toBeVisible();
  await expect(page.getByRole('switch', { name: 'Dosen Tomaten' })).toHaveCount(0);
});

test("a list of one's own is made, switched to, and deleted", async ({ page }) => {
  await page.goto('/einkauf');
  await page.locator('summary', { hasText: 'Listen verwalten' }).click();
  const name = 'Nur ich ' + Date.now();
  await page.getByLabel('Neue Liste').fill(name);
  await page.getByLabel('Nur für mich').check();
  await page.getByRole('button', { name: 'Anlegen' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(name);
  await expect(page.getByRole('status')).toContainText('angelegt');
  await expect(page.getByRole('navigation', { name: 'Listen' }).getByRole('link', { name: name })).toHaveAttribute('aria-current', 'page');
  await page.getByLabel('Was fehlt?').fill('Geheimnis');
  await page.getByRole('button', { name: 'Dazu' }).click();
  await expect(page.getByRole('switch', { name: 'Geheimnis' })).toBeVisible();
  await page.getByRole('navigation', { name: 'Listen' }).getByRole('link', { name: 'Einkauf' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Einkauf');
  await expect(page.getByRole('switch', { name: 'Geheimnis' })).toHaveCount(0);
  await page.getByRole('navigation', { name: 'Listen' }).getByRole('link', { name: name }).click();
  await page.locator('summary', { hasText: 'Listen verwalten' }).click();
  page.once('dialog', (d) => d.accept());
  await page.getByRole('button', { name: 'Diese Liste löschen' }).click();
  await expect(page.getByRole('status')).toContainText('gelöscht');
  await expect(page.getByRole('link', { name: name })).toHaveCount(0);
});

test('music goes up in pieces and arrives whole', async ({ page, request }) => {
  await page.goto('/musik');
  const big = Buffer.alloc(9 * 1024 * 1024, 7); // two pieces
  await page.setInputFiles('#musik-dateien', [
    { name: 'lied.mp3', mimeType: 'audio/mpeg', buffer: big },
    { name: 'notiz.txt', mimeType: 'text/plain', buffer: Buffer.from('nein') },
  ]);
  await expect(page.locator('#musik-stand')).toContainText('1 von 2 Dateien hochgeladen', { timeout: 20_000 });
  await expect(page.locator('#musik-liste')).toContainText('keine Musikdatei');
  const got = await (await request.get('/demo/musik')).json();
  expect(got['lied.mp3']).toBe(big.length);
  expect(Object.keys(got).filter((k) => k.includes('.part'))).toEqual([]);
});

test('a form in a card stacks its fields on a phone', async ({ page }) => {
  await page.goto('/kalender/verwalten');
  await page.locator('summary', { hasText: 'Neuen Kalender anlegen' }).click();
  const name = page.locator('form.eingabe input[name=name]').last();
  const box = await name.boundingBox();
  expect(box.width, 'the name field has room').toBeGreaterThan(250);
});

// Every page, every field, on a phone (owner 2026-10-24: "werden alle
// Felder geprüft?"): with every fold open, the page is no wider than the
// screen, a field to type in has room, a button is big enough for a
// finger, and no text runs out of its box.
const PAGES = ['/start', '/raeume', '/raeume/light.kueche', '/medien', '/energie', '/kalender', '/kalender/verwalten',
  '/notizen', '/notizen?tag=kochen', '/notizen/einkaufsliste.md', '/notizen/einkaufsliste.md?bearbeiten=1', '/mehr', '/musik', '/podcasts',
  '/einkauf', '/einkauf?liste=liste-baumarkt', '/einkauf/rezepte', '/einkauf/rezepte?n=rezepte%2Flasagne.md', '/notizen/rezepte/lasagne.md'];
for (const path of PAGES) {
  test(`layout of ${path}`, async ({ page }) => {
    await page.goto(path);
    await page.evaluate(() => document.querySelectorAll('details').forEach((d) => { d.open = true; }));
    const problems = await page.evaluate(() => {
      const out = [];
      const vw = document.documentElement.clientWidth;
      if (document.documentElement.scrollWidth > vw + 1) out.push(`page ${document.documentElement.scrollWidth}px wide on a ${vw}px screen`);
      const seen = (el) => { const r = el.getBoundingClientRect(); const cs = getComputedStyle(el);
        return r.width > 0 && r.height > 0 && cs.visibility !== 'hidden' && cs.display !== 'none'; };
      const what = (el) => (el.getAttribute('aria-label') || el.name || el.textContent || el.tagName).trim().slice(0, 40);
      for (const el of document.querySelectorAll('input, select, textarea')) {
        if (!seen(el) || ['hidden', 'checkbox', 'radio', 'range', 'file', 'submit'].includes(el.type)) continue;
        const r = el.getBoundingClientRect();
        if (r.width < 120) out.push(`field "${what(el)}" only ${Math.round(r.width)}px wide`);
        if (r.right > vw + 1) out.push(`field "${what(el)}" runs off the screen`);
      }
      for (const el of document.querySelectorAll('button, .knopf, nav a, summary')) {
        if (!seen(el)) continue;
        const r = el.getBoundingClientRect();
        if (r.height < 40) out.push(`"${what(el)}" only ${Math.round(r.height)}px high`);
        if (r.right > vw + 1) out.push(`"${what(el)}" runs off the screen`);
      }
      for (const el of document.querySelectorAll('p, li, h1, h2, label, td, th, .k')) {
        if (!seen(el)) continue;
        if (el.scrollWidth > el.clientWidth + 2 && getComputedStyle(el).overflowX === 'visible') out.push(`text overflows: "${what(el)}"`);
      }
      return out;
    });
    expect(problems, `${path} on a phone`).toEqual([]);
  });
}

// Pictures of the main pages as a phone shows them, for a look at the
// run (the CI keeps them).
for (const path of ['/start', '/raeume', '/medien', '/energie', '/notizen', '/mehr', '/musik', '/kalender/verwalten']) {
  test(`picture of ${path}`, async ({ page }, info) => {
    await page.goto(path);
    await page.screenshot({ path: info.outputPath(path.slice(1) + '.png'), fullPage: true });
  });
}
