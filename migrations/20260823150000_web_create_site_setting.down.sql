-- 回滚顺序：先删依赖表，再摘除站点扩展列。
DROP TABLE IF EXISTS web_rewrite_rule;
ALTER TABLE web_site DROP COLUMN settings_json;
