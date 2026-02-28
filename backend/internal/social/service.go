package social

import (
	"context"
	"errors"
	"feedsystem_video_go/internal/account"
	"feedsystem_video_go/internal/middleware/rabbitmq"
)

// SocialService 关注服务层，处理关注业务逻辑
// - 支持MQ异步处理（推荐）
// - MQ失败时Fallback：直接写数据库
type SocialService struct {
	repo        *SocialRepository          // 关注仓储层，负责数据库操作
	accountrepo *account.AccountRepository // 账户仓储层，校验账户是否存在
	socialMQ    *rabbitmq.SocialMQ         // 关注消息队列，异步处理关注事件
}

// NewSocialService 创建关注服务实例
func NewSocialService(repo *SocialRepository, accountrepo *account.AccountRepository, socialMQ *rabbitmq.SocialMQ) *SocialService {
	return &SocialService{repo: repo, accountrepo: accountrepo, socialMQ: socialMQ}
}

// Follow 关注博主
// 业务流程：
// 1. 校验关注者和博主是否存在
// 2. 防止自己关注自己
// 3. 校验是否已关注（防止重复关注）
// 4. 发送关注事件到MQ（Worker异步处理：插入关注记录、更新粉丝数、更新视频热度）
// 5. MQ失败时Fallback：直接写入数据库
// 参数：
//   - ctx: 上下文
//   - social: 关注对象
func (s *SocialService) Follow(ctx context.Context, social *Social) error {
	// 1. 校验关注者是否存在
	_, err := s.accountrepo.FindByID(ctx, social.FollowerID)
	if err != nil {
		return err
	}

	// 2. 校验博主是否存在
	_, err = s.accountrepo.FindByID(ctx, social.VloggerID)
	if err != nil {
		return err
	}

	// 3. 防止自己关注自己
	if social.FollowerID == social.VloggerID {
		return errors.New("can not follow self")
	}

	// 4. 校验是否已关注（防止重复关注）
	isFollowed, err := s.repo.IsFollowed(ctx, social)
	if err != nil {
		return err
	}
	if isFollowed {
		return errors.New("already followed")
	}

	// 5. 发送关注事件到MQ（Worker异步处理）
	if s.socialMQ != nil {
		s.socialMQ.Follow(ctx, social.FollowerID, social.VloggerID)
	}

	// 6. Fallback: MQ发送失败时，直接写入数据库
	return s.repo.Follow(ctx, social)
}

// Unfollow 取消关注
// 业务流程：
// 1. 校验关注者和博主是否存在
// 2. 校验是否已关注（防止取消未关注的用户）
// 3. 发送取关事件到MQ（Worker异步处理：删除关注记录、更新粉丝数、更新视频热度）
// 4. MQ失败时Fallback：直接删除数据库记录
// 参数：
//   - ctx: 上下文
//   - social: 关注对象
func (s *SocialService) Unfollow(ctx context.Context, social *Social) error {
	// 1. 校验关注者是否存在
	_, err := s.accountrepo.FindByID(ctx, social.FollowerID)
	if err != nil {
		return err
	}

	// 2. 校验博主是否存在
	_, err = s.accountrepo.FindByID(ctx, social.VloggerID)
	if err != nil {
		return err
	}

	// 3. 校验是否已关注（防止取消未关注的用户）
	isFollowed, err := s.repo.IsFollowed(ctx, social)
	if err != nil {
		return err
	}
	if !isFollowed {
		return errors.New("not followed")
	}

	// 4. 发送取关事件到MQ（Worker异步处理）
	if s.socialMQ != nil {
		s.socialMQ.UnFollow(ctx, social.FollowerID, social.VloggerID)
	}

	// 5. Fallback: MQ发送失败时，直接删除数据库记录
	return s.repo.Unfollow(ctx, social)
}

// GetAllFollowers 查询指定博主的粉丝列表
// 参数：
//   - ctx: 上下文
//   - VloggerID: 博主ID
// 返回：
//   - []*account.Account: 粉丝列表
//   - error: 错误信息
func (s *SocialService) GetAllFollowers(ctx context.Context, VloggerID uint) ([]*account.Account, error) {
	// 校验博主是否存在
	_, err := s.accountrepo.FindByID(ctx, VloggerID)
	if err != nil {
		return nil, err
	}
	return s.repo.GetAllFollowers(ctx, VloggerID)
}

// GetAllVloggers 查询指定用户关注的博主列表
// 参数：
//   - ctx: 上下文
//   - FollowerID: 关注者ID
// 返回：
//   - []*account.Account: 关注的博主列表
//   - error: 错误信息
func (s *SocialService) GetAllVloggers(ctx context.Context, FollowerID uint) ([]*account.Account, error) {
	// 校验关注者是否存在
	_, err := s.accountrepo.FindByID(ctx, FollowerID)
	if err != nil {
		return nil, err
	}
	return s.repo.GetAllVloggers(ctx, FollowerID)
}

// IsFollowed 查询是否已关注
// 参数：
//   - ctx: 上下文
//   - social: 关注对象
// 返回：
//   - bool: 是否已关注
//   - error: 错误信息
func (s *SocialService) IsFollowed(ctx context.Context, social *Social) (bool, error) {
	// 校验关注者是否存在
	_, err := s.accountrepo.FindByID(ctx, social.FollowerID)
	if err != nil {
		return false, err
	}

	// 校验博主是否存在
	_, err = s.accountrepo.FindByID(ctx, social.VloggerID)
	if err != nil {
		return false, err
	}

	return s.repo.IsFollowed(ctx, social)
}
