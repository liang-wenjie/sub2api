# Sub2API Plugin Service

This service is an independently deployable plugin host. The first plugin key is `gen`, intended for image/text generation pages embedded by the existing custom menu iframe.

## Runtime

```bash
cd plugin-service
go test ./...
go run ./cmd/server
```

Default address: `http://localhost:8091`.

## Auth Model

The plugin service does not need the long-lived Sub2API bearer token. It expects a short-lived launch ticket signed with `PLUGIN_SERVICE_LAUNCH_SHARED_SECRET`.

Launch flow:

1. Main service opens `/launch?ticket=<ticket>&path=/app`.
2. Plugin service verifies the ticket.
3. Plugin service creates its own `plugin_session` httpOnly cookie.
4. Plugin API requests use the plugin session cookie.

The standalone service already enforces role-aware history access:

- `admin` can list and read all plugin history records.
- `user` can list and read only records matching their `user_id`.

## Local Development Login

For local development without Sub2API ticket minting, enable:

```env
PLUGIN_SERVICE_DEV_LOGIN_ENABLED=true
```

Create a real `.env` file from `.env.example`. Editing `.env.example` itself does not affect the running service.

Then open a URL like:

```text
/dev/login?user_id=7&role=admin&email=admin@example.com&username=dev-admin&path=/app
```

This creates the `plugin_session` cookie directly so `/api/me`, `/api/generate`, and `/api/history` can be exercised without the main service.

## API

- `GET /healthz`
- `GET /launch?ticket=...&path=/app`
- `GET /api/me`
- `GET /api/config`
- `POST /api/generate`
- `GET /api/creations`
- `GET /api/history`
- `GET /api/history/{id}`
- `POST /api/history/{id}/retry`
- `POST /api/history/{id}/cancel`

`POST /api/generate` now proxies image generation requests to an OpenAI-compatible upstream configured by `PLUGIN_SERVICE_IMAGE_PROVIDER_BASE_URL`. The request must include `provider_api_key`, and the plugin service will persist structured history plus a flattened creations list for gallery-style views.
