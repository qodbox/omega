# Interface Omega

L'application Nuxt 4 que consomme l'API JSON d'Omega. Elle n'en dépend que par
HTTP : pas de rendu serveur, pas de template partagé.

```bash
bun install --frozen-lockfile   # une fois bun.lock committé
bun run dev      # http://localhost:5173, /api et /graphql renvoyés vers Omega
bun run build    # sortie statique dans .output/public
bun run lint     # oxlint
```

`OMEGA_URL` change la cible du proxy en développement (défaut `http://127.0.0.1:3000`).

## SPA, pas de SSR

`ssr: false` : le build produit des fichiers statiques, donc **aucun serveur
Node en production**. Omega ne sert que du JSON — un second serveur qui
dupliquerait le routage n'apporterait rien à un tableau de bord derrière
authentification.

## Ce qu'elle contient

| | |
|---|---|
| Nuxt 4 · Vue 3 · TypeScript | la base |
| Tailwind 4 | les jetons du système de design |
| `app/components/ui` | 17 composants maison, aucune dépendance UI |
| `useAsyncData` | cache et états de chargement, fournis par Nuxt |
| `useI18n` maison | 50 lignes — anglais, français, espagnol |
| @lucide/vue | les icônes, arborescence secouée |

Pages : accueil, documentation, connexion, tableau de bord, utilisateurs.

Les overlays utilisent l'élément natif `<dialog>` : le navigateur gère le focus
piégé, le fond et la touche Échap. Aucune bibliothèque d'overlay.

## Le système de design

Les jetons vivent dans `app/assets/css/main.css` et suivent trois échelles
sémantiques qui basculent seules en mode sombre — n'écrivez pas de variante
`dark:` pour ce qu'une échelle couvre déjà.

| Échelle | Rôle |
|---|---|
| `text-ink-gray-1..9` | la hiérarchie du texte — `9` le plus fort, `5` les méta |
| `bg-surface-canvas` · `surface-base` · `surface-gray-1..10` | les fonds |
| `border-outline-gray-1..5` | les bordures et les anneaux |

L'action primaire est **grise, presque noire** : la couleur ne sert qu'à porter
une information. `--brand-6` dans `:root` est le seul jeton à changer pour
reteinter l'ensemble.

Deux échelles typographiques de mêmes tailles : `text-*` pour les libellés
d'une ligne, `text-p-*` pour le texte qui passe à la ligne.

## Le client est typé depuis l'API

`app/lib/api-types.ts` est généré, jamais écrit à la main. Le serveur tournant :

```bash
curl -s http://127.0.0.1:3000/api/openapi.json > openapi.json
bun x openapi-typescript openapi.json -o app/lib/api-types.ts
```

## Authentification

La session vit dans des cookies que le navigateur gère seul : aucun jeton
n'est lisible depuis JavaScript (`app/lib/api.ts`, partagé avec React et Next).

- `omega_access` et `omega_refresh` sont `HttpOnly` ; chaque appel part avec
  `credentials: 'include'`.
- `POST`, `PUT`, `PATCH`, `DELETE` renvoient le cookie lisible `omega_csrf`
  dans `X-CSRF-Token` ; `GET /api/auth/csrf` le pose au démarrage.
- un `401` déclenche un seul `POST /api/auth/refresh`, partagé par toutes les
  requêtes en échec, puis la requête est rejouée une fois.
- l'utilisateur connecté vient de `GET /api/auth/me` ; une déconnexion dans un
  onglet déconnecte les autres (`BroadcastChannel('omega-auth')`).
- `?next=` après connexion n'est suivi que s'il désigne un chemin de cette
  origine.

Seule `NUXT_PUBLIC_API_BASE` (base publique de l'API, vide = même origine) est
lue depuis l'environnement : tout ce qui est sous `runtimeConfig.public` part
chez chaque visiteur, n'y mettez jamais de secret.

## En-têtes de sécurité

- `server/plugins/csp.ts` calcule au `nuxt generate` l'empreinte sha256 des
  scripts en ligne de la page (config d'exécution, importmap) et pose une CSP
  qui n'autorise qu'eux : en `<meta>` dans le HTML statique, en en-tête quand
  Nitro sert la page.
- `routeRules` dans `nuxt.config.ts` ajoute `X-Content-Type-Options`,
  `Referrer-Policy`, `Permissions-Policy`, `X-Frame-Options`, HSTS là où Nitro
  sert l'application ou génère un fichier `_headers`.
- Un hébergeur statique simple n'applique ni l'un ni l'autre : configurez ces
  en-têtes sur le serveur, `frame-ancestors 'none'` compris (une `<meta>` ne
  peut pas le porter). `security.ts` fait foi ; le README de l'interface React
  en donne la version nginx et Caddy.

Aucun composant ne rend de HTML brut (`v-html`) : la documentation est faite de
blocs typés (`app/lib/docs.ts`) rendus en texte.

## Installations

`.npmrc` et `bunfig.toml` figent les versions exactes et refusent les scripts
d'installation des dépendances. Committez le lockfile et installez depuis lui
en CI (`bun install --frozen-lockfile` ou `npm ci`). Nuxt 4.6 demande Node
22.22 ou plus récent.
