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

const stays = (page) => page.evaluate(() => window.homesBleibt === 1);
const mark = (page) => page.evaluate(() => { window.homesBleibt = 1; });

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
  await expect(page.locator('#homes-hinweis')).toContainText('Das hat nicht geklappt');
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

// owner 2026-11-03 (a screenshot of /raeume): scrolling the page moved
// the sliders. The card has none: the level in its line ("an · 60 %") is
// a button that opens a sheet with the thing's name, a large slider,
// quick steps and Fertig. A swipe over the card changes nothing.
const level = (page, name) => page.getByRole('button', { name: new RegExp('^' + name + ' (Helligkeit|Position) (\\d+ %|aus), einstellen$') });
const sheet = (page, name) => page.getByRole('dialog', { name });
const slider = (page, name) => page.getByRole('slider', { name });
async function swipe(page, from, dx, dy) {
  const cdp = await page.context().newCDPSession(page);
  const touch = (type, x, y) => cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x, y }] });
  await touch('touchStart', from.x, from.y);
  for (let i = 1; i <= 12; i++) await touch('touchMove', from.x + (dx * i) / 12, from.y + (dy * i) / 12);
  await touch('touchEnd');
  await cdp.detach();
}
const posts = (page) => { const sent = []; page.on('request', (r) => { if (r.method() === 'POST') sent.push(r.url() + ' ' + (r.postData() || '')); }); return sent; };

