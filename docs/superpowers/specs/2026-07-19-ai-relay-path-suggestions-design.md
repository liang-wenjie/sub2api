# AI Relay Path Suggestions Design

## Scope

Improve the standalone AI Relay Path mappings editor. Do not modify backend path normalization, route forwarding, or main-site frontend files.

## Source Path Input

Keep Source path as a normal editable text input and associate it with a plugin-local datalist. Users can type any value or quickly select one of these OpenAI-compatible paths:

- `v1/models`
- `v1/chat/completions`
- `v1/responses`
- `v1/responses/compact`
- `v1/embeddings`
- `v1/images/generations`
- `v1/images/edits`

All mapping rows share the same suggestion list. Existing canonicalization continues to remove an optional leading slash and `v1/` prefix when saving.

## Examples And Styling

Use `v1/responses/compact` as the Source path placeholder and `api/paas/v4/chat/completions` as the Target path placeholder. Apply an explicit muted gray placeholder color to mapping inputs while keeping entered values in the existing dark text color.

## Verification

- Component tests verify the source input references the datalist and all expected suggestions exist.
- Tests verify users can still enter a custom source path.
- Style tests verify mapping placeholders use the muted gray color.
- Existing normalization, route editor, Toast, pagination, and layout tests remain passing.
- No main-site frontend files are modified.
