-- PostgreSQL 方言覆盖：用户与会话。
CREATE TABLE sys_user (
  id BIGINT NOT NULL,
  username VARCHAR(64) NOT NULL,
  nickname VARCHAR(64) NOT NULL DEFAULT '',
  email VARCHAR(128) NOT NULL DEFAULT '',
  mobile VARCHAR(32) NOT NULL DEFAULT '',
  password_hash VARCHAR(128) NOT NULL,
  avatar VARCHAR(512) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  is_super BOOLEAN NOT NULL DEFAULT FALSE,
  two_factor_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  allow_ip_json TEXT,
  last_login_at TIMESTAMP(3) NULL,
  last_login_ip VARCHAR(64) NOT NULL DEFAULT '',
  login_fail_count INTEGER NOT NULL DEFAULT 0,
  locked_until TIMESTAMP(3) NULL,
  password_updated_at TIMESTAMP(3) NULL,
  must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
  lang VARCHAR(16) NOT NULL DEFAULT 'zh-CN',
  timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
  remark VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_user_username ON sys_user (tenant_id, username, deleted_at);
CREATE INDEX idx_user_email ON sys_user (email);
CREATE INDEX idx_user_status ON sys_user (status, deleted_at);
CREATE INDEX idx_user_tenant ON sys_user (tenant_id, deleted_at);

CREATE TABLE sys_session (
  id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  jti VARCHAR(64) NOT NULL,
  refresh_token_hash VARCHAR(128) NOT NULL,
  device_type VARCHAR(32) NOT NULL DEFAULT 'web',
  user_agent VARCHAR(512) NOT NULL DEFAULT '',
  client_ip VARCHAR(64) NOT NULL DEFAULT '',
  login_at TIMESTAMP(3) NOT NULL,
  last_active_at TIMESTAMP(3) NOT NULL,
  access_expire_at TIMESTAMP(3) NOT NULL,
  refresh_expire_at TIMESTAMP(3) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  revoke_reason VARCHAR(128) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_session_jti ON sys_session (jti);
CREATE INDEX idx_session_refresh ON sys_session (refresh_token_hash);
CREATE INDEX idx_session_user ON sys_session (user_id, status, last_active_at);
CREATE INDEX idx_session_gc ON sys_session (refresh_expire_at);
