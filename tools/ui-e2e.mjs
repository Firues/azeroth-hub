// Headless UI check for Azeroth Hub.
//
// Drives the built frontend (frontend/dist) in Chromium, with the Go side
// replaced by a stub installed before any module runs. The backend is not what
// is being checked here. What is being checked is that the built page loads,
// the three screens open, and the buttons reach the backend - the class of bug
// a Go test cannot see and that only shows up as a screen which quietly stops
// working.
//
// Two things this harness has to get right or it lies:
//   1. Served over http, never file://. ES modules are blocked by CORS on
//      file://, so the page would load with every button dead and every
//      assertion below would still "pass" on a static document.
//   2. Every assertion is awaited. A synchronous isVisible() on an element the
//      painters have not reached yet returns false and reads as a failure, and
//      a missed await turns a real failure into a pass.
//
// Run it from the wow-hub directory with playwright resolvable - it is
// installed in the managed node workspace, which is why the script is passed
// absolute paths:
//
//   cd "$NODE_WORKSPACE"
//   node "<abs>/wow-hub/tools/ui-e2e.mjs" "<abs>/wow-hub/frontend/dist" "<abs>/shots"
//
// usage: node ui-e2e.mjs <dist dir> <screenshot dir>

import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { mkdirSync } from 'node:fs';
import { dirname, extname, join, normalize } from 'node:path';
import { chromium } from 'playwright';

const DIR = process.argv[2];
const SHOTS = process.argv[3];
if (!DIR || !SHOTS) {
  console.error('usage: node tools/ui-e2e.mjs <frontend/dist dir> <screenshot dir>');
  process.exit(2);
}
mkdirSync(SHOTS, { recursive: true });

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js':   'text/javascript; charset=utf-8',
  '.mjs':  'text/javascript; charset=utf-8',
  '.css':  'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
};

const server = createServer(async (req, res) => {
  const rel = normalize(decodeURIComponent(req.url.split('?')[0])).replace(/^[\\/]+/, '');

  // The v3 runtime always probes /wails/custom.js and injects it if it exists.
  // It is optional by design, but a 404 lands in the console - and "the console
  // reported no errors" is one of the checks below, so an expected 404 would
  // have to be tolerated there and would teach that check to ignore things.
  //
  // The comparison is slash-normalised: normalize() turns the leading slash
  // into a backslash on Windows, so a plain === against a forward-slash
  // literal never matches and the 404 arrives anyway.
  if (rel.replace(/\\/g, '/') === 'wails/custom.js') {
    res.writeHead(200, { 'Content-Type': MIME['.js'] }).end('');
    return;
  }

  const file = join(DIR, rel || 'index.html');
  try {
    const body = await readFile(file);
    res.writeHead(200, { 'Content-Type': MIME[extname(file)] || 'application/octet-stream' });
    res.end(body);
  } catch {
    res.writeHead(404).end('not found');
  }
});

await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = 'http://127.0.0.1:' + server.address().port;

const fails = [];
const notes = [];
const ok = (cond, msg) => { if (!cond) fails.push(msg); };

// A crash mid-harness must not swallow what was already found. Playwright
// throws on a timeout, and a timeout in the last section would otherwise
// discard every result before it.
let reported = false;
process.on('exit', () => {
  if (reported || !fails.length) return;
  console.error('\nFAIL (' + fails.length + ') — прервано до конца прогона');
  for (const f of fails) console.error('  - ' + f);
});

