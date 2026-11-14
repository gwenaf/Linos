import { signal } from '@preact/signals'
import { Banner } from './ui'

// Shown at /control on a machine without the host's rights: the way in is the admin token of a
// self-hosted server, or an invite made on the machine already running the game.
const login = signal(false)
fetch('/login', { method: 'HEAD' })
  .then((r) => (login.value = r.ok))
  .catch(() => {})

// openInvite accepts the link copied from the control page, or the code alone.
function openInvite(e: Event) {
  e.preventDefault()
  const text = String(new FormData(e.target as HTMLFormElement).get('invite') ?? '').trim()
  const code = text.match(/invite=([\w-]+)/)?.[1] ?? text
  if (code) location.href = `/control?invite=${encodeURIComponent(code)}`
}

export default function Access() {
  return (
    <main>
      <Banner />
      <h1>Piloter la partie</h1>
      <p>Ce poste n'a pas encore le droit de piloter Linos.</p>
      {login.value && (
        <section>
          <h2>Avec le jeton administrateur</h2>
          <form method="post" action="/login">
            <input type="password" name="token" placeholder="Jeton administrateur" autocomplete="current-password" required />{' '}
            <button class="primary">Se connecter</button>
          </form>
          <p>
            <small>Le jeton est la valeur de LINOS_ADMIN_TOKEN donnée au serveur. Ce navigateur reste connecté 30 jours.</small>
          </p>
        </section>
      )}
      <section>
        <h2>Avec une invitation</h2>
        <p>
          Sur le poste qui pilote déjà la partie, cliquez « Inviter un maître du jeu », puis collez ici le lien affiché (valable 2 minutes, usage
          unique).
        </p>
        <form onSubmit={openInvite}>
          <input name="invite" placeholder="Lien ou code d'invitation" required /> <button>Rejoindre</button>
        </form>
      </section>
    </main>
  )
}
