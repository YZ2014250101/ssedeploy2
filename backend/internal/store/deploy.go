package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"ssedeploy/internal/model"
)

// deployColumns 统一查询字段。
const deployColumns = `id, namespace, pod_name, name, owner_id, source, repo_url, branch, sub_dir, type,
	schedule_type, gpu, token, health_path, timeout_sec, auto_deploy, webhook_secret, status, message, created_at, updated_at`

// scanDeploy 把一行数据映射成 Deploy。
func scanDeploy(row interface{ Scan(...any) error }) (*model.Deploy, error) {
	var (
		d          model.Deploy
		autoDeploy int
	)
	err := row.Scan(
		&d.ID, &d.Namespace, &d.PodName, &d.Name, &d.OwnerID, &d.Source, &d.RepoURL, &d.Branch, &d.SubDir,
		&d.Type, &d.ScheduleType, &d.GPU, &d.Token, &d.HealthPath, &d.TimeoutSec, &autoDeploy,
		&d.WebhookSecret, &d.Status, &d.Message, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.AutoDeploy = autoDeploy == 1
	return &d, nil
}

// CreateDeploy 新增一条部署记录。
func (s *Store) CreateDeploy(d *model.Deploy) error {
	res, err := s.DB.Exec(
		`INSERT INTO deployments(namespace, pod_name, name, owner_id, source, repo_url, branch, sub_dir, type,
			schedule_type, gpu, token, health_path, timeout_sec, auto_deploy, webhook_secret, status, message)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Namespace, d.PodName, d.Name, d.OwnerID, d.Source, d.RepoURL, d.Branch, d.SubDir, d.Type,
		d.ScheduleType, d.GPU, d.Token, d.HealthPath, d.TimeoutSec, boolToInt(d.AutoDeploy), d.WebhookSecret, d.Status, d.Message,
	)
	if err != nil {
		if isUniqueErr(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("保存部署记录失败: %w", err)
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

// GetDeploy 按命名空间 + 名称查询，找不到返回 ErrNotFound。
func (s *Store) GetDeploy(namespace, name string) (*model.Deploy, error) {
	row := s.DB.QueryRow(`SELECT `+deployColumns+` FROM deployments WHERE namespace = ? AND name = ?`, namespace, name)
	return scanDeploy(row)
}

// ListDeploys 查询命名空间下全部部署记录，按创建时间倒序。
func (s *Store) ListDeploys(namespace string) ([]*model.Deploy, error) {
	rows, err := s.DB.Query(
		`SELECT `+deployColumns+` FROM deployments WHERE namespace = ? ORDER BY created_at DESC, id DESC`, namespace,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.Deploy
	for rows.Next() {
		d, err := scanDeploy(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// UpdateDeploy 更新可编辑字段，名称与负责人不可修改。
func (s *Store) UpdateDeploy(d *model.Deploy) error {
	_, err := s.DB.Exec(
		`UPDATE deployments
		 SET repo_url = ?, branch = ?, sub_dir = ?, type = ?, schedule_type = ?, gpu = ?, token = ?,
		     health_path = ?, timeout_sec = ?, auto_deploy = ?, updated_at = strftime('%s','now')
		 WHERE namespace = ? AND name = ?`,
		d.RepoURL, d.Branch, d.SubDir, d.Type, d.ScheduleType, d.GPU, d.Token,
		d.HealthPath, d.TimeoutSec, boolToInt(d.AutoDeploy), d.Namespace, d.Name,
	)
	if err != nil {
		return fmt.Errorf("更新部署记录失败: %w", err)
	}
	return nil
}

// UpdateDeployStatus 同步部署实例的运行时状态。
func (s *Store) UpdateDeployStatus(namespace, name, status, message string) error {
	_, err := s.DB.Exec(
		`UPDATE deployments SET status = ?, message = ?, updated_at = strftime('%s','now')
		 WHERE namespace = ? AND name = ?`,
		status, message, namespace, name,
	)
	return err
}

// DeleteDeploy 删除部署记录。
func (s *Store) DeleteDeploy(namespace, name string) error {
	_, err := s.DB.Exec(`DELETE FROM deployments WHERE namespace = ? AND name = ?`, namespace, name)
	return err
}

// isUniqueErr 粗略判断唯一索引冲突（不同驱动返回的文案略有差异）。
func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// boolToInt 把布尔值转成 SQLite 的 0/1。
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
