// Smoke test of the whole app in a real browser: builds and starts Linos on a temporary home,
// creates a pack in /edit, exports it, loads it with "Jouer ce pack", then plays a track
// from the TV screen and a phone. Run with `npm run e2e` (needs Go, and Chrome or Edge;
// CHROME_PATH overrides the browser).
import { execFileSync, spawn } from 'node:child_process'
import { existsSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import puppeteer from 'puppeteer-core'

const root = resolve(import.meta.dirname, '../..')
const home = mkdtempSync(join(tmpdir(), 'linos-e2e-'))
const browsers = [
  process.env.CHROME_PATH,
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
]
const executablePath = browsers.find((p) => p && existsSync(p))
if (!executablePath) throw new Error('no Chrome or Edge found: set CHROME_PATH')

// A short silent WAV: a real media file every browser plays and ffmpeg can cut.
function wav(seconds) {
  const rate = 8000
  const data = rate * seconds
  const b = Buffer.alloc(44 + data)
  b.write('RIFF', 0)
  b.writeUInt32LE(36 + data, 4)
  b.write('WAVEfmt ', 8)
  b.writeUInt32LE(16, 16)
  b.writeUInt16LE(1, 20)
  b.writeUInt16LE(1, 22)
  b.writeUInt32LE(rate, 24)
  b.writeUInt32LE(rate, 28)
  b.writeUInt16LE(1, 32)
  b.writeUInt16LE(8, 34)
  b.write('data', 36)
  b.writeUInt32LE(data, 40)
  b.fill(128, 44)
  return b
}

const exe = join(home, process.platform === 'win32' ? 'linos.exe' : 'linos')
execFileSync('go', ['build', '-o', exe, './cmd/linos'], { cwd: root, stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0' } })
const server = spawn(exe, [], { env: { ...process.env, LINOS_HOME: home, LINOS_ADDR: '127.0.0.1:0' } })
const base = await new Promise((ok, ko) => {
  server.on('exit', (code) => ko(new Error(`linos exited with ${code}`)))
  server.stderr.on('data', (chunk) => {
    const port = /"control":"http:\/\/localhost:(\d+)\/control"/.exec(String(chunk))?.[1]
    if (port) ok(`http://localhost:${port}`)
  })
})

// The TV, the phone and the control page share one browser: background tabs must keep playing and ticking.
const browser = await puppeteer.launch({
  executablePath,
  headless: true,
  args: [
    '--autoplay-policy=no-user-gesture-required',
    '--disable-background-media-suspend',
    '--disable-background-timer-throttling',
    '--disable-renderer-backgrounding',
    '--disable-backgrounding-occluded-windows',
  ],
})
const errors = []
async function open(path, viewport = { width: 1280, height: 900 }) {
  const page = await browser.newPage()
  await page.setViewport(viewport)
  watch(page, path)
  await page.goto(base + path, { waitUntil: 'domcontentloaded' })
  return page
}
function watch(page, name) {
  page.on('pageerror', (e) => errors.push(`${name}: ${e.message}`))
  page.on('dialog', (d) => d.accept())
}
const wait = (ms) => new Promise((r) => setTimeout(r, ms))
// Waits poll on an interval: puppeteer's default animation-frame polling stalls in background tabs.
const until = (page, fn, arg) => page.waitForFunction(fn, { polling: 200, timeout: 10_000 }, arg)
const expectText = (page, text) => until(page, (t) => document.body.innerText.includes(t), text)
// Waits for a text, then clicks the element holding it through the DOM.
async function click(page, text) {
  await expectText(page, text)
  await (await page.$(`::-p-text(${text})`)).evaluate((b) => b.click())
}

let failed = false
let host, phone
try {
  // Editor: create a pack from two media, check a problem is reported, then export it.
  const edit = await open('/edit')
  await expectText(edit, 'Nouveau pack')
  await edit.type('input[placeholder="Nom du dossier"]', 'smoke')
  await click(edit, 'Créer')
  await expectText(edit, '1 Pack')
  await click(edit, '4 Pistes')
  const media = [join(home, 'Artist - First.wav'), join(home, 'Artist - Second.wav')]
  media.forEach((p) => writeFileSync(p, wav(3)))
  await (await edit.$('.dropzone input[type=file]')).uploadFile(...media)
  await expectText(edit, 'Artist - Second.wav')

  await click(edit, '+ manche')
  await click(edit, 'Enregistrer')
  await expectText(edit, 'aucune piste dans la manche')
  await click(edit, 'Supprimer')
  await click(edit, 'Enregistrer')
  await click(edit, 'Export')
  await expectText(edit, 'Le pack est jouable.')
  await click(edit, 'Exporter en .linospack')
  await expectText(edit, 'Exporté : packs/smoke.linospack')

  // "Jouer ce pack" opens the control page with the pack loaded.
  const opened = new Promise((ok) => browser.once('targetcreated', (t) => ok(t.page())))
  await click(edit, 'Jouer ce pack')
  const control = await opened
  watch(control, '/control')
  await control.setViewport({ width: 1280, height: 900 })
  await until(control, () => document.querySelector('button.selected')?.textContent.includes('smoke'))

  // A game: the TV, one phone, one track bought by a buzz and validated.
  host = await open('/host')
  await host.click('main')
  phone = await open('/play', { width: 390, height: 800, isMobile: true })
  await phone.type('input[name=name]', 'Alice')
  await click(phone, 'Rejoindre')
  await click(phone, 'Je suis prêt')
  await until(control, () => [...document.querySelectorAll('button')].some((b) => b.textContent.includes('Lancer la partie') && !b.disabled))
  // The TV must be the visible tab: browsers defer loading media in background tabs.
  await host.bringToFront()
  await click(control, 'Lancer la partie')
  await expectText(host, 'Piste 1 / 2')
  await until(phone, () => !!document.querySelector('button.buzz:not([disabled])'))
  // The buzzer reacts on pointerdown; a real click would wait for the background tab to paint.
  await phone.$eval('button.buzz', (b) => b.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true })))
  await until(control, () => !!document.querySelector('button.good'))
  await (await control.$('button.good')).evaluate((b) => b.click())
  // One of the two guesses found: the phone and the control page show the points.
  await expectText(phone, 'Bravo ! +100')
  await until(control, () => /Alice\s*100/.test(document.body.innerText))
  console.log('smoke: editor, export, control, TV and phone OK')
} catch (e) {
  failed = true
  console.error('smoke failed:', e.message, e.stack.split('\n').find((l) => l.includes('smoke.mjs')))
  if (phone) console.error('phone:', await phone.evaluate(() => ({ text: document.body.innerText.slice(0, 300), buzz: document.querySelector('button.buzz')?.outerHTML })))
  if (host) console.error('TV:', await host.evaluate(() => { const v = document.querySelector('.stage video'); return { text: document.body.innerText.slice(0, 200), media: v && { src: v.src, ready: v.readyState, paused: v.paused, error: v.error?.code, net: v.networkState } } }))
} finally {
  if (errors.length > 0) {
    failed = true
    console.error('JavaScript errors:', errors)
  }
  await browser.close()
  server.kill()
  await wait(300)
  rmSync(home, { recursive: true, force: true })
  process.exit(failed ? 1 : 0)
}