test('a swipe over a card sends nothing; the level opens its sheet', async ({ page }, info) => {
  const sent = posts(page);
  await page.goto('/raeume');
  const kueche = level(page, 'Küchenlicht');
  await expect(kueche).toHaveText('60 %');
  await expect(slider(page, 'Küchenlicht Helligkeit')).toBeHidden();
  expect((await kueche.boundingBox()).height, 'a finger hits the level').toBeGreaterThanOrEqual(44);
  await kueche.scrollIntoViewIfNeeded();
  await page.screenshot({ path: info.outputPath('raeume-karte.png') });

  // a finger scrolling the page over the card, up, across and aslant: nothing opens, nothing is sent
  const b = await kueche.boundingBox();
  const at = { x: b.x + b.width / 2, y: b.y + b.height / 2 };
  await swipe(page, at, 0, -220);
  await swipe(page, { x: 30, y: at.y }, 300, 0);
  await swipe(page, at, 200, 160);
  await page.waitForTimeout(500);
  await expect(sheet(page, 'Küchenlicht')).toBeHidden();
  expect(sent, 'a swipe sends nothing').toEqual([]);

  // a tap on the level opens the sheet, titled with the thing
  await kueche.scrollIntoViewIfNeeded();
  await kueche.tap();
  await expect(sheet(page, 'Küchenlicht')).toBeVisible();
  await expect(kueche).toHaveAttribute('aria-expanded', 'true');
  await expect(slider(page, 'Küchenlicht Helligkeit')).toHaveValue('60');
  await expect(sheet(page, 'Küchenlicht').locator('output')).toHaveText('60 %');
  const w = (await sheet(page, 'Küchenlicht').boundingBox()).width;
  expect(w, 'the sheet fits the phone').toBeLessThanOrEqual(page.viewportSize().width);
  await page.waitForTimeout(200);
  await page.screenshot({ path: info.outputPath('blatt-licht.png') });
  // Fertig, Escape and a tap outside close it
  await sheet(page, 'Küchenlicht').getByRole('button', { name: 'Fertig' }).tap();
  await expect(sheet(page, 'Küchenlicht')).toBeHidden();
  await expect(kueche).toHaveAttribute('aria-expanded', 'false');
  await kueche.tap();
  await expect(sheet(page, 'Küchenlicht')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(sheet(page, 'Küchenlicht')).toBeHidden();
  await kueche.tap();
  await expect(sheet(page, 'Küchenlicht')).toBeVisible();
  await page.touchscreen.tap(20, 20);
  await expect(sheet(page, 'Küchenlicht')).toBeHidden();

  // a cover's sheet: Auf, ½, Zu
  const rollo = level(page, 'Rollo');
  await rollo.scrollIntoViewIfNeeded();
  await rollo.tap();
  await expect(sheet(page, 'Rollo')).toBeVisible();
  for (const step of ['Auf', '½', 'Zu']) await expect(sheet(page, 'Rollo').getByRole('button', { name: 'Position ' + step })).toBeVisible();
  await page.waitForTimeout(200);
  await page.screenshot({ path: info.outputPath('blatt-rollo.png') });
  await sheet(page, 'Rollo').getByRole('button', { name: 'Fertig' }).tap();
  expect(sent, 'opening and closing send nothing').toEqual([]);
});

// the lamp back at 60 %, for the next test
const kuecheAt60 = (request, info) => request.post('/schalten', { form: { geraet: 'light.kueche', tun: 'light.turn_on', wert: '60' }, headers: { 'X-Homes-Skript': '1', Origin: info.project.use.baseURL } });

test('a quick step sends exactly one request, the slider one when let go', async ({ page, request }, info) => {
  const sent = posts(page);
  await page.goto('/raeume');
  try {
    await level(page, 'Küchenlicht').tap();
    await sheet(page, 'Küchenlicht').getByRole('button', { name: 'Helligkeit 25 %' }).tap();
    await expect.poll(() => sent.length).toBe(1);
    await page.waitForTimeout(800);
    expect(sent).toHaveLength(1);
    expect(sent[0]).toContain('/schalten');
    expect(sent[0]).toContain('geraet=light.kueche');
    expect(sent[0]).toContain('wert=25');
    await expect(sheet(page, 'Küchenlicht'), 'the sheet stays open after the answer').toBeVisible();
    const s = slider(page, 'Küchenlicht Helligkeit');
    await expect(s, 'the sheet shows the level the step set').toHaveValue('25');
    await s.focus();
    await s.press('ArrowRight');
    await expect.poll(() => sent.length).toBe(2);
    expect(sent[1]).toContain('wert=26');
  } finally {
    await kuecheAt60(request, info);
  }
});

// owner 2026-11-04 (a screenshot of /raeume): a lamp that is off said
// "aus" and nothing opened its sheet. A lamp HA can dim has "aus" as its
// level, accented like "60 %"; its sheet says "aus" large, and a step
// turns it on at that level in one request, the sheet staying open. A
// lamp that only switches says a plain "aus"; one HA does not reach has
// nothing to tap.
test('a dimmer that is off says aus as its level; a step turns it on', async ({ page, request }, info) => {
  const sent = posts(page);
  await page.goto('/raeume');
  const sw = page.getByRole('switch', { name: 'Küchenlicht' });
  await expect(sw).toHaveAttribute('aria-checked', 'true');
  try {
    await sw.click();
    await expect(page.getByRole('switch', { name: 'Küchenlicht' })).toHaveAttribute('aria-checked', 'false');
    const kueche = level(page, 'Küchenlicht');
    await expect(kueche).toHaveText('aus');
    await expect(kueche).toHaveAccessibleName('Küchenlicht Helligkeit aus, einstellen');
    await expect(kueche, 'accented like the %').toHaveCSS('text-decoration-style', 'dotted');
    expect((await kueche.boundingBox()).height, 'a finger hits it').toBeGreaterThanOrEqual(44);
    // a lamp that only switches says a plain "aus"
    await expect(page.getByRole('switch', { name: 'Flurlicht' })).toHaveAttribute('aria-checked', 'false');
    await expect(level(page, 'Flurlicht')).toHaveCount(0);
    await kueche.scrollIntoViewIfNeeded();
    await page.screenshot({ path: info.outputPath('aus-karte.png') });

    await kueche.tap();
    await expect(sheet(page, 'Küchenlicht')).toBeVisible();
    await expect(sheet(page, 'Küchenlicht').locator('output')).toHaveText('aus');
    await expect(slider(page, 'Küchenlicht Helligkeit')).toHaveValue('0');
    await page.waitForTimeout(200);
    await page.screenshot({ path: info.outputPath('aus-licht.png') });

    sent.length = 0;
    await sheet(page, 'Küchenlicht').getByRole('button', { name: 'Helligkeit 25 %' }).tap();
    await expect.poll(() => sent.length).toBe(1);
    await page.waitForTimeout(800);
    expect(sent, 'one request').toHaveLength(1);
    expect(sent[0]).toContain('geraet=light.kueche');
    expect(sent[0]).toContain('tun=light.turn_on');
    expect(sent[0]).toContain('wert=25');
    await expect(sheet(page, 'Küchenlicht'), 'the sheet stays open').toBeVisible();
    await expect(page.getByRole('switch', { name: 'Küchenlicht' })).toHaveAttribute('aria-checked', 'true');
    await expect(sheet(page, 'Küchenlicht').locator('output')).toHaveText('25 %');
    await expect(level(page, 'Küchenlicht')).toHaveText('25 %');
    await sheet(page, 'Küchenlicht').getByRole('button', { name: 'Fertig' }).tap();
  } finally {
    await kuecheAt60(request, info);
  }
  await page.goto('/raeume');
  await expect(level(page, 'Küchenlicht')).toHaveText('60 %');
});

test('a lamp HA does not reach has nothing to tap', async ({ page, request }) => {
  await request.get('/demo/setze?id=light.flur&state=unavailable&name=Flurlicht&dimmbar=1');
  await page.goto('/raeume');
  await expect(page.locator('section[aria-labelledby=karte-flur]')).toContainText('nicht erreichbar');
  await expect(level(page, 'Flurlicht')).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: 'Flurlicht' })).toHaveCount(0);
  await request.get('/demo/setze?id=light.flur&state=off&name=Flurlicht&dimmbar=1');
  await page.goto('/raeume');
  await expect(level(page, 'Flurlicht'), 'the same lamp, off, says aus').toHaveText('aus');
});

