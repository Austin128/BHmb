-- 站点设置与伪静态（docs/07 7.8、7.10.1）。本文件为 SQLite（默认驱动）。
-- settings_json 承载可下发到 vhost 的站点级开关：目录浏览、静态缓存、防盗链、
-- 访问限制、重定向。限速沿用 web_site.rate_limit_json，日志开关沿用
-- access_log_enabled / error_log_enabled，默认文档沿用 index_files。
ALTER TABLE web_site ADD COLUMN settings_json TEXT;

CREATE TABLE web_rewrite_rule (
  id BIGINT NOT NULL,
  site_id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL DEFAULT '',
  template_code VARCHAR(64) NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  deleted_at DATETIME NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE INDEX idx_rewrite_site ON web_rewrite_rule (site_id, enabled, sort);
-- 一个站点当前只保留一份伪静态规则。deleted_at 为 NULL 时不参与唯一性比较，
-- 因此该索引只拦软删残留下的重复行，存活行的串行化由应用层站点行锁保证。
CREATE UNIQUE INDEX uk_rewrite_site ON web_rewrite_rule (site_id, deleted_at);
