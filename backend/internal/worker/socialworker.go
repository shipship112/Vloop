package worker

import (
	"context"
	"encoding/json"
	"errors"
	"feedsystem_video_go/internal/middleware/rabbitmq"
	"feedsystem_video_go/internal/social"
	"feedsystem_video_go/internal/video"
	"log"

	"github.com/go-sql-driver/mysql"
	amqp "github.com/rabbitmq/amqp091-go"
)

type SocialWorker struct {
	ch    *amqp.Channel
	repo  *social.SocialRepository
	videoRepo *video.VideoRepository
	queue string
}

func NewSocialWorker(ch *amqp.Channel, repo *social.SocialRepository, videoRepo *video.VideoRepository,queue string) *SocialWorker {
	return &SocialWorker{ch: ch, repo: repo,videoRepo: videoRepo ,queue: queue}
}

func (w *SocialWorker) Run(ctx context.Context) error {
	if w == nil || w.ch == nil || w.repo == nil {
		return errors.New("social worker is not initialized")
	}
	if w.queue == "" {
		return errors.New("queue is required")
	}

	deliveries, err := w.ch.Consume(
		w.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries channel closed")
			}
			w.handleDelivery(ctx, d)
		}
	}
}

func (w *SocialWorker) handleDelivery(ctx context.Context, d amqp.Delivery) {
	if err := w.process(ctx, d.Body); err != nil {
		log.Printf("social worker: failed to process message: %v", err)
		// 重新入队，稍后重试
		_ = d.Nack(false, true)
		return
	}
	_ = d.Ack(false)
}

func (w *SocialWorker) process(ctx context.Context, body []byte) error {
	var evt rabbitmq.SocialEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		// 解析事件失败，直接丢弃
		return nil
	}
	if evt.FollowerID == 0 || evt.VloggerID == 0 {
		return nil
	}

	switch evt.Action {
	case "follow":
		err := w.repo.Follow(ctx, &social.Social{
			FollowerID: evt.FollowerID,
			VloggerID:  evt.VloggerID,
		})
		if err!=nil{
			var mysqlErr *mysql.MySQLError
			if errors.As(err,&mysqlErr) && mysqlErr.Number==1062{
				return nil
			}
			return err
		}
		// 查询被关注者的最新视频并更新热度（+10）
		latestVideo, err := w.videoRepo.GetLatestByAuthorID(ctx, evt.VloggerID)
		if err != nil {
			log.Printf("social worker: failed to get latest video for vlogger %d: %v", evt.VloggerID, err)
			return nil
		}

		if latestVideo != nil {
			if err := w.videoRepo.ChangePopularity(ctx, latestVideo.ID, 10); err != nil {
				log.Printf("social worker: failed to update popularity for video %d: %v", latestVideo.ID, err)
			}
		}

	  return nil

	case "unfollow":
	err := w.repo.Unfollow(ctx, &social.Social{
		FollowerID: evt.FollowerID,
		VloggerID:  evt.VloggerID,
	})
	if err != nil {
		return err
	}

	// 查询被关注者的最新视频并更新热度（-10）
	latestVideo, err := w.videoRepo.GetLatestByAuthorID(ctx, evt.VloggerID)
	if err != nil {
		log.Printf("social worker: failed to get latest video for vlogger %d: %v", evt.VloggerID, err)
		return nil
	}

	if latestVideo != nil {
		if err := w.videoRepo.ChangePopularity(ctx, latestVideo.ID, -10); err != nil {
			log.Printf("social worker: failed to update popularity for video %d: %v", latestVideo.ID, err)
		}
	}

	return nil

	default:
		return nil
	}
}
