package social

import (
	"feedsystem_video_go/internal/middleware/jwt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SocialHandler 关注处理器，负责处理关注相关的HTTP请求
type SocialHandler struct {
	service *SocialService // 关注服务层
}

// NewSocialHandler 创建关注处理器实例
func NewSocialHandler(service *SocialService) *SocialHandler {
	return &SocialHandler{service: service}
}

// Follow 关注博主接口
// 路由：POST /social/follow
// 功能：当前用户关注指定博主（支持MQ异步处理）
// 请求体：{"vlogger_id": 博主ID}
func (h *SocialHandler) Follow(c *gin.Context) {
	// 1. 解析JSON请求体
	var req FollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验博主ID
	if req.VloggerID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vlogger_id is required"})
		return
	}

	// 3. 从JWT中间件获取当前登录用户ID（关注者ID）
	FollowerID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 4. 构造关注对象
	social := &Social{
		FollowerID: FollowerID, // 关注者ID
		VloggerID:  req.VloggerID, // 被关注者（博主）ID
	}

	// 5. 调用Service层处理关注（含MQ异步处理）
	if err := h.service.Follow(c.Request.Context(), social); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回成功消息
	c.JSON(http.StatusOK, gin.H{"message": "followed"})
}

// Unfollow 取消关注接口
// 路由：POST /social/unfollow
// 功能：当前用户取消关注指定博主（支持MQ异步处理）
// 请求体：{"vlogger_id": 博主ID}
func (h *SocialHandler) Unfollow(c *gin.Context) {
	// 1. 解析JSON请求体
	var req UnfollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验博主ID
	if req.VloggerID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vlogger_id is required"})
		return
	}

	// 3. 从JWT中间件获取当前登录用户ID（关注者ID）
	FollowerID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 4. 构造关注对象
	social := &Social{
		FollowerID: FollowerID, // 关注者ID
		VloggerID:  req.VloggerID, // 被关注者（博主）ID
	}

	// 5. 调用Service层处理取消关注（含MQ异步处理）
	if err := h.service.Unfollow(c.Request.Context(), social); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回成功消息
	c.JSON(http.StatusOK, gin.H{"message": "unfollowed"})
}

// GetAllFollowers 查询粉丝列表接口
// 路由：POST /social/followers
// 功能：查询指定博主的所有粉丝
// 请求体：{"vlogger_id": 博主ID}（可选，不传则查询当前用户的粉丝）
func (h *SocialHandler) GetAllFollowers(c *gin.Context) {
	// 1. 解析JSON请求体
	var req GetAllFollowersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. 获取博主ID（如果请求体未指定，则使用当前登录用户ID）
	vloggerID := req.VloggerID
	if vloggerID == 0 {
		accountID, err := jwt.GetAccountID(c)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		vloggerID = accountID
	}

	// 3. 调用Service层查询粉丝列表
	followers, err := h.service.GetAllFollowers(c.Request.Context(), vloggerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回粉丝列表
	c.JSON(http.StatusOK, GetAllFollowersResponse{Followers: followers})
}

// GetAllVloggers 查询关注列表接口
// 路由：POST /social/following
// 功能：查询指定用户关注的所有博主
// 请求体：{"follower_id": 关注者ID}（可选，不传则查询当前用户的关注列表）
func (h *SocialHandler) GetAllVloggers(c *gin.Context) {
	// 1. 解析JSON请求体
	var req GetAllVloggersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. 获取关注者ID（如果请求体未指定，则使用当前登录用户ID）
	followerID := req.FollowerID
	if followerID == 0 {
		accountID, err := jwt.GetAccountID(c)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		followerID = accountID
	}

	// 3. 调用Service层查询关注列表
	vloggers, err := h.service.GetAllVloggers(c.Request.Context(), followerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回关注列表
	c.JSON(http.StatusOK, GetAllVloggersResponse{Vloggers: vloggers})
}
