CREATE TABLE plugin_sessions (
  id TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL,
  role TEXT NOT NULL,
  email TEXT NOT NULL,
  username TEXT NOT NULL,
  plugin_key TEXT NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_plugin_sessions_expires_at
  ON plugin_sessions(expires_at);

CREATE TABLE plugin_generation_history (
  id TEXT PRIMARY KEY,
  user_id BIGINT NOT NULL,
  user_email TEXT NOT NULL,
  plugin_key TEXT NOT NULL,
  prompt TEXT NOT NULL,
  status TEXT NOT NULL,
  request_payload TEXT NOT NULL,
  result_payload TEXT,
  error_message TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_plugin_generation_history_user_created
  ON plugin_generation_history(user_id, created_at DESC);

CREATE INDEX idx_plugin_generation_history_created
  ON plugin_generation_history(created_at DESC);
