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

Default address: `http://localhost:8091`.

## Auth Model

The plugin host does not need the long-lived Sub2API bearer token. It expects a short-lived launch ticket signed with `PLUGIN_SERVICE_LAUNCH_SHARED_SECRET`.

Launch flow:

1. Main service opens `/launch?ticket=<ticket>`.
2. Plugin service verifies the ticket.
3. Plugin service creates its own `plugin_session` httpOnly cookie.
4. The launch redirects to the plugin entry path from the registered manifest.
5. Plugin API requests use the plugin session cookie.

The standalone host enforces role-aware history access:

- `admin` can list and read all plugin history records.
- `user` can list and read only records matching their `user_id`.

## Plugin Host Layout

- Host routes:
  - `GET /healthz`
  - `GET /launch`
  - `GET /dev/login`
  - `GET /api/me`
  - `GET /api/plugins`
  - `GET /api/plugins/{key}`
- Hosted image-generation page:
  - `GET /plugins/image-generation`
  - `GET /plugins/image-generation/assets/app.css`
  - `GET /plugins/image-generation/assets/app.js`
- Namespaced image-generation APIs:
  - `GET /api/plugins/image-generation/config`
  - `POST /api/plugins/image-generation/generate`
  - `GET /api/plugins/image-generation/creations`
  - `GET /api/plugins/image-generation/history`
  - `GET /api/plugins/image-generation/history/{id}`
  - `POST /api/plugins/image-generation/history/{id}/retry`
  - `POST /api/plugins/image-generation/history/{id}/cancel`

Compatibility aliases remain available during migration:

- `GET /app`
- `GET /api/config`
- `POST /api/generate`
- `GET /api/creations`
- `GET /api/history`
- `GET /api/history/{id}`
- `POST /api/history/{id}/retry`
- `POST /api/history/{id}/cancel`

`POST /api/generate` and `POST /api/plugins/image-generation/generate` both proxy image generation requests to an OpenAI-compatible upstream configured by `PLUGIN_SERVICE_IMAGE_PROVIDER_BASE_URL`. The request must include `provider_api_key`, and the plugin service persists structured history plus a flattened creations list for gallery-style views.

## Local Development Login

For local development without Sub2API ticket minting, enable:

```env
PLUGIN_SERVICE_DEV_LOGIN_ENABLED=true
```

Create a real `.env` file from `.env.example`. Editing `.env.example` itself does not affect the running service.

Then open a URL like:

```text
/dev/login?user_id=7&role=admin&email=admin@example.com&username=dev-admin&plugin=image-generation&path=/plugins/image-generation
```

This creates the `plugin_session` cookie directly so `/api/me`, `/api/plugins/image-generation/generate`, and `/api/plugins/image-generation/history` can be exercised without the main service.

## Adding a New Plugin

1. Copy `plugins/image-generation/` as the starting template.
2. Update the manifest metadata and entry path.
3. Implement plugin backend handlers and register their routes.
4. Add hosted frontend assets under `web/`.
5. Register the plugin in router bootstrap.
