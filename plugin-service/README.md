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

The main frontend turns this entry into the shared launch URL:

- `/launch?plugin=image-generation&path=/plugins/image-generation`

For deployment, keep the browser-facing entry on the main site domain. Configure
the plugin service port in the shared deployment env, for example:

```env
PLUGIN_SERVER_PORT=8091
```

Then reverse-proxy these paths from the main site domain to the fixed internal
plugin service host in the same Docker network or environment:

- `/launch`
- `/plugins/*`
- `/api/plugins/*`

## Auth Model

The plugin host keeps its own short-lived `plugin_session` cookie. The only
shared entry point is `/launch`, which reads the Sub2API auth token or same-site
cookie from the incoming request, calls the main site `/api/v1/auth/me`, and
then creates the plugin session.

Launch flow:

1. Main custom menu stores `http://plugin-server/plugins/image-generation` as the plugin base URL.
2. Main frontend opens `/launch?plugin=image-generation&path=/plugins/image-generation` on the same domain.
3. Browser carries the current Sub2API token query parameter or same-site cookie.
4. Plugin service loads the current user from the main site.
5. Plugin service creates its own `plugin_session` httpOnly cookie.
6. The launch redirects to the plugin entry path from the registered manifest.
7. Plugin API requests use the plugin session cookie.

The standalone host enforces role-aware history access:

- `admin` can list and read all plugin history records.
- `user` can list and read only records matching their `user_id`.

## Plugin Host Layout

- Host routes:
  - `GET /healthz`
  - `GET /launch`
  - `GET /dev/login`
  - `GET /api/plugins`
  - `GET /api/plugins/{key}`
- Hosted image-generation page:
  - `GET /plugins/image-generation`
  - `GET /plugins/image-generation/assets/app.css`
  - `GET /plugins/image-generation/assets/app.js`
- Namespaced image-generation APIs:
  - `GET /api/plugins/image-generation/me`
  - `GET /api/plugins/image-generation/config`
  - `POST /api/plugins/image-generation/generate`
  - `GET /api/plugins/image-generation/creations`
  - `GET /api/plugins/image-generation/history`
  - `GET /api/plugins/image-generation/history/{id}`
  - `POST /api/plugins/image-generation/history/{id}/retry`
  - `POST /api/plugins/image-generation/history/{id}/cancel`

`POST /api/plugins/image-generation/generate` proxies image generation requests to the main Sub2API gateway resolved from the same-origin request headers. The request must include `provider_api_key`, and the plugin service persists structured history plus a flattened creations list for gallery-style views.

## Local Development Login

For local development without Sub2API credentials, enable:

```env
PLUGIN_SERVER_DEV_LOGIN_ENABLED=true
```

Configure plugin settings in the project root `.env`. The plugin service shares that same environment file with the main service.

Then open a URL like:

```text
http://localhost:8091/dev/login?user_id=7&role=admin&email=admin@example.com&username=dev-admin&plugin=image-generation&path=/plugins/image-generation
```

This creates the `plugin_session` cookie directly so `/api/plugins/image-generation/me`, `/api/plugins/image-generation/generate`, and `/api/plugins/image-generation/history` can be exercised without the main service.

## Adding a New Plugin

1. Copy `plugins/image-generation/` as the starting template.
2. Update the manifest metadata and entry path.
3. Implement plugin backend handlers and register their routes.
4. Add hosted frontend assets under `web/`.
5. Register the plugin from `plugins.RegisterAll`; the host router discovers routes through the registry.
