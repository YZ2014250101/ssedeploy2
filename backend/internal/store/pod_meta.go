package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"ssedeploy/internal/model"
)

// podMetaColumns 统一查询字段。
const podMetaColumns = `id, namespace, name, owner_id, image, cpu, memory, disk, replicas, env, command, created_at, updated_at`

// scanPodMeta 把一行数据映射成 PodMeta，env/command 以 JSON 文本存储。
func scanPodMeta(row interface{ Scan(...any) error }) (*model.PodMeta, error) {
	var (
		m       model.PodMeta
		envJSON string
		cmdJSON string
	)
	err := row.Scan(
		&m.ID, &m.Namespace, &m.Name, &m.OwnerID, &m.Image, &m.CPU, &m.Memory, &m.Disk,
		&m.Replicas, &envJSON, &cmdJSON, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// 解析失败时返回空集合，保证接口可用
	if err := json.Unmarshal([]byte(envJSON), &m.Env); err != nil {
		m.Env = []model.EnvVar{}
	}
	if err := json.Unmarshal([]byte(cmdJSON), &m.Command); err != nil {
		m.Command = []string{}
	}
	return &m, nil
}

// CreatePodMeta 新增一条 Pod 元数据。
func (s *Store) CreatePodMeta(m *model.PodMeta) error {
	envJSON, _ := json.Marshal(m.Env)
	cmdJSON, _ := json.Marshal(m.Command)
	res, err := s.DB.Exec(
		`INSERT INTO pod_metas(namespace, name, owner_id, image, cpu, memory, disk, replicas, env, command)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Namespace, m.Name, m.OwnerID, m.Image, m.CPU, m.Memory, m.Disk, m.Replicas, string(envJSON), string(cmdJSON),
	)
	if err != nil {
		return fmt.Errorf("保存 Pod 元数据失败: %w", err)
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

// GetPodMeta 按命名空间 + 名称查询，找不到返回 ErrNotFound。
func (s *Store) GetPodMeta(namespace, name string) (*model.PodMeta, error) {
	row := s.DB.QueryRow(`SELECT `+podMetaColumns+` FROM pod_metas WHERE namespace = ? AND name = ?`, namespace, name)
	return scanPodMeta(row)
}

// ListPodMetas 查询命名空间下全部元数据。
func (s *Store) ListPodMetas(namespace string) ([]*model.PodMeta, error) {
	rows, err := s.DB.Query(`SELECT `+podMetaColumns+` FROM pod_metas WHERE namespace = ?`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.PodMeta
	for rows.Next() {
		m, err := scanPodMeta(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// UpdatePodMeta 更新镜像、规格、环境变量与启动命令。
func (s *Store) UpdatePodMeta(m *model.PodMeta) error {
	envJSON, _ := json.Marshal(m.Env)
	cmdJSON, _ := json.Marshal(m.Command)
	_, err := s.DB.Exec(
		`UPDATE pod_metas
		 SET image = ?, cpu = ?, memory = ?, env = ?, command = ?, updated_at = strftime('%s','now')
		 WHERE namespace = ? AND name = ?`,
		m.Image, m.CPU, m.Memory, string(envJSON), string(cmdJSON), m.Namespace, m.Name,
	)
	if err != nil {
		return fmt.Errorf("更新 Pod 元数据失败: %w", err)
	}
	return nil
}

// UpdateReplicas 启停后同步期望副本数，保证列表规格展示与集群一致。
func (s *Store) UpdateReplicas(namespace, name string, replicas int32) error {
	_, err := s.DB.Exec(
		`UPDATE pod_metas SET replicas = ?, updated_at = strftime('%s','now') WHERE namespace = ? AND name = ?`,
		replicas, namespace, name,
	)
	return err
}

// DeletePodMeta 删除元数据（Pod 被删除时调用）。
func (s *Store) DeletePodMeta(namespace, name string) error {
	_, err := s.DB.Exec(`DELETE FROM pod_metas WHERE namespace = ? AND name = ?`, namespace, name)
	return err
}
