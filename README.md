# React + TypeScript + Vite

## Configuration

`COOKIE_SECURE` (default `true`): whether the Refresh token cookie is marked `Secure`. Leave this at its default for any deployment reachable over HTTPS — including behind a reverse proxy that terminates TLS for you. Set it to `false` only if you're deliberately running this instance over plain, unencrypted HTTP (for example, on a private LAN with no TLS in front of it): a browser silently discards a `Secure` cookie sent over plain HTTP, so with `COOKIE_SECURE` left on, login on such a deployment will appear broken — you get an access token, then get bounced back to the login screen on the next page load or once it expires, with no error shown. Setting `COOKIE_SECURE=false` on an instance that's actually reachable over plain HTTP from an untrusted network sends the Refresh token in the clear, letting anyone on-path steal it and hijack the session — only turn it off when you've deliberately chosen not to run TLS. See ADR-0009.

## Data & backups

Everything this instance keeps is under `DATA_DIR` (default `/data`): `calich.db` (SQLite) and, as of Attachments (#132, ADR-0040), an `attachments/` directory holding every uploaded file's bytes. A backup that copies only `calich.db` silently loses every Attachment — back up the whole `DATA_DIR`, not just the database file.

This template provides a minimal setup to get React working in Vite with HMR and some ESLint rules.

Currently, two official plugins are available:

- [@vitejs/plugin-react](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react) uses [Oxc](https://oxc.rs)
- [@vitejs/plugin-react-swc](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react-swc) uses [SWC](https://swc.rs/)

## React Compiler

The React Compiler is enabled on this template. See [this documentation](https://react.dev/learn/react-compiler) for more information.

Note: This will impact Vite dev & build performances.

## Expanding the ESLint configuration

If you are developing a production application, we recommend updating the configuration to enable type-aware lint rules:

```js
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...

      // Remove tseslint.configs.recommended and replace with this
      tseslint.configs.recommendedTypeChecked,
      // Alternatively, use this for stricter rules
      tseslint.configs.strictTypeChecked,
      // Optionally, add this for stylistic rules
      tseslint.configs.stylisticTypeChecked,

      // Other configs...
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])

```

You can also install [eslint-plugin-react-x](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-x) and [eslint-plugin-react-dom](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-dom) for React-specific lint rules:

```js
// eslint.config.js
import reactX from 'eslint-plugin-react-x'
import reactDom from 'eslint-plugin-react-dom'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...
      // Enable lint rules for React
      reactX.configs['recommended-typescript'],
      // Enable lint rules for React DOM
      reactDom.configs.recommended,
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])

```