// The stub. Wails v3 talks to Go over POST /wails/runtime, so the Go side is
// replaced by a route handler on that URL rather than by globals: the page runs
// the real @wailsio/runtime and the real generated bindings, and only the wire
// is fake. The events go the other way - the runtime only registers listeners
// locally, and delivery arrives through window._wails.dispatchWailsEvent.
const STUB = `
window.__errors = [];
window.addEventListener('error', (e) => window.__errors.push(String(e.message)));
window.addEventListener('unhandledrejection', (e) =>
  window.__errors.push(String((e.reason && e.reason.message) || e.reason)));

// v3 delivers a WailsEvent object, not the bare payload. Pushing the payload
// straight through would leave every handler holding an object with no fields,
// and the checks below would fail on empty screens while looking like a UI bug.
window.__fire = (name, data) =>
  window._wails && window._wails.dispatchWailsEvent({ name, data, sender: 'main' });

// Mutable so a test can change what the backend reports and re-fire. The
// backend itself lives in the route handler below, not in the page.
window.__state = {
  services: [
    { ID: 'mysql', State: 'running', PID: 111, MemoryMB: 320, Detail: '' },
    { ID: 'auth',  State: 'running', PID: 222, MemoryMB: 90,  Detail: '' },
    { ID: 'world', State: 'stopped', PID: 0,   MemoryMB: 0,   Detail: '' },
  ],
  checks: [
    { ID: 'repack', OK: true,  Label: 'Repack in place', Detail: 'all the files are there' },
    { ID: 'mysql.process', OK: true, Label: 'Process', Detail: 'running' },
    { ID: 'mysql.port', OK: true, Label: 'Port 3306', Detail: 'listening' },
    { ID: 'mysql.ping', OK: true, Label: 'Answering queries', Detail: 'ok' },
    // auth is running, so a failure here is a real one: it is the only check
    // shape that may show an exclamation and an action. world.process below
    // fails while its service is stopped, which must stay neutral.
    { ID: 'auth.port', OK: false, Label: 'Authserver listens on 3724', Detail: 'port closed' },
    { ID: 'world.process', OK: false, Label: 'Worldserver runs', Detail: 'not running' },
  ],
  setup: [
    { Key: 'repack', Label: 'Repack in place', OK: true, Detail: 'all the files are there', Fix: '' },
    { Key: 'database', Label: 'The database answers', OK: true, Detail: '4 schemas, 977 MB', Fix: '' },
    { Key: 'rollback', Label: 'A fresh copy exists', OK: false, Detail: 'the latest copy is 12 days old', Fix: 'make a copy on the Copies screen' },
    { Key: 'space', Label: 'There is enough disk space', OK: true, Detail: '64 GB free', Fix: '' },
    { Key: 'server', Label: 'The server is running', OK: false, Detail: '2 of 3 services running', Fix: 'press Start the server' },
  ],
  backup: { Running: false, Kind: '', Step: '', Index: 0, Total: 0, Bytes: 0,
            ElapsedSec: 0, Dir: '', SavedBytes: 0, Databases: [], Err: '', Warnings: [], At: '' },
  data: {
    Loaded: true,
    ReadAt: '2026-09-30T12:00:00Z',
    Realm: {
      Databases: [
        { Name: 'acore_world', Tables: 412, SizeBytes: 1024000000 },
        { Name: 'acore_characters', Tables: 98, SizeBytes: 185000000 },
        { Name: 'acore_playerbots', Tables: 12, SizeBytes: 104000000 },
        { Name: 'acore_auth', Tables: 6, SizeBytes: 3145728 },
      ],
      TotalBytes: 1316572800, Characters: 1420, Accounts: 380, Err: '',
      ReadAt: '2026-09-30T12:00:00Z',
    },
    Backups: {
      Dir: 'C:/games/wow/server/backups', Points: [
        { Name: '20260930_075446_datadir', Path: 'C:/x/20260930_075446_datadir', Kind: 'physical', Files: 1,
          SizeBytes: 137000000, Created: '2026-09-30T07:54:46Z', Databases: [], Note: '' },
        { Name: '2026-09-29_bots_current', Path: 'C:/x', Kind: 'logical', Files: 4,
          SizeBytes: 385000000, Created: '2026-09-29T09:11:54Z', Databases: ['acore_world'], Note: '' },
      ],
      TotalBytes: 522000000, FreeBytes: 68719476736, TotalDiskBytes: 1000204886016, Err: '',
    },
  },
  accounts: { Total: 3, Err: '', List: [
    { ID: 1, Username: 'ADMIN', GMLevel: 3, Online: true,  Locked: false, Joined: '2026-01-05T10:00:00Z', LastLogin: '2026-09-30T11:00:00Z', LastIP: '10.0.0.5', TotalTimeSec: 90000 },
    { ID: 2, Username: 'bob',   GMLevel: 0, Online: false, Locked: false, Joined: '2026-02-05T10:00:00Z', LastLogin: '2026-08-01T10:00:00Z', LastIP: '192.168.1.7', TotalTimeSec: 3600 },
    { ID: 3, Username: 'carol', GMLevel: 0, Online: false, Locked: true,  Joined: '2026-03-05T10:00:00Z', LastLogin: '', LastIP: '', TotalTimeSec: 0 },
  ] },
  characters: { Total: 2, Err: '', List: [
    { GUID: 1, Name: 'Гном Кирин', Level: 80, Race: 7, Class: 3, Account: 'ADMIN', Online: true },
    { GUID: 2, Name: 'Thrald', Level: 42, Race: 2, Class: 1, Account: 'bob', Online: false },
  ] },
  schedule: { Enabled: false, EveryDays: 7, AutoRun: true, Due: false,
              NextAt: '', DaysLeft: 0, LastRun: '', Err: '' },
};

`;

/* ---------------- the Go side, on the wire rather than in a global --------- */

// Wails v3 addresses methods by a numeric id over POST /wails/runtime. The ids
// are stable, but they are not readable, so they are read back out of the
// sources that define them: regenerating the bindings or bumping the runtime
// must not silently turn every call into "unknown method" and a blank page.
//
// Everything is resolved from the dist directory that was passed in, not from
// this file's location - the script is copied into a node workspace to get
// playwright, and a path relative to itself would point nowhere.
const FRONTEND = normalize(join(DIR, '..'));

async function readIds(file, re) {
  const src = await readFile(file, 'utf8');
  const map = new Map();
  for (const m of src.matchAll(re)) map.set(m[2], m[1]);
  return map;
}

const OBJECT_CALL = 0;
const OBJECT_APPLICATION = 2;
const OBJECT_WINDOW = 6;

