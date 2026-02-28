package video

import (
	"context"
	"errors"
	"feedsystem_video_go/internal/middleware/rabbitmq"
	rediscache "feedsystem_video_go/internal/middleware/redis"
	"strings"

	"gorm.io/gorm"
)

type CommentService struct {
	repo            *CommentRepository
	VideoRepository *VideoRepository
	cache           *rediscache.Client
	commentMQ       *rabbitmq.CommentMQ
	popularityMQ    *rabbitmq.PopularityMQ
}

func NewCommentService(repo *CommentRepository, videoRepo *VideoRepository, cache *rediscache.Client, commentMQ *rabbitmq.CommentMQ, popularityMQ *rabbitmq.PopularityMQ) *CommentService {
	return &CommentService{repo: repo, VideoRepository: videoRepo, cache: cache, commentMQ: commentMQ, popularityMQ: popularityMQ}
}

func (s *CommentService) Publish(ctx context.Context, comment *Comment) error {
	if comment == nil {
		return errors.New("comment is nil")
	}
	comment.Username = strings.TrimSpace(comment.Username)
	comment.Content = strings.TrimSpace(comment.Content)
	if comment.VideoID == 0 || comment.AuthorID == 0 {
		return errors.New("video_id and author_id are required")
	}
	if comment.Content == "" {
		return errors.New("content is required")
	}

	exists, err := s.VideoRepository.IsExist(ctx, comment.VideoID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("video not found")
	}

	mysqlEnqueued := false
	redisEnqueued := false
	if s.commentMQ != nil {
		if err := s.commentMQ.Publish(ctx, comment.Username, comment.VideoID, comment.AuthorID, comment.Content); err == nil {
			mysqlEnqueued = true
		}
	}
	if s.popularityMQ != nil {
		if err := s.popularityMQ.Update(ctx, comment.VideoID, 1); err == nil {
			redisEnqueued = true
		}
	}
	if mysqlEnqueued && redisEnqueued {
		return nil
	}

	// Fallback: direct MySQL write when comment MQ publish fails.
	if !mysqlEnqueued {
		if err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 再次校验视频是否存在（事务内）
			if err := tx.Select("id").First(&Video{}, comment.VideoID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("video not found")
				}
				return err
			}
			// 插入评论记录
			if err := tx.Create(comment).Error; err != nil {
				return err
			}
			// 更新视频热度（评论+1）
			return tx.Model(&Video{}).Where("id = ?", comment.VideoID).
				UpdateColumn("popularity", gorm.Expr("popularity + 1")).Error
		}); err != nil {
			return err
		}
	}

	// Fallback: direct Redis update when popularity MQ publish fails.
	if !redisEnqueued {
		UpdatePopularityCache(ctx, s.cache, comment.VideoID, 1)
	}
	return nil
}

// Delete 删除评论
// 业务流程：
// 1. 查询评论是否存在
// 2. 校验操作者是否为评论作者（防止删除他人评论）
// 3. 优先使用MQ异步处理：发送删除评论消息到队列
// 4. MQ失败时Fallback：直接删除数据库记录
// 参数：
//   - ctx: 上下文
//   - commentID: 评论ID
//   - accountID: 操作者的账户ID
func (s *CommentService) Delete(ctx context.Context, commentID uint, accountID uint) error {
	// 1. 查询评论是否存在
	comment, err := s.repo.GetByID(ctx, commentID)
	if err != nil {
		return err
	}
	if comment == nil {
		return errors.New("comment not found")
	}

	// 2. 校验操作者是否为评论作者
	if comment.AuthorID != accountID {
		return errors.New("permission denied")
	}

	// 3. 尝试使用MQ异步处理
	if s.commentMQ != nil {
		if err := s.commentMQ.Delete(ctx, commentID); err == nil {
			return nil
		}
	}

	// 4. Fallback: MQ发送失败时，直接删除数据库记录
	return s.repo.DeleteComment(ctx, comment)
}

// GetAll 查询视频的所有评论
// 业务流程：
// 1. 校验视频是否存在
// 2. 查询指定视频的所有评论（按创建时间倒序）
// 参数：
//   - ctx: 上下文
//   - videoID: 视频ID
// 返回：
//   - []Comment: 评论列表
//   - error: 错误信息
func (s *CommentService) GetAll(ctx context.Context, videoID uint) ([]Comment, error) {
	// 1. 校验视频是否存在
	exists, err := s.VideoRepository.IsExist(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("video not found")
	}

	// 2. 查询指定视频的所有评论
	return s.repo.GetAllComments(ctx, videoID)
}
