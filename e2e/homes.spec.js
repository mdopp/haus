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

  await page.locator('[data-seite="/raeume/lock.haustuer"]').scrollIntoViewIfNeeded(); // below the Sprechen field on a 360 px phone
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

// Owner 2026-11-04 (no link to Paperless on the web): the archive's way in leads, with its LAN-only line.
test('Dokumente leads with Paperless, then filing, the title after a file', async ({ page }, info) => {
  await page.goto('/dokumente');
  await expect(page.locator('a.vorne', { hasText: 'In Paperless öffnen' })).toHaveAttribute('href', /^https:\/\/paperless\.example\//);
  expect(await above(page, 'a.vorne', '.zustand')).toBe(true);
  await expect(page.locator('.zustand').first()).toContainText('Nur zu Hause im WLAN');
  const big = page.locator('label.vorne', { hasText: 'Foto machen oder Datei ablegen' });
  await expect(big).toBeVisible();
  expect(await above(page, 'a.vorne', 'label.vorne')).toBe(true);
  await page.screenshot({ path: info.outputPath('dokumente.png'), fullPage: true });
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
  await expect(rows).toHaveText([/Sommerplatte.zip\s*wird einsortiert/, /kassette-1994.rar\s*nicht übernommen · .*: RAR/, /Erfundenes Album.zip\s*fertig: 2 Alben, 24 Titel/]);
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
const PAGES = ['/start', '/raeume', '/raeume/light.kueche', '/medien', '/energie', '/energie?zeitraum=jahr', '/kalender', '/kalender/verwalten', '/kontakte', '/kontakte/h3', '/kontakte/privat', '/kontakte/dieselbe',
  '/notizen', '/notizen?tag=kochen', '/notizen/einkaufsliste.md', '/notizen/markdown.md', '/notizen/einkaufsliste.md?bearbeiten=1', '/mehr', '/musik', '/platte', '/discogs', '/aktivierungswort', '/aufgaben', '/dokumente', '/dokumente/suche?q=strom', '/dokumente/suche?nur=versicherungen', '/willkommen', '/einrichten/fotos', '/box/geraete', '/pair-device', '/podcasts', '/gesehen', '/standort',
  '/daten-sichern', '/netzlaufwerk', '/box', '/box/aufgaben', '/box/auslastung', '/box/haushalt', '/box/personen', '/box/module', '/adresse',
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
        if (r.right > vw + 1) out.push(`"${what(el)}" runs off the screen`); // Box's four tabs fit too (owner 2026-11-06)
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

// owner 2026-11-06: Box has four tabs that fit a 411 px phone without scrolling
// sideways; the Übersicht says only what needs the admin, red before yellow,
// the Operator folded with only the skills that did not run well; Haushalt
// is a list of rows with their state, each opening its page.
test('Box: four tabs fit a phone, the Übersicht says what needs me, Haushalt lists its rows', async ({ page }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/box');
  const tabs = page.locator('nav.unter a');
  await expect(tabs).toHaveText(['Übersicht', 'Aufgaben', 'Auslastung', 'Haushalt']);
  const nav = await page.locator('nav.unter').evaluate((n) => ({ sw: n.scrollWidth, cw: n.clientWidth }));
  expect(nav.sw, 'the tabs do not scroll sideways').toBeLessThanOrEqual(nav.cw);
  for (const t of await tabs.all()) {
    const b = await t.boundingBox();
    expect(b.x + b.width, 'each tab on the screen').toBeLessThanOrEqual(411);
  }
  const stand = page.locator('#box-stand');
  await expect(stand).toContainText('Machine-Learning-Container startet nicht');
  await expect(stand).toContainText('Nachrichten über Signal kommen vielleicht nicht an.');
  await expect(stand).toContainText('1 Entscheidung wartet');
  await expect(stand).not.toContainText('kein Wächter');
  await expect(stand).not.toContainText('Alles in Ordnung');
  const text = await stand.innerText();
  expect(text.indexOf('Immich'), 'red first').toBeLessThan(text.indexOf('Paperless'));
  const op = page.locator('details#operator');
  await expect(op.locator('summary')).toContainText('Operator: läuft');
  expect(await op.evaluate((d) => d.open), 'the Operator is folded').toBe(false);
  await page.screenshot({ path: info.outputPath('box-uebersicht-411.png'), fullPage: true });
  await op.locator('summary').click();
  await expect(op.getByText('Alle Skills liefen zuletzt gut.')).toBeVisible();
  const skill = op.locator('li', { hasText: 'Schreibt jeden Morgen' });
  await expect(skill).toBeHidden();
  await op.getByText('Alle Skills zeigen').click();
  await expect(skill).toBeVisible();
  await page.screenshot({ path: info.outputPath('box-operator-411.png'), fullPage: true });

  await tabs.filter({ hasText: 'Haushalt' }).click();
  await expect(page).toHaveURL(/\/box\/haushalt$/);
  const rows = page.locator('#box-haushalt li');
  await expect(rows.filter({ hasText: 'Personen' })).toContainText('1 Einladung offen');
  await expect(rows.filter({ hasText: 'Geräte' })).toContainText(/Telefone? gekoppelt/);
  await expect(rows.filter({ hasText: 'Adresse' })).toContainText('Adresse gesetzt');
  await expect(rows.filter({ hasText: 'Module' })).toContainText('Modulen an');
  await page.screenshot({ path: info.outputPath('box-haushalt-411.png'), fullPage: true });
  await rows.filter({ hasText: 'Personen' }).locator('a').click();
  await expect(page).toHaveURL(/\/box\/personen$/);
  await expect(page.locator('nav.unter a[aria-current=page]')).toHaveText('Haushalt');
  await page.goto('/box/operator');
  await expect(page).toHaveURL(/\/box#operator$/);
});

// owner 2026-11-06: Box → Auslastung, the box's values; only they come again,
// every 5 s while the tab is visible, the page itself is not reloaded
test('Auslastung shows the box and refreshes its values in place', async ({ page }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/box/auslastung');
  await expect(page.getByText('Gerade frei für den Haushalt, Modell qwen3.6-35b-a3b.')).toBeVisible();
  await expect(page.locator('.balken.gelb')).toHaveCount(1);
  await page.evaluate(() => { window.nichtNeu = 1; });
  await page.waitForResponse((r) => r.url().includes('/box/teil?was=auslastung'), { timeout: 8000 });
  await page.waitForResponse((r) => r.url().includes('/box/teil?was=auslastung'), { timeout: 8000 });
  expect(await page.evaluate(() => window.nichtNeu), 'the page was not reloaded').toBe(1);
  await page.screenshot({ path: info.outputPath('auslastung-411.png'), fullPage: true });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.screenshot({ path: info.outputPath('auslastung-1280.png'), fullPage: true });
});

// owner 2026-11-05 („Web zeigt, Handy bearbeitet“): the household's address book, by name;
// typing filters at once, the number is a tap of its own, the row opens the contact
test('Kontakte lists the address book, the search filters while typing, a row opens the contact', async ({ page }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/kontakte');
  const rows = page.locator('.kontakte li');
  await expect(rows).toHaveCount(9);
  await expect(page.getByText('Neue Kontakte und Änderungen machst du auf dem Handy')).toBeVisible();
  await expect(page.locator('a.ruf', { hasText: '0711 556677' })).toHaveAttribute('href', 'tel:0711556677');
  await page.screenshot({ path: info.outputPath('kontakte-411.png'), fullPage: true });
  const suche = page.getByRole('searchbox', { name: 'Name, Nummer, E-Mail' });
  await suche.fill('müller');
  await expect(page.locator('.kontakte li:visible')).toHaveCount(1);
  await suche.fill('0711 9');
  await expect(page.locator('.kontakte li:visible')).toHaveText(/Kita Sonnenschein/);
  await suche.fill('niemand');
  await expect(page.locator('.kontakte li:visible')).toHaveCount(0);
  await expect(page.getByText('Kein Kontakt passt zu „niemand“.')).toBeVisible();
  await suche.fill('');
  await expect(rows).toHaveCount(9);
  await page.getByRole('link', { name: 'Oma Hilde' }).click();
  await expect(page).toHaveURL(/\/kontakte\/h6$/);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Oma Hilde');
  await page.goto('/kalender');
  await expect(page.getByText('Neue Termine und Änderungen machst du auf dem Handy')).toBeVisible();
});

// owner 2026-11-06: Immich's people are no longer offered as contacts; every contact is a person in
// Immich, and the key's owner opens each of their own Handy-Kontakte's photos from Meine Kontakte
test('Meine Kontakte: the own Handy-Kontakte, each to its person in Immich', async ({ page }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/kontakte');
  await expect(page.getByText('In Immich, aber noch kein Haushalts-Kontakt')).toHaveCount(0);
  await page.getByRole('link', { name: 'Meine Kontakte' }).click();
  await expect(page).toHaveURL(/\/kontakte\/privat$/);
  await expect(page.locator('.kontakte li')).toHaveCount(5);
  await expect(page.getByRole('link', { name: 'Oma Hilde' })).toHaveAttribute('href', '/kontakte/privat/k0/fotos');
  await page.screenshot({ path: info.outputPath('meine-kontakte-411.png'), fullPage: true });
  const r = await page.request.get('/kontakte/privat/k0/fotos', { maxRedirects: 0 });
  expect(r.status()).toBe(302);
  expect(r.headers()['location']).toBe('https://photos.example/people/p-hilde');
});

// owner 2026-11-06: „Dieselbe Person?“ — faces Immich holds close, two by two, for the key's owner;
// Nein is said at once on the button and on the page, and the pair is gone
test('Dieselbe Person? shows pairs of faces and takes a Nein', async ({ page }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/kontakte/privat');
  await page.getByRole('link', { name: 'Dieselbe Person?' }).click();
  await expect(page).toHaveURL(/\/kontakte\/dieselbe$/);
  const paare = page.locator('section.paar:not(.frage)');
  await expect(paare).toHaveCount(6);
  await expect(paare.first()).toContainText('Oma Hilde');
  await expect(paare.first()).toContainText('ohne Namen');
  await expect(paare.first()).toContainText('412 Fotos');
  await page.screenshot({ path: info.outputPath('dieselbe-411.png'), fullPage: true });
  await paare.last().getByRole('button', { name: 'Nein' }).click();
  await expect(page.getByText('Gemerkt: Nach diesen beiden fragt Homes nicht wieder.')).toBeVisible();
  await expect(paare).toHaveCount(5);
  await page.screenshot({ path: info.outputPath('dieselbe-nein-411.png'), fullPage: true });
});

// owner 2026-11-06: a contact's picture is a suggestion, never an answer — „Ist das …?“ shows it
// beside the person Immich gave its face; Nein takes Homes' upload out at once and the question goes
test('Ist das …? asks about a contact picture and takes a Nein', async ({ page }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/kontakte/dieselbe');
  const fragen = page.locator('section.frage');
  await expect(fragen).toHaveCount(2);
  await expect(fragen.first()).toContainText('Ist das Lena Schmidt?');
  await expect(fragen.first()).toContainText('Bild im Kontakt');
  await expect(fragen.first().getByRole('button', { name: 'Ja, das ist Lena Schmidt' })).toBeVisible();
  await expect(fragen.last()).toContainText('Oma Hilde');
  await page.screenshot({ path: info.outputPath('ist-das-411.png'), fullPage: true });
  await fragen.last().getByRole('button', { name: 'Nein' }).click();
  await expect(page.getByText('Gemerkt: Homes hat das Bild aus Immich genommen und fragt nach dieser Person nicht wieder.')).toBeVisible();
  await expect(fragen).toHaveCount(1);
  await page.screenshot({ path: info.outputPath('ist-das-nein-411.png'), fullPage: true });
});

// Pictures of the main pages as a phone shows them, for a look at the
// run (the CI keeps them).
for (const path of ['/start', '/raeume', '/medien', '/energie', '/notizen', '/mehr', '/musik', '/kalender/verwalten', '/kontakte']) {
  test(`picture of ${path}`, async ({ page }, info) => {
    await page.goto(path);
    await page.screenshot({ path: info.outputPath(path.slice(1) + '.png'), fullPage: true });
  });
}

// owner 2026-11-07 („Rigoros einfach“): Start is at most four blocks, each its state and at most one
// action — Braucht dich (filled after the page from Laib: the box's red and yellow, a decision waiting),
// the Sprechen field, Deine Geräte, Heute (three lines, today and tomorrow, then Kalender), Im Haus (one line, Energie a link)
test('Start is four blocks, state first, one action each', async ({ page }) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/start');
  const braucht = page.getByRole('region', { name: 'Braucht dich' });
  await expect(braucht.getByRole('link', { name: /^Box: 1 rot · \d gelb$/ })).toHaveAttribute('href', '/box');
  await expect(braucht.getByRole('link', { name: '1 Entscheidung wartet' })).toHaveAttribute('href', 'https://laib.example/inbox');
  const blocks = await page.evaluate(() => [...document.querySelectorAll('section.karte h2')].map((h) => h.textContent));
  expect(blocks).toEqual(['Braucht dich', 'Deine Geräte', 'Heute', 'Im Haus']);
  // the chat stays (owner 2026-11-07): one line to type or speak, right on Start, between Braucht dich and Deine Geräte
  const feld = page.getByRole('textbox', { name: 'Sprechen' });
  await expect(feld).toBeVisible();
  const y = async (sel) => (await page.locator(sel).first().boundingBox()).y;
  expect(await y('#karte-braucht-dich')).toBeLessThan(await y('form.sprechen'));
  expect(await y('form.sprechen')).toBeLessThan(await y('#karte-deine-geraete'));
  const zeile = await page.locator('form.sprechen > div').boundingBox();
  expect(zeile.height, 'the field is one line').toBeLessThan(60);
  const heute = page.getByRole('region', { name: 'Heute' });
  await expect(heute.locator('li')).toHaveCount(3);
  await expect(heute).toContainText('Müll gelb');
  await expect(heute).not.toContainText('Elternabend');
  await expect(heute.getByRole('link', { name: /^Kalender/ })).toHaveAttribute('href', '/kalender');
  const imHaus = page.getByRole('region', { name: 'Im Haus' });
  await expect(imHaus.locator('li')).toHaveCount(1);
  await expect(imHaus.getByRole('link', { name: /Dach .* Haus/ })).toHaveAttribute('href', '/energie');
  await expect(imHaus.locator('form')).toHaveCount(1);
  for (const gone of ['Heute und demnächst', 'Alles in Ordnung', 'einzurichten', 'Netzlaufwerk']) await expect(page.getByText(gone)).toHaveCount(0);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/start');
  const wide = await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1);
  expect(wide, 'no sideways scroll at 1280 px').toBe(true);
});

// Start at 411 px (owner 2026-11-07, „Rigoros einfach“): the picture the owner compares before and after.
test('picture of /start at 411 px', async ({ page }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/start');
  await page.waitForLoadState('networkidle');
  await page.screenshot({ path: info.outputPath('start-411.png'), fullPage: true });
});

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
  // switched off on purpose (MODULE_AUS) is no trace at all (owner 2026-11-07, ADR 0001 §12)
  await expect(card.locator('#spur-netzlaufwerk')).toHaveCount(0);
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
  await request.get('/demo/wunsch?modul=standort&stand=abgelehnt'); // offered again, whatever ran before
  await page.goto('/mehr');
  await mark(page);
  const spur = page.locator('#spur-standort'); // its backend missing in the demo
  await expect(spur.getByRole('button')).toBeVisible();
  await request.get('/demo/laib?ms=0&fehler=1');
  await spur.getByRole('button').click();
  await expect(page.locator('#homes-hinweis')).toContainText('nicht aufschreiben');
  await expect(spur.getByRole('button')).toHaveText('Hier anfordern');
  expect(await stays(page)).toBe(true);
  await page.screenshot({ path: info.outputPath('spuren-7-fehler.png') });
  await request.get('/demo/laib?ms=0&fehler=0');
});

// owner 2026-11-05: „So hängt alles zusammen“, from Mehr; the web fits a 411 px phone in
// both themes, and each „Das geht“ moment is a tap to the real place
for (const scheme of ['dark', 'light']) {
  test.describe(`zusammen ${scheme}`, () => {
    test.use({ colorScheme: scheme });
    test(`So hängt alles zusammen at 411 px, ${scheme}`, async ({ page }, info) => {
      await page.setViewportSize({ width: 411, height: 900 });
      await page.goto('/mehr');
      await page.getByRole('link', { name: /So hängt alles zusammen/ }).first().click();
      await expect(page).toHaveURL(/\/zusammen$/);
      await expect(page.getByRole('img', { name: /Haushalts-Kontakt/ })).toBeVisible();
      await expect(page.getByRole('link', { name: 'Fotos vom Handy landen von selbst in Immich' })).toHaveAttribute('href', 'https://photos.example');
      // owner 2026-11-05: the contact's Fotos and the Standortverlauf are in the picture
      const netz = page.locator('svg.netz');
      await expect(netz.locator('text', { hasText: 'Zeitleiste' })).toBeVisible();
      await expect(netz.locator('text', { hasText: /^Fotos$/ })).toBeVisible();
      await expect(netz.locator('line[x2="180"][y2="316"]')).toHaveCount(1);
      await expect(page.getByRole('link', { name: 'Ein Tipp vom Kontakt zu allen Fotos dieser Person' })).toHaveAttribute('href', '/kontakte');
      await expect(page.getByRole('link', { name: 'Google-Zeitachse teilen, die Zeitleiste füllt sich' })).toHaveAttribute('href', '/standort');
      const wide = await page.evaluate(() => document.documentElement.scrollWidth);
      expect(wide).toBeLessThanOrEqual(411);
      const geht = page.locator('section[aria-labelledby=karte-das-geht] li a');
      await expect(geht).not.toHaveCount(0);
      for (const href of await geht.evaluateAll((as) => as.map((a) => a.getAttribute('href')))) {
        expect(href).toMatch(/^(\/(kontakte|kalender|standort|raeume|mehr#spuren)|https:\/\/photos\.example)$/);
      }
      await page.screenshot({ path: info.outputPath(`zusammen-411-${scheme}.png`), fullPage: true });
    });
  });
}

// owner 2026-11-06 („Dokumente … von der Oberfläche suchbar“): the Dokumente page opens with one
// search field, the shortcuts, the Bereiche and the types; typing fetches the list into the page
// (no reload), a number's piece or an IBAN's finds its document, a tile opens its list in Homes.
for (const scheme of ['dark', 'light']) {
  test.describe(`dokumente suche ${scheme}`, () => {
    test.use({ colorScheme: scheme });
    test(`Dokumente finden at 411 px, ${scheme}`, async ({ page }, info) => {
      await page.setViewportSize({ width: 411, height: 900 });
      await page.goto('/dokumente');
      const finden = page.getByRole('region', { name: 'Finden' });
      await expect(finden.locator('.bereiche a', { hasText: 'Haus' })).toContainText('3 Dokumente');
      await expect(finden.getByRole('link', { name: 'Versicherungen' })).toBeVisible();
      await expect(finden.getByRole('link', { name: /^Rechnung/ })).toBeVisible();
      await expect(finden.getByRole('link', { name: 'Für Anna' })).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(411);
      await page.screenshot({ path: info.outputPath(`dokumente-finden-411-${scheme}.png`), fullPage: true });
      await mark(page);
      const suche = finden.getByRole('searchbox', { name: 'Name, Nummer, Produkt' });
      await suche.fill('104711');
      await expect(page.locator('#dsuche-treffer')).toContainText('Stromrechnung Oktober');
      await expect(page.locator('#dsuche-treffer')).toContainText('Rechnung SW-2026-104711 · 84,50 €');
      expect(await stays(page), 'no reload').toBe(true);
      await page.screenshot({ path: info.outputPath(`dokumente-suche-nummer-411-${scheme}.png`), fullPage: true });
      await suche.fill('3704 0044');
      await expect(page.locator('#dsuche-treffer')).toContainText('Abschlag Strom 2027');
      await suche.fill('kfz123456');
      await expect(page.locator('#dsuche-treffer')).toContainText('Kfz-Versicherung 2026');
      await suche.fill('gibtsnicht');
      await expect(page.locator('#dsuche-treffer')).toContainText('Nichts gefunden für „gibtsnicht“');
      await suche.fill('');
      await expect(page.locator('#dsuche-treffer')).toBeEmpty();
      expect(await stays(page), 'still no reload').toBe(true);
      await finden.locator('.bereiche a', { hasText: 'Haus' }).click();
      await expect(page).toHaveURL(/\/dokumente\/suche\?bereich=Haus$/);
      await expect(page.locator('h1')).toHaveText('Haus');
      await expect(page.locator('.karte li')).toHaveCount(3);
      await page.screenshot({ path: info.outputPath(`dokumente-bereich-411-${scheme}.png`), fullPage: true });
      await page.goto('/dokumente/suche?nur=versicherungen');
      await expect(page.locator('.karte li', { hasText: 'Vollkasko' })).toContainText('HUK-Coburg · Vers.-Nr. KFZ 123 456 789 · kündbar bis 01.12.2026');
      await expect(page.locator('.karte li', { hasText: 'Privathaftpflicht' })).toContainText('kündbar bis 15.12.2026');
      await page.screenshot({ path: info.outputPath(`dokumente-versicherungen-411-${scheme}.png`), fullPage: true });
      await page.goto('/dokumente/suche?nur=garantie');
      await expect(page.locator('.karte li')).toHaveText(/Waschmaschine WM 7 Pro.*Garantie bis 03\.06\.2027/);
      await page.screenshot({ path: info.outputPath(`dokumente-garantie-411-${scheme}.png`), fullPage: true });
    });
  });
}

// owner 2026-11-07: Kontakt prüfen lassen — an admin makes the link from a house contact, the
// contact checks and corrects their own fields on a page without a login (its own host and
// listener, kontakt.<domain>), the link dies on the answer, and the review shows each field
// before → after with the checks; only what is ticked is written
test('Kontakt prüfen lassen: link, the public form, the review', async ({ page, browser }, info) => {
  await page.setViewportSize({ width: 411, height: 900 });
  await page.goto('/kontakte/h6');
  await page.getByRole('link', { name: 'Kontakt prüfen lassen' }).click();
  await expect(page.getByRole('heading', { name: 'Oma Hilde prüfen lassen' })).toBeVisible();
  await expect(page.locator('dl.felder')).toContainText('0711 445566');
  await page.getByRole('button', { name: 'Link erstellen' }).click();
  await expect(page.getByText('Link erstellt.')).toBeVisible();
  const text = await page.locator('#pruefen-text').inputValue();
  expect(text).toContain('Hallo Oma,');
  const link = text.match(/http:\/\/127\.0\.0\.1:8652\/[A-Za-z0-9_-]{43}/)[0];
  await page.screenshot({ path: info.outputPath('pruefen-link-411.png'), fullPage: true });

  // the contact, without a login, in a browser of their own
  const ctx = await browser.newContext({ viewport: { width: 411, height: 900 }, colorScheme: 'light' });
  const kontakt = await ctx.newPage();
  kontakt.on('pageerror', (e) => errors.push(String(e)));
  await kontakt.goto(link);
  await expect(kontakt.getByRole('heading', { name: 'Stimmen deine Kontaktdaten?' })).toBeVisible();
  await expect(kontakt.locator('input[name=vorname]')).toHaveValue('Oma');
  await expect(kontakt.locator('input[name=nachname]')).toHaveValue('Hilde');
  await expect(kontakt.locator('input[name=tel]').first()).toHaveValue('0711 445566');
  await expect(kontakt.getByText('Anna Beispiel')).toHaveCount(0);
  // nothing wider than the phone: no field pushed off the right edge
  expect(await kontakt.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(411);
  expect(await kontakt.locator('input[name=tel]').first().evaluate((e) => e.offsetWidth)).toBeGreaterThan(150); // the number readable, not „071“
  await kontakt.screenshot({ path: info.outputPath('kontakt-formular-411.png'), fullPage: true });
  await kontakt.locator('input[name=tel]').nth(1).fill('0171 2345678');
  await kontakt.locator('input[name=mail]').first().fill('hilde@nowhere.example');
  await kontakt.locator('input[name=adr-strasse]').first().fill('Lindenweg 3');
  await kontakt.locator('input[name=adr-plz]').first().fill('70173');
  await kontakt.locator('input[name=adr-ort]').first().fill('Stuttgart');
  await kontakt.locator('input[name=geb-tag]').fill('8');
  await kontakt.locator('input[name=geb-monat]').fill('3');
  await kontakt.locator('input[name=geb-jahr]').fill('1941');
  await kontakt.getByRole('button', { name: 'Änderungen senden' }).click();
  await expect(kontakt.getByRole('heading', { name: 'Danke!' })).toBeVisible();
  await kontakt.screenshot({ path: info.outputPath('kontakt-danke-411.png'), fullPage: true });
  await kontakt.goto(link);
  await expect(kontakt.getByRole('heading', { name: 'Link ungültig oder abgelaufen' })).toBeVisible();
  await ctx.close();

  // the review: before → after, the e-mail flagged and so not ticked
  await page.goto('/aufgaben');
  await page.getByRole('link', { name: '1 Kontakt-Antwort prüfen' }).first().click();
  await expect(page).toHaveURL(/\/kontakte\/antworten$/);
  await page.getByRole('link', { name: 'Oma Hilde' }).click();
  const zeilen = page.locator('.aenderung');
  await expect(zeilen).toHaveCount(4);
  await expect(zeilen.filter({ hasText: 'hilde@nowhere.example' })).toContainText('gibt es nicht');
  await expect(zeilen.filter({ hasText: 'hilde@nowhere.example' }).locator('input')).not.toBeChecked();
  await expect(zeilen.filter({ hasText: '0171 2345678' }).locator('input')).toBeChecked();
  await page.screenshot({ path: info.outputPath('kontakt-antwort-411.png'), fullPage: true });
  await page.getByRole('button', { name: 'Ausgewählte übernehmen' }).click();
  await expect(page.getByText('3 Änderungen ins Adressbuch geschrieben')).toBeVisible();
  await expect(zeilen.filter({ hasText: 'hilde@nowhere.example' })).toContainText('nicht übernommen');
  await page.screenshot({ path: info.outputPath('kontakt-antwort-entschieden-411.png'), fullPage: true });
  await page.goto('/kontakte/h6');
  await expect(page.locator('body')).toContainText('0171 2345678');
  await expect(page.locator('body')).not.toContainText('nowhere.example');
});