const serviceIds = await readIds(
  join(FRONTEND, 'bindings/wow-hub/appservice.ts'),
  /export function (\w+)\([\s\S]*?\)\s*:[^{]*\{\s*return \$Call\.ByID\((\d+)/g
);
const windowIds = await readIds(
  join(FRONTEND, 'node_modules/@wailsio/runtime/dist/window.js'),
  /const (\w+)Method = (\d+);/g
);
const appIds = await readIds(
  join(FRONTEND, 'node_modules/@wailsio/runtime/dist/application.js'),
  /const (\w+)Method = (\d+);/g
);

// CloseMethod -> Close. The runtime names its constants <Name>Method.
const unMethod = (n) => n.replace(/Method$/, '');

// Every call the page made, in order. Readiness checks read this rather than
// the page, because the page never sees it: the call happened here.
const callLog = [];

const wait = (ms) => new Promise((r) => setTimeout(r, ms));

// Section 11 makes Status start failing, which in v2 was a property on a
// global. Here it is a flag the route handler reads.
let statusFails = false;

// Health is deliberately slow, see the comment in the stub above.
const BACKEND = {
  Status: (st) => ({ Services: st.services }),
  // The language is stubbed rather than inherited: the OS locale of the
  // machine running this harness must not decide which wording the
  // assertions below have to match. English is the reference the dictionaries
  // are checked against, so it is the stable choice.
  Language: () => 'en',
  SetLanguage: () => undefined,
  Health: async (st) => { await wait(700); return st.checks; },
  Setup: (st) => st.setup,
  Data: (st) => st.data,
  RefreshData: () => undefined,
  BackupStatus: (st) => st.backup,
  Schedule: (st) => st.schedule,
  SetSchedule: () => undefined,
  Characters: (st) => st.characters,
  Accounts: (st) => st.accounts,
  StartAll: () => undefined,
  StopAll: () => undefined,
  Start: () => undefined,
  Stop: () => undefined,
  Restart: () => undefined,
  RespondConfirm: () => undefined,
  CreateBackup: () => undefined,
  CancelBackup: () => undefined,
  RestoreSnapshot: () => undefined,
  DeleteBackup: () => undefined,
  OpenFolder: () => undefined,
  // The window has no system frame, so the page drives it. Only IsMaximised
  // has an answer worth faking; the rest record the call and return nothing.
  IsMaximised: () => false,
};

async function serveRuntime(route) {
  const body = JSON.parse(route.request().postData() || '{}');
  const { object } = body;

  // Two envelopes share this endpoint. A service call arrives as
  // {object:0, method:0, args:{call-id, methodID, args:[...]}} - Call.ByID
  // wraps the real id one level down. A runtime call (window, application)
  // arrives flat as {object:6, method:14}. Reading body.method for both gives
  // every service call the id 0, which is a page that loads and stays blank.
  const inner = body.args && typeof body.args === 'object' && !Array.isArray(body.args)
    ? body.args
    : null;
  const method = inner ? inner.methodID : body.method;
  const args = inner ? inner.args : null;

  let name;
  if (object === OBJECT_CALL) name = serviceIds.get(String(method));
  else if (object === OBJECT_WINDOW) name = windowIds.get(String(method)) && unMethod(windowIds.get(String(method)));
  else if (object === OBJECT_APPLICATION) name = appIds.get(String(method)) && unMethod(appIds.get(String(method)));

  callLog.push({ name, args: args || [] });

  const fn = name && BACKEND[name];
  if (!fn) {
    await route.fulfill({ status: 200, contentType: 'application/json', body: 'null' });
    return;
  }
  if (name === 'Status' && statusFails) {
    await route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ kind: 'RuntimeError', message: 'связь потеряна' }),
    });
    return;
  }

  const state = await page.evaluate(() => window.__state);
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify((await fn(state, args)) ?? null),
  });
}

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
// The one place the page reaches Go. Everything else about the bundle is real.
await page.route('**/wails/runtime', serveRuntime);
const consoleErrors = [];
page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); });
page.on('pageerror', (e) => consoleErrors.push(String(e)));

await page.addInitScript(STUB);
// Watches every text node from before any module runs, for the same reason the
// stub above is slow: a flash is a label that exists for a moment, and with
// instant reads nothing exists for a moment. Kept out of the main body so it
// costs one init script and nothing else.
await page.addInitScript(() => {
  window.__flash = [];
  const t0 = performance.now();
  const rows = () => {
    const out = [];
    const it = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = it.nextNode(); n; n = it.nextNode()) {
      const s = n.nodeValue.replace(/\s+/g, ' ').trim();
      if (s) out.push(s);
    }
    return out;
  };
  const tick = () => {
    window.__flash.push({ t: Math.round(performance.now() - t0), rows: rows() });
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
});
await page.goto(base, { waitUntil: 'load' });

const shot = async (name) => {
  await page.screenshot({ path: join(SHOTS, name + '.png') });
};
const calls = (name) => callLog.filter((c) => c.name === name);
// toLocaleString('ru-RU') puts a narrow no-break space between the groups, so
// "1 420" in an assertion has to compare against the same character the page
// rendered. Normalising here beats pasting the invisible one into every check.
const text = async (sel) =>
  (await page.textContent(sel).catch(() => null))?.replace(/[  ]/g, ' ') ?? null;
const visible = (sel) => page.isVisible(sel).catch(() => false);
// The sidebar button "Ещё" mentions "Персонажи" in its hint, so an accessible
// name alone cannot tell the two apart. Scoped to the accordion trigger, which
// is what these four sections are.
const section = (name) =>
  page.locator('[data-accordion-trigger]').filter({ hasText: name }).first();

/* ============================== 1. загрузка ============================== */

// Readiness is not "the page loaded": the first paint needs the Status()
// answer, and the app paints nothing until it lands. A screen that renders
// empty and stays empty would otherwise pass every check below.
await page.waitForSelector('[data-role="headline"]', { timeout: 15000 });
// Readiness is not "the page loaded" and not even "the first paint". With the
// slow stub above, hub.start() awaits Status, Health, Setup, BackupStatus and
// Schedule before it ever reaches Data(), so a screen can look finished and
// still have no snapshot behind it - and every assertion about figures would
// then be asserting on a screen that has not loaded yet.
// Polled here rather than in the page: in v3 the call happens in the route
// handler, so the page never learns that Data was asked for.
await (async () => {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const names = new Set(callLog.map((c) => c.name));
    if (names.has('Data') && names.has('Health')) return;
    await wait(50);
  }
  throw new Error('панель не спросила ни Data, ни Health за 15 с');
})();

// Freeze the startup window here, while the Server screen is still the only
// screen. Read at the end it would compare a startup against whatever screen
// the last section left open, and report every label on both as a flash.
//
// At least one frame first: the poll above resolves as soon as the backend
// calls happen, which on a fast machine is before the first animation frame,
// and an empty trace compared against nothing reports nothing - the check
// would pass without having watched anything.
await page.waitForFunction(() => window.__flash.length > 0, { timeout: 15000 });
const startup = await page.evaluate(() => window.__flash);

