-- PostgreSQL 方言覆盖：RBAC 四表。
CREATE TABLE sys_role (
  id BIGINT NOT NULL,
  code VARCHAR(64) NOT NULL,
  name VARCHAR(64) NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  is_builtin BOOLEAN NOT NULL DEFAULT FALSE,
  data_scope VARCHAR(32) NOT NULL DEFAULT 'self',
  scope_node_json TEXT,
  sort INTEGER NOT NULL DEFAULT 0,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_role_code ON sys_role (tenant_id, code, deleted_at);
CREATE INDEX idx_role_tenant ON sys_role (tenant_id, deleted_at);

CREATE TABLE sys_user_role (
  id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  role_id BIGINT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_ur_user_role ON sys_user_role (user_id, role_id);
CREATE INDEX idx_ur_role ON sys_user_role (role_id);

CREATE TABLE sys_permission (
  id BIGINT NOT NULL,
  code VARCHAR(128) NOT NULL,
  name VARCHAR(64) NOT NULL,
  module VARCHAR(32) NOT NULL,
  resource VARCHAR(64) NOT NULL,
  action VARCHAR(32) NOT NULL,
  api_path VARCHAR(255) NOT NULL DEFAULT '',
  http_method VARCHAR(16) NOT NULL DEFAULT '',
  is_sensitive BOOLEAN NOT NULL DEFAULT FALSE,
  need_audit BOOLEAN NOT NULL DEFAULT TRUE,
  sort INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_perm_code ON sys_permission (code);
CREATE INDEX idx_perm_module ON sys_permission (module, resource);

CREATE TABLE sys_role_permission (
  id BIGINT NOT NULL,
  role_id BIGINT NOT NULL,
  permission_id BIGINT NOT NULL,
  permission_code VARCHAR(128) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_rp_role_perm ON sys_role_permission (role_id, permission_id);
CREATE INDEX idx_rp_code ON sys_role_permission (permission_code);