// owner 2026-11-04 („Das Design ist großartig! Bitte auf der Webseite genau so umsetzen!“): Deine
// Geräte as the app's tiles. On a 360 px phone no name is cut and ▲ ■ ▼ stay inside their tile; a
// lamp's tile switches in place and says „schaltet …“; a blind's tile opens its sheet with Stopp; a
// garage asks before it opens; a long press opens the thing's page. Wider, every tile is one cell.
test('Deine Geräte are the app\'s tiles', async ({ page }, info) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/start');
  await mark(page);
  const tiles = page.locator('.kachel');
  await expect(tiles).toHaveCount(6);
  const fit = await page.evaluate(() => [...document.querySelectorAll('.kachel')].map((k) => {
    const n = k.querySelector('.name'), r = k.getBoundingClientRect();
    const out = [...k.querySelectorAll('.knoepfe > form button, .knoepfe > button')].filter((b) => { const x = b.getBoundingClientRect(); return x.left < r.left || x.right > r.right; });
    return n.scrollWidth > n.clientWidth || out.length ? n.textContent : '';
  }).filter(Boolean));
  expect(fit, 'cut names or buttons outside their tile').toEqual([]);
  const wide = await page.evaluate(() => [...document.querySelectorAll('.kachel')].map((k) => Math.round(k.getBoundingClientRect().width)));
  expect(wide[0], 'Bürolicht, alone before a blind, takes the row').toBe(wide[1]);
  expect(wide[4], 'Küchenlicht and Haustür share a row').toBeLessThan(wide[1] / 2 + 1);
  await page.screenshot({ path: info.outputPath('kacheln-360.png'), fullPage: true });

  const buero = page.getByRole('switch', { name: 'Bürolicht' });
  await expect(buero).toHaveAttribute('aria-checked', 'false');
  await buero.click();
  await expect(page.getByRole('switch', { name: 'Bürolicht' })).toHaveAttribute('aria-checked', 'true');
  await expect(page.locator('[data-seite="/raeume/light.buero"] .bild path')).toHaveCount(2); // the lit lamp
  await page.getByRole('switch', { name: 'Bürolicht' }).click();
  await expect(page.getByRole('switch', { name: 'Bürolicht' })).toHaveAttribute('aria-checked', 'false');

  await page.getByRole('button', { name: 'Esszimmerjalousien', exact: true }).click();
  const blatt = page.getByRole('dialog', { name: 'Esszimmerjalousien' });
  await expect(blatt).toBeVisible();
  for (const b of ['Position Auf', 'Position ½', 'Position Zu', 'Stopp']) await expect(blatt.getByRole('button', { name: b })).toBeVisible();
  await blatt.getByRole('button', { name: 'Fertig' }).click();

  const sent = posts(page);
  await page.locator('.knoepfe').getByRole('button', { name: 'Garagentor: Hoch' }).click();
  await expect(page.getByRole('dialog', { name: 'Garagentor öffnen?' })).toBeVisible();
  expect(sent, 'asking sends nothing').toEqual([]);
  await page.getByRole('dialog', { name: 'Garagentor öffnen?' }).getByRole('button', { name: 'Abbrechen' }).click();
  expect(await stays(page)).toBe(true);

  const lock = await page.locator('[data-seite="/raeume/lock.haustuer"]').boundingBox();
  await page.mouse.move(lock.x + 30, lock.y + 30);
  await page.mouse.down();
  await page.waitForURL(/\/raeume\/lock\.haustuer$/);
  await page.mouse.up();

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/start');
  const cells = await page.evaluate(() => [...new Set([...document.querySelectorAll('.kachel')].map((k) => Math.round(k.getBoundingClientRect().width)))]);
  expect(cells, 'one width for every tile').toHaveLength(1);
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
  // the Mehr audit (owner 2026-11-04): with a subscription the newest episodes first, subscribing last
  await expect(page.locator('#karte-neueste-folgen')).toBeVisible();
  expect(await above(page, '#karte-neueste-folgen', '#k-meine')).toBe(true);
  expect(await above(page, '#k-meine', '#k-abo')).toBe(true);
  page.once('dialog', (d) => d.accept());
  await page.getByRole('button', { name: 'Abo beenden' }).click();
  await expect(page.getByRole('status')).toContainText('Abo beendet');
});

