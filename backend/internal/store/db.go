// Package store 封装 SQLite 数据访问：建表、初始化管理员、各业务表的读写。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	// 纯 Go 实现的 SQLite 驱动（无需 CGO，方便容器化）。
	// 与 GORM 共用同一个驱动，避免重复注册 sqlite 驱动导致 panic。
	_ "github.com/glebarez/go-sqlite"

	"ssedeploy/internal/config"
	"ssedeploy/internal/model"
)

// ErrNotFound 表示查询的记录不存在，handler 层据此返回 404。
var ErrNotFound = errors.New("记录不存在")

// ErrDuplicate 表示写入时命中了唯一索引（并发注册时的兜底错误）。
var ErrDuplicate = errors.New("记录已存在")

// Store 持有数据库连接。
// DB 供既有原生 SQL 使用，G 是 GORM 实例（账号认证模块使用），两者共享同一个连接池。
type Store struct {
	DB *sql.DB
	G  *gorm.DB
}

// schema 为全部建表语句，使用 IF NOT EXISTS 保证重复启动幂等。
const schema = `
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name  TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'admin',
  created_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  email         TEXT,
  updated_at    INTEGER
);

CREATE TABLE IF NOT EXISTS pod_metas (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  namespace  TEXT NOT NULL,
  name       TEXT NOT NULL,
  owner_id   INTEGER NOT NULL,
  image      TEXT NOT NULL,
  cpu        TEXT NOT NULL,
  memory     TEXT NOT NULL,
  disk       TEXT NOT NULL,
  replicas   INTEGER NOT NULL DEFAULT 1,
  env        TEXT NOT NULL DEFAULT '[]',
  command    TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  UNIQUE(namespace, name)
);

CREATE TABLE IF NOT EXISTS deployments (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  namespace     TEXT NOT NULL,
  pod_name      TEXT NOT NULL DEFAULT '',
  name          TEXT NOT NULL,
  owner_id      INTEGER NOT NULL,
  source        TEXT NOT NULL DEFAULT 'repo',
  repo_url      TEXT NOT NULL DEFAULT '',
  branch        TEXT NOT NULL DEFAULT 'main',
  sub_dir       TEXT NOT NULL DEFAULT '',
  type          TEXT NOT NULL DEFAULT 'service',
  schedule_type TEXT NOT NULL DEFAULT 'inference',
  gpu           TEXT NOT NULL DEFAULT '',
  token         TEXT NOT NULL DEFAULT '',
  health_path   TEXT NOT NULL DEFAULT '',
  timeout_sec   INTEGER NOT NULL DEFAULT 5,
  auto_deploy   INTEGER NOT NULL DEFAULT 0,
  webhook_secret TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'pending',
  message       TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  updated_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  UNIQUE(namespace, name)
);

CREATE TABLE IF NOT EXISTS metric_samples (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  namespace TEXT NOT NULL,
  name      TEXT NOT NULL,
  cpu_milli INTEGER NOT NULL,
  mem_bytes INTEGER NOT NULL,
  ts        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_metric_samp ON metric_samples(namespace, name, ts);
`

// New 打开数据库、建表并在首次启动时创建管理员账号。
func New(cfg *config.Config) (*Store, error) {
	// 确保数据库所在目录存在（容器里通常是 /data）
	if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}

	// WAL 模式提升并发读性能，busy_timeout 避免瞬时锁冲突
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", cfg.DBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// 单连接写入，规避 SQLite 写锁竞争
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}

	// 历史库补列：deployments.webhook_secret 用于 push 自动部署的回调校验
	// 重复执行会返回 duplicate column 错误，这里忽略即可
	_, _ = db.Exec(`ALTER TABLE deployments ADD COLUMN webhook_secret TEXT NOT NULL DEFAULT ''`)
	// 历史库补列：pod_name 记录部署所属的 Pod（部署脚本在 Pod 工作区内执行）
	_, _ = db.Exec(`ALTER TABLE deployments ADD COLUMN pod_name TEXT NOT NULL DEFAULT ''`)

	// GORM 复用上面的连接池；TranslateError 让唯一索引冲突统一返回 ErrDuplicatedKey；
	// 日志交给业务层打印，这里静默避免唯一索引冲突时输出大段红色错误。
	gormDB, err := gorm.Open(sqlite.Dialector{Conn: db}, &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 GORM 失败: %w", err)
	}

	// 自动迁移用户表：补齐 email / updated_at 字段与 email 唯一索引
	if err := gormDB.AutoMigrate(&model.User{}); err != nil {
		return nil, fmt.Errorf("自动迁移用户表失败: %w", err)
	}

	s := &Store{DB: db, G: gormDB}
	if err := s.seedAdmin(cfg.DefaultAdmin, cfg.DefaultPassword); err != nil {
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error {
	return s.DB.Close()
}

// seedAdmin 在没有用户时创建默认管理员，密码使用 bcrypt 哈希存储。
func (s *Store) seedAdmin(username, password string) error {
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("统计用户失败: %w", err)
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}
	_, err = s.DB.Exec(
		`INSERT INTO users(username, password_hash, display_name, role) VALUES(?, ?, ?, ?)`,
		username, string(hash), "管理员", "admin",
	)
	if err != nil {
		return fmt.Errorf("创建默认管理员失败: %w", err)
	}
	return nil
}
