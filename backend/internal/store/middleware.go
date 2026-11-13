package store

import (
	"database/sql"
	"errors"
	"fmt"

	"ssedeploy/internal/model"
)

// middlewareColumns 统一查询字段，顺序与 scanMiddleware 保持一致。
const middlewareColumns = `id, namespace, pod_name, owner_id, type, name, version, image,
	storage, username, password, database, port, created_at`

// scanMiddleware 把一行记录映射为 Middleware。
func scanMiddleware(row interface{ Scan(...any) error }) (*model.Middleware, error) {
	var m model.Middleware
	err := row.Scan(
		&m.ID, &m.Namespace, &m.PodName, &m.OwnerID, &m.Type, &m.Name, &m.Version, &m.Image,
		&m.Storage, &m.Username, &m.Password, &m.Database, &m.Port, &m.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateMiddleware 新增一条中间件连接信息。
// 名称在命名空间内唯一，重复时返回 ErrDuplicate 便于接口层给出 409。
func (s *Store) CreateMiddleware(m *model.Middleware) error {
	res, err := s.DB.Exec(
		`INSERT INTO middlewares(namespace, pod_name, owner_id, type, name, version, image,
		 storage, username, password, database, port)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Namespace, m.PodName, m.OwnerID, m.Type, m.Name, m.Version, m.Image,
		m.Storage, m.Username, m.Password, m.Database, m.Port,
	)
	if err != nil {
		// SQLite 唯一索引冲突：统一转换为业务错误
		if isUniqueErr(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("保存中间件失败: %w", err)
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

// ListMiddlewares 查询某个 Pod 名下的全部中间件（按创建时间倒序）。
func (s *Store) ListMiddlewares(namespace, podName string) ([]*model.Middleware, error) {
	rows, err := s.DB.Query(
		`SELECT `+middlewareColumns+` FROM middlewares
		 WHERE namespace = ? AND pod_name = ? ORDER BY created_at DESC`,
		namespace, podName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.Middleware
	for rows.Next() {
		m, err := scanMiddleware(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// GetMiddleware 按服务名查询，找不到返回 ErrNotFound。
func (s *Store) GetMiddleware(namespace, name string) (*model.Middleware, error) {
	row := s.DB.QueryRow(
		`SELECT `+middlewareColumns+` FROM middlewares WHERE namespace = ? AND name = ?`,
		namespace, name,
	)
	return scanMiddleware(row)
}

// DeleteMiddleware 删除记录（K8s 资源由 k8s 层负责回收）。
func (s *Store) DeleteMiddleware(namespace, name string) error {
	_, err := s.DB.Exec(`DELETE FROM middlewares WHERE namespace = ? AND name = ?`, namespace, name)
	return err
}