// above says a's top is above b's on the page.
async function above(page, a, b) {
  const [ya, yb] = [await page.locator(a).first().boundingBox(), await page.locator(b).first().boundingBox()];
  return ya.y < yb.y;
}

// The Mehr audit (owner 2026-11-04): filing is the first and biggest thing on Dokumente; the title
// asks once a file is chosen; the mail is one quiet line below.
// Daten sichern: the state first, one line per source with a dot, the app's one sentence, the change folded last.
test('Daten sichern leads with the state, the phone change folded', async ({ page }) => {
  await page.goto('/daten-sichern');
  const stand = page.locator('#karte-dein-stand, section:has(h2:text-is("Dein Stand"))').first();
  for (const q of ['WhatsApp', 'Signal', 'SMS und Anrufe', 'Standort', 'Kontakte', 'Kalender']) await expect(stand.locator('li.quelle', { hasText: q })).toHaveCount(1);
  expect(await above(page, 'li.quelle', 'text=Das Sichern macht die Homes-App')).toBe(true);
  await expect(page.locator('details.wechsel')).not.toHaveAttribute('open', '');
  expect(await above(page, 'text=Das Sichern macht die Homes-App', 'details.wechsel')).toBe(true);
});

// Netzlaufwerk once the password is set: the addresses first, the state in a line, the change folded.
test('Netzlaufwerk leads with the addresses once set up', async ({ page, request }) => {
  await request.get('/demo/samba?gesetzt=1');
  await page.goto('/netzlaufwerk');
  expect(await above(page, 'text=So verbindest du dich', '.zustand')).toBe(true);
  await expect(page.locator('.zustand')).toContainText('Passwort seit');
  await expect(page.getByRole('button', { name: 'Kopieren' })).toHaveCount(3);
  await expect(page.locator('details.aendern')).not.toHaveAttribute('open', '');
});

test('Dokumente leads with filing, the title after a file', async ({ page }) => {
  await page.goto('/dokumente');
  const big = page.locator('label.vorne', { hasText: 'Foto machen oder Datei ablegen' });
  await expect(big).toBeVisible();
  expect(await above(page, 'label.vorne', 'text=Oder per Mail an')).toBe(true);
  await expect(page.getByLabel('Titel (wenn du magst)')).toBeHidden();
  await page.locator('input[type=file]').setInputFiles({ name: 'brief.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4') });
  await expect(page.getByLabel('Titel (wenn du magst)')).toBeVisible();
  await expect(page.locator('.gewaehlt')).toHaveText('brief.pdf');
});

// Mehr: what a person uses day to day, then what is set up once, then the account in one row.
test('Mehr lists the everyday pages first, the setup quieter, the account in a row', async ({ page }) => {
  await page.goto('/mehr');
  expect(await above(page, '#karte-seiten', '#karte-einrichten')).toBe(true);
  expect(await above(page, '#karte-einrichten', '#karte-konto')).toBe(true);
  await expect(page.locator('#karte-einrichten').locator('..')).toContainText('Deine Aufgaben');
  await expect(page.locator('section[aria-labelledby=karte-konto] li')).toHaveCount(1);
});