// Every label that was visible during startup and is gone from the settled
// screen is something someone saw flash. This is the check that found the badge
// saying «проверки…» and then «всё в порядке», one after the other in the same
// corner, and the database breakdown saying «нет данных» before anything had
// been read.
const seenText = new Map();
for (const f of startup) {
  for (const txt of f.rows) {
    if (!seenText.has(txt)) seenText.set(txt, { first: f.t, last: f.t });
    else seenText.get(txt).last = f.t;
  }
}
const settled = new Set(startup[startup.length - 1].rows);

// A flash is a label nobody could read. How long that takes is not a guess:
// PENDING_NOTICE_MS in state.svelte.ts is the same number and the same rule -
// a label shorter than that is not something a person saw.
//
// The threshold matters more than it did in v2. This harness used to answer
// instantly, so no placeholder ever reached a frame; the v3 transport makes a
// real round trip, and the headline legitimately exists for one round trip
// before the first Status() lands. That is a few dozen milliseconds, and
// reporting it would mean the check fires on correct code.
const READABLE_MS = 700;
const flashed = [...seenText.entries()].filter(
  ([txt, w]) => !settled.has(txt) && w.last - w.first >= READABLE_MS
);
ok(
  flashed.length === 0,
  'при открытии мелькнули и исчезли: ' +
    flashed.map(([t, w]) => `«${t}» ${w.first}–${w.last}ms`).join(', ')
);
ok(
  !seenText.has('being taken…'),
  'вернулась надпись-заглушка «being taken…», которую сменяет вердикт'
);
ok(
  !seenText.has('no data'),
  'разбивка по базам говорит «no data» до первого чтения'
);

// Two of three services are up and the world is not: the headline has to say
// the world is not there, not "2 of 3".
const headline = await text('[data-role="headline"]');
ok(headline && headline.includes('no world yet'), 'headline должен говорить, что мира нет, а не: ' + headline);
ok(!(headline || '').includes('The world is up'), 'headline не должен обещать игру при остановленном мире');
ok((await calls('Status')).length > 0, 'Status() не вызван при старте');
ok((await calls('Data')).length > 0, 'Data() не вызван при старте');
await shot('01-server');

/* ============================ 2. навигация =============================== */

// Each screen has to render something of its own. The Server screen has no
// <h1> any more - the hero's title was the thing that got removed - so it is
// checked by the one line it does have.
for (const [route, marker] of [['backups', 'h1'], ['more', 'h1'], ['server', '[data-role="headline"]']]) {
  await page.click(`[data-route="${route}"]`);
  await page.waitForTimeout(120);
  ok(await visible(marker), `экран ${route} не показал заголовок (${marker})`);
}
await shot('02-server-again');

/* ======================= 3. одна кнопка на мир ========================== */

// Two buttons carry the same role - start and stop - and exactly one of them
// is shown at a time. :visible picks the real one; without it the locator
// resolves to both and every click is a strict-mode violation.
//
// The frame is the page's own now, so the three buttons that close, minimise
// and restore the window must actually reach the runtime. Nothing else tests
// this: the window itself cannot be opened from here.
//
// The cross asks first when anything is running, and at this point in the run
// two of the three are. That window is section 14's business; here it is only
// proof that the button reaches the runtime at all.
const setService = (id, state) =>
  page.evaluate(([svc, st]) => {
    window.__state.services = window.__state.services.map((s) =>
      s.ID === svc ? { ...s, State: st, PID: st === 'stopped' ? 0 : 333, MemoryMB: st === 'stopped' ? 0 : 480 } : s);
    window.__fire('status', { Services: window.__state.services });
  }, [id, state]);

const setAllServices = (state) =>
  page.evaluate((st) => {
    window.__state.services = window.__state.services.map((s) => ({
      ...s,
      State: st,
      PID: st === 'stopped' ? 0 : 333,
      MemoryMB: st === 'stopped' ? 0 : 480
    }));
    window.__fire('status', { Services: window.__state.services });
  }, state);

await page.click('[data-role="win-minimise"]');
await page.click('[data-role="win-maximise"]');
await page.click('[data-role="win-close"]');
await page.waitForTimeout(250);
ok(await visible('[data-role="quit-ask"]'),
  'крестик не спросил про работающие службы — с двумя поднятыми он обязан спрашивать');
await page.click('[data-role="quit-stay"]');
await page.waitForTimeout(200);

// With nothing running the same button quits, which is the path the window
// needs. Then the two services this section's start state had are put back,
// because the next section is about the world coming up beside them.
await setAllServices('stopped');
await page.waitForTimeout(200);
await page.click('[data-role="win-close"]');
await page.waitForTimeout(250);
await setAllServices('running');
await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'world' ? { ...s, State: 'stopped', PID: 0, MemoryMB: 0 } : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(200);
// The three window buttons. Names are the v3 runtime's own: the minimise
// button CLOSES the window (section 13 explains why), the maximise button asks
// whether it is maximised and then Maximise or UnMaximise, and the cross quits.
// In v2 these were the global WindowMinimise / WindowToggleMaximise / Quit.
ok((await calls('Close')).length === 1, 'кнопка свернуть не закрыла окно');
ok((await calls('Minimise')).length === 0, 'кнопка свернуть прячет окно вместо закрытия');
ok((await calls('Maximise')).length === 1, 'кнопка развернуть не вызвала Maximise');
ok((await calls('Quit')).length === 1, 'крестик не вызвал Quit — окно нечем будет закрыть');
// Quit, not close: the backend's OnShutdown deliberately leaves the server
// running, and that must not change because the frame changed.
ok(!seenText.has('close'), 'закрытие окна идёт мимо Quit');

