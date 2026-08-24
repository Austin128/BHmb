-- PostgreSQL 方言覆盖：网站、域名、配置版本三表。
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
  proxy_websocket BOOLEAN NOT NULL DEFAULT TRUE,
  listen_port INTEGER NOT NULL DEFAULT 80,
  ssl_port INTEGER NOT NULL DEFAULT 443,
  ssl_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  cert_id BIGINT NOT NULL DEFAULT 0,
  force_https BOOLEAN NOT NULL DEFAULT FALSE,
  http2_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  http3_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  waf_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  access_log_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  error_log_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  access_log_path VARCHAR(512) NOT NULL DEFAULT '',
  error_log_path VARCHAR(512) NOT NULL DEFAULT '',
  rate_limit_json TEXT,
  status VARCHAR(32) NOT NULL DEFAULT 'running',
  config_version INTEGER NOT NULL DEFAULT 1,
  expire_at TIMESTAMP(3) NULL,
  group_name VARCHAR(64) NOT NULL DEFAULT 'default',
  remark VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
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
  is_primary BOOLEAN NOT NULL DEFAULT FALSE,
  is_wildcard BOOLEAN NOT NULL DEFAULT FALSE,
  cert_id BIGINT NOT NULL DEFAULT 0,
  dns_resolved_ip VARCHAR(64) NOT NULL DEFAULT '',
  dns_check_at TIMESTAMP(3) NULL,
  dns_check_result VARCHAR(32) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
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
  is_current BOOLEAN NOT NULL DEFAULT FALSE,
  deploy_status VARCHAR(32) NOT NULL DEFAULT 'pending',
  deploy_at TIMESTAMP(3) NULL,
  deploy_error VARCHAR(512) NOT NULL DEFAULT '',
  change_summary VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_cfg_site_version ON web_site_config (site_id, config_type, version);
CREATE INDEX idx_cfg_current ON web_site_config (site_id, is_current);
CREATE INDEX idx_cfg_hash ON web_site_config (content_hash);