test('a film goes on the wish list from Gesehen & Gehört, one the box has does not', async ({ page }) => {
  await page.goto('/gesehen');
  await expect(page.getByText('Dark')).toBeVisible();
  await expect(page.getByText('Krabat')).toBeVisible();
  const title = 'Arrival ' + Date.now();
  await page.getByLabel('Titel').fill(title);
  await page.getByRole('button', { name: 'Auf die Wunschliste' }).click();
  await expect(page.getByRole('status')).toContainText('Auf der Wunschliste „Filme & Bücher“: „' + title + '“');
  await expect(page.getByText(title + ' (Film)')).toBeVisible();
  await page.getByLabel('Titel').fill('Dune');
  await page.getByRole('button', { name: 'Auf die Wunschliste' }).click();
  await expect(page.getByRole('status')).toContainText('Hast du schon gesehen');
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
  await page.getByRole('button', { name: 'Auf die Liste' }).click();
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

// Musik verwalten (owner 2026-11-04: „nur Dateien darstellen, aber nichts Komplexes“): how music
// gets in, one list of the files under it, a failed one with its actions on its row; Platten
// by whether one's Discogs is connected; with nothing uploaded only Hinzufügen and nothing refreshes.
test('Musik is how music gets in, its files, and the records by Discogs', async ({ page, request }, info) => {
  await request.get('/demo/musik?leer=0');
  await page.goto('/musik');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Musik');
  await expect(page.getByRole('heading', { level: 2 })).toHaveText(['Hinzufügen', 'Platten']);
  const rows = page.locator('.musik-liste li');
  await expect(rows).toHaveText([/Sommerplatte.zip\s*wird einsortiert/, /kassette-1994.rar\s*nicht einsortiert: RAR/, /Erfundenes Album.zip\s*fertig: 2 Alben, 24 Titel/]);
  await expect(rows.nth(1).getByRole('button')).toHaveText(['Noch einmal einsortieren', 'Löschen']);
  await expect(page.locator('.musik-platten')).toContainText('Platte erfassen');
  await expect(page.locator('p.ruhig').last()).toContainText('Discogs: verbunden mit');
  await page.screenshot({ path: info.outputPath('musik-demo.png'), fullPage: true });
  await request.get('/demo/musik?leer=1&discogs=0');
  await page.reload();
  await expect(rows).toHaveCount(0);
  await expect(page.locator('[data-auffrischen]')).toHaveCount(0);
  await expect(page.locator('.musik-platten a')).toHaveText([/Wunschliste · \d+/, 'Mit Discogs verbinden']);
  await page.screenshot({ path: info.outputPath('musik-leer.png'), fullPage: true });
  await request.get('/demo/musik?leer=0');
});

// owner 2026-10-26: after the photo the page stood still at „1 Foto(s)
// bereit“; it goes on by itself and ends with a sentence (here: this
// demo has no model to read photos, and no barcode was found)
test('a record photo is searched without a second tap', async ({ page }) => {
  await page.goto('/platte');
  await mark(page);
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');
  await page.setInputFiles('#platte-fotos', [{ name: 'rueckseite.png', mimeType: 'image/png', buffer: png }]);
  await expect(page.locator('#platte-gewaehlt')).toContainText('1 Foto(s) bereit');
  await expect(page.locator('#homes-hinweis')).toContainText('Homes hat nichts, wonach es suchen kann', { timeout: 15_000 });
  expect(await stays(page)).toBe(true);
});

// owner 2026-10-27 (a screenshot): on a wide screen the content stood at the left edge; it is centred
// in the room beside the menu
test('on a wide screen the content is centred beside the menu', async ({ page }) => {
  await page.setViewportSize({ width: 1800, height: 900 });
  await page.goto('/start');
  const m = await page.evaluate(() => {
    const menu = document.querySelector('.tabs').getBoundingClientRect();
    const cards = [...document.querySelectorAll('body > .karte, body > section, body > h1')].map((e) => e.getBoundingClientRect()).filter((r) => r.width > 0);
    const left = Math.min(...cards.map((r) => r.left)), right = Math.max(...cards.map((r) => r.right));
    return { links: left, rechts: menu.left - right };
  });
  expect(Math.abs(m.links - m.rechts), JSON.stringify(m)).toBeLessThan(60);
});

// haus#172: the forms on Geräte had no action, so the script posted to /null; a phone is added and removed
test('a phone access is made and removed on Box → Geräte', async ({ page }) => {
  page.on('dialog', (d) => d.accept());
  await page.goto('/box/geraete');
  await mark(page);
  await page.fill('input[name=name]', 'Pixel von Test');
  await page.getByRole('button', { name: 'Anlegen' }).click();
  await expect(page.locator('h2', { hasText: 'Pixel von Test angelegt' })).toBeVisible();
  const row = page.locator('tr', { hasText: 'Pixel von Test' });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: 'Entfernen' }).click();
  await expect(page.locator('tr', { hasText: 'Pixel von Test' })).toHaveCount(0);
  expect(await stays(page)).toBe(true);
});

