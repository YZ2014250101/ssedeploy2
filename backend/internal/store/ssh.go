package store

import (
	"database/sql"
	"errors"
	"fmt"

	"ssedeploy/internal/model"
)

// sshCredentialColumns 统一查询字段，顺序与 scanSSHCredential 保持一致。
const sshCredentialColumns = `id, namespace, pod_name, host, port, username, password, created_at`

// scanSSHCredential 把一行记录映射为 SSHCredential。
func scanSSHCredential(row interface{ Scan(...any) error }) (*model.SSHCredential, error) {
	var c model.SSHCredential
	err := row.Scan(
		&c.ID, &c.Namespace, &c.PodName, &c.Host, &c.Port, &c.Username, &c.Password, &c.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveSSHCredential 保存 Pod 的 SSH 凭证，同一 Pod 重复保存时覆盖旧记录。
func (s *Store) SaveSSHCredential(c *model.SSHCredential) error {
	res, err := s.DB.Exec(
		`INSERT INTO pod_ssh_credentials(namespace, pod_name, host, port, username, password)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(namespace, pod_name) DO UPDATE SET
		   host = excluded.host,
		   port = excluded.port,
		   username = excluded.username,
		   password = excluded.password`,
		c.Namespace, c.PodName, c.Host, c.Port, c.Username, c.Password,
	)
	if err != nil {
		return fmt.Errorf("保存 SSH 凭证失败: %w", err)
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

// GetSSHCredential 按命名空间 + Pod 名查询凭证，找不到返回 ErrNotFound。
func (s *Store) GetSSHCredential(namespace, podName string) (*model.SSHCredential, error) {
	row := s.DB.QueryRow(
		`SELECT `+sshCredentialColumns+` FROM pod_ssh_credentials WHERE namespace = ? AND pod_name = ?`,
		namespace, podName,
	)
	return scanSSHCredential(row)
}

// GetSSHCredentialByPort 按命名空间 + SSH 端口查询凭证，SSH 网关用它完成认证。
func (s *Store) GetSSHCredentialByPort(namespace string, port int32) (*model.SSHCredential, error) {
	row := s.DB.QueryRow(
		`SELECT `+sshCredentialColumns+` FROM pod_ssh_credentials WHERE namespace = ? AND port = ?`,
		namespace, port,
	)
	return scanSSHCredential(row)
}

// ListSSHCredentials 返回命名空间内全部 SSH 凭证，
// SSH 网关启动时据此为每个端口恢复监听。
func (s *Store) ListSSHCredentials(namespace string) ([]*model.SSHCredential, error) {
	rows, err := s.DB.Query(
		`SELECT `+sshCredentialColumns+` FROM pod_ssh_credentials WHERE namespace = ? ORDER BY port`,
		namespace,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []*model.SSHCredential
	for rows.Next() {
		c, err := scanSSHCredential(rows)
		if err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

// DeleteSSHCredential 删除 Pod 的 SSH 凭证（Pod 被删除时调用）。
func (s *Store) DeleteSSHCredential(namespace, podName string) error {
	_, err := s.DB.Exec(`DELETE FROM pod_ssh_credentials WHERE namespace = ? AND pod_name = ?`, namespace, podName)
	return err
}

// UsedSSHPorts 返回命名空间中已被占用的 SSH 端口及其归属 Pod，
// 用于创建 Pod 时分配一个不与其他 Pod 冲突的专属端口。
func (s *Store) UsedSSHPorts(namespace string) (map[int32]string, error) {
	rows, err := s.DB.Query(
		`SELECT pod_name, port FROM pod_ssh_credentials WHERE namespace = ?`, namespace,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	owners := make(map[int32]string)
	for rows.Next() {
		var (
			podName string
			port    int32
		)
		if err := rows.Scan(&podName, &port); err != nil {
			return nil, err
		}
		owners[port] = podName
	}
	return owners, rows.Err()
}

// sshEventLimit 是每个 Pod 保留的连接记录条数上限，避免日志无限增长。
const sshEventLimit = 500

// SaveSSHEvent 记录一次 SSH 连接事件，并裁剪该 Pod 的历史记录。
func (s *Store) SaveSSHEvent(e *model.SSHEvent) error {
	res, err := s.DB.Exec(
		`INSERT INTO pod_ssh_events(namespace, pod_name, username, remote_addr, event, message)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		e.Namespace, e.PodName, e.Username, e.RemoteAddr, e.Event, e.Message,
	)
	if err != nil {
		return fmt.Errorf("保存 SSH 连接记录失败: %w", err)
	}
	e.ID, _ = res.LastInsertId()
	_, _ = s.DB.Exec(
		`DELETE FROM pod_ssh_events
		 WHERE namespace = ? AND pod_name = ? AND id NOT IN (
		   SELECT id FROM pod_ssh_events WHERE namespace = ? AND pod_name = ? ORDER BY id DESC LIMIT ?
		 )`,
		e.Namespace, e.PodName, e.Namespace, e.PodName, sshEventLimit,
	)
	return nil
}

// ListSSHEvents 返回某个 Pod 最近的 SSH 连接记录（按时间倒序）。
func (s *Store) ListSSHEvents(namespace, podName string, limit int) ([]*model.SSHEvent, error) {
	if limit <= 0 || limit > sshEventLimit {
		limit = sshEventLimit
	}
	rows, err := s.DB.Query(
		`SELECT id, namespace, pod_name, username, remote_addr, event, message, created_at
		 FROM pod_ssh_events WHERE namespace = ? AND pod_name = ?
		 ORDER BY id DESC LIMIT ?`,
		namespace, podName, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*model.SSHEvent, 0, limit)
	for rows.Next() {
		var e model.SSHEvent
		if err := rows.Scan(
			&e.ID, &e.Namespace, &e.PodName, &e.Username, &e.RemoteAddr, &e.Event, &e.Message, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}
