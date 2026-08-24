-- 网站域：站点、域名绑定、配置版本。本文件为 SQLite（默认驱动）。
-- 字段定义对齐 docs/04 4.7.1~4.7.3，本期实现静态站与反向代理站，
-- 其余字段（runtime_id、cert_id、waf_enabled 等）先落表，供后续里程碑填充。
CREATE TABLE web_site (
  id BIGINT NOT NULL,
  node_id BIGINT NOT NULL DEFAULT 0,
  name VARCHAR(128) NOT NULL,
  type VARCHAR(32) NOT NULL DEFAULT 'static',
  server_type VARCHAR(32) NOT NULL DEFAULT 'nginx',
  root_path VARCHAR(512) NOT NULL DEFAULT '',
  index_files VARCHAR(255) NOT NULL DEFAULT 'index.html index.htm',
  runtime_id BIGINT NOT NULL DEFAULT 0,
  php_version VARCHAR(32) NOT NULL DEFAULT '',
  proxy_target VARCHAR(512) NOT NULL DEFAULT '',
  proxy_host VARCHAR(255) NOT NULL DEFAULT '',
  proxy_websocket INTEGER NOT NULL DEFAULT 1,
  listen_port INTEGER NOT NULL DEFAULT 80,
  ssl_port INTEGER NOT NULL DEFAULT 443,
  ssl_enabled INTEGER NOT NULL DEFAULT 0,
  cert_id BIGINT NOT NULL DEFAULT 0,
  force_https INTEGER NOT NULL DEFAULT 0,
  http2_enabled INTEGER NOT NULL DEFAULT 1,
  http3_enabled INTEGER NOT NULL DEFAULT 0,
  waf_enabled INTEGER NOT NULL DEFAULT 0,
  access_log_enabled INTEGER NOT NULL DEFAULT 1,
  error_log_enabled INTEGER NOT NULL DEFAULT 1,
  access_log_path VARCHAR(512) NOT NULL DEFAULT '',
  error_log_path VARCHAR(512) NOT NULL DEFAULT '',
  rate_limit_json TEXT,
  status VARCHAR(32) NOT NULL DEFAULT 'running',
  config_version INTEGER NOT NULL DEFAULT 1,
  expire_at DATETIME NULL,
  group_name VARCHAR(64) NOT NULL DEFAULT 'default',
  remark VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  deleted_at DATETIME NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_site_node_name ON web_site (node_id, name, deleted_at);
CREATE INDEX idx_site_status ON web_site (status, deleted_at);
CREATE INDEX idx_site_cert ON web_site (cert_id);
CREATE INDEX idx_site_tenant ON web_site (tenant_id, deleted_at);

CREATE TABLE web_site_domain (
  id BIGINT NOT NULL,
  site_id BIGINT NOT NULL,
  domain VARCHAR(255) NOT NULL,
  port INTEGER NOT NULL DEFAULT 80,
  is_primary INTEGER NOT NULL DEFAULT 0,
  is_wildcard INTEGER NOT NULL DEFAULT 0,
  cert_id BIGINT NOT NULL DEFAULT 0,
  dns_resolved_ip VARCHAR(64) NOT NULL DEFAULT '',
  dns_check_at DATETIME NULL,
  dns_check_result VARCHAR(32) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  deleted_at DATETIME NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_domain_port ON web_site_domain (domain, port, deleted_at);
CREATE INDEX idx_domain_site ON web_site_domain (site_id, deleted_at);

CREATE TABLE web_site_config (
  id BIGINT NOT NULL,
  site_id BIGINT NOT NULL,
  version INTEGER NOT NULL,
  config_type VARCHAR(32) NOT NULL DEFAULT 'nginx',
  content TEXT NOT NULL,
  content_hash VARCHAR(64) NOT NULL,
  source VARCHAR(32) NOT NULL DEFAULT 'panel',
  is_current INTEGER NOT NULL DEFAULT 0,
  deploy_status VARCHAR(32) NOT NULL DEFAULT 'pending',
  deploy_at DATETIME NULL,
  deploy_error VARCHAR(512) NOT NULL DEFAULT '',
  change_summary VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  deleted_at DATETIME NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_cfg_site_version ON web_site_config (site_id, config_type, version);
CREATE INDEX idx_cfg_current ON web_site_config (site_id, is_current);
CREATE INDEX idx_cfg_hash ON web_site_config (content_hash);