// haus#176: the connection drops while a form is sent: the page says so and sends nothing twice
test('a dropped connection is said, not sent again', async ({ page }) => {
  await page.goto('/einkauf');
  let posts = 0;
  await page.route('**/einkauf', (r) => { if (r.request().method() === 'POST') { posts++; return r.abort('internetdisconnected'); } return r.continue(); });
  await page.fill('input[name=was]', 'Milch');
  await page.locator('input[name=was]').press('Enter');
  await expect(page.locator('#homes-hinweis')).toContainText('Homes ist gerade nicht erreichbar');
  expect(posts).toBe(1);
  await page.locator('#homes-hinweis .zu').click();
  await expect(page.locator('#homes-hinweis')).not.toHaveClass(/zeigt/);
});

test('a form in a card stacks its fields on a phone', async ({ page }) => {
  await page.goto('/kalender/verwalten');
  await page.locator('summary', { hasText: 'Neuen Kalender anlegen' }).click();
  const name = page.locator('form.eingabe input[name=name]').last();
  const box = await name.boundingBox();
  expect(box.width, 'the name field has room').toBeGreaterThan(250);
});

// owner 2026-11-04: a module's setup is a task; „Ist eingerichtet“ says it
// runs, the block then keeps its steps without the tick, in place, and the
// counter beside Aufgaben goes down by one
test('ticking a setup by hand shrinks the counter in place', async ({ page }) => {
  await page.goto('/aufgaben');
  const n = Number(await page.locator('h1 .anzahl').textContent());
  await page.goto('/einrichten/filme');
  await mark(page);
  const block = page.locator('#einrichten-filme');
  await expect(block).toContainText('Lade die App „Jellyfin“ aufs Handy.');
  await page.route('**/einrichten/filme', async (r) => { await new Promise((res) => setTimeout(res, 400)); await r.continue(); });
  await block.getByRole('button', { name: 'Ist eingerichtet' }).click();
  await expect(block.getByRole('button')).toHaveText('Hakt ab …');
  await expect(block.getByRole('button')).toHaveCount(0);
  await expect(block).toContainText('https://media.example');
  expect(await stays(page), 'ticked in place').toBe(true);
  await page.goto('/aufgaben');
  await expect(page.locator('h1')).toContainText(String(n - 1));
  await expect(page.getByRole('link', { name: 'Noch einmal' })).toHaveCount(0);
  await expect(page.locator('section[aria-labelledby=karte-erledigt]')).toContainText('Filme und Musik auf dem Handy einrichten');
});

// owner 2026-10-30: the wake word the speaker reports, and „Hey Mycroft“ Laib selects
test("Mehr's Deine-Aufgaben card fills itself after the page", async ({ page, request }) => {
  await request.get('/demo/laib?ms=1500');
  await page.goto('/mehr');
  await mark(page);
  const card = page.locator('section[aria-labelledby=karte-deine-aufgaben]');
  await expect(card).toContainText('Deine Aufgaben von der Box werden gerade gelesen');
  await expect(card.getByRole('link', { name: 'Alle Aufgaben' })).toHaveAttribute('href', '/aufgaben');
  await expect(card.getByRole('link', { name: 'Homes-App koppeln' })).toBeVisible();
  await expect(card).not.toContainText('werden gerade gelesen');
  await expect(page.locator('[data-nachladen]')).toHaveCount(0);
  expect(await stays(page), 'filled in place').toBe(true);
  await request.get('/demo/laib?ms=0');
});

test("Mehr's Deine-Aufgaben card says so when Laib does not answer", async ({ page, request }) => {
  await request.get('/demo/laib?fehler=1');
  await page.goto('/mehr');
  const card = page.locator('section[aria-labelledby=karte-deine-aufgaben]');
  await expect(card).toContainText('Die Aufgaben von der Box lassen sich gerade nicht lesen.');
  await expect(card.getByRole('link', { name: 'Alle Aufgaben' })).toHaveAttribute('href', '/aufgaben');
  await request.get('/demo/laib?ms=0');
});

test('Aktivierungswort shows the wake word the speaker listens for', async ({ page }) => {
  await page.goto('/aktivierungswort');
  const card = page.locator('section[aria-labelledby=karte-aktivierungswort]');
  await expect(card).toContainText('„Okay Nabu“');
  await expect(card).toContainText('Lautsprecher „Voice PE“ · Küche');
  await expect(card).toContainText('„Hey Mycroft“ kann der Lautsprecher schon, Laib wählt es für ihn aus. Bis dahin hört er auf „Okay Nabu“.');
});

