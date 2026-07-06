package handler

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/httpx"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

type AppDeps struct {
	Config     config.Config
	History    *service.HistoryService
	Generation *service.GenerationService
}

type App struct {
	cfg        config.Config
	history    *service.HistoryService
	generation *service.GenerationService
}

func NewApp(deps AppDeps) *App {
	return &App{
		cfg:        deps.Config,
		history:    deps.History,
		generation: deps.Generation,
	}
}

func (a *App) WithCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if a.cfg.MainSiteOrigin != "" {
			w.Header().Set("Content-Security-Policy", "frame-ancestors "+a.cfg.MainSiteOrigin)
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) Health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"plugin": a.cfg.PluginKey,
	})
}

func (a *App) AppPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Image Generation Plugin</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f2f3ec;
      --surface: #fffdf7;
      --surface-strong: #ffffff;
      --surface-muted: #eef0e8;
      --line: #d7dbc9;
      --text: #1f261f;
      --muted: #667061;
      --accent: #243428;
      --accent-strong: #16241a;
      --accent-soft: #dde8de;
      --success: #1f7a43;
      --danger: #b64747;
      --shadow: 0 22px 60px rgba(28, 36, 30, 0.10);
      --radius-lg: 28px;
      --radius-md: 20px;
      --radius-sm: 14px;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Segoe UI", "PingFang SC", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(222, 232, 221, 0.9), transparent 24%),
        linear-gradient(180deg, #f6f7f2 0%, var(--bg) 100%);
      color: var(--text);
    }
    a { color: inherit; }
    .shell {
      min-height: 100vh;
      padding: 28px;
    }
    .workspace {
      display: grid;
      grid-template-columns: 320px minmax(0, 1fr);
      gap: 20px;
      min-height: calc(100vh - 56px);
    }
    .rail, .panel {
      background: rgba(255, 255, 255, 0.78);
      backdrop-filter: blur(14px);
      border: 1px solid rgba(215, 219, 201, 0.9);
      border-radius: var(--radius-lg);
      box-shadow: var(--shadow);
    }
    .rail {
      display: flex;
      flex-direction: column;
      padding: 18px;
      gap: 18px;
      min-height: 0;
    }
    .panel {
      display: grid;
      grid-template-rows: auto auto minmax(0, 1fr) auto;
      gap: 16px;
      padding: 20px;
      min-height: 0;
    }
    .eyebrow {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      padding: 8px 12px;
      border-radius: 999px;
      background: var(--accent-soft);
      color: var(--accent);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.04em;
      text-transform: uppercase;
    }
    h1, h2, h3, p { margin: 0; }
    h1 { font-size: 30px; line-height: 1.15; }
    .subtle { color: var(--muted); line-height: 1.6; }
    .stat-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
    }
    .stat-card, .module, .card {
      border: 1px solid var(--line);
      border-radius: var(--radius-md);
      background: var(--surface-strong);
    }
    .stat-card {
      padding: 14px;
    }
    .stat-label {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.04em;
      margin-bottom: 8px;
    }
    .stat-value {
      font-size: 14px;
      font-weight: 600;
      word-break: break-all;
    }
    .module {
      padding: 16px;
      display: flex;
      flex-direction: column;
      gap: 12px;
      min-height: 0;
    }
    .module-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
    }
    .stack {
      display: flex;
      flex-direction: column;
      gap: 12px;
      min-height: 0;
    }
    .scroll {
      overflow: auto;
      min-height: 0;
    }
    .history-list, .creation-grid {
      display: grid;
      gap: 12px;
    }
    .history-item {
      padding: 14px;
      border-radius: var(--radius-sm);
      background: var(--surface);
      border: 1px solid var(--line);
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .history-meta {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      color: var(--muted);
      font-size: 12px;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 7px 12px;
      border-radius: 999px;
      background: var(--surface-muted);
      font-size: 12px;
      color: var(--accent);
      font-weight: 600;
      border: 1px solid var(--line);
    }
    .hero {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 18px;
      align-items: start;
      padding: 4px 2px 8px;
    }
    .hero-actions {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      justify-content: flex-end;
    }
    .button, button {
      border: 0;
      border-radius: 999px;
      padding: 11px 16px;
      font: inherit;
      cursor: pointer;
      transition: transform 0.2s ease, opacity 0.2s ease, background 0.2s ease;
    }
    button:hover { transform: translateY(-1px); }
    button:disabled { cursor: not-allowed; transform: none; opacity: 0.6; }
    .button-primary {
      background: var(--accent);
      color: #fff;
    }
    .button-secondary {
      background: var(--surface-muted);
      color: var(--text);
      border: 1px solid var(--line);
    }
    .button-danger {
      background: #f8e3e3;
      color: var(--danger);
      border: 1px solid #ecc3c3;
    }
    .composer {
      display: grid;
      gap: 14px;
      border-radius: var(--radius-lg);
      border: 1px solid var(--line);
      background: linear-gradient(180deg, rgba(255,255,255,0.97) 0%, rgba(249,250,245,0.96) 100%);
      padding: 18px;
    }
    .field-grid {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px;
    }
    label {
      display: flex;
      flex-direction: column;
      gap: 8px;
      font-size: 13px;
      color: var(--muted);
      font-weight: 600;
    }
    input, textarea, select {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 16px;
      background: #fff;
      color: var(--text);
      font: inherit;
      padding: 12px 14px;
      outline: none;
    }
    textarea { min-height: 140px; resize: vertical; }
    input:focus, textarea:focus, select:focus {
      border-color: #9db39f;
      box-shadow: 0 0 0 3px rgba(157, 179, 159, 0.18);
    }
    .dropzone {
      display: grid;
      gap: 10px;
      border: 1px dashed #a9b39e;
      border-radius: var(--radius-md);
      background: rgba(239, 243, 233, 0.7);
      padding: 14px;
    }
    .dropzone small { color: var(--muted); }
    .reference-list {
      display: grid;
      gap: 10px;
    }
    .reference-item {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      padding: 10px 12px;
      border-radius: 14px;
      background: #fff;
      border: 1px solid var(--line);
    }
    .message {
      padding: 12px 14px;
      border-radius: 16px;
      background: #eef4ee;
      color: var(--accent-strong);
      border: 1px solid #d5e0d4;
      min-height: 22px;
    }
    .message.error {
      background: #f9e8e8;
      color: var(--danger);
      border-color: #efc7c7;
    }
    .message.success {
      background: #e6f4ea;
      color: var(--success);
      border-color: #c8e3d1;
    }
    .creation-grid {
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    }
    .card {
      overflow: hidden;
      display: flex;
      flex-direction: column;
      min-height: 100%;
    }
    .card-preview {
      aspect-ratio: 1;
      background:
        linear-gradient(135deg, rgba(223, 232, 219, 0.7), rgba(237, 238, 228, 0.9));
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .card-preview img {
      width: 100%;
      height: 100%;
      object-fit: cover;
      display: block;
    }
    .card-body {
      padding: 14px;
      display: flex;
      flex-direction: column;
      gap: 10px;
    }
    .card-title {
      font-weight: 700;
      line-height: 1.5;
    }
    .card-meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      color: var(--muted);
      font-size: 12px;
    }
    .empty {
      border: 1px dashed var(--line);
      border-radius: var(--radius-md);
      padding: 26px;
      text-align: center;
      color: var(--muted);
      background: rgba(255, 255, 255, 0.6);
    }
    @media (max-width: 1080px) {
      .workspace {
        grid-template-columns: 1fr;
      }
      .hero {
        grid-template-columns: 1fr;
      }
      .hero-actions {
        justify-content: flex-start;
      }
    }
    @media (max-width: 760px) {
      .shell {
        padding: 16px;
      }
      .field-grid, .stat-grid {
        grid-template-columns: 1fr;
      }
      .panel {
        grid-template-rows: auto auto auto auto;
      }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="workspace" data-testid="image-workspace">
      <aside class="rail" data-testid="image-history">
        <div class="stack">
          <span class="eyebrow">Plugin Host</span>
          <h1>Image Generation</h1>
          <p class="subtle">这个插件页已经完全独立部署在插件服务里。你可以直接在这里发起图片生成、查看创建列表，以及按权限查看历史记录。</p>
        </div>

        <section class="stat-grid">
          <article class="stat-card">
            <div class="stat-label">Plugin</div>
            <div class="stat-value" id="plugin-key">` + html.EscapeString(a.cfg.PluginKey) + `</div>
          </article>
          <article class="stat-card">
            <div class="stat-label">Provider</div>
            <div class="stat-value" id="provider-base-url">` + html.EscapeString(a.cfg.ImageProviderBaseURL) + `</div>
          </article>
          <article class="stat-card">
            <div class="stat-label">History</div>
            <div class="stat-value" id="history-enabled">` + html.EscapeString(strconv.FormatBool(a.cfg.HistoryEnabled)) + `</div>
          </article>
          <article class="stat-card">
            <div class="stat-label">Dev Login</div>
            <div class="stat-value">` + html.EscapeString(strconv.FormatBool(a.cfg.DevLoginEnabled)) + `</div>
          </article>
        </section>

        <section class="module">
          <div class="module-head">
            <div>
              <h2>Session</h2>
              <p class="subtle">自动读取插件会话并展示当前身份。</p>
            </div>
            <button class="button-secondary" type="button" id="refresh-session">刷新</button>
          </div>
          <div class="scroll">
            <div class="history-list">
              <article class="history-item">
                <div class="history-meta"><span>用户</span><span id="session-user">未加载</span></div>
                <div class="history-meta"><span>角色</span><span id="session-role">未加载</span></div>
                <div class="history-meta"><span>ID</span><span id="session-user-id">-</span></div>
              </article>
              <article class="history-item">
                <div class="history-meta"><span>快捷入口</span><span>API</span></div>
                <div class="links">
                  <a href="/api/me" target="_blank" rel="noreferrer">/api/me</a>
                  <a href="/api/config" target="_blank" rel="noreferrer">/api/config</a>
                  <a href="/api/creations" target="_blank" rel="noreferrer">/api/creations</a>
                  <a href="/api/history" target="_blank" rel="noreferrer">/api/history</a>
                </div>
              </article>
            </div>
          </div>
        </section>

        <section class="module">
          <div class="module-head">
            <div>
              <h2>History</h2>
              <p class="subtle">管理员可看全站记录，普通用户只看自己的。</p>
            </div>
            <button class="button-secondary" type="button" id="refresh-history">刷新</button>
          </div>
          <div class="scroll">
            <div class="history-list" id="history-list"></div>
          </div>
        </section>
      </aside>

      <section class="panel">
        <div class="hero">
          <div class="stack">
            <span class="eyebrow">Standalone Extension</span>
            <h2>独立图像生成工作台</h2>
              <p class="subtle">这版页面直接运行在插件服务中，不依赖主站前端构建。后端走插件服务的 /api/generate、/api/creations 和 /api/history。</p>
          </div>
          <div class="hero-actions">
            <button class="button-secondary" type="button" id="refresh-creations">刷新创建列表</button>
            <button class="button-primary" type="button" id="prefill-dev-login">填充调试示例</button>
          </div>
        </div>

        <section class="module">
          <div class="module-head">
            <div>
              <h2>Create List</h2>
              <p class="subtle">扁平化的图片创建列表，适合后续直接挂到菜单分栏里做图库视图。</p>
            </div>
            <span class="pill" id="creation-count">0 items</span>
          </div>
          <div class="scroll">
            <div class="creation-grid" id="creation-grid" data-testid="image-creation-grid"></div>
          </div>
        </section>

        <section class="module">
          <div class="module-head">
            <div>
              <h2>Request Message</h2>
              <p class="subtle">这里会展示当前请求状态，方便联调插件服务和主站嵌入链路。</p>
            </div>
          </div>
          <div id="request-message" class="message">等待操作。</div>
        </section>

        <form class="composer" id="image-composer" data-testid="image-composer">
          <div class="field-grid">
            <label>
              Provider API Key
              <input id="provider-api-key" name="provider_api_key" placeholder="例如 sk-..." autocomplete="off">
            </label>
            <label>
              Model
              <input id="model" name="model" value="gpt-image-1" placeholder="gpt-image-1">
            </label>
            <label>
              Size
              <select id="size" name="size">
                <option value="1024x1024">1024 x 1024</option>
                <option value="1536x1024">1536 x 1024</option>
                <option value="1024x1536">1024 x 1536</option>
              </select>
            </label>
          </div>

          <label>
            Prompt
            <textarea id="prompt" name="prompt" placeholder="描述你想生成的图像内容，比如：为一款极简护肤品设计一张高级电商主图。"></textarea>
          </label>

          <div class="dropzone">
            <label>
              参考图 URL（可选，多个请换行）
              <textarea id="reference-urls" placeholder="https://cdn.example.com/reference.png"></textarea>
            </label>
            <label>
              本地参考图（可选，当前只取第一张）
              <input id="reference-file-input" data-testid="reference-file-input" type="file" accept="image/*">
            </label>
            <div id="reference-file-preview" class="subtle">未选择本地参考图。</div>
            <small>远程 URL 和本地参考图都可以使用。本地文件会在浏览器端转换成 data URL 后提交给插件服务。</small>
          </div>

          <div class="hero-actions" style="justify-content:flex-start;">
            <button class="button-primary" type="submit">生成图片</button>
            <button class="button-secondary" type="button" id="reset-form">重置表单</button>
          </div>
        </form>
      </section>
    </section>
  </main>
  <script>
    const state = {
      config: null,
      me: null,
      history: [],
      creations: [],
      localReferenceImage: null,
    };

    function byId(id) {
      return document.getElementById(id);
    }

    function setMessage(text, kind) {
      const box = byId('request-message');
      box.textContent = text;
      box.className = 'message' + (kind ? ' ' + kind : '');
    }

    async function fetchJSON(url, options) {
      const response = await fetch(url, Object.assign({ credentials: 'same-origin' }, options || {}));
      const contentType = response.headers.get('content-type') || '';
      const payload = contentType.includes('application/json') ? await response.json() : await response.text();
      if (!response.ok) {
        const message = payload && typeof payload === 'object' ? (payload.error || payload.message || JSON.stringify(payload)) : String(payload);
        throw new Error(message || ('HTTP ' + response.status));
      }
      return payload;
    }

    function renderSession() {
      byId('session-user').textContent = state.me ? (state.me.username || state.me.email || '-') : '未登录';
      byId('session-role').textContent = state.me ? state.me.role : '未登录';
      byId('session-user-id').textContent = state.me ? String(state.me.user_id) : '-';
    }

    function renderHistory() {
      const list = byId('history-list');
      const items = state.history || [];
      if (!items.length) {
        list.innerHTML = '<div class="empty">还没有历史记录。</div>';
        return;
      }
      list.innerHTML = items.map(function(item) {
        const model = item.request && item.request.model ? item.request.model : '-';
        const size = item.request && item.request.size ? item.request.size : '-';
        const imageCount = item.result && Array.isArray(item.result.images) ? item.result.images.length : 0;
        const prompt = escapeHTML(item.prompt || '');
        return '' +
          '<article class="history-item">' +
            '<div class="history-meta"><span>' + escapeHTML(item.status || '-') + '</span><span>' + formatDate(item.created_at) + '</span></div>' +
            '<div class="card-title">' + prompt + '</div>' +
            '<div class="card-meta"><span>' + escapeHTML(model) + '</span><span>' + escapeHTML(size) + '</span><span>' + imageCount + ' images</span></div>' +
            '<div class="hero-actions" style="justify-content:flex-start;">' +
              '<button class="button-secondary" type="button" data-action="retry" data-id="' + escapeHTML(item.id) + '">重试</button>' +
            '</div>' +
          '</article>';
      }).join('');
    }

    function renderCreations() {
      const grid = byId('creation-grid');
      const items = state.creations || [];
      byId('creation-count').textContent = items.length + ' items';
      if (!items.length) {
        grid.innerHTML = '<div class="empty">还没有创建内容。先提交一次生成请求，创建列表就会出现在这里。</div>';
        return;
      }
      grid.innerHTML = items.map(function(item) {
        const imageSrc = item.image_url || (item.b64_json ? ('data:image/png;base64,' + item.b64_json) : '');
        const prompt = escapeHTML(item.prompt || 'Untitled');
        const metaModel = escapeHTML(item.model || '-');
        const metaSize = escapeHTML(item.size || '-');
        const metaUser = escapeHTML(item.user_email || '-');
        return '' +
          '<article class="card">' +
            '<div class="card-preview">' +
              (imageSrc ? '<img src="' + escapeHTML(imageSrc) + '" alt="' + prompt + '">' : '<span class="subtle">No Preview</span>') +
            '</div>' +
            '<div class="card-body">' +
              '<div class="card-title">' + prompt + '</div>' +
              '<div class="card-meta"><span>' + metaModel + '</span><span>' + metaSize + '</span></div>' +
              '<div class="card-meta"><span>' + metaUser + '</span><span>' + formatDate(item.created_at) + '</span></div>' +
            '</div>' +
          '</article>';
      }).join('');
    }

    function formatDate(value) {
      if (!value) return '-';
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return value;
      return date.toLocaleString();
    }

    function escapeHTML(value) {
      return String(value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
    }

    async function loadConfig() {
      state.config = await fetchJSON('/api/config');
      if (state.config && state.config.plugin_key) {
        byId('plugin-key').textContent = state.config.plugin_key;
      }
      if (state.config && state.config.image_provider_base_url) {
        byId('provider-base-url').textContent = state.config.image_provider_base_url;
      }
    }

    async function loadSession() {
      state.me = await fetchJSON('/api/me');
      renderSession();
    }

    async function loadHistory() {
      const payload = await fetchJSON('/api/history');
      state.history = payload.items || [];
      renderHistory();
    }

    async function loadCreations() {
      const payload = await fetchJSON('/api/creations');
      state.creations = payload.items || [];
      renderCreations();
    }

    async function submitGenerate(event) {
      event.preventDefault();
      const providerApiKey = byId('provider-api-key').value.trim();
      const prompt = byId('prompt').value.trim();
      const model = byId('model').value.trim();
      const size = byId('size').value;
      const referenceURLs = byId('reference-urls').value
        .split(/\r?\n/)
        .map(function(item) { return item.trim(); })
        .filter(Boolean);
      const referenceImages = referenceURLs.map(function(url) {
        return { remote_url: url };
      });
      if (state.localReferenceImage) {
        referenceImages.unshift(state.localReferenceImage);
      }

      const payload = {
        provider_api_key: providerApiKey,
        prompt: prompt,
        model: model,
        size: size,
        reference_images: referenceImages
      };

      setMessage('正在生成图片，请稍候...', '');
      try {
        const result = await fetchJSON('/api/generate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        setMessage('生成成功，已刷新创建列表和历史记录。任务 ID: ' + result.job_id, 'success');
        await Promise.all([loadHistory(), loadCreations()]);
      } catch (error) {
        setMessage(error && error.message ? error.message : '生成失败', 'error');
      }
    }

    async function retryGenerate(id) {
      setMessage('正在重试该条历史记录...', '');
      try {
        const result = await fetchJSON('/api/history/' + encodeURIComponent(id) + '/retry', {
          method: 'POST'
        });
        setMessage('重试成功，已创建新任务。任务 ID: ' + result.job_id, 'success');
        await Promise.all([loadHistory(), loadCreations()]);
      } catch (error) {
        setMessage(error && error.message ? error.message : '重试失败', 'error');
      }
    }

    function resetForm() {
      byId('provider-api-key').value = '';
      byId('prompt').value = '';
      byId('model').value = 'gpt-image-1';
      byId('size').value = '1024x1024';
      byId('reference-urls').value = '';
      byId('reference-file-input').value = '';
      state.localReferenceImage = null;
      byId('reference-file-preview').textContent = '未选择本地参考图。';
      setMessage('表单已重置。', '');
    }

    function fillDevExample() {
      byId('provider-api-key').value = 'sk-your-provider-key';
      byId('prompt').value = '为一款茶饮品牌生成一张干净、克制、带自然高光的电商主图';
      byId('model').value = 'gpt-image-1';
      byId('size').value = '1024x1024';
      byId('reference-urls').value = '';
      byId('reference-file-input').value = '';
      state.localReferenceImage = null;
      byId('reference-file-preview').textContent = '未选择本地参考图。';
      setMessage('已填入调试示例，可以直接改 prompt 或 key 后提交。', '');
    }

    function handleReferenceFileChange(event) {
      const input = event.target;
      if (!(input instanceof HTMLInputElement) || !input.files || !input.files[0]) {
        state.localReferenceImage = null;
        byId('reference-file-preview').textContent = '未选择本地参考图。';
        return;
      }
      const file = input.files[0];
      const reader = new FileReader();
      reader.onload = function() {
        state.localReferenceImage = {
          name: file.name,
          mime_type: file.type || 'image/png',
          data_url: String(reader.result || '')
        };
        byId('reference-file-preview').textContent = '已选择: ' + file.name;
      };
      reader.onerror = function() {
        state.localReferenceImage = null;
        byId('reference-file-preview').textContent = '本地参考图读取失败，请重试。';
        setMessage('本地参考图读取失败，请重试。', 'error');
      };
      reader.readAsDataURL(file);
    }

    document.addEventListener('click', function(event) {
      const target = event.target;
      if (!(target instanceof HTMLElement)) return;
      const action = target.getAttribute('data-action');
      if (action === 'retry') {
        const id = target.getAttribute('data-id');
        if (id) {
          retryGenerate(id);
        }
      }
    });

    byId('image-composer').addEventListener('submit', submitGenerate);
    byId('refresh-session').addEventListener('click', function() {
      loadSession().catch(function(error) {
        setMessage(error && error.message ? error.message : '加载会话失败', 'error');
      });
    });
    byId('refresh-history').addEventListener('click', function() {
      loadHistory().catch(function(error) {
        setMessage(error && error.message ? error.message : '加载历史失败', 'error');
      });
    });
    byId('refresh-creations').addEventListener('click', function() {
      loadCreations().catch(function(error) {
        setMessage(error && error.message ? error.message : '加载创建列表失败', 'error');
      });
    });
    byId('prefill-dev-login').addEventListener('click', fillDevExample);
    byId('reset-form').addEventListener('click', resetForm);
    byId('reference-file-input').addEventListener('change', handleReferenceFileChange);

    Promise.all([loadConfig(), loadSession(), loadHistory(), loadCreations()])
      .then(function() {
        setMessage('插件服务已就绪，可以开始生成图片。', 'success');
      })
      .catch(function(error) {
        setMessage((error && error.message ? error.message : '初始化失败') + '。如果你在本地联调，先访问 /dev/login 创建插件会话。', 'error');
      });
  </script>
</body>
</html>`))
}

func (a *App) Config(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"plugin_key":              a.cfg.PluginKey,
		"history_enabled":         a.cfg.HistoryEnabled,
		"image_provider_base_url": a.cfg.ImageProviderBaseURL,
		"user_id":                 principal.UserID,
		"role":                    principal.Role,
	})
}

func (a *App) Generate(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	var req model.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	resp, err := a.generation.Generate(r.Context(), principal, req)
	if err != nil {
		if errors.Is(err, service.ErrPromptRequired) || errors.Is(err, service.ErrProviderKeyRequired) {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "generation failed")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (a *App) ListCreations(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	records, err := a.generation.ListCreations(r.Context(), principal, parseHistoryQuery(r.URL.Query()))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list creations")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": records,
	})
}

func (a *App) ListHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	records, err := a.history.List(r.Context(), principal, parseHistoryQuery(r.URL.Query()))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list history")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": sanitizeHistoryRecords(records),
	})
}

func (a *App) GetHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := a.history.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sanitizeHistoryRecord(record))
}

func (a *App) RetryHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	resp, err := a.generation.Retry(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (a *App) CancelHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := a.generation.Cancel(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sanitizeHistoryRecord(record))
}

func parseHistoryQuery(values url.Values) model.HistoryQuery {
	return model.HistoryQuery{
		Page:     parsePositiveInt(values.Get("page"), 1),
		PageSize: parsePositiveInt(values.Get("page_size"), 20),
	}
}

func parsePositiveInt(raw string, fallback int) int {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "history record not found")
	case errors.Is(err, service.ErrHistoryForbidden):
		httpx.WriteError(w, http.StatusForbidden, "history record is not accessible")
	default:
		httpx.WriteError(w, http.StatusConflict, err.Error())
	}
}

func sanitizeHistoryRecords(records []model.HistoryRecord) []model.HistoryRecord {
	sanitized := make([]model.HistoryRecord, 0, len(records))
	for _, record := range records {
		current := sanitizeHistoryRecord(&record)
		sanitized = append(sanitized, *current)
	}
	return sanitized
}

func sanitizeHistoryRecord(record *model.HistoryRecord) *model.HistoryRecord {
	if record == nil {
		return nil
	}
	safe := *record
	safe.Request = sanitizeRequestPayload(record.Request)
	return &safe
}

func sanitizeRequestPayload(request map[string]any) map[string]any {
	if request == nil {
		return nil
	}
	safe := make(map[string]any, len(request))
	for key, value := range request {
		if key == "provider_api_key" {
			continue
		}
		safe[key] = value
	}
	return safe
}
