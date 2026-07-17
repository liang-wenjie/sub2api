const app = document.getElementById('app')
const apiBase = app.dataset.pluginApiBase
const notice = document.getElementById('notice')
const routes = document.getElementById('routes')
const form = document.getElementById('route-form')

function show(message, error = false) {
  notice.textContent = message
  notice.className = error ? 'error' : 'success'
}

function cell(text) {
  const value = document.createElement('td')
  value.textContent = text
  return value
}

async function loadRoutes() {
  const response = await fetch(`${apiBase}/routes?platform=agnes`)
  const payload = await response.json()
  if (!response.ok) throw new Error(payload.error || 'Failed to load routes')
  routes.replaceChildren()
  for (const route of payload.items || []) {
    const row = document.createElement('tr')
    row.append(cell(route.slug), cell(route.default_model), cell(route.base_url), cell(route.enabled ? 'Enabled' : 'Disabled'))
    const actions = document.createElement('td')
    const remove = document.createElement('button')
    remove.type = 'button'
    remove.textContent = 'Delete'
    remove.addEventListener('click', async () => {
      if (!window.confirm(`Delete ${route.slug}?`)) return
      const result = await fetch(`${apiBase}/routes/agnes/${encodeURIComponent(route.slug)}`, { method: 'DELETE' })
      if (!result.ok) throw new Error('Failed to delete route')
      await loadRoutes()
    })
    actions.append(remove)
    row.append(actions)
    routes.append(row)
  }
}

form.addEventListener('submit', async (event) => {
  event.preventDefault()
  const values = new FormData(form)
  const slug = String(values.get('slug') || '').trim().toLowerCase()
  const response = await fetch(`${apiBase}/routes/agnes/${encodeURIComponent(slug)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      base_url: values.get('base_url'), default_model: values.get('default_model'),
      max_n: Number(values.get('max_n')), enabled: values.get('enabled') === 'on'
    })
  })
  const payload = await response.json()
  if (!response.ok) throw new Error(payload.error || 'Failed to save route')
  show(`Saved ${payload.slug}`)
  form.reset()
  await loadRoutes()
})

document.getElementById('reload').addEventListener('click', () => loadRoutes().catch(error => show(error.message, true)))
loadRoutes().catch(error => show(error.message, true))