// Standortverlauf once connected (owner 2026-11-04: „Ab dem Moment der
// Koppelung ist es aber das wichtigste“): the timeline first and big, the
// state, the step only while no point has come, Trennen last and quiet.
test('Standortverlauf, connected, leads with the timeline and ends with a quiet Trennen', async ({ page, request }, info) => {
  const y = async (sel) => (await page.locator(sel).boundingBox()).y;
  await request.get('/demo/standort?punkte=0');
  await page.goto('/standort');
  const open = page.getByRole('link', { name: 'Zeitleiste öffnen' });
  await expect(open).toHaveAttribute('href', 'https://reitti.example');
  expect((await open.boundingBox()).height).toBeGreaterThanOrEqual(48);
  await expect(page.locator('.zustand')).toContainText('Verbunden seit');
  await expect(page.locator('.schritt')).toContainText('die Aufzeichnung ein');
  await expect(page.locator('form.leise-weg')).toHaveAttribute('data-frage', /Reitti/);
  expect(await y('.vorne')).toBeLessThan(await y('.zustand'));
  expect(await y('.zustand')).toBeLessThan(await y('.schritt'));
  expect(await y('.schritt')).toBeLessThan(await y('form.leise-weg'));
  await page.screenshot({ path: info.outputPath('standort-verbunden-ohne-punkte.png'), fullPage: true });

  await request.get('/demo/standort?punkte=1');
  await page.reload();
  await expect(page.locator('.zustand')).toContainText(/Verbunden · zuletzt aufgezeichnet (heute|gestern) \d\d:\d\d/);
  await expect(page.locator('.schritt')).toHaveCount(0);
  expect(await y('.vorne')).toBeLessThan(await y('.zustand'));
  expect(await y('.zustand')).toBeLessThan(await y('form.leise-weg'));
  await page.screenshot({ path: info.outputPath('standort-verbunden.png'), fullPage: true });
});

// Every page, every field, on a phone (owner 2026-10-24: "werden alle
// Felder geprüft?"): with every fold open, the page is no wider than the
// screen, a field to type in has room, a button is big enough for a
// finger, and no text runs out of its box.
const PAGES = ['/start', '/raeume', '/raeume/light.kueche', '/medien', '/energie', '/energie?zeitraum=jahr', '/kalender', '/kalender/verwalten',
  '/notizen', '/notizen?tag=kochen', '/notizen/einkaufsliste.md', '/notizen/markdown.md', '/notizen/einkaufsliste.md?bearbeiten=1', '/mehr', '/musik', '/platte', '/discogs', '/aktivierungswort', '/aufgaben', '/dokumente', '/willkommen', '/einrichten/fotos', '/box/geraete', '/pair-device', '/podcasts', '/gesehen', '/standort',
  '/daten-sichern', '/netzlaufwerk', '/box', '/box/aufgaben', '/box/dienste', '/box/operator', '/box/personen', '/box/module', '/adresse',
  '/einkauf', '/einkauf?liste=liste-baumarkt', '/einkauf?liste=liste-musik', '/einkauf/rezepte', '/einkauf/rezepte?n=rezepte%2Flasagne.md', '/notizen/rezepte/lasagne.md'];
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
        if (r.right > vw + 1 && !el.closest('nav.unter')) out.push(`"${what(el)}" runs off the screen`); // Box's pills scroll sideways
      }
      for (const el of document.querySelectorAll('p, li, h1, h2, label, td, th, .k')) {
        if (!seen(el)) continue;
        if (el.scrollWidth > el.clientWidth + 2 && getComputedStyle(el).overflowX === 'visible') out.push(`text overflows: "${what(el)}"`);
      }
      return out;
    });
    expect(problems, `${path} on a phone`).toEqual([]);
    // haus#176: every POST form names where it goes and says it runs (or is a switch)
    const forms = await page.evaluate(() => [...document.querySelectorAll('form')].filter((f) => (f.getAttribute('method') || '').toLowerCase() === 'post')
      .map((f) => ({ a: f.getAttribute('action'), ok: f.hasAttribute('data-ganz') || [...f.querySelectorAll('button:not([type=button])')].every((b) => b.hasAttribute('data-laeuft') || b.getAttribute('role') === 'switch'), html: f.outerHTML.slice(0, 120) }))
      .filter((f) => !f.a || !f.ok));
    expect(forms, `${path}: POST forms without an action or a running label`).toEqual([]);
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

