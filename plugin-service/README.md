# Sub2API Plugin Service

This service is now a unified, independently deployable plugin host. It keeps shared auth/session handling in one place and mounts each plugin through a standard structure:

- `plugins/<plugin-key>/manifest`
- `plugins/<plugin-key>/backend`
- `plugins/<plugin-key>/web`

The first hosted plugin is `image-generation`.

## Runtime

```bash
cd plugin-service
go test ./...
go run ./cmd/server
```

Default local listener: `http://localhost:8091`.

Configure the plugin entry base URL in the main Sub2API custom menu, not in the
plugin service. Keep the original absolute URL shape, using `plugin-server` as
the fixed placeholder host:

- `http://plugin-server/plugins/image-generation`

The main frontend turns this entry into the same-origin plugin URL:

- `/plugins/image-generation`

For deployment, keep the browser-facing entry on the main site domain. Configure
the plugin service port in the shared deployment env, for example:

```env
PLUGIN_SERVER_PORT=8091
```

Then reverse-proxy this path prefix from the main site domain to the fixed
internal plugin service host in the same Docker network or environment:

- `/plugins/*`

The plugin service reuses the same PostgreSQL configuration as the main site.
If `DATABASE_URL` or the shared `DATABASE_*` environment variables are present,
image generation history is persisted into `plugin_generation_history`;
otherwise it falls back to in-memory history for local-only runs.

## Auth Model

The plugin host fully reuses the main Sub2API login state. It does not create
its own session cookie, does not persist plugin login state, and does not
provide a standalone development login entry.

Embed flow:

1. Main custom menu stores `http://plugin-server/plugins/image-generation` as the plugin base URL.
2. Main frontend opens `/plugins/image-generation` on the same domain.
3. Browser carries the current Sub2API token query parameter or same-site cookie.
4. Every protected plugin API request forwards those credentials to the main site `/api/v1/auth/me`.

The main site base URL for auth verification is discovered automatically inside
the deployment environment. The plugin host tries these candidates in order:

- `http://sub2api:8080`
- `http://localhost:8080`
- `http://127.0.0.1:8080`

The standalone host enforces role-aware history access:

- `admin` can list and read all plugin history records.
- `user` can list and read only records matching their `user_id`.

## Plugin Host Layout

- Host routes:
  - `GET /healthz`
- Hosted image-generation page:
  - `GET /plugins/image-generation`
  - `GET /plugins/image-generation/assets/app.css`
  - `GET /plugins/image-generation/assets/app.js`
- Namespaced image-generation APIs:
  - `GET /plugins/image-generation/api/me`
  - `GET /plugins/image-generation/api/config`
  - `POST /plugins/image-generation/api/generate`
  - `GET /plugins/image-generation/api/creations`
  - `GET /plugins/image-generation/api/history`
  - `GET /plugins/image-generation/api/history/{id}`
  - `POST /plugins/image-generation/api/history/{id}/retry`
  - `POST /plugins/image-generation/api/history/{id}/cancel`

`POST /plugins/image-generation/api/generate` proxies image generation requests to the main Sub2API gateway resolved from the same-origin request headers. The request must include `provider_api_key`, and the plugin service persists structured history plus a flattened creations list for gallery-style views.

## Adding a New Plugin

1. Copy `plugins/image-generation/` as the starting template.
2. Update the manifest metadata and entry path.
3. Implement plugin backend handlers and register their routes.
4. Add hosted frontend assets under `web/`.
5. Register the plugin from `plugins.RegisterAll`; the host router discovers routes through the registry.
