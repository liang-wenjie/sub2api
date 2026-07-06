const API_BASE = document.documentElement.getAttribute("data-plugin-api-base")
  || document.body.getAttribute("data-plugin-api-base")
  || (document.querySelector("[data-plugin-api-base]") && document.querySelector("[data-plugin-api-base]").getAttribute("data-plugin-api-base"))
  || "/api/plugins/image-generation";

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
  const box = byId("request-message");
  box.textContent = text;
  box.className = "message" + (kind ? " " + kind : "");
}

async function fetchJSON(url, options) {
  const response = await fetch(url, Object.assign({ credentials: "same-origin" }, options || {}));
  const contentType = response.headers.get("content-type") || "";
  const payload = contentType.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) {
    const message = payload && typeof payload === "object" ? (payload.error || payload.message || JSON.stringify(payload)) : String(payload);
    throw new Error(message || ("HTTP " + response.status));
  }
  return payload;
}

function renderSession() {
  byId("session-user").textContent = state.me ? (state.me.username || state.me.email || "-") : "未登录";
  byId("session-role").textContent = state.me ? state.me.role : "未登录";
  byId("session-user-id").textContent = state.me ? String(state.me.user_id) : "-";
}

function renderHistory() {
  const list = byId("history-list");
  const items = state.history || [];
  if (!items.length) {
    list.innerHTML = '<div class="empty">还没有历史记录。</div>';
    return;
  }
  list.innerHTML = items.map(function(item) {
    const model = item.request && item.request.model ? item.request.model : "-";
    const size = item.request && item.request.size ? item.request.size : "-";
    const imageCount = item.result && Array.isArray(item.result.images) ? item.result.images.length : 0;
    const prompt = escapeHTML(item.prompt || "");
    return "" +
      '<article class="history-item">' +
        '<div class="history-meta"><span>' + escapeHTML(item.status || "-") + '</span><span>' + formatDate(item.created_at) + "</span></div>" +
        '<div class="card-title">' + prompt + "</div>" +
        '<div class="card-meta"><span>' + escapeHTML(model) + "</span><span>" + escapeHTML(size) + "</span><span>" + imageCount + " images</span></div>" +
        '<div class="hero-actions form-actions">' +
          '<button class="button-secondary" type="button" data-action="retry" data-id="' + escapeHTML(item.id) + '">重试</button>' +
        "</div>" +
      "</article>";
  }).join("");
}

function renderCreations() {
  const grid = byId("creation-grid");
  const items = state.creations || [];
  byId("creation-count").textContent = items.length + " items";
  if (!items.length) {
    grid.innerHTML = '<div class="empty">还没有创建内容。先提交一次生成请求，创建列表就会出现在这里。</div>';
    return;
  }
  grid.innerHTML = items.map(function(item) {
    const imageSrc = item.image_url || (item.b64_json ? ("data:image/png;base64," + item.b64_json) : "");
    const prompt = escapeHTML(item.prompt || "Untitled");
    const metaModel = escapeHTML(item.model || "-");
    const metaSize = escapeHTML(item.size || "-");
    const metaUser = escapeHTML(item.user_email || "-");
    return "" +
      '<article class="card">' +
        '<div class="card-preview">' +
          (imageSrc ? '<img src="' + escapeHTML(imageSrc) + '" alt="' + prompt + '">' : '<span class="subtle">No Preview</span>') +
        "</div>" +
        '<div class="card-body">' +
          '<div class="card-title">' + prompt + "</div>" +
          '<div class="card-meta"><span>' + metaModel + "</span><span>" + metaSize + "</span></div>" +
          '<div class="card-meta"><span>' + metaUser + "</span><span>" + formatDate(item.created_at) + "</span></div>" +
        "</div>" +
      "</article>";
  }).join("");
}

