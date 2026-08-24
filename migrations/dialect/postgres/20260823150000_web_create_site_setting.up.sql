-- 站点设置与伪静态（docs/07 7.8、7.10.1）。PostgreSQL 方言。
ALTER TABLE web_site ADD COLUMN settings_json TEXT;

COMMENT ON COLUMN web_site.settings_json IS '站点级设置 JSON：目录浏览/缓存/防盗链/访问限制/重定向';

CREATE TABLE web_rewrite_rule (
  id BIGINT NOT NULL,
  site_id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL DEFAULT '',
  template_code VARCHAR(64) NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE INDEX idx_rewrite_site ON web_rewrite_rule (site_id, enabled, sort);
CREATE UNIQUE INDEX uk_rewrite_site ON web_rewrite_rule (site_id, deleted_at);
