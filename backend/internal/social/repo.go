package social

import (
	"context"
	"feedsystem_video_go/internal/account"

	"gorm.io/gorm"
)

// SocialRepository 关注仓储层，负责关注相关数据库操作
type SocialRepository struct {
	db *gorm.DB // GORM数据库实例
}

// NewSocialRepository 创建关注仓储实例
func NewSocialRepository(db *gorm.DB) *SocialRepository {
	return &SocialRepository{db: db}
}

// Follow 添加关注记录
// 参数：
//   - ctx: 上下文
//   - social: 关注对象
func (r *SocialRepository) Follow(ctx context.Context, social *Social) error {
	return r.db.WithContext(ctx).Create(social).Error
}

// Unfollow 删除关注记录
// 参数：
//   - ctx: 上下文
//   - social: 关注对象
func (r *SocialRepository) Unfollow(ctx context.Context, social *Social) error {
	return r.db.WithContext(ctx).
		Where("follower_id = ? AND vlogger_id = ?", social.FollowerID, social.VloggerID).
		Delete(&Social{}).Error
}

// GetAllFollowers 查询指定博主的所有粉丝
// 使用两次查询：
// 1. 查询关注关系表，获取粉丝ID列表
// 2. 根据粉丝ID列表查询账户信息
// 参数：
//   - ctx: 上下文
//   - VloggerID: 博主ID
// 返回：
//   - []*account.Account: 粉丝列表
//   - error: 错误信息
func (r *SocialRepository) GetAllFollowers(ctx context.Context, VloggerID uint) ([]*account.Account, error) {
	// 1. 查询关注关系表，获取粉丝ID列表
	var relations []Social
	if err := r.db.WithContext(ctx).
		Model(&Social{}).
		Where("vlogger_id = ?", VloggerID).
		Find(&relations).Error; err != nil {
		return nil, err
	}

	// 2. 提取粉丝ID列表
	followerIDs := make([]uint, 0, len(relations))
	for _, rel := range relations {
		followerIDs = append(followerIDs, rel.FollowerID)
	}
	if len(followerIDs) == 0 {
		return []*account.Account{}, nil
	}

	// 3. 根据粉丝ID列表查询账户信息
	var followers []*account.Account
	if err := r.db.WithContext(ctx).
		Model(&account.Account{}).
		Where("id IN ?", followerIDs).
		Find(&followers).Error; err != nil {
		return nil, err
	}
	return followers, nil
}

// GetAllVloggers 查询指定用户关注的所有博主
// 使用两次查询：
// 1. 查询关注关系表，获取博主ID列表
// 2. 根据博主ID列表查询账户信息
// 参数：
//   - ctx: 上下文
//   - FollowerID: 关注者ID
// 返回：
//   - []*account.Account: 关注的博主列表
//   - error: 错误信息
func (r *SocialRepository) GetAllVloggers(ctx context.Context, FollowerID uint) ([]*account.Account, error) {
	// 1. 查询关注关系表，获取博主ID列表
	var relations []Social
	if err := r.db.WithContext(ctx).
		Model(&Social{}).
		Where("follower_id = ?", FollowerID).
		Find(&relations).Error; err != nil {
		return nil, err
	}

	// 2. 提取博主ID列表
	vloggerIDs := make([]uint, 0, len(relations))
	for _, rel := range relations {
		vloggerIDs = append(vloggerIDs, rel.VloggerID)
	}
	if len(vloggerIDs) == 0 {
		return []*account.Account{}, nil
	}

	// 3. 根据博主ID列表查询账户信息
	var vloggers []*account.Account
	if err := r.db.WithContext(ctx).
		Model(&account.Account{}).
		Where("id IN ?", vloggerIDs).
		Find(&vloggers).Error; err != nil {
		return nil, err
	}
	return vloggers, nil
}

// IsFollowed 查询是否已关注
// 参数：
//   - ctx: 上下文
//   - social: 关注对象
// 返回：
//   - bool: 是否已关注
//   - error: 错误信息
func (r *SocialRepository) IsFollowed(ctx context.Context, social *Social) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&Social{}).
		Where("follower_id = ? AND vlogger_id = ?", social.FollowerID, social.VloggerID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
