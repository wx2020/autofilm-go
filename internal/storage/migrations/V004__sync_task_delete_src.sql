-- V004: sync_tasks 增加 delete_src（同步成功后删源），默认不同删
ALTER TABLE sync_tasks ADD COLUMN delete_src INTEGER NOT NULL DEFAULT 0;
