package video

import (
	"feedsystem_video_go/internal/account"
	"feedsystem_video_go/internal/middleware/jwt"

	"github.com/gin-gonic/gin"
)

// CommentHandler 评论处理器，负责处理评论相关的HTTP请求
type CommentHandler struct {
	service        *CommentService            // 评论服务层
	accountService *account.AccountService     // 账户服务层（查询用户名）
}

// NewCommentHandler 创建评论处理器实例
func NewCommentHandler(service *CommentService, accountService *account.AccountService) *CommentHandler {
	return &CommentHandler{service: service, accountService: accountService}
}

// PublishComment 发布评论接口
// 路由：POST /comment/publish
// 功能：用户对指定视频发布评论（支持MQ异步处理）
// 请求体：{"video_id": 视频ID, "content": "评论内容"}
func (h *CommentHandler) PublishComment(c *gin.Context) {
	// 1. 解析JSON请求体
	var req PublishCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验评论内容
	if req.Content == "" {
		c.JSON(400, gin.H{"error": "content is required"})
		return
	}

	// 3. 校验视频ID
	if req.VideoID <= 0 {
		c.JSON(400, gin.H{"error": "video_id is required"})
		return
	}

	// 4. 从JWT中间件获取当前登录用户ID
	authorId, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 5. 查询用户信息（获取用户名）
	user, err := h.accountService.FindByID(c.Request.Context(), authorId)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 6. 构造评论对象
	comment := &Comment{
		Username: user.Username, // 评论者用户名（冗余存储，便于查询）
		VideoID:  req.VideoID,  // 视频ID
		AuthorID: authorId,     // 评论者ID
		Content:  req.Content,  // 评论内容
	}

	// 7. 调用Service层发布评论（含MQ异步处理）
	if err := h.service.Publish(c.Request.Context(), comment); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 8. 返回成功消息
	c.JSON(200, gin.H{"message": "comment published successfully"})
}

// DeleteComment 删除评论接口
// 路由：POST /comment/delete
// 功能：删除指定评论（仅允许删除自己的评论）
// 请求体：{"comment_id": 评论ID}
func (h *CommentHandler) DeleteComment(c *gin.Context) {
	// 1. 解析JSON请求体
	var req DeleteCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 从JWT中间件获取当前登录用户ID
	accountID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 3. 校验评论ID
	if req.CommentID <= 0 {
		c.JSON(400, gin.H{"error": "comment_id is required"})
		return
	}

	// 4. 调用Service层删除评论（会验证是否为评论作者）
	if err := h.service.Delete(c.Request.Context(), req.CommentID, accountID); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 5. 返回成功消息
	c.JSON(200, gin.H{"message": "comment deleted successfully"})
}

// GetAllComments 查询视频的所有评论接口
// 路由：POST /comment/get-all
// 功能：查询指定视频的所有评论（按时间倒序）
// 请求体：{"video_id": 视频ID}
func (h *CommentHandler) GetAllComments(c *gin.Context) {
	// 1. 解析JSON请求体
	var req GetAllCommentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验视频ID
	if req.VideoID == 0 {
		c.JSON(400, gin.H{"error": "video_id is required"})
		return
	}

	// 3. 调用Service层查询评论列表
	comments, err := h.service.GetAll(c.Request.Context(), req.VideoID)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回评论列表
	c.JSON(200, comments)
}
