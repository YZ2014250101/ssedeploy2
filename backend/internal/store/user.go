package store

import (
	"database/sql"
	"errors"

	"ssedeploy/internal/model"
)

// userColumns 统一查询字段，避免各处重复书写。
const userColumns = `id, username, password_hash, display_name, role, created_at`

// scanUser 把一行数据映射成 User。
func scanUser(row interface{ Scan(...any) error }) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByUsername 按账号名查询用户，登录时使用。
func (s *Store) GetUserByUsername(username string) (*model.User, error) {
	row := s.DB.QueryRow(`SELECT `+userColumns+` FROM users WHERE username = ?`, username)
	return scanUser(row)
}

// GetUserByID 按主键查询用户。
func (s *Store) GetUserByID(id int64) (*model.User, error) {
	row := s.DB.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// ListUsers 返回 id -> User 的映射，列表页批量补充负责人名称时避免 N+1 查询。
func (s *Store) ListUsers() (map[int64]*model.User, error) {
	rows, err := s.DB.Query(`SELECT ` + userColumns + ` FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]*model.User)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		result[u.ID] = u
	}
	return result, rows.Err()
}