// Energie in the widget's look (owner, 2026-11-03), for the owner to see
// before it merges: the Jahr span, and the page in the light.
test('picture of /energie in Jahr', async ({ page }, info) => {
  await page.goto('/energie?zeitraum=jahr');
  await page.screenshot({ path: info.outputPath('energie-jahr.png'), fullPage: true });
});
test.describe('light', () => {
  test.use({ colorScheme: 'light' });
  test('picture of /energie in the light', async ({ page }, info) => {
    await page.goto('/energie');
    await page.screenshot({ path: info.outputPath('energie-hell.png'), fullPage: true });
  });
});

// ADR 0001 §10 (owner 2026-11-04: „Keine Funktion soll versteckt werden.“):
// Mehr ends with what this box lacks, quiet; „Hier anfordern“ answers at
// once and ends with the wish's state, which follows Laib.
test('what the box lacks is a quiet trace; a wish follows Laib', async ({ page, request }, info) => {
  await request.get('/demo/laib?ms=0&fehler=0');
  await page.goto('/mehr');
  const card = page.getByRole('region', { name: 'Gibt es hier noch nicht' });
  const standort = card.locator('#spur-standort');
  await expect(standort).toContainText('Standortverlauf');
  await expect(standort).toContainText('auf dieser Box nicht eingerichtet');
  await expect(card.locator('#spur-netzlaufwerk')).toContainText('ausgeschaltet');
  await expect(card.locator('#spur-geraete')).toHaveCount(0);
  // quiet: the trace's text is the muted colour, never the danger colour
  const color = await standort.evaluate((el) => getComputedStyle(el).color);
  const muted = await page.evaluate(() => { const d = document.createElement('span'); d.style.color = 'var(--muted)'; document.body.appendChild(d); const c = getComputedStyle(d).color; d.remove(); return c; });
  expect(color).toBe(muted);
  await card.scrollIntoViewIfNeeded();
  await card.screenshot({ path: info.outputPath('spuren-1-nicht-eingerichtet.png') });

  await mark(page);
  const ask = standort.getByRole('button', { name: 'Standortverlauf: Hier anfordern' });
  await request.get('/demo/laib?ms=600&fehler=0'); // Laib takes a moment: the button says what runs
  await ask.click();
  await expect(standort.getByRole('button')).toHaveText('Wird angefordert …');
  await standort.screenshot({ path: info.outputPath('spuren-2-wird-angefordert.png') });
  await expect(page.locator('#spur-standort')).toContainText('angefordert', { timeout: 10_000 });
  await expect(page.locator('#spur-standort').getByRole('button')).toHaveCount(0);
  expect(await stays(page), 'no reload').toBe(true);
  await page.locator('#spur-standort').screenshot({ path: info.outputPath('spuren-3-angefordert.png') });

  await request.get('/demo/laib?ms=0&fehler=0');
  await request.get('/demo/wunsch?modul=standort&stand=wartet');
  await page.goto('/mehr');
  await expect(page.locator('#spur-standort')).toContainText('wartet auf Freigabe');
  await page.locator('#spur-standort').screenshot({ path: info.outputPath('spuren-4-wartet.png') });

  await request.get('/demo/wunsch?modul=standort&stand=eingerichtet');
  await page.goto('/mehr');
  await expect(page.locator('#spur-standort')).toContainText('wird eingerichtet');
  await page.locator('#spur-standort').screenshot({ path: info.outputPath('spuren-5-wird-eingerichtet.png') });

  await request.get('/demo/wunsch?modul=standort&stand=abgelehnt');
  await page.goto('/mehr');
  await expect(page.locator('#spur-standort')).toContainText('abgelehnt');
  await expect(page.locator('#spur-standort').getByRole('button', { name: 'Standortverlauf: Hier anfordern' })).toBeVisible();
  await page.getByRole('region', { name: 'Gibt es hier noch nicht' }).screenshot({ path: info.outputPath('spuren-6-abgelehnt.png') });
});

test('a wish Laib cannot take is said on the page, the trace stays', async ({ page, request }, info) => {
  await request.get('/demo/laib?ms=0&fehler=0');
  await page.goto('/mehr');
  await mark(page);
  const spur = page.locator('#spur-netzlaufwerk'); // switched off in the demo
  await expect(spur.getByRole('button')).toBeVisible();
  await request.get('/demo/laib?ms=0&fehler=1');
  await spur.getByRole('button').click();
  await expect(page.locator('#homes-hinweis')).toContainText('nicht aufschreiben');
  await expect(spur.getByRole('button')).toHaveText('Hier anfordern');
  expect(await stays(page)).toBe(true);
  await page.screenshot({ path: info.outputPath('spuren-7-fehler.png') });
  await request.get('/demo/laib?ms=0&fehler=0');
});