// The bug this guards is invisible to a click test and is the whole reason a
// click test would not have caught it: the drag region is decided by a computed
// custom property that INHERITS, and the runtime defers the drag to the first
// mousemove. So a button that merely looks outside the drag region is still in
// it if the property reaches it, and every click needs two tries.
//
// This reads exactly what the runtime reads - getComputedStyle on the element
// under the mouse - which is the only honest way to test it from a browser.
const drag = await page.evaluate(() => {
  const read = (sel) =>
    getComputedStyle(document.querySelector(sel)).getPropertyValue('--wails-draggable').trim();
  return {
    dragArea: read('[data-role="titlebar-drag"]'),
    strip: read('[data-role="titlebar"]'),
    buttons: ['minimise', 'maximise', 'close'].map((b) => read(`[data-role="win-${b}"]`))
  };
});
ok(drag.dragArea === 'drag', 'зона перетаскивания не помечена: ' + drag.dragArea);
ok(
  drag.buttons.every((v) => v !== 'drag'),
  'кнопки окна оказались внутри зоны перетаскивания — клик по ним будет съеден: ' +
    drag.buttons.join(', ')
);
ok(drag.strip !== 'drag', 'перетаскиваемым сделана вся полоса, а не её пустая часть');

// Double click on the drag area is the restore button, as in a system
// titlebar. It cannot fight the drag: two presses in one spot move nothing, so
// the runtime never starts a drag. The same command is called as by the button,
// which is the whole point - one command, two ways to ask.
await page.dblclick('[data-role="titlebar-drag"]');
await page.waitForTimeout(150);
ok(
  (await calls('Maximise')).length === 2,
  'двойной клик по полосе не развернул окно'
);

const toggle = page.locator('[data-role="world-toggle"]:visible');
ok((await toggle.textContent()).trim() === 'Start the server', 'главная кнопка должна предлагать запуск, когда мир выключен');

await toggle.click();
await page.waitForTimeout(200);
ok((await calls('StartAll')).length === 1, 'кнопка мира не вызвала StartAll');
ok((await text('[data-role="last-action"]')).includes('Starting the world'), 'полоса внизу не показала, что мир запускается');

/* ==================== 4. кнопка меняется на остановку ==================== */

await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'world' ? { ...s, State: 'running', PID: 333, MemoryMB: 480 } : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(150);
ok((await toggle.textContent()).trim() === 'Stop the server', 'кнопка не стала «Stop the server», когда мир поднялся');
ok((await text('[data-role="headline"]')).includes('The world is up'), 'headline не сообщил, что мир работает');

await shot('03-world-up');

await toggle.click();
await page.waitForTimeout(200);
ok((await calls('StopAll')).length === 1, 'кнопка мира не вызвала StopAll');

// The stub is not the backend: StopAll() answers and changes nothing, so the
// world is still up here. Put it off deliberately - otherwise the section below
// asserts against the state the previous one happened to leave behind.
//
// All three go down, not just the world, because that is what the stop ladder
// does and it is the whole point of this section: with only the world stopped
// and MySQL still up, the database is genuinely readable and the numbers
// genuinely belong on screen.
await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'world' ? { ...s, State: 'stopped', PID: 0, MemoryMB: 0 } : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(200);
await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'mysql' ? { ...s, State: 'stopped', PID: 0, MemoryMB: 0 } : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(200);

/* ===================== 5. остановленный мир ============================ */

// The world is off, and MySQL went down after it: that is the stop ladder, so
// the database is unreachable because of the button rather than in spite of
// it. Neither the database nor the stopped world may be reported.
//
// The stale copy does stay. It is read from backups\, not from the database, so
// stopping the world does not make it false - and it is the one thing on this
// screen a person can act on without starting anything.
// Scoped to the block itself, not to the whole page: the collapsed technical
// details still carry the full checklist with its labels, and asserting on
// `body` would find "Сервер запущен" in a section that is closed and was never
// meant to be read.
const idleProblems = await text('[data-role="problems"]');
ok(!(await visible('[data-role="tiles"]')), 'плитки показаны при выключенном мире: их нечем заполнить');
ok(!(await visible('[data-role="read-error"]')), 'ошибка чтения показана при выключенной базе');
ok(!(idleProblems || '').includes('The database answers'), 'база названа проблемой при выключенном мире: ' + idleProblems);
ok(!(idleProblems || '').includes('The server is running'), 'остановленный мир назван проблемой: ' + idleProblems);
ok((idleProblems || '').includes('12 days'), 'устаревшая копия потеряна вместе с базой: ' + idleProblems);
ok((idleProblems || '').includes('make a new copy'), 'устаревшая копия не назвала, что делать: ' + idleProblems);

// The hero is the screen: the world is not there, and it does not pretend it is.
const idleHeadline = await text('[data-role="headline"]');
ok(idleHeadline.includes('no world yet'), 'hero должен говорить, что мира нет, а не: ' + idleHeadline);
ok(!idleHeadline.includes('The world is up'), 'hero не должен обещать игру при остановленном мире');

/* ================== 5b. настоящая проблема остаётся ==================== */

// A stale copy is real and stays reported. It is read from backups\, not from
// the database, so stopping the world does not make it false - unlike the
// database, which the stop ladder took with it. The filter that hides the
// second must not swallow the first.
await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'world' ? { ...s, State: 'running', PID: 333, MemoryMB: 480 }
      : s.ID === 'mysql' ? { ...s, State: 'running', PID: 111, MemoryMB: 320 }
      : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(200);

// Both halves of the rule, together: the base is readable, so the realm section
// is back with real numbers, and the stale copy is still named.
ok(await visible('[data-role="tiles"]'), 'плитки не вернулись при работающей базе');
const upTiles = await text('[data-role="tiles"]');
ok(upTiles.includes('1.2 GB'), 'плитка размера баз пуста при работающей базе: ' + upTiles);
ok(upTiles.includes('1,420'), 'плитка персонажей пуста при работающей базе: ' + upTiles);
ok(!upTiles.includes('Copies on disk'), 'счётчик копий вернулся на главный экран, хотя у него есть свой раздел');