function formatDate(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function escapeHTML(value) {
  return String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

async function loadConfig() {
  state.config = await fetchJSON(API_BASE + "/config");
  if (state.config && state.config.plugin_key) {
    byId("plugin-key").textContent = state.config.plugin_key;
  }
  if (state.config && state.config.image_provider_base_url) {
    byId("provider-base-url").textContent = state.config.image_provider_base_url;
  }
  if (state.config && typeof state.config.history_enabled !== "undefined") {
    byId("history-enabled").textContent = String(state.config.history_enabled);
  }
}

async function loadSession() {
  state.me = await fetchJSON("/api/me");
  renderSession();
}

async function loadHistory() {
  const payload = await fetchJSON(API_BASE + "/history");
  state.history = payload.items || [];
  renderHistory();
}

async function loadCreations() {
  const payload = await fetchJSON(API_BASE + "/creations");
  state.creations = payload.items || [];
  renderCreations();
}

async function submitGenerate(event) {
  event.preventDefault();
  const providerApiKey = byId("provider-api-key").value.trim();
  const prompt = byId("prompt").value.trim();
  const model = byId("model").value.trim();
  const size = byId("size").value;
  const referenceURLs = byId("reference-urls").value
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
    reference_images: referenceImages,
  };

  setMessage("正在生成图片，请稍候...", "");
  try {
    const result = await fetchJSON(API_BASE + "/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    setMessage("生成成功，已刷新创建列表和历史记录。任务 ID: " + result.job_id, "success");
    await Promise.all([loadHistory(), loadCreations()]);
  } catch (error) {
    setMessage(error && error.message ? error.message : "生成失败", "error");
  }
}

async function retryGenerate(id) {
  setMessage("正在重试该条历史记录...", "");
  try {
    const result = await fetchJSON(API_BASE + "/history/" + encodeURIComponent(id) + "/retry", {
      method: "POST",
    });
    setMessage("重试成功，已创建新任务。任务 ID: " + result.job_id, "success");
    await Promise.all([loadHistory(), loadCreations()]);
  } catch (error) {
    setMessage(error && error.message ? error.message : "重试失败", "error");
  }
}

function resetForm() {
  byId("provider-api-key").value = "";
  byId("prompt").value = "";
  byId("model").value = "gpt-image-1";
  byId("size").value = "1024x1024";
  byId("reference-urls").value = "";
  byId("reference-file-input").value = "";
  state.localReferenceImage = null;
  byId("reference-file-preview").textContent = "未选择本地参考图。";
  setMessage("表单已重置。", "");
}

function fillDevExample() {
  byId("provider-api-key").value = "sk-your-provider-key";
  byId("prompt").value = "为一款茶饮品牌生成一张干净、克制、带自然高光的电商主图";
  byId("model").value = "gpt-image-1";
  byId("size").value = "1024x1024";
  byId("reference-urls").value = "";
  byId("reference-file-input").value = "";
  state.localReferenceImage = null;
  byId("reference-file-preview").textContent = "未选择本地参考图。";
  setMessage("已填入调试示例，可以直接改 prompt 或 key 后提交。", "");
}

function handleReferenceFileChange(event) {
  const input = event.target;
  if (!(input instanceof HTMLInputElement) || !input.files || !input.files[0]) {
    state.localReferenceImage = null;
    byId("reference-file-preview").textContent = "未选择本地参考图。";
    return;
  }
  const file = input.files[0];
  const reader = new FileReader();
  reader.onload = function() {
    state.localReferenceImage = {
      name: file.name,
      mime_type: file.type || "image/png",
      data_url: String(reader.result || ""),
    };
    byId("reference-file-preview").textContent = "已选择: " + file.name;
  };
  reader.onerror = function() {
    state.localReferenceImage = null;
    byId("reference-file-preview").textContent = "本地参考图读取失败，请重试。";
    setMessage("本地参考图读取失败，请重试。", "error");
  };
  reader.readAsDataURL(file);
}

document.addEventListener("click", function(event) {
  const target = event.target;
  if (!(target instanceof HTMLElement)) return;
  const action = target.getAttribute("data-action");
  if (action === "retry") {
    const id = target.getAttribute("data-id");
    if (id) {
      retryGenerate(id);
    }
  }
});

byId("image-composer").addEventListener("submit", submitGenerate);
byId("refresh-session").addEventListener("click", function() {
  loadSession().catch(function(error) {
    setMessage(error && error.message ? error.message : "加载会话失败", "error");
  });
});
byId("refresh-history").addEventListener("click", function() {
  loadHistory().catch(function(error) {
    setMessage(error && error.message ? error.message : "加载历史失败", "error");
  });
});
byId("refresh-creations").addEventListener("click", function() {
  loadCreations().catch(function(error) {
    setMessage(error && error.message ? error.message : "加载创建列表失败", "error");
  });
});
byId("prefill-dev-login").addEventListener("click", fillDevExample);
byId("reset-form").addEventListener("click", resetForm);
byId("reference-file-input").addEventListener("change", handleReferenceFileChange);

Promise.all([loadConfig(), loadSession(), loadHistory(), loadCreations()])
  .then(function() {
    setMessage("插件服务已就绪，可以开始生成图片。", "success");
  })
  .catch(function(error) {
    setMessage((error && error.message ? error.message : "初始化失败") + "。如果你在本地联调，先访问 /dev/login 创建插件会话。", "error");
  });
