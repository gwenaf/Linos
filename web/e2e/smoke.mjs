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
const server = spawn(exe, [], { env: { ...process.env, LINOS_HOME: home, LINOS_ADDR: '127.0.0.1:0', LINOS_BROWSER: 'off' } })
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
let host, phone, edit
try {
  // Editor: an empty pack is set up by the wizard, then opens on its first question; each question gets its file, the first one
  // reveals with the other file; a theme round with no theme is reported, then removed; the pack is exported.
  edit = await open('/edit')
  await expectText(edit, 'Nouveau pack')
  await (await edit.waitForSelector('input[placeholder="Nom du pack"]')).type('smoke')
  await click(edit, 'Créer')
  // The guided setup: one player each, one classic round, then its first question.
  await expectText(edit, 'Qui joue ?')
  await click(edit, 'Suivant')
  await click(edit, 'Classique')
  await click(edit, 'Terminer')
  await expectText(edit, 'Question 1')
  const media = [join(home, 'Artist - First.wav'), join(home, 'Artist - Second.wav')]
  media.forEach((p, i) => writeFileSync(p, wav(3 + i))) // different files: identical ones are one song
  const dropInQuestion = async (file) => (await edit.waitForSelector('.question .dropzone input[type=file]')).uploadFile(file)
  await dropInQuestion(media[0])
  await expectText(edit, 'Artist · First')
  await until(edit, () => !!document.querySelector('.question .waveform canvas'))
  await click(edit, '+ question')
  await expectText(edit, 'Question 2')
  await dropInQuestion(media[1])
  await expectText(edit, 'Artist · Second')

  // A theme gets its own questions, each with its file.
  await (await edit.waitForSelector('.new-theme input')).type('Rock')
  await click(edit, '+ thème')
  await expectText(edit, 'Thème · Rock')
  await (await edit.waitForSelector('.outline .theme > button')).evaluate((b) => b.click())
  await expectText(edit, 'Question 3')
  await dropInQuestion(media[1])
  await until(edit, () => document.querySelector('.outline .theme .questions')?.innerText.includes('Artist · Second'))
  // The same song dropped again in bulk is not added a second time.
  await (await edit.waitForSelector('.outline > .dropzone input[type=file]')).uploadFile(media[0])
  await expectText(edit, 'Déjà dans le pack')
  // A question dragged from its theme to the round, then back.
  const drag = (from, to) =>
    edit.evaluate(
      (from, to) => {
        const data = new DataTransfer()
        document.querySelector(from).dispatchEvent(new DragEvent('dragstart', { bubbles: true, dataTransfer: data }))
        document.querySelector(to).dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: data }))
      },
      from,
      to,
    )
  await drag('.outline .theme .questions li', '.outline > section:first-child')
  await until(edit, () => document.querySelectorAll('.outline > section:first-child .questions li').length === 3)
  await drag('.outline > section:first-child .questions li:last-child', '.outline .theme')
  await until(edit, () => document.querySelectorAll('.outline > section:first-child .questions li').length === 2 && !!document.querySelector('.outline .theme .questions li'))

  await click(edit, '1. Artist · First')
  await click(edit, 'un autre fichier (le morceau')
  const outroChoice = (await edit.$$('.question .media-choice select')).at(-1)
  await outroChoice.select('Artist - Second.wav')
  await until(edit, () => document.querySelectorAll('.question .waveform').length === 2)

  await click(edit, '+ manche')
  await click(edit, 'Thème entier')
  await expectText(edit, 'Chaque choix enchaîne toutes les questions du thème')
  await expectText(edit, 'aucun thème choisi')
  // A theme created in the round is drawn by it and listed under it.
  await (await edit.waitForSelector('.panel .new-theme input')).type('Jazz\n')
  await expectText(edit, 'Thème · Jazz')
  await until(edit, () => [...document.querySelectorAll('.outline > section')].some((s) => s.innerText.includes('Manche 2') && s.innerText.includes('Jazz')))
  await click(edit, 'Manche 2')
  await click(edit, 'Supprimer')
  await click(edit, 'Exporter')
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
  await (await host.waitForSelector('main')).click()
  phone = await open('/play', { width: 390, height: 800, isMobile: true })
  await (await phone.waitForSelector('input[name=name]')).type('Alice')
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
  // The gamemaster ticks the artist only, then validates.
  await until(control, () => !!document.querySelector('.validation input[type=checkbox]'))
  await (await control.$('.validation input[type=checkbox]')).evaluate((c) => c.click())
  await (await control.$('.validation button.primary')).evaluate((b) => b.click())
  // One of the two guesses found: the phone and the control page show the points.
  // Speed scoring: a slow machine buzzes a little later and earns a little less than 100.
  await until(phone, () => /Bravo ! \+\d+/.test(document.body.innerText))
  await until(control, () => /Alice\s*\d+/.test(document.body.innerText))
  // Ending the track: the TV plays the outro file instead of the rest of the question.
  await click(control, 'Passer la piste')
  await until(host, () => decodeURIComponent(document.querySelector('.stage video')?.src ?? '').endsWith('Artist - Second.wav'))
  console.log('smoke: editor, export, control, TV with outro file, and phone OK')
} catch (e) {
  failed = true
  console.error('smoke failed:', e.message, e.stack.split('\n').find((l) => l.includes('smoke.mjs')))
  if (edit) console.error('editor:', await edit.evaluate(() => document.body.innerText.slice(0, 600)))
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