const upProblems = await text('[data-role="problems"]');
ok(upProblems.includes('12 days'), 'проблема копии потеряла подробность: ' + upProblems);
ok(upProblems.includes('make a new copy'), 'проблема копии не назвала, что делать: ' + upProblems);
ok(!upProblems.includes('The server is running'), 'работающий мир назван проблемой: ' + upProblems);
ok(!upProblems.includes('The database answers'), 'работающая база названа проблемой: ' + upProblems);

// And down again, to prove the tiles are conditional rather than gone.
await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'mysql' ? { ...s, State: 'stopped', PID: 0, MemoryMB: 0 } : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(200);
ok(!(await visible('[data-role="tiles"]')), 'плитки не убрались после остановки базы');

/* ==================== 6. службы и правила кнопок ======================== */

// Back up, because this section is about a service that is running: it is the
// state in which CAN has something to say. MySQL goes up with it - the next
// section needs a live database too.
await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'world' ? { ...s, State: 'running', PID: 333, MemoryMB: 480 }
      : s.ID === 'mysql' ? { ...s, State: 'running', PID: 111, MemoryMB: 320 }
      : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(200);

// The services block is not collapsible and has no heading: the per-service
// buttons and the checklist are worth having without a click, and a frame with
// a title that must be clicked to read is a title people do not click.
ok(!(await visible('[data-role="tech-toggle"]')), 'у блока служб осталась кнопка раскрытия');
const techText = await text('[data-role="tech"]');
ok(!techText.includes('Technical details'), 'надпись «Technical details» осталась');
ok(!techText.includes('what the server is made of'), 'подпись блока осталась');
ok(await visible('[data-act="start"][data-svc="world"]'), 'кнопки служб не видны без клика');

// The quick-copy button is gone from the hero: copies have a screen, and a
// shortcut whose progress lives elsewhere is how a person presses it twice.
ok(!(await visible('[data-role="quick-backup"]')), 'на главном экране осталась кнопка «Создать копию»');

const startWorldDisabled = await page.isDisabled('[data-act="start"][data-svc="world"]');
ok(startWorldDisabled, '«Запустить» у уже работающего мира должна быть серой');
ok(!(await page.isDisabled('[data-act="stop"][data-svc="world"]')), '«Остановить» у работающего мира должна быть живой');
ok(techText.includes('Worldserver'), 'строка службы потеряла имя процесса');
await shot('04-tech');

/* ============================ 7. копии ================================== */

await page.click('[data-route="backups"]');
await page.waitForTimeout(150);
ok(await visible('[data-role="backup-logical"]'), 'нет кнопки «копия без остановки сервера»');

// The full copy needs the server stopped; with the world up the button must be
// grey and the line must say why. This is the decision the whole screen is for.
const physDisabled = await page.isDisabled('[data-role="backup-physical"]');
ok(physDisabled, '«Полная копия» доступна при работающем мире');
ok((await text('body')).includes('stop the world first'), 'нет подсказки «сначала остановите мир»');

await page.click('[data-role="backup-logical"]');
await page.waitForTimeout(200);
const bc = await calls('CreateBackup');
ok(bc.length === 1 && bc[0].args[0] === 'logical', 'быстрая копия не вызвала CreateBackup("logical")');
await shot('05-backups');

// Only a full copy carries the restore button.
const restoreBtns = page.locator('[data-role="restore"]');
ok((await restoreBtns.count()) === 1, 'кнопка отката должна быть только у полной копии');
// The verb has to name what it acts on: a bare "Restore" next to "Delete"
// read as the opposite of it, and promised data coming back when it goes.
ok(
  (await text('[data-role="restore"]')).includes('Roll the database back'),
  'кнопка отката не называет, что она делает с базой'
);
await restoreBtns.first().click();
await page.waitForTimeout(200);
ok(await visible('[data-role="restore-ask"]'), 'диалог возврата не открылся');
await shot('06-restore-ask');
await page.click('[data-role="restore-yes"]');
await page.waitForTimeout(200);
ok((await calls('RestoreSnapshot')).length === 1, 'ответ «Откатить» не вызвал RestoreSnapshot');

// The line under a restorable row repeated the button's own words, and the
// line under one without a button is the only place saying the rollback
// exists and where to reach it. So the first must be gone and the second kept.
ok(
  !(await text('[data-role="points"] li:has([data-role="restore"])')).includes('Roll the database back'),
  'строка под полной копией повторяет слова кнопки'
);
ok(
  (await text('[data-role="points"] li:not(:has([data-role="restore"]))')).includes(
    'restore-all-db.cmd'
  ),
  'под копией без кнопки должно быть сказано, чем её возвращать'
);

// Delete is on every row, including the ones with no restore button, and it
// asks first - the one window here with no way back.
const delBtns = page.locator('[data-role="delete"]');
ok(
  (await delBtns.count()) === (await page.locator('[data-role="points"] li').count()),
  'кнопка «Удалить» должна быть у каждой копии'
);
await delBtns.last().click();
await page.waitForTimeout(200);
ok(await visible('[data-role="delete-ask"]'), 'диалог удаления не открылся');
ok(
  (await text('[data-role="delete-ask"]')).includes('nothing can bring it back'),
  'в диалоге удаления не сказано, что копию не вернуть'
);
await shot('06b-delete-ask');
await page.click('[data-role="delete-no"]');
await page.waitForTimeout(200);
ok(!(await visible('[data-role="delete-ask"]')), '«Оставить» не закрыл диалог');
ok((await calls('DeleteBackup')).length === 0, '«Оставить» всё равно удалил копию');
await delBtns.last().click();
await page.waitForTimeout(150);
await page.click('[data-role="delete-yes"]');
await page.waitForTimeout(200);
const dc = await calls('DeleteBackup');
ok(dc.length === 1 && typeof dc[0].args[0] === 'string' && dc[0].args[0].length > 0,
  'ответ «Удалить» не вызвал DeleteBackup с именем папки');

