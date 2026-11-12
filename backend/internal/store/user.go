package store

import (
	"errors"

	"gorm.io/gorm"

	"ssedeploy/internal/model"
)

// GetUserByUsername 按账号名查询用户，登录时使用。
func (s *Store) GetUserByUsername(username string) (*model.User, error) {
	var u model.User
	if err := s.G.Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// GetUserByID 按主键查询用户。
func (s *Store) GetUserByID(id int64) (*model.User, error) {
	var u model.User
	if err := s.G.First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// ListUsers 返回 id -> User 的映射，列表页批量补充负责人名称时避免 N+1 查询。
func (s *Store) ListUsers() (map[int64]*model.User, error) {
	var users []*model.User
	if err := s.G.Find(&users).Error; err != nil {
		return nil, err
	}

	result := make(map[int64]*model.User, len(users))
	for _, u := range users {
		result[u.ID] = u
	}
	return result, nil
}

// UsernameExists 判断账号名是否已被占用（注册前校验）。
func (s *Store) UsernameExists(username string) (bool, error) {
	var count int64
	if err := s.G.Model(&model.User{}).Where("username = ?", username).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// EmailExists 判断邮箱是否已被注册（注册前校验）。
func (s *Store) EmailExists(email string) (bool, error) {
	var count int64
	if err := s.G.Model(&model.User{}).Where("email = ?", email).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateUser 在事务中写入新用户。
// 唯一索引冲突时返回 ErrDuplicate，作为并发重复注册的最终兜底。
func (s *Store) CreateUser(u *model.User) error {
	err := s.G.Transaction(func(tx *gorm.DB) error {
		return tx.Create(u).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}