/* ========================= 8. прогресс копии ============================ */

await page.evaluate(() => {
  window.__state.backup = { Running: true, Kind: 'logical', Step: 'acore_world', Index: 1,
    Total: 4, Bytes: 12000000, ElapsedSec: 42, Dir: '', SavedBytes: 0, Databases: [],
    Err: '', Warnings: [], At: '' };
  window.__fire('backup', window.__state.backup);
});
await page.waitForTimeout(200);
ok(await visible('[data-role="backup-progress"]'), 'полоса прогресса не показалась во время копии');
ok((await text('[data-role="backup-progress"]')).includes('step 1 of 4'), 'в прогрессе нет шага');
ok(!(await page.isDisabled('[data-role="backup-cancel"]')), '«Отменить» должна быть живой, пока идёт копия');
await shot('07-progress');

// During a restore the cancel is hidden, not grey: an archive unpacked over a
// live database folder cannot be interrupted.
await page.evaluate(() => {
  window.__state.backup = { ...window.__state.backup, Kind: 'restore' };
  window.__fire('backup', window.__state.backup);
});
await page.waitForTimeout(200);
ok(!(await visible('[data-role="backup-cancel"]')), '«Отменить» видна во время отката, который не отменяется');

await page.evaluate(() => {
  window.__state.backup = { ...window.__state.backup, Running: false, Kind: 'logical', At: '2026-10-02T10:00:00Z',
    SavedBytes: 385000000, ElapsedSec: 105, Dir: 'C:/games/wow/server/backups/2026-10-02_current' };
  window.__fire('backup', window.__state.backup);
});
await page.waitForTimeout(200);
const result = await text('[data-role="backup-result"]');
ok(result && result.includes('Done'), 'итог копии не показан после её завершения');
await shot('08-backup-done');

/* ============================= 9. ещё =================================== */

await page.click('[data-route="more"]');
await page.waitForTimeout(150);

await section('Characters').click();
await page.waitForTimeout(300);
ok((await text('body')).includes('Гном Кирин'), 'список персонажей не загрузился');
ok((await calls('Characters')).length > 0, 'персонажи грузятся только при открытии раздела, но Characters() не вызван');

// The search looks at every row, not only the painted ones.
await page.fill('[data-role="chars-search"]', 'thrald');
await page.waitForTimeout(200);
const filtered = await text('body');
ok(filtered.includes('Thrald') && !filtered.includes('Гном Кирин'), 'поиск по персонажам не фильтрует список');
await page.fill('[data-role="chars-search"]', '');
await page.waitForTimeout(150);

await section('Accounts').click();
await page.waitForTimeout(300);
ok((await text('body')).includes('ADMIN'), 'список аккаунтов не загрузился');

// Stop the world first: the rule under test is that a check belonging to a
// service that is deliberately stopped stays neutral, so the world has to be
// stopped for the assertion to mean anything.
await page.evaluate(() => {
  window.__state.services = window.__state.services.map((s) =>
    s.ID === 'world' ? { ...s, State: 'stopped', PID: 0, MemoryMB: 0 } : s);
  window.__fire('status', { Services: window.__state.services });
});
await page.waitForTimeout(150);

await section('Checks').click();
await page.waitForTimeout(300);
const checks = await text('body');
ok(checks.includes('✓'), 'проверки не показали знак «всё в порядке»');
// A settled service with a failing check names the action; a stopped one stays
// neutral, because its port is supposed to be closed.
ok(checks.includes('Look at its window and the logs folder'), 'упавшая проверка не назвала, что делать');
ok(!checks.includes('The world did not start'), 'остановленный мир назван поломкой');
await shot('09-more');

await section('Server folders').click();
await page.waitForTimeout(200);
await page.getByRole('button', { name: 'Open the logs' }).click();
await page.waitForTimeout(200);
ok((await calls('OpenFolder')).length === 1, 'кнопка папки не вызвала OpenFolder');

/* ==================== 10. подтверждение и диалоги ======================= */

await page.evaluate(() =>
  window.__fire('confirm-kill', { service: 'world', pid: 333 }));
await page.waitForTimeout(300);
ok(await visible('[data-role="confirm-kill"]'), 'диалог «процесс не завершился» не открылся');
ok((await text('[data-role="confirm-kill"]')).includes('character'),
  'диалог не предупреждает о несохранённом персонаже');
await shot('10-confirm-kill');
await page.click('[data-role="confirm-yes"]');
await page.waitForTimeout(200);
ok((await calls('RespondConfirm')).length === 1 && (await calls('RespondConfirm'))[0].args[0] === true,
  'ответ в диалоге не дошёл до RespondConfirm(true)');

// The backend gives up on a dialog nobody answers and says so; the page must
// not keep showing a question that is no longer being listened to.
await page.evaluate(() => window.__fire('confirm-kill', { service: 'world', pid: 333 }));
await page.waitForTimeout(200);
await page.evaluate(() => window.__fire('confirm-cancel'));
await page.waitForTimeout(200);
ok(!(await visible('[data-role="confirm-kill"]')), 'диалог остался на экране после confirm-cancel');

/* ====================== 11. ошибка не остаётся без слов ================== */

statusFails = true;
await page.click('[data-role="refresh"]');
await page.waitForTimeout(300);
ok((await text('[data-role="last-action"]')).length > 1, 'строка последнего действия молчит после ошибки');

/* ============ 13. сворачивание закрывает окно, а не выходит ============= */

// The button used to call WindowMinimise, which leaves the webview alive and
// the twelve WebView2 processes with it - 285 MB for a window nobody is
// looking at. It must now close the window instead, and it must not take the
// program with it: the panel comes back from the tray.
//
// Both halves are asserted. A click test alone would pass on a button that
// quits, which is exactly the bug that was here before.
const quitsBefore = calls('Quit').length;
await page.click('[data-role="win-minimise"]');
await page.waitForTimeout(600);
ok(calls('Close').length > 0, 'кнопка «Свернуть» не закрывает окно');
ok(calls('Minimise').length === 0, 'кнопка «Свернуть» всё ещё прячет окно вместо закрытия');
ok(calls('Quit').length === quitsBefore, 'панель вышла, когда окно свернули');

/* ============ 14. крестик спрашивает, когда что-то работает ============== */

// The world outlives the panel now - WinOps.Start hands it a pipe that cannot
// reach end of stream - so closing the panel while it runs leaves something
// behind that the next panel can only kill. The cross is therefore the one
// place where stopping it first is still cheap: the pipe is held only while
// this panel lives, and "server shutdown 1" is the stop that saves.
//
// One of the three counts, not only the world: MySQL and the login server are
// up almost whenever the world is, and a panel that quits over their heads
// leaves a database writing to nothing.

const setWorld = (state) => setService('world', state);
const setAuth = (state) => setService('auth', state);

// Nothing running, nothing to ask: the cross leaves immediately. Section 3
// already proved that path with the whole stack down.
await setAllServices('stopped');
await page.waitForTimeout(200);
let quitBefore = (await calls('Quit')).length;
await page.click('[data-role="win-close"]');
await page.waitForTimeout(250);
ok(!(await visible('[data-role="quit-ask"]')), 'крестик спросил при выключенных серверах');
ok((await calls('Quit')).length === quitBefore + 1, 'крестик не вышел, когда ничего не работает');

// Only the database is up. This is the case the check exists for: the world
// is not there, and the panel must still not quit over MySQL's head.
await setService('mysql', 'running');
await page.waitForTimeout(200);
await page.click('[data-role="win-close"]');
await page.waitForTimeout(250);
ok(await visible('[data-role="quit-ask"]'), 'крестик не спросил при работающей базе');
const askText = await text('[data-role="quit-ask"]');
ok(askText.includes('Running right now'), 'диалог не называет, что именно работает: ' + askText.slice(0, 120));
ok(askText.includes('in the right order'), 'диалог не называет порядок остановки');
await page.click('[data-role="quit-stay"]');
await page.waitForTimeout(200);
ok(!(await visible('[data-role="quit-ask"]')), 'отказ не закрыл окно вопроса');

await setWorld('running');
await setAuth('running');
await page.waitForTimeout(200);

quitBefore = (await calls('Quit')).length;
let stopBefore = (await calls('StopAll')).length;
await page.click('[data-role="win-close"]');
await page.waitForTimeout(250);
ok(await visible('[data-role="quit-ask"]'), 'крестик не спросил при работающем мире');
ok((await calls('Quit')).length === quitBefore, 'крестик вышел из панели, не спросив');
ok((await calls('StopAll')).length === stopBefore, 'диалог остановки что-то остановил сам');

// Declining is "cancel the close", not "close anyway": leaving the world
// running is a thing to do on purpose, and the tray's "Выход" is how.
await page.click('[data-role="quit-stay"]');
await page.waitForTimeout(200);
ok(!(await visible('[data-role="quit-ask"]')), 'отказ не закрыл окно вопроса');
ok((await calls('Quit')).length === quitBefore, 'отказ всё же вышел из панели');
ok((await calls('StopAll')).length === stopBefore, 'отказ остановил мир');

// A world this panel did not start cannot be asked to stop. The dialog has to
// say so, or a person agrees to something they are not getting.
await page.click('[data-role="win-close"]');
await page.waitForTimeout(200);
await setWorld('external');
await page.waitForTimeout(200);
ok(await visible('[data-role="quit-ask-forced"]'),
  'диалог молчит о принудительной остановке чужого мира');
await shot('14-quit-ask');

// Agreeing stops the stack and only then leaves. The order is the assertion:
// StopAll before Quit, or the panel quits in front of a live world.
await setWorld('running');
await page.waitForTimeout(150);
const beforeExit = callLog.length;
await page.click('[data-role="quit-stop"]');
await page.waitForTimeout(400);
const exitCalls = callLog.slice(beforeExit).map((c) => c.name).filter((n) => n === 'StopAll' || n === 'Quit');
ok(exitCalls.length === 2 && exitCalls[0] === 'StopAll' && exitCalls[1] === 'Quit',
  'кнопка «Остановить и выйти» пошла не так: ' + JSON.stringify(exitCalls));

/* ============================ итог ====================================== */

const pageErrors = await page.evaluate(() => window.__errors);
ok(pageErrors.length === 0, 'страница бросила ошибки: ' + pageErrors.join(' | '));
ok(consoleErrors.length === 0, 'консоль сообщила об ошибках: ' + consoleErrors.join(' | '));

reported = true;
await browser.close();
server.close();

if (notes.length) {
  console.log('\nЗамечания:');
  for (const n of notes) console.log('  - ' + n);
}

if (fails.length) {
  console.error('\nFAIL (' + fails.length + ')');
  for (const f of fails) console.error('  - ' + f);
  process.exit(1);
}

console.log('\nOK — 3 экрана, кнопки, диалоги, прогресс копии, поиск.');